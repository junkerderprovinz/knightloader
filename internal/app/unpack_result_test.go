package app

// How an unpacking ended, kept on the task so the status column can still say
// it once the job that said it is gone.

import (
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/settings"
)

// lockedZip holds one note under PKWARE's legacy cipher, password "keep",
// written by 7-Zip (it is internal/extract's zipCryptoZip).
const lockedZip = "UEsDBBQAAQAIAERqCV1bMHvNLgAAAFABAAAIAAAAbm90ZS50eHR3U1WomitWeaGhAe+RqwBkJyvs" +
	"9fjz0D4tCvVFUxKKz344qfoneyL86s1NMeJQUEsBAj8AFAABAAgARGoJXVswe80uAAAAUAEAAAgA" +
	"JAAAAAAAAAAgAAAAAAAAAG5vdGUudHh0CgAgAAAAAAABABgAgJHIwPAn3QEAAAAAAAAAAAAAAAAA" +
	"AAAAUEsFBgAAAAABAAEAWgAAAFQAAAAAAA=="

// bootUnpackApp starts an instance on dataDir that downloads into base, the
// way a restart finds the same instance again. The returned func closes it,
// and does nothing once it has.
func bootUnpackApp(t *testing.T, dataDir, base string) (*App, func()) {
	t.Helper()
	a, err := newApp(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	var once sync.Once
	stop := func() { once.Do(func() { a.Close() }) }
	t.Cleanup(stop)
	s := settings.Defaults()
	s.DownloadDir = base
	s.Extract, s.VerifyChecksums, s.Crawl = false, false, false
	if _, err := a.ApplySettings(s); err != nil {
		t.Fatal(err)
	}
	return a, stop
}

func waitForJob(t *testing.T, a *App, taskID string, status ExtractStatus) {
	t.Helper()
	waitFor(t, fmt.Sprintf("the job of %s to end %s", taskID, status), func() bool {
		j, ok := jobFor(a, taskID)
		return ok && j.Status == status
	})
}

// Every part of a set still reads as unpacked after a restart, when the job
// that said so has gone with the process.
func TestAnUnpackedSetStillSaysSoAfterARestart(t *testing.T) {
	dataDir, base := t.TempDir(), t.TempDir()
	a, stop := bootUnpackApp(t, dataDir, base)
	parts := []string{"notes.txt.001", "notes.txt.002", "notes.txt.003"}
	for i, name := range parts {
		if err := os.WriteFile(filepath.Join(base, name), []byte(fmt.Sprintf("part %d\n", i+1)), 0o644); err != nil {
			t.Fatal(err)
		}
		stageDone(t, a, fmt.Sprint(i+1), name)
	}

	if err := a.StartExtraction([]string{"1"}); err != nil {
		t.Fatal(err)
	}
	waitForJob(t, a, "1", ExtractDone)
	for i := range parts {
		if got := liveTask(a, fmt.Sprint(i+1)).Unpack; got != core.UnpackDone {
			t.Errorf("part %d reads %q once the job is done, want %q", i+1, got, core.UnpackDone)
		}
	}
	stop()

	again, _ := bootUnpackApp(t, dataDir, base)
	if jobs := again.ExtractJobs(); len(jobs) != 0 {
		t.Fatalf("the restarted instance knows %d jobs, so this proves nothing about the task", len(jobs))
	}
	for i := range parts {
		live := liveTask(again, fmt.Sprint(i+1))
		if live.Status != core.StatusDone || live.Unpack != core.UnpackDone {
			t.Errorf("part %d is %q with unpack %q after the restart, want done and %q",
				i+1, live.Status, live.Unpack, core.UnpackDone)
		}
	}
}

// A failure is kept as well, and one for want of a password apart from the
// rest, since that is the one somebody comes back to fix.
func TestAFailedUnpackingIsRememberedAfterARestart(t *testing.T) {
	dataDir, base := t.TempDir(), t.TempDir()
	a, stop := bootUnpackApp(t, dataDir, base)
	if err := os.WriteFile(filepath.Join(base, "broken.zip"), []byte("this is not an archive at all"), 0o644); err != nil {
		t.Fatal(err)
	}
	raw, err := base64.StdEncoding.DecodeString(lockedZip)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(base, "locked.zip"), raw, 0o644); err != nil {
		t.Fatal(err)
	}
	stageDone(t, a, "broken", "broken.zip")
	stageDone(t, a, "locked", "locked.zip")

	if err := a.StartExtraction([]string{"broken", "locked"}); err != nil {
		t.Fatal(err)
	}
	waitForJob(t, a, "broken", ExtractFailed)
	waitForJob(t, a, "locked", ExtractFailed)
	stop()

	again, _ := bootUnpackApp(t, dataDir, base)
	want := map[string]core.UnpackResult{"broken": core.UnpackFailed, "locked": core.UnpackPassword}
	for id, result := range want {
		if got := liveTask(again, id).Unpack; got != result {
			t.Errorf("%s reads %q after the restart, want %q", id, got, result)
		}
	}
}

// An unpacking that starts leaves its parts without a result until it ends, so
// one cancelled halfway or cut off by a restart does not pass for the last
// one's.
func TestAStartingUnpackingForgetsHowTheLastOneEnded(t *testing.T) {
	a, base := newRuleApp(t, func(s *settings.Settings, _ string) { s.Extract = false })
	for id, name := range map[string]string{"1": "film.zip", "2": "film.z01", "3": "film.z02"} {
		stageDone(t, a, id, name).Unpack = core.UnpackDone
	}

	a.mu.Lock()
	a.unpackLocked().busy = true
	job := a.enqueueExtractLocked(a.tasks["2"], filepath.Join(base, "film.z01"))
	changed := a.beginUnpackLocked(job, a.tasks[job.TaskID])
	a.mu.Unlock()

	if len(changed) != 3 {
		t.Fatalf("%d rows are published as the job starts, want the whole set", len(changed))
	}
	for _, c := range changed {
		if c.Unpack != core.UnpackNone || c.ArchivePart == 0 {
			t.Errorf("%s is published with unpack %q and part %d, want no result and its number", c.Name, c.Unpack, c.ArchivePart)
		}
	}
	for _, id := range []string{"1", "2", "3"} {
		if got := liveTask(a, id).Unpack; got != core.UnpackNone {
			t.Errorf("task %s still reads %q while the next unpacking runs", id, got)
		}
	}
}

// A download fetched again says nothing about how its archive was last
// unpacked.
func TestRestartingADownloadForgetsHowItsArchiveWasUnpacked(t *testing.T) {
	a := newQueueApp(t)
	// Held, so the restart leaves it queued instead of reaching for a network.
	putTask(t, a, core.Task{ID: "film", URL: "https://host.example/film.zip", Name: "film.zip",
		Status: core.StatusDone, Enabled: true, Hold: true, Unpack: core.UnpackDone})

	a.RestartTasks([]string{"film"})

	if live := liveTask(a, "film"); live.Status != core.StatusQueued || live.Unpack != core.UnpackNone {
		t.Errorf("after the restart the task is %q with unpack %q, want queued with none", live.Status, live.Unpack)
	}
}
