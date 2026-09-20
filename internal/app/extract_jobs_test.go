package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/settings"
)

// stageDone puts a finished download in the list without a backend having run.
func stageDone(t *testing.T, a *App, id, name string) *core.Task {
	t.Helper()
	task := &core.Task{ID: id, URL: "https://host.example/" + name, Name: name, Status: core.StatusDone, Enabled: true}
	a.mu.Lock()
	a.tasks[id] = task
	a.mu.Unlock()
	return task
}

// jobFor reads back the job that belongs to one task.
func jobFor(a *App, taskID string) (ExtractJob, bool) {
	for _, j := range a.ExtractJobs() {
		if j.TaskID == taskID {
			return j, true
		}
	}
	return ExtractJob{}, false
}

// An archive can be unpacked on demand. The auto-extract switch is not
// consulted, since pressing "unpack" is the answer to that question.
func TestStartExtractionUnpacksOnDemand(t *testing.T) {
	a, base := newRuleApp(t, func(s *settings.Settings, _ string) {
		s.Extract, s.VerifyChecksums = false, false
	})
	writeZip(t, filepath.Join(base, "release.zip"), "inside.txt", "unpacked")
	task := stageDone(t, a, "1", "release.zip")

	if err := a.StartExtraction([]string{task.ID}); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the archive being unpacked on demand", func() bool {
		_, err := os.Stat(filepath.Join(base, "release", "inside.txt"))
		return err == nil
	})
	waitFor(t, "the job settling", func() bool {
		j, ok := jobFor(a, task.ID)
		return ok && j.Status == ExtractDone
	})

	j, _ := jobFor(a, task.ID)
	if j.Name != "release.zip" || j.Files != 1 {
		t.Errorf("job = %+v, want the archive's name and the one file it held", j)
	}
	if live := liveTask(a, task.ID); live.Status != core.StatusDone || live.Error != "" {
		t.Errorf("task settled as %q with %q", live.Status, live.Error)
	}
}

// A later successful attempt clears the error an earlier failure left on the
// row.
func TestStartExtractionRetriesAFailedExtraction(t *testing.T) {
	a, base := newRuleApp(t, func(s *settings.Settings, _ string) {
		s.Extract, s.VerifyChecksums = false, false
	})
	arc := filepath.Join(base, "release.zip")
	if err := os.WriteFile(arc, []byte("this is not an archive at all"), 0o644); err != nil {
		t.Fatal(err)
	}
	task := stageDone(t, a, "1", "release.zip")

	if err := a.StartExtraction([]string{task.ID}); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the broken archive failing", func() bool {
		j, ok := jobFor(a, task.ID)
		return ok && j.Status == ExtractFailed
	})
	if live := liveTask(a, task.ID); !strings.HasPrefix(live.Error, "extract: ") {
		t.Fatalf("the task reads %q, want the extraction's own reason", live.Error)
	}

	// Replace the archive with one that opens.
	writeZip(t, arc, "inside.txt", "unpacked")
	if err := a.StartExtraction([]string{task.ID}); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the retry clearing the old failure", func() bool {
		live := liveTask(a, task.ID)
		return live.Error == "" && live.Status == core.StatusDone
	})
	if _, err := os.Stat(filepath.Join(base, "release", "inside.txt")); err != nil {
		t.Errorf("the retry did not unpack the archive: %v", err)
	}
}

// The refusal names each file and why: not an archive, or a part still
// downloading.
func TestStartExtractionSaysWhatItRefused(t *testing.T) {
	a, _ := newRuleApp(t, func(s *settings.Settings, _ string) { s.Extract = false })
	stageDone(t, a, "1", "film.mkv")
	waiting := stageDone(t, a, "2", "set.part01.rar")
	stageDone(t, a, "3", "set.part02.rar").Status = core.StatusRunning

	err := a.StartExtraction([]string{"1", "2"})
	if err == nil {
		t.Fatal("a plain file and an incomplete set were both accepted")
	}
	if !strings.Contains(err.Error(), "film.mkv") || !strings.Contains(err.Error(), "not an archive") {
		t.Errorf("the refusal reads %q, want the plain file named", err)
	}
	if !strings.Contains(err.Error(), "set.part02.rar") {
		t.Errorf("the refusal reads %q, want the part that is still downloading named", err)
	}
	if live := liveTask(a, waiting.ID); live.Status == core.StatusExtracting {
		t.Error("an incomplete set was moved into extracting anyway")
	}
}

// Aborting a queued job returns the task to done instead of leaving it on
// "extracting".
func TestAbortingAQueuedJobHandsTheTaskBack(t *testing.T) {
	a, base := newRuleApp(t, func(s *settings.Settings, _ string) { s.Extract = false })
	writeZip(t, filepath.Join(base, "release.zip"), "inside.txt", "unpacked")
	task := stageDone(t, a, "1", "release.zip")

	// The worker is marked busy so the job stays queued.
	a.mu.Lock()
	a.unpackLocked().busy = true
	job := a.enqueueExtractLocked(task, filepath.Join(base, "release.zip"))
	a.mu.Unlock()
	if job == nil {
		t.Fatal("the job was not queued")
	}
	if live := liveTask(a, task.ID); live.Status != core.StatusExtracting {
		t.Fatalf("the queued task reads %q, want extracting", live.Status)
	}

	if err := a.AbortExtraction(job.ID); err != nil {
		t.Fatal(err)
	}
	if j, _ := jobFor(a, task.ID); j.Status != ExtractCancelled {
		t.Errorf("job status = %q, want cancelled", j.Status)
	}
	if live := liveTask(a, task.ID); live.Status != core.StatusDone {
		t.Errorf("the task reads %q after the abort, want done", live.Status)
	}
	if _, err := os.Stat(filepath.Join(base, "release")); err == nil {
		t.Error("a job that never ran unpacked something")
	}
	if err := a.AbortExtraction("no-such-job"); err == nil {
		t.Error("aborting a job that does not exist was accepted")
	}
}

// Numbered split parts are joined into one file once the last part lands.
func TestASplitDownloadIsJoined(t *testing.T) {
	a, base := newRuleApp(t, func(s *settings.Settings, _ string) { s.Extract, s.VerifyChecksums = true, false })
	parts := []string{"once upon ", "a time ", "in the west"}
	for i, body := range parts {
		name := filepath.Join(base, "notes.txt.00"+string(rune('1'+i)))
		if err := os.WriteFile(name, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	first := stageDone(t, a, "1", "notes.txt.001")
	stageDone(t, a, "2", "notes.txt.002")
	last := stageDone(t, a, "3", "notes.txt.003")

	// Nothing is due while a part is missing.
	a.mu.Lock()
	last.Status = core.StatusRunning
	due, _ := a.extractionDueLocked(first, a.Settings.Get())
	a.mu.Unlock()
	if due != nil {
		t.Fatal("a split set was joined with a part still downloading")
	}

	a.mu.Lock()
	last.Status = core.StatusDone
	target := a.extractNowLocked(last, a.Settings.Get())
	a.mu.Unlock()
	if target == nil || target.ID != first.ID {
		t.Fatalf("the join started on %v, want the first part", target)
	}
	waitFor(t, "the parts being joined", func() bool {
		body, err := os.ReadFile(filepath.Join(base, "notes.txt"))
		return err == nil && string(body) == strings.Join(parts, "")
	})
}

// A spanned zip ends with its .zip file, unlike a spanned rar, so a plain name
// sort would number the last part first.
func TestASpannedZipIsNumberedWithItsZipLast(t *testing.T) {
	a, _ := newRuleApp(t, func(s *settings.Settings, _ string) { s.Extract = false })
	last := stageDone(t, a, "1", "film.zip")
	stageDone(t, a, "2", "film.z01")
	stageDone(t, a, "3", "film.z02")

	a.mu.Lock()
	changed := a.stampPartsLocked(last)
	a.mu.Unlock()
	if len(changed) != 3 {
		t.Fatalf("%d rows were numbered, want the whole set", len(changed))
	}
	want := map[string]int{"film.z01": 1, "film.z02": 2, "film.zip": 3}
	for _, c := range changed {
		if c.ArchivePart != want[c.Name] {
			t.Errorf("%s is part %d, want %d", c.Name, c.ArchivePart, want[c.Name])
		}
	}
}
