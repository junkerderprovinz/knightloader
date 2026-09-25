package app

// The seam between a finished package and the address a drawer points at: the
// right addresses are picked for a package, the call waits until the files have
// landed, and the bus subscription is wired.

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"sync/atomic"
	"testing"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/mediahook"
	"github.com/junkerderprovinz/knightloader/internal/script"
	"github.com/junkerderprovinz/knightloader/internal/settings"
	"github.com/junkerderprovinz/knightloader/internal/workdir"
)

// stageFiled is stageDone with the two fields this feature turns on: which
// package the file belongs to and which drawer it was filed in.
func stageFiled(t *testing.T, a *App, id, name, pkg, category string) *core.Task {
	t.Helper()
	task := &core.Task{
		ID: id, URL: "https://host.example/" + name, Name: name,
		Status: core.StatusDone, Enabled: true, Package: pkg, Category: category,
	}
	a.mu.Lock()
	a.tasks[id] = task
	a.mu.Unlock()
	return task
}

func twoDrawers(s *settings.Settings, jellyURL, plexURL string) {
	s.MediaHooks = []mediahook.Hook{
		{ID: "jellyfin", URL: jellyURL, Method: mediahook.MethodGet},
		{ID: "plex", URL: plexURL, Method: mediahook.MethodGet},
	}
	s.Categories = []settings.Category{
		{ID: "serien", Notify: "jellyfin"},
		{ID: "filme", Notify: "plex"},
		{ID: "musik"},
	}
}

// A package whose links belong in different drawers is ordinary (the sample
// beside the film, the subtitle beside the episode), so the answer is a set
// rather than the first task's drawer.
func TestAPackageInTwoDrawersCallsBothAddressesOnce(t *testing.T) {
	a, _ := newRuleApp(t, func(s *settings.Settings, _ string) {
		twoDrawers(s, "http://jelly.invalid/", "http://plex.invalid/")
	})
	stageFiled(t, a, "1", "e01.mkv", "Die.Serie.S01", "serien")
	stageFiled(t, a, "2", "e01.nfo", "Die.Serie.S01", "serien")
	stageFiled(t, a, "3", "sample.mkv", "Die.Serie.S01", "filme")
	// Another package entirely, in a third drawer that calls nothing.
	stageFiled(t, a, "4", "album.zip", "Das.Album", "musik")

	got := a.hookIDsForPackage("Die.Serie.S01")
	// Sorted, because map order is random and a different call order on every
	// run makes an intermittent report impossible to reproduce.
	if want := []string{"jellyfin", "plex"}; !reflect.DeepEqual(got, want) {
		t.Errorf("hookIDsForPackage = %v, want %v", got, want)
	}
	if got := a.hookIDsForPackage("Das.Album"); len(got) != 0 {
		t.Errorf("a drawer that calls nothing produced %v", got)
	}
	if got := a.hookIDsForPackage("Nichts.Davon"); len(got) != 0 {
		t.Errorf("a package nothing is filed under produced %v", got)
	}
	// The empty name is not a package. scriptPackageTallies already skips it,
	// and answering for it here would call an address once per unpackaged
	// download on the box.
	if got := a.hookIDsForPackage("   "); len(got) != 0 {
		t.Errorf("the empty package name produced %v", got)
	}
}

// Until an address is stored, the walk of a.tasks on the package sweep's
// goroutine is skipped.
func TestNoStoredAddressMeansNoWork(t *testing.T) {
	a, _ := newRuleApp(t, func(s *settings.Settings, _ string) {
		s.Categories = []settings.Category{{ID: "serien", Notify: "jellyfin"}}
	})
	stageFiled(t, a, "1", "e01.mkv", "Die.Serie.S01", "serien")
	if got := a.hookIDsForPackage("Die.Serie.S01"); len(got) != 0 {
		t.Errorf("a drawer pointing at an address that is not stored produced %v", got)
	}
}

// app_dispatch.go marks a task Done under a.mu and spawns the checksum and the
// move afterwards, and deliverDownload can take minutes for a large file across
// a filesystem boundary. package.done fires on the sweep's next tick anyway, so
// a media server told to scan too early finds nothing and never looks again.
// Asserted against the mover rather than against a flag beside it.
func TestTheCallWaitsUntilTheFileHasLeftTheWorkingFolder(t *testing.T) {
	work := t.TempDir()
	a, base := newRuleApp(t, func(s *settings.Settings, _ string) {
		s.Extract, s.VerifyChecksums = false, false
		s.WorkDir = work
	})
	folder := workdir.For(work, base)
	stagedIn(t, folder, "film.mkv", "the whole film")
	stageFiled(t, a, "1", "film.mkv", "Der.Film", "filme")

	if a.packageFilesLanded("Der.Film") {
		t.Fatal("the package reads as landed while its file is still in the working folder")
	}

	a.deliverDownload("1")

	if _, err := os.Stat(filepath.Join(base, "film.mkv")); err != nil {
		t.Fatalf("the delivery did not happen, so this test proves nothing: %v", err)
	}
	if !a.packageFilesLanded("Der.Film") {
		t.Error("the package still reads as unlanded after its file was moved")
	}
}

// With no working folder the bytes are written straight into the folder they
// belong in, so there is nothing to wait for.
func TestAnInstallWithNoWorkingFolderNeverWaits(t *testing.T) {
	a, base := newRuleApp(t, func(s *settings.Settings, _ string) { s.WorkDir = "" })
	stagedIn(t, base, "film.mkv", "the whole film")
	stageFiled(t, a, "1", "film.mkv", "Der.Film", "filme")
	if !a.packageFilesLanded("Der.Film") {
		t.Error("an install with no working folder is waiting for a move that never happens")
	}
}

// deliverDownload does nothing for a task it cannot deliver, so a call held for
// one would wait out the grace period every time.
func TestAFileThisAppWillNotMoveIsNotWaitedFor(t *testing.T) {
	work := t.TempDir()
	a, base := newRuleApp(t, func(s *settings.Settings, _ string) { s.WorkDir = work })
	folder := workdir.For(work, base)
	stagedIn(t, folder, "season.mkv", "a torrent's own file")
	task := stageFiled(t, a, "1", "season.mkv", "Die.Serie", "serien")
	a.mu.Lock()
	// A torrent writes a folder named after the torrent while the task is named
	// after the first file, so deliverable in app_deliver.go refuses it and
	// nothing is moved.
	task.InfoHash = "abc"
	a.mu.Unlock()

	if !a.packageFilesLanded("Die.Serie") {
		t.Error("a file this app never moves is being waited for")
	}
}

// End to end over the real bus rather than a check that Subscribe was called: a
// trigger that does not match, a nil payload and a runner that never started all
// leave a subscription in place and no call made.
func TestAFinishedPackageReachesTheAddress(t *testing.T) {
	t.Parallel()
	var hits atomic.Int32
	var token atomic.Value
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		token.Store(r.Header.Get("X-Emby-Token"))
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(srv.Close)

	a, _ := newRuleApp(t, func(s *settings.Settings, _ string) {
		s.MediaHooks = []mediahook.Hook{{ID: "jellyfin", URL: srv.URL, Method: mediahook.MethodGet, HeaderName: "X-Emby-Token"}}
		s.Categories = []settings.Category{{ID: "serien", Notify: "jellyfin"}}
	})
	if err := a.MediaHookStore().SetValue("jellyfin", "the-sealed-token"); err != nil {
		t.Fatal(err)
	}
	stageFiled(t, a, "1", "e01.mkv", "Die.Serie.S01", "serien")

	// Published exactly as watchPackagesForScripts publishes it, off the lock.
	a.publishEvent(script.Firing{Trigger: script.TriggerPackageDone, Package: &script.PackageView{Name: "Die.Serie.S01", Files: 1, Done: 1}})

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) && hits.Load() == 0 {
		time.Sleep(20 * time.Millisecond)
	}
	if hits.Load() != 1 {
		t.Fatalf("the address was called %d times, want once", hits.Load())
	}
	if got, _ := token.Load().(string); got != "the-sealed-token" {
		t.Errorf("the server saw the header %q, want the sealed value", got)
	}
	last, ok := a.LastMediaHookCall("jellyfin")
	if !ok || !last.OK || last.Package != "Die.Serie.S01" {
		t.Errorf("LastMediaHookCall = %+v, ok=%v", last, ok)
	}
}

// Every trigger on the bus reaches every subscriber, so the one acting on
// package.done ignores the rest: firing on task.done would call once per file.
func TestAnEventThatIsNotAFinishedPackageCallsNothing(t *testing.T) {
	t.Parallel()
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(srv.Close)

	a, _ := newRuleApp(t, func(s *settings.Settings, _ string) {
		s.MediaHooks = []mediahook.Hook{{ID: "jellyfin", URL: srv.URL, Method: mediahook.MethodGet}}
		s.Categories = []settings.Category{{ID: "serien", Notify: "jellyfin"}}
	})
	stageFiled(t, a, "1", "e01.mkv", "Die.Serie.S01", "serien")

	tv := scriptTaskView(*a.tasks["1"])
	a.publishEvent(script.Firing{Trigger: script.TriggerTaskDone, Task: &tv})
	// A package.done with no payload, as a firing built wrong would arrive.
	a.publishEvent(script.Firing{Trigger: script.TriggerPackageDone})

	time.Sleep(300 * time.Millisecond)
	if hits.Load() != 0 {
		t.Errorf("the address was called %d times for an event that is not a finished package", hits.Load())
	}
}
