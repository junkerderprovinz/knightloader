package app

// Category overrides for unpacking and file collisions reach the code that acts
// on them. Each test sets the instance-wide value opposite to the category's,
// so a result cannot come from the global fallback.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/junkerderprovinz/knightloader/internal/collide"
	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/settings"
)

// A category can turn unpacking on where the instance says off, and off where
// it says on.
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
			t.Fatal("an archive filed in a category that unpacks was left alone; the instance-wide switch decided")
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
			t.Fatal("an archive in a category that does not unpack was unpacked anyway; the category's false must beat the global true")
		}
		if _, err := os.Stat(filepath.Join(base, "album")); err == nil {
			t.Error("the album was unpacked beside itself")
		}
	})
}

// The dispatcher applies a category's collision rule. Skip is the policy
// decided before the handover, so it is observable without a backend that
// honours a name.
func TestADrawersCollisionRuleSettlesTheDownload(t *testing.T) {
	t.Parallel()
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
		t.Fatalf("a download filed in a category that skips reads %q, want it settled rather than started", status)
	}
	if !strings.Contains(msg, filepath.Join(dir, "clash.bin")) {
		t.Fatalf("the reason %q does not name the file that is in the way", msg)
	}

	// A task in no category follows the instance, which says overwrite, so it
	// starts.
	queueOne(a, "in-no-drawer", "")
	if started := collect(t, stub.got, 1); !started["in-no-drawer"] {
		t.Fatalf("the untagged download was turned down as well; the category's rule leaked onto the instance")
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

// moveOptions applies the category's collision rule to the final move after
// unpacking, too.
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
		t.Fatalf("the episode that was already there reads %q (%v), want it untouched: the category says skip and the move overwrote it", b, err)
	}
	j, _ := jobFor(a, task.ID)
	if j.Moved != 0 || j.Error == "" {
		t.Errorf("the job says %d entries moved and reads %q, want nothing moved and a sentence saying where the files were left", j.Moved, j.Error)
	}
	if _, err := os.Stat(filepath.Join(base, "release", "ep01.mkv")); err != nil {
		t.Errorf("the unpacked episode was neither delivered nor left where it was unpacked: %v", err)
	}
}
