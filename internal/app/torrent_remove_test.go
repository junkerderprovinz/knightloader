package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/anacrolix/torrent/metainfo"
	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/settings"
	"github.com/junkerderprovinz/knightloader/internal/store"
	"github.com/junkerderprovinz/knightloader/internal/testenv"
)

// After a restart the engine knows no torrent, and a torrent removed with its
// files then goes by the file list the task keeps: an uploaded .torrent's own.
// A magnet keeps none, so its folder is left as it is.
func TestATorrentRemovedAfterARestartTakesTheFilesItNames(t *testing.T) {
	t.Parallel()
	dir, downloads := t.TempDir(), t.TempDir()
	write := func(rel string) {
		t.Helper()
		p := filepath.Join(downloads, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(rel), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	for _, rel := range []string{
		"Show.S01.1/Show.S01/Show.S01E01.mkv", "Show.S01.1/Show.S01/Subs/Show.S01E01.srt",
		"Show.S01.1/Show.S01/Show.S01E02.mkv.part", "Show.S01.1/Show.S01/notes.txt",
		"Other/Other.E01.mkv",
	} {
		write(rel)
	}
	upload := testTorrentURI(t, "Show.S01", []metainfo.FileInfo{
		{Length: 10, Path: []string{"Show.S01E01.mkv"}},
		{Length: 10, Path: []string{"Show.S01E02.mkv"}},
		{Length: 10, Path: []string{"Subs", "Show.S01E01.srt"}},
	})
	done := func(id, url, file string) core.Task {
		return core.Task{
			ID: id, URL: url, Name: filepath.Base(file), Resolver: "torrent", File: file,
			Status: core.StatusDone, Enabled: true, CreatedAt: time.Now(),
		}
	}
	st, err := store.Open(filepath.Join(dir, "knightloader.db"))
	if err != nil {
		t.Fatal(err)
	}
	for _, task := range []core.Task{
		done("upload", upload, filepath.Join(downloads, "Show.S01.1", "Show.S01")),
		done("magnet", "magnet:?xt=urn:btih:"+strings.Repeat("ab", 20)+"&dn=Other", filepath.Join(downloads, "Other")),
	} {
		if err := st.Save(&task); err != nil {
			t.Fatal(err)
		}
	}
	st.Close()
	a, err := newApp(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	s := a.Settings.Get()
	s.DownloadDir = downloads
	if _, err := a.ApplySettings(s); err != nil {
		t.Fatal(err)
	}

	a.RemoveTasks([]string{"upload", "magnet"}, true)
	for rel, want := range map[string]bool{
		"Show.S01.1/Show.S01/Show.S01E01.mkv":      false,
		"Show.S01.1/Show.S01/Subs":                 false,
		"Show.S01.1/Show.S01/Show.S01E02.mkv.part": false,
		"Show.S01.1/Show.S01/notes.txt":            true,
		"Other/Other.E01.mkv":                      true,
	} {
		_, err := os.Stat(filepath.Join(downloads, filepath.FromSlash(rel)))
		if got := err == nil; got != want {
			t.Errorf("%s is there after the removal: %v, want %v", rel, got, want)
		}
	}
}

// With the collision policy on skip, a torrent that starts again is not
// refused for its own files, which it carries on with.
func TestATorrentStartingAgainIsNoCollisionWithItself(t *testing.T) {
	testenv.RequireWideListener(t)
	if testing.Short() {
		t.Skip("this starts a torrent client")
	}
	a := newTorrentTestApp(t)
	a.Engine.SetMetadataTimeout(200 * time.Millisecond)
	downloads := t.TempDir()
	s := settings.Defaults()
	s.DownloadDir = downloads
	s.CollisionPolicy = "skip"
	if _, err := a.ApplySettings(s); err != nil {
		t.Fatal(err)
	}
	place := filepath.Join(downloads, "Show.S01")
	if err := os.MkdirAll(place, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(place, "Show.S01E01.mkv.part"), []byte("half"), 0o644); err != nil {
		t.Fatal(err)
	}
	created := a.AddLinksFrom([]string{"magnet:?xt=urn:btih:" + strings.Repeat("cd", 20) + "&dn=Show.S01"}, "Show.S01", OriginPaste)
	if len(created) != 1 {
		t.Fatalf("staged %d tasks, want 1", len(created))
	}
	id := created[0].ID
	a.mu.Lock()
	a.tasks[id].Name, a.tasks[id].File = "Show.S01", place
	a.mu.Unlock()
	a.StartTasks([]string{id})

	var last *core.Task
	deadline := time.Now().Add(10 * time.Second)
	for last == nil || last.Status != core.StatusError {
		if time.Now().After(deadline) {
			t.Fatalf("the start did not settle: %+v", last)
		}
		time.Sleep(20 * time.Millisecond)
		for _, tsk := range a.Tasks() {
			if tsk.ID == id {
				last = tsk
			}
		}
	}
	// No swarm answers, so the engine gives up on the metadata, which it only
	// asks for once the start is past the collision check.
	if strings.Contains(last.Error, "already exists") || !strings.Contains(last.Error, "torrent") {
		t.Errorf("the start ended with %q, want the engine's metadata timeout rather than a collision", last.Error)
	}
}

// A torrent that has seeded to its target is what Sonarr removes, with its
// files, once it has imported it. By then the torrent library has closed the
// torrent itself, and the removal must still take the row and the files.
func TestATorrentThatStoppedSeedingIsRemovedWithItsFiles(t *testing.T) {
	testenv.RequireWideListener(t)
	if testing.Short() {
		t.Skip("this starts a torrent client")
	}
	if raceEnabled {
		t.Skip("gopeed v1.9.3's own bt.Fetcher has an internal data race once a torrent runs")
	}
	// Not parallel: the library shares one torrent client in the process and
	// closes it only when no torrent is left.
	_, magnet := testenv.SeedTorrent(t, "Season", map[string]int{"Season.E01.mkv": 64 << 10, "Season.E02.mkv": 48 << 10})
	a := newTorrentTestApp(t)
	downloads := t.TempDir()
	s := settings.Defaults()
	s.DownloadDir = downloads
	s.Crawl = false
	s.Torrent.SeedDurationSeconds = 1
	if _, err := a.ApplySettings(s); err != nil {
		t.Fatal(err)
	}
	created := a.AddLinksFrom([]string{magnet}, "Season", OriginPaste)
	if len(created) != 1 {
		t.Fatalf("staged %d tasks, want 1", len(created))
	}
	id := created[0].ID
	a.StartTasks([]string{id})

	var task *core.Task
	deadline := time.Now().Add(time.Minute)
	for task == nil || task.Status != core.StatusDone || task.SeedingEnded.IsZero() {
		if time.Now().After(deadline) {
			t.Fatalf("the torrent has not finished seeding: %+v", task)
		}
		time.Sleep(100 * time.Millisecond)
		for _, tsk := range a.Tasks() {
			if tsk.ID == id {
				task = tsk
			}
		}
	}
	root := filepath.Join(downloads, "Season")
	if _, err := os.Stat(filepath.Join(root, "Season.E01.mkv")); err != nil {
		t.Fatalf("the torrent is not where it should be: %v", err)
	}
	// The library lets go of its client just after the fetcher closes.
	time.Sleep(500 * time.Millisecond)

	a.RemoveTasks([]string{id}, true)
	if n := len(a.Tasks()); n != 0 {
		t.Errorf("%d tasks are left after the removal", n)
	}
	rows, err := a.Store.All()
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 0 {
		t.Errorf("the store keeps %d rows after the removal", len(rows))
	}
	if _, err := os.Stat(root); !os.IsNotExist(err) {
		t.Errorf("the torrent's folder %s is still there (%v)", root, err)
	}
}
