package app

// The seam between a finished package and the address a drawer points at. Three
// separate claims live here, and each of them is a way the feature is useless if
// it is wrong: the right ADDRESSES are picked for a package, the call waits until
// the files have actually LANDED, and the bus subscription is wired at all.

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

// TestAPackageInTwoDrawersCallsBothAddressesOnce is the claim
// settings_categories.go makes at length and this feature has to honour: a
// package whose links belong in different drawers is NORMAL - the sample beside
// the film, the subtitle beside the episode - so the answer is a set, and a
// first-task-wins implementation would pick the sample's drawer about half the
// time and read as random.
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
	// Sorted, because map order is random and two addresses called in a
	// different order on every run is the kind of nondeterminism that makes an
	// intermittent report impossible to reproduce.
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

// TestNoStoredAddressMeansNoWork is the state every install is in until somebody
// stores an address, and it is asserted because it is also the fast path: the
// walk of a.tasks is skipped entirely, on the package sweep's own goroutine.
func TestNoStoredAddressMeansNoWork(t *testing.T) {
	a, _ := newRuleApp(t, func(s *settings.Settings, _ string) {
		s.Categories = []settings.Category{{ID: "serien", Notify: "jellyfin"}}
	})
	stageFiled(t, a, "1", "e01.mkv", "Die.Serie.S01", "serien")
	if got := a.hookIDsForPackage("Die.Serie.S01"); len(got) != 0 {
		t.Errorf("a drawer pointing at an address that is not stored produced %v", got)
	}
}

// TestTheCallWaitsUntilTheFileHasLeftTheWorkingFolder is the trap this whole
// feature turns on, asserted against the mover itself rather than against a
// flag beside it.
//
// app_dispatch.go sets a task's status to Done under a.mu and THEN spawns the
// checksum and the move; deliverDownload is what takes the finished file out of
// the working folder, which for a 40 GB film across a filesystem boundary is
// minutes. package.done fires on the sweep's next tick regardless, so a media
// server told to scan then finds nothing and never looks again.
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

// TestAnInstallWithNoWorkingFolderNeverWaits. The bytes were written straight
// into the folder they belong in, so there was never anything to wait for - and
// this is the majority of installs, on the path that runs once per tick per
// waiting call.
func TestAnInstallWithNoWorkingFolderNeverWaits(t *testing.T) {
	a, base := newRuleApp(t, func(s *settings.Settings, _ string) { s.WorkDir = "" })
	stagedIn(t, base, "film.mkv", "the whole film")
	stageFiled(t, a, "1", "film.mkv", "Der.Film", "filme")
	if !a.packageFilesLanded("Der.Film") {
		t.Error("an install with no working folder is waiting for a move that never happens")
	}
}

// TestAFileThisAppWillNotMoveIsNotWaitedFor keeps this predicate tied to the
// mover's own. deliverDownload does nothing for a task it cannot deliver, so a
// call held for one would be held until the grace ran out, every time.
func TestAFileThisAppWillNotMoveIsNotWaitedFor(t *testing.T) {
	work := t.TempDir()
	a, base := newRuleApp(t, func(s *settings.Settings, _ string) { s.WorkDir = work })
	folder := workdir.For(work, base)
	stagedIn(t, folder, "season.mkv", "a torrent's own file")
	task := stageFiled(t, a, "1", "season.mkv", "Die.Serie", "serien")
	a.mu.Lock()
	// A torrent writes a folder named after the torrent while the task is named
	// after the first file, so deliverable() refuses it and nothing is ever
	// moved - see app_deliver.go.
	task.InfoHash = "abc"
	a.mu.Unlock()

	if !a.packageFilesLanded("Die.Serie") {
		t.Error("a file this app never moves is being waited for")
	}
}

// TestAFinishedPackageReachesTheAddress is the wiring test: it publishes the real
// event on the real bus and waits for a real HTTP call.
//
// It is deliberately end to end rather than a check that Subscribe was called.
// The three things that can be wrong here - the trigger not matching, the payload
// being nil, the runner never started - all leave a subscription in place and no
// call ever made, which is exactly what "complete, tested and unreachable" looked
// like the last three times this codebase found it.
func TestAFinishedPackageReachesTheAddress(t *testing.T) {
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

// TestAnEventThatIsNotAFinishedPackageCallsNothing. Every trigger on the bus
// reaches every subscriber, so the one that acts on package.done has to ignore
// the other ten - a call fired on task.done would be one per FILE, which is the
// exact thing package.done exists to avoid.
func TestAnEventThatIsNotAFinishedPackageCallsNothing(t *testing.T) {
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
	// A package.done with no payload at all, which is what a firing built wrong
	// would look like.
	a.publishEvent(script.Firing{Trigger: script.TriggerPackageDone})

	time.Sleep(300 * time.Millisecond)
	if hits.Load() != 0 {
		t.Errorf("the address was called %d times for an event that is not a finished package", hits.Load())
	}
}
