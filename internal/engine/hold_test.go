package engine

import (
	"bytes"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/core"
)

// moveInto returns a Release mapping that puts what was in from into to.
func moveInto(from, to string) func(string) string {
	return func(p string) string {
		if rel, err := filepath.Rel(from, p); err == nil && !strings.HasPrefix(rel, "..") {
			return filepath.Join(to, rel)
		}
		return p
	}
}

// A held transfer's file can be moved, and the transfer carries on in the new
// place from the bytes it has, without the app hearing of a pause.
func TestAHeldTransferCarriesOnWhereItsFileWasMoved(t *testing.T) {
	e, o, u, dir := startSlow(t)
	if !e.Hold("t1") {
		t.Fatal("a running HTTP download was not held")
	}
	held := u.loaded()
	time.Sleep(time.Second)
	if moved := u.loaded() - held; moved > 0 {
		t.Errorf("%d bytes moved while the transfer was held", moved)
	}
	before := o.requests()
	to := filepath.Join(t.TempDir(), "renamed")
	if err := os.MkdirAll(to, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(filepath.Join(dir, "f.bin"), filepath.Join(to, "f.bin")); err != nil {
		t.Fatal(err)
	}

	e.Release("t1", moveInto(dir, to), true)
	waitUntil(t, "the download finishing", func() bool { return u.last().Status == core.StatusDone })

	if u.saw(core.StatusPaused) {
		t.Error("the app was told the task paused")
	}
	if got := u.last().File; got != filepath.Join(to, "f.bin") {
		t.Errorf("the finished download reports %q, want the file in its new folder", got)
	}
	got, err := os.ReadFile(filepath.Join(to, "f.bin"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, o.data) {
		t.Errorf("the finished file (%d bytes) is not what was served (%d bytes)", len(got), len(o.data))
	}
	if _, err := os.Stat(filepath.Join(dir, "f.bin")); err == nil {
		t.Error("a file was written in the old place again")
	}
	o.mu.Lock()
	after := o.ranges[before:]
	o.mu.Unlock()
	for _, rg := range after {
		if rg == "" || strings.HasPrefix(rg, "bytes=0-") {
			t.Errorf("after the release the file was asked for from the start (Range %q)", rg)
		}
	}
}

// A transfer released without resume stays stopped where its file went, for a
// later Resume to carry on.
func TestAReleasedTransferStaysStoppedWithoutResume(t *testing.T) {
	e, _, u, dir := startSlow(t)
	if !e.Hold("t1") {
		t.Fatal("a running HTTP download was not held")
	}
	to := t.TempDir()
	if err := os.Rename(filepath.Join(dir, "f.bin"), filepath.Join(to, "f.bin")); err != nil {
		t.Fatal(err)
	}
	e.Release("t1", moveInto(dir, to), false)
	held := u.loaded()
	time.Sleep(2 * time.Second)
	if moved := u.loaded() - held; moved > 0 {
		t.Errorf("%d bytes moved after a release without resume", moved)
	}

	e.Resume("t1")
	waitUntil(t, "the download finishing", func() bool { return u.last().Status == core.StatusDone })
	if got := u.last().File; got != filepath.Join(to, "f.bin") {
		t.Errorf("the resumed download reports %q, want the file in its new folder", got)
	}
}

// What the engine has no transfer for, or only a torrent, is not held.
func TestHoldLeavesWhatItCannotCarryOver(t *testing.T) {
	e, err := New(t.TempDir(), func(string, core.Update) {})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = e.Close() })
	if e.Hold("unknown") {
		t.Error("a task the engine never started was held")
	}
	e.mu.Lock()
	e.toGopeed["t1"], e.toKL["g1"], e.torrents["t1"] = "g1", "t1", true
	e.mu.Unlock()
	if e.Hold("t1") {
		t.Error("a torrent was held, whose files the library keeps open in the old folder")
	}
}

// A paused download whose folder moves carries on in the new folder after a
// restart, whether it was moved before the restart or after one.
func TestAPausedDownloadMovedToAnotherFolderCarriesOnAfterARestart(t *testing.T) {
	for _, when := range []string{"before the restart", "after a restart"} {
		t.Run(when, func(t *testing.T) {
			if raceEnabled {
				t.Skip("gopeed v1.9.3 has internal data races in every real HTTP transfer; see startSlow")
			}
			o := newSlowOrigin(t, 16<<20)
			root, state := t.TempDir(), t.TempDir()
			from, to := filepath.Join(root, "pkg"), filepath.Join(root, "renamed")
			if err := os.MkdirAll(from, 0o755); err != nil {
				t.Fatal(err)
			}
			j := Job{TaskID: "t1", URL: o.srv.URL + "/big.bin", Conns: 4, Dir: from}
			e, u := restarted(t, root, state)
			e.Start(j)
			waitUntil(t, "the first megabyte", func() bool { return u.loaded() >= 1<<20 })
			e.Pause(j.TaskID)
			waitUntil(t, "the pause", func() bool { return u.last().Status == core.StatusPaused })
			file := u.last().File
			if when == "after a restart" {
				if err := e.Close(); err != nil {
					t.Fatal(err)
				}
				e, _ = restarted(t, root, state)
			}

			if !e.Hold(j.TaskID) {
				t.Fatal("the paused download was not held")
			}
			if err := os.Rename(from, to); err != nil {
				t.Fatal(err)
			}
			e.Release(j.TaskID, moveInto(from, to), false)
			if err := e.Close(); err != nil {
				t.Fatal(err)
			}
			asked := o.requests()

			e, u = restarted(t, root, state)
			file = moveInto(from, to)(file)
			if !e.Resumes(j.TaskID, file) {
				t.Fatal("the restarted engine does not carry on with the moved file")
			}
			j.Dir = to
			e.Start(j)
			waitUntil(t, "the download finishing", func() bool { return u.last().Status == core.StatusDone })
			o.mu.Lock()
			later := slices.Clone(o.ranges[asked:])
			o.mu.Unlock()
			if whole := fromTheStart(later); len(whole) > 0 {
				t.Errorf("after the restart the moved file was asked for from the start (Range %q)", whole)
			}
			got, err := os.ReadFile(file)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(got, o.data) {
				t.Errorf("the finished file (%d bytes) is not what was served (%d bytes)", len(got), len(o.data))
			}
		})
	}
}
