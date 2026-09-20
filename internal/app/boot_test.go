package app

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/settings"
)

// bootFixture is a data directory with tasks in the store and settings saved,
// as a process that died leaves it. The tests boot a real App against it.
type bootFixture struct {
	dataDir string
	dlDir   string
}

func newBootFixture(t *testing.T, mutate func(s *settings.Settings), tasks ...core.Task) bootFixture {
	t.Helper()
	f := bootFixture{dataDir: t.TempDir(), dlDir: t.TempDir()}

	first, err := New(f.dataDir)
	if err != nil {
		t.Fatal(err)
	}
	s := settings.Defaults()
	s.DownloadDir = f.dlDir
	s.Crawl = false
	if mutate != nil {
		mutate(&s)
	}
	if _, err := first.ApplySettings(s); err != nil {
		t.Fatal(err)
	}
	for i := range tasks {
		task := tasks[i]
		if task.CreatedAt.IsZero() {
			task.CreatedAt = time.Now().Add(-time.Hour)
		}
		if err := first.Store.Save(&task); err != nil {
			t.Fatal(err)
		}
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	return f
}

// boot opens the directory again.
func (f bootFixture) boot(t *testing.T) *App {
	t.Helper()
	a, err := New(f.dataDir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { a.Close() })
	return a
}

// taskOf reads one task out of a booted app.
func taskOf(t *testing.T, a *App, id string) core.Task {
	t.Helper()
	a.mu.Lock()
	defer a.mu.Unlock()
	got := a.tasks[id]
	if got == nil {
		t.Fatalf("task %s is not in the list after boot", id)
	}
	return *got
}

func writeFile(t *testing.T, dir, name string, size int) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), make([]byte, size), 0o644); err != nil {
		t.Fatal(err)
	}
}

// A stored "running" task belongs to a process that is gone, so it must not
// come back as running.
func TestABootedTaskSaysSomethingTrue(t *testing.T) {
	dl := t.TempDir()
	f := newBootFixture(t, nil, core.Task{
		ID: "was-running", URL: "https://host.example/big.bin", Name: "big.bin",
		Dir: dl, Status: core.StatusRunning, Size: 4096, Loaded: 2048, Speed: 900_000,
		Enabled: true,
	})
	writeFile(t, dl, "big.bin", 4096)

	a := f.boot(t)
	got := taskOf(t, a, "was-running")
	// Queued behind a stopped queue, so pressing play resumes it.
	if got.Status != core.StatusQueued {
		t.Errorf("status = %q, want queued: it waits, and the queue behind it is stopped", got.Status)
	}
	a.mu.Lock()
	halted, running := a.halted, len(a.active)
	a.mu.Unlock()
	if !halted {
		t.Error("the queue came up live with nothing able to run in it")
	}
	if running != 0 {
		t.Errorf("%d tasks dispatched on boot, want 0 under the default policy", running)
	}
	if got.Speed != 0 {
		t.Errorf("speed = %d, want 0: a stopped transfer cannot have a rate", got.Speed)
	}
	if got.Loaded != 2048 {
		t.Errorf("loaded = %d, want the 2048 bytes that are still on disk", got.Loaded)
	}
}

// The byte count is kept only while the partial file is still on disk.
func TestProgressWithoutBytesIsNotClaimed(t *testing.T) {
	dl := t.TempDir()
	f := newBootFixture(t, nil, core.Task{
		ID: "no-file", URL: "https://host.example/gone.bin", Name: "gone.bin",
		Dir: dl, Status: core.StatusRunning, Size: 4096, Loaded: 2048, Enabled: true,
	})

	a := f.boot(t)
	got := taskOf(t, a, "no-file")
	if got.Status != core.StatusQueued {
		t.Errorf("status = %q, want queued behind a stopped queue", got.Status)
	}
	if got.Loaded != 0 {
		t.Errorf("loaded = %d, want 0: the partial file is not there", got.Loaded)
	}
}

// By default nothing starts after a restart: no backend's handle survives the
// process, so resuming is a fresh fetch, which somebody has to ask for.
func TestTheDefaultStartsNothing(t *testing.T) {
	f := newBootFixture(t, nil, core.Task{
		ID: "r", URL: "https://host.example/a.bin", Name: "a.bin",
		Status: core.StatusRunning, Enabled: true,
	})

	a := f.boot(t)
	if got := taskOf(t, a, "r"); got.Status != core.StatusQueued {
		t.Errorf("status = %q, want queued behind a stopped queue", got.Status)
	}
	a.mu.Lock()
	active, halted := len(a.active), a.halted
	a.mu.Unlock()
	// What runs is asserted, not the queue: a task queued behind a stopped
	// queue has not started.
	if active != 0 {
		t.Errorf("%d downloads started under a policy that says never", active)
	}
	if !halted {
		t.Error("the queue came up live, so the next thing to touch it would start everything")
	}
}

// ResumeRunning is "carry on where you left off".
func TestResumeRunningPutsTheQueueBack(t *testing.T) {
	f := newBootFixture(t,
		func(s *settings.Settings) { s.ResumeOnStart = settings.ResumeRunning },
		core.Task{ID: "was-running", URL: "https://host.example/a.bin", Name: "a.bin",
			Status: core.StatusRunning, Enabled: true},
		core.Task{ID: "was-waiting", URL: "https://host.example/b.bin", Name: "b.bin",
			Status: core.StatusQueued, Enabled: true},
		core.Task{ID: "was-paused", URL: "https://host.example/c.bin", Name: "c.bin",
			Status: core.StatusPaused, Enabled: true},
	)

	a := f.boot(t)
	for _, id := range []string{"was-running", "was-waiting"} {
		if got := taskOf(t, a, id); got.Status != core.StatusQueued {
			t.Errorf("%s = %q, want queued: the queue was live when the process stopped", id, got.Status)
		}
	}
	// A task paused by hand stays paused.
	if got := taskOf(t, a, "was-paused"); got.Status != core.StatusPaused {
		t.Errorf("was-paused = %q, want it left alone", got.Status)
	}
}

// ResumeRunning resumes only if something was running; starting the waiting
// links of an idle instance would be ResumeAll.
func TestResumeRunningStaysPutWhenNothingWas(t *testing.T) {
	f := newBootFixture(t,
		func(s *settings.Settings) { s.ResumeOnStart = settings.ResumeRunning },
		core.Task{ID: "waiting", URL: "https://host.example/a.bin", Name: "a.bin",
			Status: core.StatusQueued, Enabled: true},
	)

	a := f.boot(t)
	if got := taskOf(t, a, "waiting"); got.Status != core.StatusQueued {
		t.Errorf("status = %q, want queued: it waits behind a stopped queue", got.Status)
	}
	a.mu.Lock()
	halted, running := a.halted, len(a.active)
	a.mu.Unlock()
	if !halted {
		t.Error("the queue came up live, as if the policy were ResumeAll")
	}
	if running != 0 {
		t.Errorf("%d tasks dispatched, want 0; nothing was running when the process stopped", running)
	}
}

// ResumeAll takes the waiting links too.
func TestResumeAllTakesTheWaitingOnesToo(t *testing.T) {
	f := newBootFixture(t,
		func(s *settings.Settings) { s.ResumeOnStart = settings.ResumeAll },
		core.Task{ID: "waiting", URL: "https://host.example/a.bin", Name: "a.bin",
			Status: core.StatusQueued, Enabled: true},
	)

	a := f.boot(t)
	if got := taskOf(t, a, "waiting"); got.Status != core.StatusQueued {
		t.Errorf("status = %q, want queued", got.Status)
	}
}

// An interrupted extraction comes back as a finished download, written to the
// store so the history and retention see it.
func TestAnInterruptedExtractionIsAFinishedDownload(t *testing.T) {
	f := newBootFixture(t, nil, core.Task{
		ID: "unpacking", URL: "https://host.example/set.rar", Name: "set.rar",
		Status: core.StatusExtracting, Size: 4096, Loaded: 4096, Enabled: true,
	})

	a := f.boot(t)
	if got := taskOf(t, a, "unpacking"); got.Status != core.StatusDone {
		t.Errorf("status = %q, want done: the download finished, the unpacking did not", got.Status)
	}
	hist, err := a.History(0)
	if err != nil {
		t.Fatal(err)
	}
	if len(hist) != 1 || hist[0].TaskID != "unpacking" {
		t.Fatalf("history holds %+v, want the download the boot settled", hist)
	}
	if hist[0].FinishedAt.IsZero() {
		t.Error("the settled download reached the history with no finish time")
	}
}

// NextTry is persisted but the timer behind it is not, so a restart clears it
// rather than promising a retry that will not come. The spent count stays,
// since the backoff continues from it on a manual restart.
func TestADeadlineDoesNotOutliveTheProcessCountingToIt(t *testing.T) {
	f := newBootFixture(t, nil, core.Task{
		ID: "failed", URL: "https://host.example/f.bin", Name: "f.bin",
		Status: core.StatusError, Error: "the transfer broke", Enabled: true,
		Retries: 2, NextTry: time.Now().Add(5 * time.Minute),
	})

	a := f.boot(t)
	got := taskOf(t, a, "failed")
	if !got.NextTry.IsZero() {
		t.Errorf("a retry is still promised for %v, with nothing left alive to run it", got.NextTry)
	}
	if got.Status != core.StatusError {
		t.Errorf("status = %q, want the failure left exactly where it is", got.Status)
	}
	if got.Retries != 2 {
		t.Errorf("retries = %d, want the two spent attempts kept: the ladder continues from them", got.Retries)
	}

	// Cleared in the store too, which the next boot reads.
	rows, err := a.Store.All()
	if err != nil {
		t.Fatal(err)
	}
	var stored *core.Task
	for _, r := range rows {
		if r.ID == "failed" {
			stored = r
		}
	}
	if stored == nil {
		t.Fatal("the failed task is not in the store after boot")
	}
	if !stored.NextTry.IsZero() {
		t.Errorf("the row still holds %v, so the next boot reads the dead deadline back", stored.NextTry)
	}
}

// Retention removes rows from the list and never the downloaded files.
func TestRetentionTrimsTheListAndNothingElse(t *testing.T) {
	dl := t.TempDir()
	old := time.Now().Add(-72 * time.Hour)
	f := newBootFixture(t,
		func(s *settings.Settings) { s.KeepFinishedDays = 1 },
		core.Task{ID: "ancient", URL: "https://host.example/old.bin", Name: "old.bin",
			Dir: dl, Status: core.StatusDone, Size: 4096, Loaded: 4096,
			CreatedAt: old, FinishedAt: old, Enabled: true},
		core.Task{ID: "recent", URL: "https://host.example/new.bin", Name: "new.bin",
			Dir: dl, Status: core.StatusDone, Size: 4096, Loaded: 4096,
			CreatedAt: time.Now(), FinishedAt: time.Now(), Enabled: true},
	)
	writeFile(t, dl, "old.bin", 4096)

	a := f.boot(t)
	a.mu.Lock()
	_, stillListed := a.tasks["ancient"]
	_, keptRecent := a.tasks["recent"]
	a.mu.Unlock()
	if stillListed {
		t.Error("a download finished three days ago is still in the list with a one-day retention")
	}
	if !keptRecent {
		t.Error("retention took a download that finished a moment ago")
	}
	if _, err := os.Stat(filepath.Join(dl, "old.bin")); err != nil {
		t.Errorf("retention deleted the file: %v", err)
	}
	hist, err := a.History(0)
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, e := range hist {
		if e.TaskID == "ancient" {
			found = true
		}
	}
	if !found {
		t.Error("the trimmed download is not in the history, so nothing records that this instance ever fetched it")
	}
}

// A retention of zero means keep forever.
func TestRetentionCanBeSwitchedOff(t *testing.T) {
	old := time.Now().Add(-10000 * time.Hour)
	f := newBootFixture(t,
		func(s *settings.Settings) { s.KeepFinishedDays = 0 },
		core.Task{ID: "ancient", URL: "https://host.example/old.bin", Name: "old.bin",
			Status: core.StatusDone, CreatedAt: old, FinishedAt: old, Enabled: true},
	)

	a := f.boot(t)
	a.mu.Lock()
	_, kept := a.tasks["ancient"]
	a.mu.Unlock()
	if !kept {
		t.Error("a retention of zero days removed a finished download; zero has to mean keep for ever")
	}
}

// The store stamps FinishedAt on the saved copy, not on the live task, so the
// sweep copies it back.
func TestTheAppCatchesUpWithItsOwnFinishTimes(t *testing.T) {
	f := newBootFixture(t, nil)
	a := f.boot(t)

	live := putTask(t, a, core.Task{
		ID: "settling", URL: "https://host.example/f.bin", Name: "f.bin",
		Status: core.StatusDone, Enabled: true,
	})
	// As a settle does: save a copy, keep the live task.
	c := *live
	if err := a.Store.Save(&c); err != nil {
		t.Fatal(err)
	}
	if c.FinishedAt.IsZero() {
		t.Fatal("the store did not stamp the copy it was handed")
	}
	if got := taskOf(t, a, "settling"); !got.FinishedAt.IsZero() {
		t.Fatal("the live task was stamped too, so this test is no longer about anything")
	}

	a.sweep()

	got := taskOf(t, a, "settling")
	if !got.FinishedAt.Equal(c.FinishedAt) {
		t.Errorf("the app holds %v, the row says %v", got.FinishedAt, c.FinishedAt)
	}

	// A re-queued task loses it, or retention would remove a running download.
	a.mu.Lock()
	a.tasks["settling"].Status = core.StatusQueued
	a.mu.Unlock()
	a.sweep()
	if got := taskOf(t, a, "settling"); !got.FinishedAt.IsZero() {
		t.Errorf("a re-queued task still claims to have finished at %v", got.FinishedAt)
	}
}

// A queue stopped at boot marks every waiting row with the halt as its reason.
// At boot the halt and the queue arrive together, unlike in a running app.
func TestAQueueStoppedByTheBootSaysSoOnEveryRow(t *testing.T) {
	f := newBootFixture(t, nil,
		core.Task{ID: "a", URL: "https://host.example/a.bin", Name: "a.bin", Status: core.StatusQueued, Enabled: true},
		core.Task{ID: "b", URL: "https://host.example/b.bin", Name: "b.bin", Status: core.StatusRunning, Enabled: true},
	)

	a := f.boot(t)
	a.mu.Lock()
	halted, queued := a.halted, len(a.queue)
	a.mu.Unlock()
	if !halted || queued != 2 {
		t.Fatalf("boot left halted=%v with %d queued; this test needs a stopped queue holding both rows", halted, queued)
	}

	// Polled: the schedule runner's first pass writes the reason on its own
	// goroutine.
	ok := pollUntil(t, 5*time.Second, func() bool {
		return taskOf(t, a, "a").Waiting == core.WaitingHalted &&
			taskOf(t, a, "b").Waiting == core.WaitingHalted
	})
	if !ok {
		for _, id := range []string{"a", "b"} {
			t.Errorf("task %s waits with reason %q behind a stopped queue, want %q", id, taskOf(t, a, id).Waiting, core.WaitingHalted)
		}
	}
}
