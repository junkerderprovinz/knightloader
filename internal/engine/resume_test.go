package engine

import (
	"bytes"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/junkerderprovinz/knightloader/internal/core"
)

// pausedBeforeRestart downloads part of a file from a fresh slowOrigin, pauses
// it and shuts the engine down. It returns the job, the file it was writing,
// how many requests the origin had answered by then, and the folders a new
// engine boots from.
func pausedBeforeRestart(t *testing.T) (o *slowOrigin, j Job, file string, asked int, dir, state string) {
	t.Helper()
	if raceEnabled {
		// See startSlow.
		t.Skip("gopeed v1.9.3 has internal data races in every real HTTP transfer; see startSlow")
	}
	o = newSlowOrigin(t, 16<<20)
	dir, state = t.TempDir(), t.TempDir()
	j = Job{TaskID: "t1", URL: o.srv.URL + "/big.bin", Conns: 4}
	u := &updates{}
	e, err := Open(dir, state, u.add)
	if err != nil {
		t.Fatal(err)
	}
	e.Start(j)
	waitUntil(t, "the first megabyte", func() bool { return u.loaded() >= 1<<20 })
	e.Pause(j.TaskID)
	waitUntil(t, "the pause", func() bool { return u.last().Status == core.StatusPaused })
	file = u.last().File
	if u.loaded() >= int64(len(o.data)) {
		t.Fatal("the download finished before it was paused")
	}
	if err := e.Close(); err != nil {
		t.Fatal(err)
	}
	return o, j, file, o.requests(), dir, state
}

func restarted(t *testing.T, dir, state string) (*Engine, *updates) {
	t.Helper()
	u := &updates{}
	e, err := Open(dir, state, u.add)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { e.Close() })
	return e, u
}

// A download paused before a restart carries on with the bytes it has: the
// engine booted on the same folders asks the server only for the rest.
func TestAPausedDownloadCarriesOnAfterARestart(t *testing.T) {
	t.Parallel()
	o, j, file, asked, dir, state := pausedBeforeRestart(t)
	e, u := restarted(t, dir, state)
	if !e.Resumes(j.TaskID, file) {
		t.Fatal("the restarted engine does not carry on with the paused download's file")
	}
	e.Start(j)
	waitUntil(t, "the download finishing", func() bool { return u.last().Status == core.StatusDone })

	o.mu.Lock()
	later := slices.Clone(o.ranges[asked:])
	o.mu.Unlock()
	for _, rg := range later {
		if rg == "" || strings.HasPrefix(rg, "bytes=0-") && rg != "bytes=0-0" {
			t.Errorf("after the restart the file was asked for from the start (Range %q)", rg)
		}
	}
	got, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, o.data) {
		t.Errorf("the finished file (%d bytes) is not what the origin served (%d bytes)", len(got), len(o.data))
	}
}

// A server that sends only the whole file after the restart is asked
// for all of it, and the file that comes out is that file rather than the new
// bytes written over the old ones at the wrong place.
func TestADownloadStartsOverCleanlyWhenTheServerStoppedSendingParts(t *testing.T) {
	t.Parallel()
	o, j, file, _, dir, state := pausedBeforeRestart(t)
	o.whole.Store(true)
	e, u := restarted(t, dir, state)
	e.Start(j)
	waitUntil(t, "the download finishing", func() bool { return u.last().Status == core.StatusDone })

	got, err := os.ReadFile(u.last().File)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, o.data) {
		t.Errorf("the finished file (%d bytes) is not what the origin served (%d bytes)", len(got), len(o.data))
	}
	if u.last().File != file {
		t.Errorf("the download landed in %s rather than in place of its old file %s", u.last().File, file)
	}
	if left, _ := filepath.Glob(filepath.Join(dir, "*")); len(left) != 1 {
		t.Errorf("the folder holds %v; the old file should have made way", left)
	}
}

// Removing a download after the restart, before it ever started again, takes
// its file and its record with it.
func TestRemovingAPausedDownloadAfterARestartDeletesItsFile(t *testing.T) {
	t.Parallel()
	_, j, file, _, dir, state := pausedBeforeRestart(t)
	e, _ := restarted(t, dir, state)
	e.Remove(j.TaskID, true)
	if _, err := os.Stat(file); !os.IsNotExist(err) {
		t.Errorf("the file of the removed download is still there (%v)", err)
	}
	if left, _ := os.ReadDir(state); len(left) != 0 {
		t.Errorf("the state folder still holds %d entries", len(left))
	}
	e.Close()
	again, _ := restarted(t, dir, state)
	if again.Resumes(j.TaskID, file) {
		t.Error("the removed download came back after the next restart")
	}
}
