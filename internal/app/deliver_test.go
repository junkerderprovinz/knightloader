package app

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/collide"
	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/settings"
	"github.com/junkerderprovinz/knightloader/internal/workdir"
)

// stagedIn makes the working folder and puts a file in it, as the engine does
// when a working folder is configured.
func stagedIn(t *testing.T, dir, name, body string) string {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// gone waits for a path to disappear. Removing the emptied working folder is
// the last step of delivery, after the move the callers wait for, so a single
// look can race it on a loaded machine.
func gone(t *testing.T, path string) bool {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for {
		if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
			return true
		}
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// The finished file moves from the working folder to its destination.
func TestAFinishedDownloadLeavesTheWorkingFolder(t *testing.T) {
	work := t.TempDir()
	a, base := newRuleApp(t, func(s *settings.Settings, _ string) {
		s.Extract, s.VerifyChecksums = false, false
		s.WorkDir = work
	})
	folder := workdir.For(work, base)
	stagedIn(t, folder, "film.mkv", "the whole film")
	stageDone(t, a, "1", "film.mkv")

	a.deliverDownload("1")

	if _, err := os.Stat(filepath.Join(base, "film.mkv")); err != nil {
		t.Fatalf("the finished download never reached its destination: %v", err)
	}
	if !gone(t, folder) {
		t.Error("the emptied working folder was left behind")
	}
	if live := liveTask(a, "1"); live.Error != "" {
		t.Errorf("the task reads %q after a delivery that worked", live.Error)
	}
}

// engine.Job skips the collision policy when WorkDir is set, so WorkDir must be
// empty without a working folder, or every download would lose its collision
// handling.
func TestABackendIsToldNothingItDoesNotNeedToKnow(t *testing.T) {
	work := t.TempDir()
	staged, base := newRuleApp(t, func(s *settings.Settings, _ string) {
		s.Extract, s.VerifyChecksums = false, false
		s.WorkDir = work
	})
	task := stageDone(t, staged, "1", "film.mkv")
	staged.mu.Lock()
	got := staged.stagedDirFor(task)
	staged.mu.Unlock()
	if want := workdir.For(work, base); got != want {
		t.Errorf("stagedDirFor = %q, want the working folder %q", got, want)
	}

	plain, _ := newRuleApp(t, func(s *settings.Settings, _ string) {
		s.Extract, s.VerifyChecksums = false, false
	})
	other := stageDone(t, plain, "1", "film.mkv")
	plain.mu.Lock()
	got = plain.stagedDirFor(other)
	plain.mu.Unlock()
	if got != "" {
		t.Errorf("stagedDirFor = %q with nothing configured, want the empty string", got)
	}
}

// An archive still to be unpacked stays put: internal/extract finds the other
// volumes by listing the first one's folder.
func TestADownloadThatStillOwesAnUnpackingStaysPut(t *testing.T) {
	work := t.TempDir()
	a, base := newRuleApp(t, func(s *settings.Settings, _ string) {
		s.Extract, s.VerifyChecksums = false, false
		s.WorkDir = work
	})
	folder := workdir.For(work, base)
	src := stagedIn(t, folder, "set.part01.rar", "volume one")
	task := stageDone(t, a, "1", "set.part01.rar")

	a.mu.Lock()
	task.Status = core.StatusExtracting
	a.mu.Unlock()
	a.deliverDownload("1")

	if _, err := os.Stat(src); err != nil {
		t.Fatalf("a volume was delivered while its own archive was still being unpacked: %v", err)
	}
	if _, err := os.Stat(filepath.Join(base, "set.part01.rar")); err == nil {
		t.Error("the volume reached the destination anyway")
	}
}

// The working folder is keyed by destination, not by task, so the volumes of
// one set share it.
func TestEveryPartOfOneSetSharesOneWorkingFolder(t *testing.T) {
	work := t.TempDir()
	a, base := newRuleApp(t, func(s *settings.Settings, _ string) {
		s.Extract, s.VerifyChecksums = false, false
		s.WorkDir = work
	})
	folder := workdir.For(work, base)
	first := stageDone(t, a, "1", "set.part01.rar")
	stageDone(t, a, "2", "set.part02.rar")
	last := stageDone(t, a, "3", "set.part03.rar")

	a.mu.Lock()
	target, path := a.extractCandidateLocked(last)
	a.mu.Unlock()
	if target == nil {
		t.Fatal("the three parts were not recognised as one set")
	}
	if target.ID != first.ID {
		t.Errorf("the set opens on %q, want the first volume", target.Name)
	}
	if want := filepath.Join(folder, "set.part01.rar"); path != want {
		t.Errorf("the archive would be opened at %q, want %q", path, want)
	}
}

// An archive is unpacked in the working folder and only the finished folder
// appears at the destination, so a library scanner never sees half a release.
func TestAnArchiveIsUnpackedInTheWorkingFolderAndDeliveredAfterwards(t *testing.T) {
	work := t.TempDir()
	a, base := newRuleApp(t, func(s *settings.Settings, _ string) {
		s.Extract, s.VerifyChecksums = false, false
		s.WorkDir = work
	})
	folder := workdir.For(work, base)
	if err := os.MkdirAll(folder, 0o755); err != nil {
		t.Fatal(err)
	}
	writeZip(t, filepath.Join(folder, "release.zip"), "inside.txt", "unpacked")
	task := stageDone(t, a, "1", "release.zip")

	if err := a.StartExtraction([]string{task.ID}); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the unpacked release reaching its destination", func() bool {
		_, err := os.Stat(filepath.Join(base, "release", "inside.txt"))
		return err == nil
	})
	waitFor(t, "the job settling", func() bool {
		j, ok := jobFor(a, task.ID)
		return ok && j.Status == ExtractDone
	})

	j, _ := jobFor(a, task.ID)
	if want := filepath.Join(base, "release"); j.MovedTo != want {
		t.Errorf("the job says the content went to %q, want %q", j.MovedTo, want)
	}
	if j.Error != "" {
		t.Errorf("the job reads %q", j.Error)
	}
	// The archive is kept by default, so it is delivered too.
	waitFor(t, "the kept archive following its own release out", func() bool {
		_, err := os.Stat(filepath.Join(base, "release.zip"))
		return err == nil
	})
	if !gone(t, folder) {
		t.Errorf("%s is still there after everything in it was delivered", folder)
	}
}

// The release moves to the folder it would have been unpacked into, including
// the per-package level.
func TestThePackageSubfolderSurvivesTheMoveBackOut(t *testing.T) {
	work, unpacked := t.TempDir(), t.TempDir()
	a, base := newRuleApp(t, func(s *settings.Settings, _ string) {
		s.Extract, s.VerifyChecksums = false, false
		s.WorkDir = work
		s.ExtractTo = unpacked
		s.ExtractSubfolder = true
	})
	folder := workdir.For(work, base)
	if err := os.MkdirAll(folder, 0o755); err != nil {
		t.Fatal(err)
	}
	writeZip(t, filepath.Join(folder, "release.zip"), "inside.txt", "unpacked")
	task := stageDone(t, a, "1", "release.zip")
	a.mu.Lock()
	task.Package = "The Show"
	a.mu.Unlock()

	if err := a.StartExtraction([]string{task.ID}); err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(unpacked, "The Show", "release", "inside.txt")
	waitFor(t, "the release landing under its own package folder", func() bool {
		_, err := os.Stat(want)
		return err == nil
	})
	if _, err := os.Stat(filepath.Join(unpacked, "release", "inside.txt")); err == nil {
		t.Error("the release also landed beside the package folder, so the level was applied twice")
	}
}

// ExtractMoveTo receives the unpacked entries themselves, without the release
// folder around them.
func TestTheUnpackedContentGoesWhereTheSettingSays(t *testing.T) {
	target := t.TempDir()
	a, base := newRuleApp(t, func(s *settings.Settings, _ string) {
		s.Extract, s.VerifyChecksums = false, false
		s.ExtractMoveTo = filepath.Join(target, "<jd:packagename>")
	})
	writeZip(t, filepath.Join(base, "Show.S01.COMPLETE.WEB.zip"), "ep01.mkv", "one")
	task := stageDone(t, a, "1", "Show.S01.COMPLETE.WEB.zip")
	a.mu.Lock()
	task.Package = "The Show"
	a.mu.Unlock()

	if err := a.StartExtraction([]string{task.ID}); err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(target, "The Show")
	waitFor(t, "the episode landing where the episodes go", func() bool {
		_, err := os.Stat(filepath.Join(want, "ep01.mkv"))
		return err == nil
	})
	waitFor(t, "the job settling", func() bool {
		j, ok := jobFor(a, task.ID)
		return ok && j.Status == ExtractDone
	})

	j, _ := jobFor(a, task.ID)
	if j.MovedTo != want || j.Moved != 1 {
		t.Errorf("the job says %d entries went to %q, want 1 to %q", j.Moved, j.MovedTo, want)
	}
	if _, err := os.Stat(filepath.Join(want, "Show.S01.COMPLETE.WEB")); err == nil {
		t.Error("the release folder came along, which is the level this setting exists to drop")
	}
	if !gone(t, filepath.Join(base, "Show.S01.COMPLETE.WEB")) {
		t.Error("the emptied unpack folder was left behind beside the archive")
	}
}

// A skipped delivery is reported on the job, so the files left behind are
// accounted for.
func TestASkippedDeliveryIsSaidOutLoud(t *testing.T) {
	target := t.TempDir()
	a, base := newRuleApp(t, func(s *settings.Settings, _ string) {
		s.Extract, s.VerifyChecksums = false, false
		s.ExtractMoveTo = target
		s.CollisionPolicy = string(collide.Skip)
	})
	if err := os.WriteFile(filepath.Join(target, "ep01.mkv"), []byte("mine"), 0o644); err != nil {
		t.Fatal(err)
	}
	writeZip(t, filepath.Join(base, "release.zip"), "ep01.mkv", "the new one")
	task := stageDone(t, a, "1", "release.zip")

	if err := a.StartExtraction([]string{task.ID}); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the job settling", func() bool {
		j, ok := jobFor(a, task.ID)
		return ok && j.Status == ExtractDone
	})

	j, _ := jobFor(a, task.ID)
	if j.Error == "" || j.Moved != 0 {
		t.Errorf("the job says %d moved and reads %q, want nothing moved and a reason", j.Moved, j.Error)
	}
	if b, err := os.ReadFile(filepath.Join(target, "ep01.mkv")); err != nil || string(b) != "mine" {
		t.Errorf("the file that was already there reads %q, %v", b, err)
	}
	if _, err := os.Stat(filepath.Join(base, "release", "ep01.mkv")); err != nil {
		t.Errorf("the unpacked file was neither delivered nor left where it was: %v", err)
	}
}

// By default nothing is moved.
func TestNothingMovesWithoutBeingAskedTo(t *testing.T) {
	a, base := newRuleApp(t, func(s *settings.Settings, _ string) {
		s.Extract, s.VerifyChecksums = false, false
	})
	writeZip(t, filepath.Join(base, "release.zip"), "inside.txt", "unpacked")
	task := stageDone(t, a, "1", "release.zip")

	a.mu.Lock()
	if got := a.workDirFor(task); got != base {
		t.Errorf("workDirFor = %q with nothing configured, want the destination %q", got, base)
	}
	a.mu.Unlock()

	if err := a.StartExtraction([]string{task.ID}); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the archive unpacking beside itself as it always did", func() bool {
		_, err := os.Stat(filepath.Join(base, "release", "inside.txt"))
		return err == nil
	})
	j, _ := jobFor(a, task.ID)
	if j.MovedTo != "" {
		t.Errorf("the job says something was moved to %q; nothing was configured", j.MovedTo)
	}
	if _, err := os.Stat(filepath.Join(base, "release.zip")); err != nil {
		t.Errorf("the archive was moved out of the folder it was downloaded into: %v", err)
	}
}
