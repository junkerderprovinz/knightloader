package engine

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/core"
)

// Three files on piece boundaries, so no piece belongs to two of them and what
// is on disk at the end is exactly what was selected.
var pickFiles = []seedFile{
	{"a.bin", 256 << 10},
	{"b.bin", 256 << 10},
	{"c.bin", 64 << 10},
}

// waitFor polls cond until it holds or the deadline passes.
func waitFor(t *testing.T, what string, within time.Duration, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(within)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("%s did not happen within %s", what, within)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func TestAFileAddedToARunningTorrentIsFetchedWithoutLosingWhatIsHere(t *testing.T) {
	requireTorrentClient(t)
	_, magnet := seedTorrentAt(t, "Pick", pickFiles, 128<<10)
	dir := t.TempDir()
	sink := &taskSink{}
	e, err := New(dir, sink.apply)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { e.Close() })
	e.SetMetadataTimeout(30 * time.Second)
	const id = "pick-1"
	e.Start(Job{TaskID: id, URL: magnet, Dir: dir, TorrentSelect: []int{0}})
	t.Cleanup(func() { e.Remove(id, true) })

	var before int64
	waitFor(t, "a first reading of a.bin", 30*time.Second, func() bool {
		done, ok := e.TorrentFileProgress(id)
		if ok && done[0] > 0 && done[0] < 256<<10 {
			before = done[0]
			return true
		}
		return false
	})
	if got := sink.snapshot(); got.Status == core.StatusDone {
		t.Fatal("the torrent finished before the change; the seeder is not slow enough for this test")
	}
	if listed := sink.files(); len(listed) != 3 || !listed[0].Selected || listed[1].Selected || listed[2].Selected {
		t.Fatalf("the engine reported the files %+v, want all three with only a.bin selected", listed)
	}

	if err := e.SelectTorrentFiles(id, []int{0, 2}); err != nil {
		t.Fatalf("SelectTorrentFiles = %v", err)
	}
	waitFor(t, "a reading over the new selection", 10*time.Second, func() bool {
		done, ok := e.TorrentFileProgress(id)
		if !ok {
			return false
		}
		if done[0] < before {
			t.Fatalf("a.bin went from %d to %d bytes when c.bin was added", before, done[0])
		}
		return done[1] == -1 && done[2] >= 0
	})
	waitFor(t, "the torrent finishing", 45*time.Second, func() bool { return sink.snapshot().Status == core.StatusDone })

	for _, f := range []seedFile{pickFiles[0], pickFiles[2]} {
		fi, err := os.Stat(filepath.Join(dir, "Pick", f.path))
		if err != nil || fi.Size() != int64(f.size) {
			t.Errorf("%s is not on disk whole: %v", f.path, err)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "Pick", "b.bin")); err == nil {
		t.Error("b.bin was left on disk although it was never selected")
	}
	if done, _ := e.TorrentFileProgress(id); !slices.Equal(done, []int64{256 << 10, -1, 64 << 10}) {
		t.Errorf("per-file progress at the end = %v, want a.bin and c.bin whole and b.bin left out", done)
	}
	if err := e.SelectTorrentFiles(id, nil); err != ErrTorrentFinished {
		t.Errorf("a change to the finished torrent = %v, want ErrTorrentFinished", err)
	}
}

func TestAFileLeftOutOfARunningTorrentIsNotFetched(t *testing.T) {
	requireTorrentClient(t)
	_, magnet := seedTorrentAt(t, "Drop", pickFiles, 128<<10)
	dir := t.TempDir()
	sink := &taskSink{}
	e, err := New(dir, sink.apply)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { e.Close() })
	e.SetMetadataTimeout(30 * time.Second)
	const id = "drop-1"
	e.Start(Job{TaskID: id, URL: magnet, Dir: dir})
	t.Cleanup(func() { e.Remove(id, true) })

	waitFor(t, "the torrent starting", 30*time.Second, func() bool {
		done, ok := e.TorrentFileProgress(id)
		return ok && len(done) == 3
	})
	if err := e.SelectTorrentFiles(id, []int{2}); err != nil {
		t.Fatalf("SelectTorrentFiles = %v", err)
	}
	waitFor(t, "the torrent finishing", 45*time.Second, func() bool { return sink.snapshot().Status == core.StatusDone })
	if fi, err := os.Stat(filepath.Join(dir, "Drop", "c.bin")); err != nil || fi.Size() != 64<<10 {
		t.Errorf("c.bin is not on disk whole: %v", err)
	}
	if got := sink.snapshot().Loaded; got != 64<<10 {
		t.Errorf("the finished torrent counts %d bytes, want c.bin's alone", got)
	}
	// Let go while it seeds, so the files are closed wherever the system keeps
	// an open file from being deleted.
	e.Remove(id, false)
	for _, left := range []string{"a.bin", "a.bin.part", "b.bin", "b.bin.part"} {
		if _, err := os.Stat(filepath.Join(dir, "Drop", left)); err == nil {
			t.Errorf("%s was left on disk after the file was left out", left)
		}
	}
}
