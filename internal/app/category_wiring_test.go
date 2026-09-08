package app

// The three category overrides this batch made live, each guarded where it
// would otherwise fall out in silence. The folder was wired and tested a wave
// ago (batch3_wiring_test.go); unpacking and the collision rule round-tripped
// through the API, resolved correctly in internal/settings, and reached nothing
// at all - a drawer that says "unpack these, overwrite what is in the way" and
// does neither.
//
// Every test here states the INSTANCE's answer as the opposite of the drawer's,
// so a result it observes cannot have come from the global setting. A test that
// agrees with the fallback passes just as well against no wiring, which is how
// nine lines shipped inert here three weeks ago.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/junkerderprovinz/knightloader/internal/collide"
	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/settings"
)

// TestADrawerDecidesWhetherAnArchiveIsUnpacked guards extractWanted's middle
// rung. Both halves are needed and neither is redundant: one drawer turns
// unpacking on where the instance says off, the other turns it off where the
// instance says on, and only the pair proves that a category's nil Extract is
// still "no opinion" rather than a third state somebody folded onto false.
func TestADrawerDecidesWhetherAnArchiveIsUnpacked(t *testing.T) {
	yes, no := true, false

	t.Run("a drawer that unpacks beats an instance that does not", func(t *testing.T) {
		a, base := newRuleApp(t, func(s *settings.Settings, _ string) {
			s.Extract, s.VerifyChecksums = false, false
			s.Categories = []settings.Category{{ID: "serien", Name: "Serien", Extract: &yes}}
		})
		writeZip(t, filepath.Join(base, "release.zip"), "inside.txt", "unpacked")
		task := stageDone(t, a, "1", "release.zip")

		a.mu.Lock()
		task.Category = "serien"
		target := a.extractNowLocked(task, a.Settings.Get())
		a.mu.Unlock()

		if target == nil {
			t.Fatal("an archive filed in a drawer that unpacks was left alone; the drawer is being asked nothing and the instance-wide switch decided")
		}
		waitFor(t, "the archive unpacking because its drawer says so", func() bool {
			_, err := os.Stat(filepath.Join(base, "release", "inside.txt"))
			return err == nil
		})
	})

	t.Run("a drawer that does not unpack beats an instance that does", func(t *testing.T) {
		a, base := newRuleApp(t, func(s *settings.Settings, _ string) {
			s.Extract, s.VerifyChecksums = true, false
			s.Categories = []settings.Category{{ID: "musik", Name: "Musik", Extract: &no}}
		})
		writeZip(t, filepath.Join(base, "album.zip"), "track01.flac", "the album")
		task := stageDone(t, a, "2", "album.zip")

		a.mu.Lock()
		task.Category = "musik"
		target := a.extractNowLocked(task, a.Settings.Get())
		a.mu.Unlock()

		if target != nil {
			t.Fatal("a drawer where the archive IS the delivery was unpacked anyway; a false in a category has to survive a global that says true")
		}
		if _, err := os.Stat(filepath.Join(base, "album")); err == nil {
			t.Error("the album was unpacked beside itself")
		}
	})
}

// TestADrawersCollisionRuleSettlesTheDownload guards the dispatcher's read.
// Skip is the only policy decided before the handover, so it is the only one
// this side can observe without a backend that honours a name - which makes it
// the one that has to be tested here, not the convenient one.
//
// The instance is set to overwrite, so a task that starts anyway is the wiring
// missing rather than the policy being applied somewhere else.
func TestADrawersCollisionRuleSettlesTheDownload(t *testing.T) {
	dir := t.TempDir()
	a := newQueueApp(t)
	s := a.Settings.Get()
	s.DownloadDir = dir
	s.CollisionPolicy = string(collide.Overwrite)
	s.Categories = []settings.Category{{ID: "serien", Name: "Serien", Collision: string(collide.Skip)}}
	if _, err := a.ApplySettings(s); err != nil {
		t.Fatal(err)
	}
	stub := &stubBackend{got: make(chan string, 4)}
	a.bmu.Lock()
	a.debrid["elsewhere"] = stub
	a.bmu.Unlock()
	a.Registry.Register(elsewhereResolver{})
	if err := os.WriteFile(filepath.Join(dir, "clash.bin"), []byte("already here"), 0o644); err != nil {
		t.Fatal(err)
	}

	filed := queueOne(a, "in-a-drawer", "serien")
	expectNone(t, stub.got)

	a.mu.Lock()
	status, msg := filed.Status, filed.Error
	a.mu.Unlock()
	if status != core.StatusError {
		t.Fatalf("a download filed in a drawer that skips reads %q, want it settled rather than started", status)
	}
	if !strings.Contains(msg, filepath.Join(dir, "clash.bin")) {
		t.Fatalf("the reason %q does not name the file that is in the way", msg)
	}

	// The other side of the same read: a task in no drawer is still governed by
	// the instance, which says overwrite, so it goes. Without this the test
	// would also pass against a build that skips everything.
	queueOne(a, "in-no-drawer", "")
	if started := collect(t, stub.got, 1); !started["in-no-drawer"] {
		t.Fatalf("the untagged download was turned down as well; the drawer's rule leaked onto the instance")
	}
}

// queueOne puts one download for the file that is already in the folder into
// the queue and runs a dispatch pass over it.
func queueOne(a *App, id, category string) *core.Task {
	task := &core.Task{
		ID: id, URL: "https://elsewhere.example/clash.bin", Name: "clash.bin",
		Resolver: "elsewhere", Category: category,
		Status: core.StatusQueued, Enabled: true,
	}
	a.mu.Lock()
	a.tasks[id] = task
	a.queue = append(a.queue, id)
	a.dispatchLocked()
	a.mu.Unlock()
	return task
}

// TestADrawersCollisionRuleReachesTheLastMove guards moveOptions, which is the
// second door onto the same setting: the dispatcher's answer decides whether a
// download starts, and this one decides what happens to the file at the end of
// the journey, minutes later and on a different goroutine.
//
// The instance says overwrite, so the file that was already there surviving is
// the drawer's doing and nothing else's.
func TestADrawersCollisionRuleReachesTheLastMove(t *testing.T) {
	target := t.TempDir()
	a, base := newRuleApp(t, func(s *settings.Settings, _ string) {
		s.Extract, s.VerifyChecksums = false, false
		s.ExtractMoveTo = target
		s.CollisionPolicy = string(collide.Overwrite)
		s.Categories = []settings.Category{{ID: "serien", Name: "Serien", Collision: string(collide.Skip)}}
	})
	if err := os.WriteFile(filepath.Join(target, "ep01.mkv"), []byte("mine"), 0o644); err != nil {
		t.Fatal(err)
	}
	writeZip(t, filepath.Join(base, "release.zip"), "ep01.mkv", "the new one")
	task := stageDone(t, a, "1", "release.zip")
	a.mu.Lock()
	task.Category = "serien"
	a.mu.Unlock()

	if err := a.StartExtraction([]string{task.ID}); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the job settling", func() bool {
		j, ok := jobFor(a, task.ID)
		return ok && j.Status == ExtractDone
	})

	if b, err := os.ReadFile(filepath.Join(target, "ep01.mkv")); err != nil || string(b) != "mine" {
		t.Fatalf("the episode that was already there reads %q (%v), want it untouched: the drawer says skip and the move overwrote it", b, err)
	}
	j, _ := jobFor(a, task.ID)
	if j.Moved != 0 || j.Error == "" {
		t.Errorf("the job says %d entries moved and reads %q, want nothing moved and a sentence saying where the files were left", j.Moved, j.Error)
	}
	if _, err := os.Stat(filepath.Join(base, "release", "ep01.mkv")); err != nil {
		t.Errorf("the unpacked episode was neither delivered nor left where it was unpacked: %v", err)
	}
}
