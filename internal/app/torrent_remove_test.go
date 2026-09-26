package app

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/settings"
	"github.com/junkerderprovinz/knightloader/internal/testenv"
)

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
