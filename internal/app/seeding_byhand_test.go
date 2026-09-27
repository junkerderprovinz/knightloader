package app

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/settings"
)

// A torrent whose seeding somebody stopped stays stopped after a restart,
// where one a shutdown stopped would be taken up again.
func TestATorrentStoppedSeedingByHandStaysStoppedAfterARestart(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	uri, root, hash := seedableTorrent(t, t.TempDir())
	a, err := newApp(dir)
	if err != nil {
		t.Fatal(err)
	}
	task := finishedSeed("seed", uri, root, hash)
	task.Seeding, task.Peers, task.Uploaded, task.Ratio = true, 3, 7000, 0.7
	putTask(t, a, task)
	a.mu.Lock()
	a.started["seed"] = true
	a.mu.Unlock()

	if got := a.StopSeeding([]string{"seed", "missing"}); len(got) != 1 || got[0] != "seed" {
		t.Fatalf("stopping the seeding touched %v, want only the seeding torrent", got)
	}
	live := liveTask(a, "seed")
	if live.Seeding || !live.SeedingOver || live.SeedingEnded.IsZero() || live.Peers != 0 {
		t.Errorf("after the stop the torrent reads seeding %v, over %v, ended %v, %d peers; want it over, ended, no peers",
			live.Seeding, live.SeedingOver, live.SeedingEnded, live.Peers)
	}
	if startedHere(a, "seed") {
		t.Error("the stopped torrent still counts as started")
	}
	if err := a.Close(); err != nil {
		t.Fatal(err)
	}

	again, err := newApp(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer again.Close()
	if startedHere(again, "seed") || liveTask(again, "seed").Seeding {
		t.Error("the torrent stopped by hand was taken up to seed again after the restart")
	}
	if got := liveTask(again, "seed"); !got.SeedingOver || got.Uploaded != 7000 {
		t.Errorf("after the restart the torrent reads over %v with %d bytes; want it over with its figures", got.SeedingOver, got.Uploaded)
	}
}

// The poll can read a torrent as seeding just before the stop and deliver it
// just after; that reading does not take the seeding up again.
func TestAReadingTakenBeforeAStopByHandDoesNotRestartTheSeeding(t *testing.T) {
	t.Parallel()
	a, err := newApp(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	task := finishedSeed("seed", "magnet:?xt=urn:btih:0123456789abcdef0123456789abcdef01234567", "", "")
	task.Seeding = true
	putTask(t, a, task)
	a.StopSeeding([]string{"seed"})

	a.onUpdate("seed", core.Update{Torrent: &core.TorrentStats{Seeding: true, Peers: 4, Uploaded: 9000}})
	if got := liveTask(a, "seed"); got.Seeding || !got.SeedingOver || got.Peers != 0 {
		t.Errorf("the late reading left the torrent seeding %v, over %v, %d peers; want it stopped", got.Seeding, got.SeedingOver, got.Peers)
	}
}

// Starting the seeding of a torrent that met its targets counts them from the
// figures it has then, and that holds across a restart. A disabled torrent
// keeps it until it may seed.
func TestAStartByHandCountsTheTargetsFromThatMomentAcrossARestart(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	uri, root, hash := seedableTorrent(t, t.TempDir())
	task := finishedSeed("seed", uri, root, hash)
	// Past the default ratio of 1.
	task.Enabled, task.SeedingOver, task.Ratio, task.SeedSeconds = false, true, 1.5, 600
	storeBefore(t, dir, task)
	a, err := newApp(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got := a.StartSeeding([]string{"seed"}); len(got) != 1 {
		t.Fatalf("starting the seeding touched %v, want the torrent", got)
	}
	got := liveTask(a, "seed")
	if got.SeedingOver || !SeedPending(&got) {
		t.Errorf("after the start the torrent reads over %v; want it owing its seeding", got.SeedingOver)
	}
	if err := a.Close(); err != nil {
		t.Fatal(err)
	}

	again, err := newApp(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer again.Close()
	got = liveTask(again, "seed")
	if want := (core.SeedMark{Ratio: 1.5, SeedSeconds: 600}); got.SeedMark != want {
		t.Errorf("after the restart the targets count from %+v, want %+v", got.SeedMark, want)
	}
	if got.SeedingOver {
		t.Error("the restart ended the seeding at the targets the torrent had met before the start")
	}
}

// A start by hand does not bring back the seeding of a torrent whose files are
// gone: it would fetch them again.
func TestAStartByHandLeavesATorrentWithFilesGoneOver(t *testing.T) {
	t.Parallel()
	uri, root, hash := seedableTorrent(t, t.TempDir())
	if err := os.RemoveAll(filepath.Join(root, "b.bin")); err != nil {
		t.Fatal(err)
	}
	a, err := newApp(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	task := finishedSeed("seed", uri, root, hash)
	task.SeedingOver = true
	task.TorrentFiles = []core.TorrentFile{
		{Path: "a.bin", Size: 80 << 10, Selected: true},
		{Path: "b.bin", Size: 80 << 10, Selected: true},
	}
	putTask(t, a, task)

	a.StartSeeding([]string{"seed"})
	if startedHere(a, "seed") || !liveTask(a, "seed").SeedingOver {
		t.Error("the torrent with a file gone was taken up to seed")
	}
}

func TestSeedingTargetsCountFromTheMark(t *testing.T) {
	t.Parallel()
	tc := settings.Torrent{SeedRatioTarget: 1, SeedDurationSeconds: 3600}
	for _, c := range []struct {
		name string
		task core.Task
		over bool
	}{
		{"no mark, ratio met", core.Task{Ratio: 1.2}, true},
		{"marked past the ratio, short of it since", core.Task{Ratio: 1.7, SeedMark: core.SeedMark{Ratio: 1.2}}, false},
		{"marked, ratio met since", core.Task{Ratio: 2.3, SeedMark: core.SeedMark{Ratio: 1.2}}, true},
		{"marked, time met since", core.Task{SeedSeconds: 9000, SeedMark: core.SeedMark{SeedSeconds: 5000}}, true},
	} {
		if over := seedingEnd(&c.task, tc) == "it has reached its seeding target"; over != c.over {
			t.Errorf("%s: over %v, want %v", c.name, over, c.over)
		}
	}
}

// A torrent started to seed by hand seeds again although its figures are past
// the targets.
func TestAStartByHandSeedsATorrentPastItsTargets(t *testing.T) {
	requireSeeding(t)
	uri, root, hash := seedableTorrent(t, t.TempDir())
	a, err := newApp(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	task := finishedSeed("seed", uri, root, hash)
	task.SeedingOver, task.Ratio, task.Uploaded = true, 1.5, 240<<10
	putTask(t, a, task)

	a.StartSeeding([]string{"seed"})
	waitFor(t, "the torrent seeding", func() bool { return liveTask(a, "seed").Seeding })
	if got := liveTask(a, "seed"); got.SeedingOver || got.Uploaded < 240<<10 {
		t.Errorf("the torrent seeds with over %v and %d bytes; want it seeding on from 240 KiB", got.SeedingOver, got.Uploaded)
	}
	a.StopSeeding([]string{"seed"})
	if got := liveTask(a, "seed"); got.Seeding || !got.SeedingOver {
		t.Errorf("after the stop the torrent reads seeding %v, over %v", got.Seeding, got.SeedingOver)
	}
}
