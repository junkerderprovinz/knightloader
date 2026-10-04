package app

import (
	"bytes"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/settings"
)

func appIn(t *testing.T, dataDir, downloads string) *App {
	t.Helper()
	a, err := newApp(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { a.Close() })
	s := settings.Defaults()
	s.DownloadDir = downloads
	s.Crawl = false
	if _, err := a.ApplySettings(s); err != nil {
		t.Fatal(err)
	}
	return a
}

// A download paused before a restart and resumed after it keeps the bytes it
// had: nothing deletes its file on the way, and the server is asked only for
// the rest.
func TestAPausedDownloadKeepsItsBytesAcrossARestart(t *testing.T) {
	if raceEnabled {
		// See TestAStalledDownloadIsReconnectedAndKeepsItsBytes.
		t.Skip("gopeed v1.9.3 has internal data races in every real HTTP transfer; see comment")
	}
	t.Parallel()
	const size = 16 << 20
	o := newFlakyOrigin(t, size)
	dataDir, downloads, dir := t.TempDir(), t.TempDir(), t.TempDir()
	a := appIn(t, dataDir, downloads)
	putTask(t, a, core.Task{
		ID: "t1", URL: o.srv.URL + "/big.bin", Name: "big.bin", Dir: dir,
		Status: core.StatusQueued, Enabled: true,
	})
	a.mu.Lock()
	a.queue = append(a.queue, "t1")
	a.dispatchLocked()
	a.mu.Unlock()

	waitFor(t, "the first MiB arriving", func() bool { return liveTask(a, "t1").Loaded >= 1<<20 })
	a.Pause("t1")
	waitFor(t, "the pause reaching the engine", func() bool {
		kept, _ := filepath.Glob(filepath.Join(dataDir, "transfers", "*.json"))
		return len(kept) == 1
	})
	if liveTask(a, "t1").Loaded >= size {
		t.Fatal("the download finished before it was paused")
	}
	a.Close()
	o.mu.Lock()
	o.healed = true
	o.mu.Unlock()

	b := appIn(t, dataDir, downloads)
	b.Resume("t1")
	waitFor(t, "the download finishing", func() bool { return liveTask(b, "t1").Status == core.StatusDone })

	o.mu.Lock()
	asked := slices.Clone(o.afterHeal)
	o.mu.Unlock()
	for _, rg := range asked {
		if rg == "" || strings.HasPrefix(rg, "bytes=0-") && rg != "bytes=0-0" {
			t.Errorf("after the restart the file was asked for from the start (Range %q)", rg)
		}
	}
	got, err := os.ReadFile(filepath.Join(dir, "big.bin"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, o.data) {
		t.Errorf("the finished file (%d bytes) is not what the origin served (%d bytes)", len(got), len(o.data))
	}
}
