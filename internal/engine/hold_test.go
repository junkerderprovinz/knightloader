package engine

import (
	"bytes"
	"os"
	"path/filepath"
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
