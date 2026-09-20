package store

import (
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/core"
)

// open builds a store in a fresh directory, closed with the test.
func open(t *testing.T) *Store {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "tasks.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

// SQLite refuses a second writer outright and every caller throws the error
// from Save away, so two updates arriving together would lose one of them
// without a trace.
func TestConcurrentSavesAllLand(t *testing.T) {
	s := open(t)
	const writers = 8
	const each = 25
	var wg sync.WaitGroup
	errs := make(chan error, writers*each)
	for w := 0; w < writers; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			for i := 0; i < each; i++ {
				// The same row from every writer, which is the case that collides:
				// one task reported on by a backend and edited in the interface.
				errs <- s.Save(&core.Task{ID: "same", Name: "file.bin", Loaded: int64(i), CreatedAt: time.Now()})
			}
		}(w)
	}
	wg.Wait()
	close(errs)
	failed := 0
	var first error
	for err := range errs {
		if err != nil {
			failed++
			if first == nil {
				first = err
			}
		}
	}
	if failed > 0 {
		t.Fatalf("%d of %d saves were refused and thrown away, first: %v", failed, writers*each, first)
	}
	all, err := s.All()
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 1 {
		t.Fatalf("the store holds %d tasks, want the one that was written", len(all))
	}
}

// A Packagizer rule fires once, while the link is staged. What it decided has
// to be written down, or after a restart the archive it excluded is unpacked
// and the connection count it set for one hoster is back to the default.
func TestEveryFieldSurvivesARestart(t *testing.T) {
	yes, no := true, false
	cases := []struct {
		name string
		task core.Task
	}{
		{
			name: "everything a rule can set",
			task: core.Task{
				ID: "a", URL: "https://host.example/one.bin", Name: "one.bin",
				Comment: "from the archive shelf", Chunks: 8, AutoExtract: &no,
				MatchedRules: []string{"archives", "big files"},
			},
		},
		{
			name: "a rule that switched unpacking on",
			task: core.Task{ID: "b", URL: "https://host.example/two.bin", AutoExtract: &yes},
		},
		{
			// The case a plain bool column would get wrong: nothing decided at all
			// has to come back as nothing decided, or the global switch is ignored.
			name: "no rule had an opinion",
			task: core.Task{ID: "c", URL: "https://host.example/three.bin"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			s, err := Open(filepath.Join(dir, "tasks.db"))
			if err != nil {
				t.Fatal(err)
			}
			task := tc.task
			task.CreatedAt = time.Now()
			if err := s.Save(&task); err != nil {
				t.Fatal(err)
			}
			s.Close()

			// Reopened rather than re-read: the reload after a restart is the only
			// moment this can go wrong.
			again, err := Open(filepath.Join(dir, "tasks.db"))
			if err != nil {
				t.Fatal(err)
			}
			defer again.Close()
			all, err := again.All()
			if err != nil {
				t.Fatal(err)
			}
			if len(all) != 1 {
				t.Fatalf("reloaded %d tasks, want 1", len(all))
			}
			got := all[0]
			if got.Comment != task.Comment {
				t.Errorf("comment = %q, want %q", got.Comment, task.Comment)
			}
			if got.Chunks != task.Chunks {
				t.Errorf("chunks = %d, want %d", got.Chunks, task.Chunks)
			}
			switch {
			case task.AutoExtract == nil && got.AutoExtract != nil:
				t.Errorf("auto-extract came back as %v, want no opinion at all", *got.AutoExtract)
			case task.AutoExtract != nil && got.AutoExtract == nil:
				t.Error("auto-extract came back as no opinion, want the rule's answer")
			case task.AutoExtract != nil && *got.AutoExtract != *task.AutoExtract:
				t.Errorf("auto-extract = %v, want %v", *got.AutoExtract, *task.AutoExtract)
			}
			if len(got.MatchedRules) != len(task.MatchedRules) {
				t.Fatalf("matched rules = %v, want %v", got.MatchedRules, task.MatchedRules)
			}
			for i, name := range task.MatchedRules {
				if got.MatchedRules[i] != name {
					t.Errorf("matched rule %d = %q, want %q", i, got.MatchedRules[i], name)
				}
			}
		})
	}
}

// A file selection is a decision the user made, unlike the swarm numbers a
// torrent task also carries (peers, seeds, ratio, uploaded, seeding), which
// are a reading of the world and are not persisted. A restart that forgot it
// would start fetching the files the user excluded.
func TestTorrentFileSelectionSurvivesARestart(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(filepath.Join(dir, "tasks.db"))
	if err != nil {
		t.Fatal(err)
	}
	task := core.Task{
		ID: "tf1", URL: "magnet:?xt=urn:btih:0000000000000000000000000000000000000000",
		Name: "a.folder", CreatedAt: time.Now(),
		TorrentFiles: []core.TorrentFile{
			{Path: "a/one.mkv", Size: 900, Selected: true},
			{Path: "a/two.srt", Size: 12, Selected: false},
		},
	}
	if err := s.Save(&task); err != nil {
		t.Fatal(err)
	}
	s.Close()

	again, err := Open(filepath.Join(dir, "tasks.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer again.Close()
	all, err := again.All()
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 1 {
		t.Fatalf("reloaded %d tasks, want 1", len(all))
	}
	got := all[0].TorrentFiles
	if len(got) != len(task.TorrentFiles) {
		t.Fatalf("torrent files = %+v, want %+v", got, task.TorrentFiles)
	}
	for i, f := range task.TorrentFiles {
		if got[i] != f {
			t.Errorf("torrent file %d = %+v, want %+v", i, got[i], f)
		}
	}
}

// A task without torrent files comes back with a nil slice rather than an
// empty one, so an ordinary save shows no spurious change.
func TestTaskWithNoTorrentFilesRoundTripsAsNilNotEmpty(t *testing.T) {
	s := open(t)
	if err := s.Save(&core.Task{ID: "plain", URL: "https://host.example/f.bin", CreatedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	all, err := s.All()
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 1 {
		t.Fatalf("got %d tasks, want 1", len(all))
	}
	if all[0].TorrentFiles != nil {
		t.Fatalf("torrent files = %+v, want nil", all[0].TorrentFiles)
	}
}

// InfoHash and Trackers say which torrent this is, fixed when the task is
// staged, so unlike the swarm numbers they are persisted.
func TestInfoHashAndTrackersSurviveARestart(t *testing.T) {
	s := open(t)
	task := core.Task{
		ID: "ih1", URL: "magnet:?xt=urn:btih:1111111111111111111111111111111111111111",
		Name: "a.folder", CreatedAt: time.Now(),
		InfoHash: "1111111111111111111111111111111111111111",
		Trackers: []string{"udp://tracker.example:80/announce", "https://tracker2.example/announce"},
	}
	if err := s.Save(&task); err != nil {
		t.Fatal(err)
	}
	all, err := s.All()
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 1 {
		t.Fatalf("reloaded %d tasks, want 1", len(all))
	}
	if all[0].InfoHash != task.InfoHash {
		t.Errorf("info hash = %q, want %q", all[0].InfoHash, task.InfoHash)
	}
	got := all[0].Trackers
	if len(got) != len(task.Trackers) {
		t.Fatalf("trackers = %+v, want %+v", got, task.Trackers)
	}
	for i, tr := range task.Trackers {
		if got[i] != tr {
			t.Errorf("tracker %d = %q, want %q", i, got[i], tr)
		}
	}
}

// The same nil-not-empty promise for Trackers.
func TestTaskWithNoInfoHashRoundTripsAsNilTrackers(t *testing.T) {
	s := open(t)
	if err := s.Save(&core.Task{ID: "plain2", URL: "https://host.example/f.bin", CreatedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	all, err := s.All()
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 1 {
		t.Fatalf("got %d tasks, want 1", len(all))
	}
	if all[0].InfoHash != "" {
		t.Fatalf("info hash = %q, want empty", all[0].InfoHash)
	}
	if all[0].Trackers != nil {
		t.Fatalf("trackers = %+v, want nil", all[0].Trackers)
	}
}
