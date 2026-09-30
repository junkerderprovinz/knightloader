package app

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/settings"
)

func TestTorrentTally_StartsFromTheTorrentsAlreadyHeldAndKeepsItsTotalsOnDisk(t *testing.T) {
	path := filepath.Join(t.TempDir(), torrentTotalsFile)
	now := time.Date(2026, 9, 30, 12, 0, 0, 0, time.Local)
	held := func() (int64, int64) { return 1000, 400 }

	tally := openTorrentTally(path, held, now)
	tally.add(now, 10, 5)
	tally.save(now, true)

	again := openTorrentTally(path, func() (int64, int64) {
		t.Fatal("a tally with a file on disk asked the list where to start")
		return 0, 0
	}, now)
	got := again.snapshot(now)
	want := torrentTotals{Day: "2026-09-30", DownloadedToday: 10, UploadedToday: 5, Downloaded: 1010, Uploaded: 405}
	if got != want {
		t.Fatalf("after a reopen the totals are %+v, want %+v", got, want)
	}
}

func TestTorrentTally_CountsTodayAfreshOnANewDay(t *testing.T) {
	now := time.Date(2026, 9, 30, 23, 59, 0, 0, time.Local)
	tally := openTorrentTally(filepath.Join(t.TempDir(), torrentTotalsFile), func() (int64, int64) { return 0, 0 }, now)
	tally.add(now, 100, 50)

	tomorrow := now.Add(2 * time.Minute)
	tally.add(tomorrow, 1, 2)
	got := tally.snapshot(tomorrow)
	if got.DownloadedToday != 1 || got.UploadedToday != 2 || got.Downloaded != 101 || got.Uploaded != 52 {
		t.Fatalf("the day after, the tally reads %+v", got)
	}
}

func TestTorrentTally_SavesWhileGrowingOnlyOnceAMinute(t *testing.T) {
	path := filepath.Join(t.TempDir(), torrentTotalsFile)
	now := time.Date(2026, 9, 30, 12, 0, 0, 0, time.Local)
	tally := openTorrentTally(path, func() (int64, int64) { return 0, 0 }, now)
	tally.add(now, 10, 0)
	tally.save(now, false)
	tally.add(now, 10, 0)
	tally.save(now.Add(30*time.Second), false)

	if got := openTorrentTally(path, nil, now).snapshot(now).Downloaded; got != 10 {
		t.Fatalf("half a minute after a write the file holds %d bytes downloaded, want the 10 of the first write", got)
	}
	tally.save(now.Add(time.Minute), false)
	if got := openTorrentTally(path, nil, now).snapshot(now).Downloaded; got != 20 {
		t.Fatalf("a minute on the file holds %d bytes downloaded, want 20", got)
	}
}

func TestTorrentTally_UploadRateComesFromTwoReadingsAndFadesWhenTheyStop(t *testing.T) {
	now := time.Date(2026, 9, 30, 12, 0, 0, 0, time.Local)
	tally := openTorrentTally(filepath.Join(t.TempDir(), torrentTotalsFile), func() (int64, int64) { return 0, 0 }, now)

	tally.readUpload("a", 1000, now)
	if got := tally.uploadRate("a", now); got != 0 {
		t.Fatalf("one reading gives a rate of %d, want 0", got)
	}
	tally.readUpload("a", 5000, now.Add(2*time.Second))
	if got := tally.uploadRate("a", now.Add(2*time.Second)); got != 2000 {
		t.Fatalf("4000 bytes in two seconds gives %d a second, want 2000", got)
	}
	if got := tally.uploadRate("a", now.Add(time.Minute)); got != 0 {
		t.Fatalf("a torrent silent for a minute still sends %d a second", got)
	}
	tally.readUpload("a", 10, now.Add(3*time.Second))
	if got := tally.uploadRate("a", now.Add(3*time.Second)); got != 0 {
		t.Fatalf("a total that fell back gives a rate of %d, want 0", got)
	}
}

func TestSeedSecondsLeft_TakesTheNearerTarget(t *testing.T) {
	task := &core.Task{Size: 1000, Ratio: 1, SeedSeconds: 3500}
	for _, c := range []struct {
		name string
		tc   settings.Torrent
		rate int64
		want int64
	}{
		{"no target", settings.Torrent{}, 100, -1},
		{"the ratio target, 1000 bytes owed at 100 a second", settings.Torrent{SeedRatioTarget: 2}, 100, 10},
		{"the time target", settings.Torrent{SeedDurationSeconds: 3600}, 100, 100},
		{"both, the ratio nearer", settings.Torrent{SeedRatioTarget: 2, SeedDurationSeconds: 3600}, 100, 10},
		{"both, the time nearer", settings.Torrent{SeedRatioTarget: 2, SeedDurationSeconds: 3600}, 1, 100},
		{"a ratio already reached", settings.Torrent{SeedRatioTarget: 0.5}, 100, 0},
	} {
		if got := seedSecondsLeft(task, c.rate, c.tc); got != c.want {
			t.Errorf("%s: %d seconds left, want %d", c.name, got, c.want)
		}
	}
}

func TestTorrentOverview_KeepsTheTotalsAcrossARestart(t *testing.T) {
	dir := t.TempDir()
	a, err := newApp(dir)
	if err != nil {
		t.Fatal(err)
	}
	a.tally.add(time.Now(), 7000, 3500)
	if err := a.Close(); err != nil {
		t.Fatal(err)
	}

	b, err := newApp(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = b.Close() })
	got := b.TorrentOverview()
	if got.Downloaded != 7000 || got.Uploaded != 3500 || got.Ratio != 0.5 || !got.Any {
		t.Fatalf("after a restart the overview reads %+v", got)
	}
}

func TestTorrentOverview_CountsWhatAnUpdateAddsToATorrent(t *testing.T) {
	a, err := newApp(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = a.Close() })
	task := &core.Task{ID: "t1", Name: "Linux", Resolver: "torrent", Status: core.StatusRunning, Size: 10000, Loaded: 1000, Uploaded: 200}
	a.mu.Lock()
	a.tasks[task.ID] = task
	a.active[task.ID] = true
	a.mu.Unlock()

	a.onUpdate(task.ID, core.Update{Loaded: 4000, Speed: 500, Torrent: &core.TorrentStats{Uploaded: 900, Ratio: 0.2}})
	got := a.TorrentOverview()
	if got.Downloaded != 3000 || got.Uploaded != 700 || got.Leeching != 1 || got.DownloadSpeed != 500 {
		t.Fatalf("after one update the overview reads %+v", got)
	}
}
