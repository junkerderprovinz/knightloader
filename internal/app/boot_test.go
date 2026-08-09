package app

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/settings"
)

// bootFixture is a data directory with tasks already in the store and settings
// already saved: the state a process that died leaves behind. Every test here
// then boots a real App against it, because a simulated boot proves nothing
// about the one path that matters.
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

// boot opens the directory again, which is the whole point of every test in
// this file.
func (f bootFixture) boot(t *testing.T) *App {
	t.Helper()
	a, err := New(f.dataDir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { a.Close() })
	return a
}

// task reads one task out of a booted app.
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

// TestABootedTaskSaysSomethingTrue is the whole of row one. A row the database
// calls "running" belongs to a process that no longer exists: there is no
// transfer behind it, nothing will ever report on it, and a list that shows it
// as running offers a pause button that does nothing to a download that is not
// happening.
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
	if got.Status != core.StatusPaused {
		t.Errorf("status = %q, want paused: nothing is running after a restart", got.Status)
	}
	if got.Speed != 0 {
		t.Errorf("speed = %d, want 0: a stopped transfer cannot have a rate", got.Speed)
	}
	if got.Loaded != 2048 {
		t.Errorf("loaded = %d, want the 2048 bytes that are still on disk", got.Loaded)
	}
}

// TestProgressWithoutBytesIsNotClaimed is the other half of it. Keeping the byte
// count is only honest while the bytes are there; a bar at 50 % of a file
// somebody deleted under the app is a claim the user only disproves by pressing
// resume and watching it start from nothing.
func TestProgressWithoutBytesIsNotClaimed(t *testing.T) {
	dl := t.TempDir()
	f := newBootFixture(t, nil, core.Task{
		ID: "no-file", URL: "https://host.example/gone.bin", Name: "gone.bin",
		Dir: dl, Status: core.StatusRunning, Size: 4096, Loaded: 2048, Enabled: true,
	})

	a := f.boot(t)
	got := taskOf(t, a, "no-file")
	if got.Status != core.StatusPaused {
		t.Errorf("status = %q, want paused", got.Status)
	}
	if got.Loaded != 0 {
		t.Errorf("loaded = %d, want 0: the partial file is not there", got.Loaded)
	}
}

// TestTheDefaultStartsNothing pins the cautious default, and the reason for it:
// no backend's handle on a running download survives this process, so a resume
// is a fresh fetch of a file that was half there. On a box that reboots at four
// in the morning that has to be something somebody asked for.
func TestTheDefaultStartsNothing(t *testing.T) {
	f := newBootFixture(t, nil, core.Task{
		ID: "r", URL: "https://host.example/a.bin", Name: "a.bin",
		Status: core.StatusRunning, Enabled: true,
	})

	a := f.boot(t)
	if got := taskOf(t, a, "r"); got.Status != core.StatusPaused {
		t.Errorf("status = %q, want paused under the default resume policy", got.Status)
	}
	a.mu.Lock()
	queued := len(a.queue)
	a.mu.Unlock()
	if queued != 0 {
		t.Errorf("%d tasks were put back in the queue by a policy that says never", queued)
	}
}

// TestResumeRunningPutsTheQueueBack is the option most people mean by "carry on
// where you left off".
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
	// A task somebody paused by hand is the one thing a restart must not undo.
	if got := taskOf(t, a, "was-paused"); got.Status != core.StatusPaused {
		t.Errorf("was-paused = %q, want it left alone", got.Status)
	}
}

// TestResumeRunningStaysPutWhenNothingWas is the "only if" in the option's name.
// An instance that was sitting idle - everything paused, or the queue halted -
// has nothing to carry on with, and starting its waiting links on boot would be
// the ALWAYS policy wearing the other one's label.
func TestResumeRunningStaysPutWhenNothingWas(t *testing.T) {
	f := newBootFixture(t,
		func(s *settings.Settings) { s.ResumeOnStart = settings.ResumeRunning },
		core.Task{ID: "waiting", URL: "https://host.example/a.bin", Name: "a.bin",
			Status: core.StatusQueued, Enabled: true},
	)

	a := f.boot(t)
	if got := taskOf(t, a, "waiting"); got.Status != core.StatusPaused {
		t.Errorf("status = %q, want paused: nothing was running when the process stopped", got.Status)
	}
}

// TestResumeAllTakesTheWaitingOnesToo covers the third option, on the same list
// that the "only if running" test leaves alone.
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

// TestAnInterruptedExtractionIsAFinishedDownload keeps the pre-existing boot
// rule and adds the half that was missing: the state has to reach the STORE. The
// download itself finished, so it belongs in the record and in the reach of
// retention, and a task left at "extracting" in the database is invisible to
// both for as long as the instance lives.
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

// TestRetentionTrimsTheListAndNothingElse is the row this whole feature has to
// get right. Removing a row and deleting what was downloaded are two different
// actions - conflating them destroyed finished downloads on the ordinary "clear
// finished" path once already - and this is that same path running unattended on
// a timer.
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

// TestRetentionCanBeSwitchedOff keeps zero meaning "keep for ever". It is the
// one value where a misreading empties somebody's whole list.
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

// TestTheAppCatchesUpWithItsOwnFinishTimes covers the gap the store's stamp
// leaves behind. The stamp lands on the copy that is saved and broadcast, never
// on the task the list is built from, so a snapshot read through the API would
// show an empty column for a download that finished a moment ago.
func TestTheAppCatchesUpWithItsOwnFinishTimes(t *testing.T) {
	f := newBootFixture(t, nil)
	a := f.boot(t)

	live := putTask(t, a, core.Task{
		ID: "settling", URL: "https://host.example/f.bin", Name: "f.bin",
		Status: core.StatusDone, Enabled: true,
	})
	// Exactly what a settle does: hand the store a copy and keep the live task.
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

	// And the reverse: a task put back in the queue must stop claiming one, or
	// retention would eventually sweep out a download that is running.
	a.mu.Lock()
	a.tasks["settling"].Status = core.StatusQueued
	a.mu.Unlock()
	a.sweep()
	if got := taskOf(t, a, "settling"); !got.FinishedAt.IsZero() {
		t.Errorf("a re-queued task still claims to have finished at %v", got.FinishedAt)
	}
}
