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
// files then goes by the file list the task has: an uploaded .torrent's own.
// A magnet the swarm never listed has none, so its folder is left as it is.
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

// The swarm lists a magnet's files once. The task keeps that list, so after a
// restart a magnet removed with its files takes exactly those, empty ones as
// well, and the folders they leave empty. The name in its link is not the
// torrent's own: the folder of that name belongs to somebody else, which is
// why the torrent landed beside it, and it stays.
func TestAMagnetRemovedAfterARestartTakesTheFilesTheSwarmListed(t *testing.T) {
	t.Parallel()
	dir, downloads := t.TempDir(), t.TempDir()
	for rel, content := range map[string]string{
		"Show.S01/mine.mkv":                             "mine",
		"Show.S01.1/Show.Season.1/Show.S01E01.mkv":      "e01",
		"Show.S01.1/Show.Season.1/Subs/Show.S01E01.srt": "srt",
		"Show.S01.1/Show.Season.1/Show.S01E02.mkv.part": "e02",
		"Show.S01.1/Show.Season.1/Show.Season.1.nfo":    "",
		"Film/Film.mkv":  "film",
		"Film/notes.txt": "mine",
	} {
		p := filepath.Join(downloads, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	a, err := newApp(dir)
	if err != nil {
		t.Fatal(err)
	}
	s := a.Settings.Get()
	s.DownloadDir = downloads
	if _, err := a.ApplySettings(s); err != nil {
		t.Fatal(err)
	}
	magnet := func(hash, dn string) *core.Task {
		return &core.Task{
			ID: hash, URL: "magnet:?xt=urn:btih:" + hash + "&dn=" + dn, Name: dn, Resolver: "torrent",
			InfoHash: hash, Status: core.StatusDone, Enabled: true, CreatedAt: time.Now(),
		}
	}
	show, film := magnet(strings.Repeat("ab", 20), "Show.S01"), magnet(strings.Repeat("cd", 20), "Film")
	a.mu.Lock()
	a.tasks[show.ID], a.tasks[film.ID] = show, film
	a.mu.Unlock()
	for _, task := range []core.Task{*show, *film} {
		if err := a.Store.Save(&task); err != nil {
			t.Fatal(err)
		}
	}
	// What the engine reported once the swarm had sent each file list.
	a.onUpdate(show.ID, core.Update{
		Status: core.StatusRunning, Name: "Show.Season.1", File: filepath.Join(downloads, "Show.S01.1", "Show.Season.1"),
		MagnetFiles: []string{"Show.S01E01.mkv", "Show.S01E02.mkv", "Subs/Show.S01E01.srt", "Show.Season.1.nfo"},
	})
	a.onUpdate(film.ID, core.Update{
		Status: core.StatusRunning, Name: "Film", File: filepath.Join(downloads, "Film"),
		MagnetFiles: []string{"Film.mkv", "Film.nfo"},
	})
	if err := a.Close(); err != nil {
		t.Fatal(err)
	}

	again, err := newApp(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer again.Close()
	again.RemoveTasks([]string{show.ID, film.ID}, true)
	for rel, want := range map[string]bool{
		"Show.S01.1":        false,
		"Show.S01/mine.mkv": true,
		"Film/Film.mkv":     false,
		"Film/notes.txt":    true,
	} {
		_, err := os.Stat(filepath.Join(downloads, filepath.FromSlash(rel)))
		if got := err == nil; got != want {
			t.Errorf("%s is there after the removal: %v, want %v", rel, got, want)
		}
	}
}

// A magnet that starts again after a restart takes up its place and waits on
// the swarm for its file list. When no peer answers, the place and the list it
// kept still say where its files are, and removing it takes them.
func TestAMagnetNoPeerAnswersAfterARestartIsStillRemovedWithItsFiles(t *testing.T) {
	testenv.RequireWideListener(t)
	if testing.Short() {
		t.Skip("this starts a torrent client")
	}
	a := newTorrentTestApp(t)
	a.Engine.SetMetadataTimeout(200 * time.Millisecond)
	downloads := t.TempDir()
	s := settings.Defaults()
	s.DownloadDir = downloads
	if _, err := a.ApplySettings(s); err != nil {
		t.Fatal(err)
	}
	place := filepath.Join(downloads, "Show.S01")
	for _, rel := range []string{"Show.S01E01.mkv", "Subs/Show.S01E01.srt"} {
		p := filepath.Join(place, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(rel), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	created := a.AddLinksFrom([]string{"magnet:?xt=urn:btih:" + strings.Repeat("ef", 20) + "&dn=Show.S01"}, "Show.S01", OriginPaste)
	if len(created) != 1 {
		t.Fatalf("staged %d tasks, want 1", len(created))
	}
	id := created[0].ID
	a.mu.Lock()
	a.tasks[id].Name, a.tasks[id].File = "Show.S01", place
	a.tasks[id].MagnetFiles = []string{"Show.S01E01.mkv", "Subs/Show.S01E01.srt"}
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
	if last.File != place {
		t.Errorf("after an attempt that heard nothing the task says it is at %q, want %s", last.File, place)
	}
	a.RemoveTasks([]string{id}, true)
	if _, err := os.Stat(place); !os.IsNotExist(err) {
		t.Errorf("the magnet's folder is still there after it was removed with its files (%v)", err)
	}
}

// A magnet whose link names it otherwise is only named by the swarm. When
// somebody else's folder has the torrent's own name, the magnet is not
// downloaded, and removing it with its files after a restart leaves that
// folder and what is in it.
func TestAMagnetOfAnotherNameLeavesAFolderThatIsNotItsOwnAlone(t *testing.T) {
	testenv.RequireWideListener(t)
	if testing.Short() {
		t.Skip("this starts a torrent client")
	}
	if raceEnabled {
		t.Skip("gopeed v1.9.3's own bt.Fetcher has an internal data race once a torrent runs")
	}
	_, magnet := testenv.SeedTorrent(t, "Show.S01", map[string]int{
		"Show.S01E01.mkv": 48 << 10, "Show.S01E02.mkv": 24 << 10, "Show.S01.nfo": 0,
	})
	proper := strings.Replace(magnet, "&dn=Show.S01&", "&dn=Show.S01.PROPER&", 1)
	if proper == magnet {
		t.Fatalf("%s carries no name to change", magnet)
	}
	dir, downloads := t.TempDir(), t.TempDir()
	foreign := filepath.Join(downloads, "Show.S01", "Show.S01E02.mkv")
	if err := os.MkdirAll(filepath.Dir(foreign), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(foreign, []byte("mine"), 0o644); err != nil {
		t.Fatal(err)
	}
	a, err := newApp(dir)
	if err != nil {
		t.Fatal(err)
	}
	s := settings.Defaults()
	s.DownloadDir = downloads
	s.Crawl = false
	if _, err := a.ApplySettings(s); err != nil {
		t.Fatal(err)
	}
	created := a.AddLinksFrom([]string{proper}, "Show.S01", OriginPaste)
	if len(created) != 1 {
		t.Fatalf("staged %d tasks, want 1", len(created))
	}
	id := created[0].ID
	a.StartTasks([]string{id})

	var last *core.Task
	deadline := time.Now().Add(time.Minute)
	for last == nil || (last.Status != core.StatusError && last.Status != core.StatusDone) {
		if time.Now().After(deadline) {
			t.Fatalf("the start did not settle: %+v", last)
		}
		time.Sleep(50 * time.Millisecond)
		for _, tsk := range a.Tasks() {
			if tsk.ID == id {
				last = tsk
			}
		}
	}
	if last.Status != core.StatusError || !strings.Contains(last.Error, "already exists") {
		t.Errorf("the magnet ended %s with %q, want it refused for the taken name", last.Status, last.Error)
	}
	if err := a.Close(); err != nil {
		t.Fatal(err)
	}

	again, err := newApp(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer again.Close()
	again.RemoveTasks([]string{id}, true)
	// The library renames a file at one of the torrent's paths to .part as
	// the file list arrives, when its size is not the torrent's.
	got, err := os.ReadFile(foreign)
	if err != nil {
		got, err = os.ReadFile(foreign + ".part")
	}
	if err != nil || string(got) != "mine" {
		t.Errorf("the file in the folder that is not the magnet's reads %q (%v)", got, err)
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
