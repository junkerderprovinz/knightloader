package app

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/store"
	"github.com/junkerderprovinz/knightloader/internal/testenv"
)

// requireSeeding skips a test that seeds a real torrent where it cannot run.
func requireSeeding(t *testing.T) {
	t.Helper()
	testenv.RequireWideListener(t)
	if raceEnabled {
		t.Skip("gopeed v1.9.3's own bt.Fetcher has an internal data race under real upload activity")
	}
}

// storeBefore writes tasks into the store in dir, as a process that was killed
// left them.
func storeBefore(t *testing.T, dir string, tasks ...core.Task) {
	t.Helper()
	st, err := store.Open(filepath.Join(dir, "knightloader.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	for i := range tasks {
		if err := st.Save(&tasks[i]); err != nil {
			t.Fatal(err)
		}
	}
}

func startedHere(a *App, id string) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.started[id]
}

// finishedSeed is a finished torrent of the built-in client that landed at
// root in dir.
func finishedSeed(id, uri, root, hash string) core.Task {
	return core.Task{
		ID: id, URL: uri, Name: "Pack", Resolver: "torrent", InfoHash: hash,
		Dir: filepath.Dir(root), File: root, Status: core.StatusDone, Enabled: true,
		CreatedAt: time.Now().Add(-2 * time.Hour), FinishedAt: time.Now().Add(-time.Hour),
	}
}

// A torrent that was seeding when the process was killed seeds again at the
// next start, from where its files are, and counts on from the figures saved
// while it seeded.
func TestATorrentSeedingAtAHardStopSeedsAgainAfterARestart(t *testing.T) {
	requireSeeding(t)
	dir := t.TempDir()
	uri, root, hash := seedableTorrent(t, t.TempDir())
	task := finishedSeed("seed", uri, root, hash)
	task.Uploaded, task.Ratio, task.SeedSeconds = 7000, 0.7, 90
	storeBefore(t, dir, task)

	a, err := newApp(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	waitFor(t, "the torrent seeding again", func() bool { return liveTask(a, "seed").Seeding })
	live := liveTask(a, "seed")
	if live.Uploaded < 7000 || live.Ratio < 0.7 || live.SeedSeconds < 90 {
		t.Errorf("the torrent seeds on with %d bytes, ratio %.2f, %d s; want it to go on from 7000, 0.7 and 90",
			live.Uploaded, live.Ratio, live.SeedSeconds)
	}
	if !live.SeedingEnded.IsZero() || live.SeedingOver {
		t.Errorf("the seeding torrent reads as ended at %v, over %v", live.SeedingEnded, live.SeedingOver)
	}
	if stored := storedTask(t, a, "seed"); stored.Uploaded < 7000 || stored.SeedSeconds < 90 {
		t.Errorf("the store holds %d bytes and %d s for the seeding torrent, want the totals so far", stored.Uploaded, stored.SeedSeconds)
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(root), "Pack.1")); err == nil {
		t.Error("the torrent was fetched again beside its files instead of taken up where they are")
	}
}

// A torrent a clean shutdown stopped seeding seeds again at the next start:
// the shutdown's stamp is not the end of its seeding.
func TestATorrentStoppedByAShutdownSeedsAgainAfterARestart(t *testing.T) {
	requireSeeding(t)
	dir := t.TempDir()
	uri, root, hash := seedableTorrent(t, t.TempDir())
	a, err := newApp(dir)
	if err != nil {
		t.Fatal(err)
	}
	task := finishedSeed("seed", uri, root, hash)
	task.Seeding, task.Uploaded, task.Ratio, task.SeedSeconds = true, 7000, 0.7, 90
	putTask(t, a, task)
	if err := a.Close(); err != nil {
		t.Fatal(err)
	}

	again, err := newApp(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer again.Close()
	waitFor(t, "the torrent seeding again", func() bool { return liveTask(again, "seed").Seeding })
	live := liveTask(again, "seed")
	if !live.SeedingEnded.IsZero() || live.SeedingOver || live.Uploaded < 7000 {
		t.Errorf("after the restart the torrent reads ended %v, over %v, %d bytes; want it seeding on from 7000",
			live.SeedingEnded, live.SeedingOver, live.Uploaded)
	}
}

// A torrent whose seeding reached a target stays done with it after a
// restart: one the engine stopped at a target, and one whose saved figures meet
// the targets as they are now.
func TestATorrentThatReachedItsSeedingTargetDoesNotSeedAfterARestart(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	stoppedURI, stoppedRoot, stoppedHash := seedableTorrent(t, t.TempDir())
	metURI, metRoot, metHash := seedableTorrent(t, t.TempDir())
	ended := time.UnixMilli(time.Now().Add(-30 * time.Minute).UnixMilli())
	stopped := finishedSeed("stopped", stoppedURI, stoppedRoot, stoppedHash)
	stopped.Ratio, stopped.SeedingOver, stopped.SeedingEnded = 1, true, ended
	// Past the default ratio of 1.
	met := finishedSeed("met", metURI, metRoot, metHash)
	met.Ratio, met.Uploaded = 1.5, 240<<10
	storeBefore(t, dir, stopped, met)

	a, err := newApp(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	for _, id := range []string{"stopped", "met"} {
		if startedHere(a, id) || liveTask(a, id).Seeding {
			t.Errorf("the torrent %s that reached its target was taken up to seed again", id)
		}
	}
	if got := liveTask(a, "stopped"); !got.SeedingEnded.Equal(ended) {
		t.Errorf("the torrent stopped at its target reads as ending at %v, want %v", got.SeedingEnded, ended)
	}
	got := storedTask(t, a, "met")
	if !got.SeedingOver || got.SeedingEnded.IsZero() || got.Uploaded != 240<<10 {
		t.Errorf("the torrent whose figures meet the target is stored over %v, ended %v, %d bytes; want it over with its figures",
			got.SeedingOver, got.SeedingEnded, got.Uploaded)
	}
}

// A torrent with a file gone is done with seeding at the next start rather
// than taken up again, which would fetch the file anew.
func TestATorrentWhoseFilesAreGoneEndsItsSeedingAtARestart(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	uri, root, hash := seedableTorrent(t, t.TempDir())
	if err := os.Remove(filepath.Join(root, "b.bin")); err != nil {
		t.Fatal(err)
	}
	task := finishedSeed("seed", uri, root, hash)
	task.TorrentFiles = []core.TorrentFile{
		{Path: "a.bin", Size: 80 << 10, Selected: true},
		{Path: "b.bin", Size: 80 << 10, Selected: true},
	}
	storeBefore(t, dir, task)

	a, err := newApp(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	if startedHere(a, "seed") {
		t.Error("the torrent with a file gone was taken up to seed")
	}
	got := storedTask(t, a, "seed")
	if !got.SeedingOver || got.SeedingEnded.IsZero() {
		t.Errorf("the torrent with a file gone is stored over %v, ended %v; want its seeding over", got.SeedingOver, got.SeedingEnded)
	}
	if _, err := os.Stat(filepath.Join(root, "b.bin")); err == nil {
		t.Error("the file that was gone came back")
	}
}

// A disabled torrent does not seed at the start but owes it still, and seeds
// once it is enabled. The same holds for the torrent module switched off.
func TestATorrentHeldBackAtTheStartSeedsOnceItMay(t *testing.T) {
	requireSeeding(t)
	dir := t.TempDir()
	first, err := newApp(dir)
	if err != nil {
		t.Fatal(err)
	}
	s := first.Settings.Get()
	s.ModulesOff = []string{"torrents"}
	if _, err := first.ApplySettings(s); err != nil {
		t.Fatal(err)
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	offURI, offRoot, offHash := seedableTorrent(t, t.TempDir())
	disabledURI, disabledRoot, disabledHash := seedableTorrent(t, t.TempDir())
	disabled := finishedSeed("disabled", disabledURI, disabledRoot, disabledHash)
	disabled.Enabled = false
	storeBefore(t, dir, finishedSeed("off", offURI, offRoot, offHash), disabled)

	a, err := newApp(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	for _, id := range []string{"off", "disabled"} {
		if startedHere(a, id) {
			t.Fatalf("the torrent %s was taken up to seed while it may not", id)
		}
		if live := liveTask(a, id); !SeedPending(&live) {
			t.Fatalf("the torrent %s held back at the start no longer owes its seeding", id)
		}
	}

	s = a.Settings.Get()
	s.ModulesOff = nil
	if _, err := a.ApplySettings(s); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the torrent seeding with the module back on", func() bool { return liveTask(a, "off").Seeding })
	if startedHere(a, "disabled") {
		t.Fatal("the disabled torrent was taken up to seed with the module")
	}
	a.SetEnabled([]string{"disabled"}, true)
	waitFor(t, "the torrent seeding once enabled", func() bool { return liveTask(a, "disabled").Seeding })
}

// While a torrent seeds, its figures reach the store now and then without
// waiting for its seeding to end, so a crash loses only the last stretch.
func TestASeedingTorrentsFiguresReachTheStoreWhileItSeeds(t *testing.T) {
	t.Parallel()
	a, err := newApp(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	task := finishedSeed("seed", "magnet:?xt=urn:btih:0123456789abcdef0123456789abcdef01234567", "", "")
	task.Seeding = true
	putTask(t, a, task)

	a.onUpdate("seed", core.Update{Torrent: &core.TorrentStats{Seeding: true, Uploaded: 4000, Ratio: 0.4, SeedSeconds: 30}})
	if got := storedTask(t, a, "seed"); got.Uploaded != 4000 || got.Ratio != 0.4 || got.SeedSeconds != 30 {
		t.Errorf("the store holds %d bytes, ratio %.2f, %d s after the first reading; want 4000, 0.4 and 30",
			got.Uploaded, got.Ratio, got.SeedSeconds)
	}
	a.onUpdate("seed", core.Update{Torrent: &core.TorrentStats{Seeding: true, Uploaded: 5000, Ratio: 0.5, SeedSeconds: 33}})
	if got := storedTask(t, a, "seed"); got.Uploaded != 4000 {
		t.Errorf("a reading three seconds on was written as well (%d bytes); the poll would write every few seconds", got.Uploaded)
	}
}
