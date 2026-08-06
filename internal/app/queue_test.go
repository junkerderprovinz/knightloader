package app

import (
	"testing"

	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/settings"
)

// TestHaltedQueueStartsNothing is the master switch doing its one job. Without
// it, "stop everything" means clicking pause on every row and hoping nothing
// new starts while you work down the list.
func TestHaltedQueueStartsNothing(t *testing.T) {
	a := newQueueApp(t)

	created := a.AddLinks([]string{
		"https://host.example/one.bin",
		"https://host.example/two.bin",
	}, "Batch")
	if len(created) != 2 {
		t.Fatalf("staged %d", len(created))
	}

	a.SetHalted(true)
	if q := a.Queue(); !q.Halted {
		t.Fatal("Queue() does not report the halt")
	}

	a.StartTasks(nil)
	a.mu.Lock()
	dispatched := len(a.active)
	queued := len(a.queue)
	a.mu.Unlock()
	if dispatched != 0 {
		t.Errorf("%d tasks were dispatched while halted", dispatched)
	}
	if queued != 2 {
		t.Errorf("%d tasks waiting, want both still queued rather than dropped", queued)
	}

	// Resuming has to let them go, or the switch is a one-way trip.
	a.SetHalted(false)
	a.mu.Lock()
	queued = len(a.queue)
	a.mu.Unlock()
	if queued == 2 {
		t.Error("resuming the queue dispatched nothing")
	}
}

// TestHaltDoesNotAbandonRunningWork pins the distinction that makes the switch
// safe to press: halting stops what has not started, and leaves what has.
func TestHaltedLeavesRunningTasksAlone(t *testing.T) {
	a := newQueueApp(t)

	running := &core.Task{ID: "r1", URL: "https://host.example/big.bin", Status: core.StatusRunning}
	a.mu.Lock()
	a.tasks[running.ID] = running
	a.active[running.ID] = true
	a.mu.Unlock()

	a.SetHalted(true)

	a.mu.Lock()
	stillActive := a.active[running.ID]
	status := running.Status
	a.mu.Unlock()
	if !stillActive || status != core.StatusRunning {
		t.Error("halting the queue interrupted a download that was already running")
	}
	if q := a.Queue(); q.Running != 1 {
		t.Errorf("Running = %d; the count is what makes a halt legible", q.Running)
	}
}

// TestStopMarkHaltsAfterThatTask is the "finish this, then stop" control. If it
// failed, the only way to stop after a specific download is to sit and watch
// for it.
func TestStopMarkHaltsAfterThatTask(t *testing.T) {
	a := newQueueApp(t)

	marked := &core.Task{ID: "m1", URL: "https://host.example/last.bin", Status: core.StatusRunning}
	other := &core.Task{ID: "o1", URL: "https://host.example/other.bin", Status: core.StatusRunning}
	a.mu.Lock()
	a.tasks[marked.ID] = marked
	a.tasks[other.ID] = other
	a.active[marked.ID] = true
	a.active[other.ID] = true
	a.mu.Unlock()

	a.SetStopMark(marked.ID)
	if q := a.Queue(); q.StopMark != marked.ID {
		t.Fatalf("StopMark = %q", q.StopMark)
	}

	// Another task finishing must not trigger it.
	a.onUpdate(other.ID, core.Update{Status: core.StatusDone, Loaded: 10})
	if a.Queue().Halted {
		t.Fatal("an unrelated download triggered the stop mark")
	}

	a.onUpdate(marked.ID, core.Update{Status: core.StatusDone, Loaded: 10})
	q := a.Queue()
	if !q.Halted {
		t.Error("the marked download finished and the queue kept going")
	}
	if q.StopMark != "" {
		t.Error("the stop mark stayed armed after firing")
	}
}

// TestResumingClearsTheStopMark stops a mark from lying in wait: re-armed
// silently, it would halt the queue again for a click made minutes earlier.
func TestResumingClearsTheStopMark(t *testing.T) {
	a := newQueueApp(t)
	task := &core.Task{ID: "m1", URL: "https://host.example/x.bin", Status: core.StatusRunning}
	a.mu.Lock()
	a.tasks[task.ID] = task
	a.mu.Unlock()

	a.SetStopMark(task.ID)
	a.SetHalted(true)
	a.SetHalted(false)
	if m := a.Queue().StopMark; m != "" {
		t.Errorf("StopMark = %q after resuming, want it cleared", m)
	}
}

func newQueueApp(t *testing.T) *App {
	t.Helper()
	a, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { a.Close() })
	if _, err := a.ApplySettings(settings.Settings{
		MaxConcurrent: 4, MaxPerHost: 4, DownloadDir: t.TempDir(),
	}); err != nil {
		t.Fatal(err)
	}
	return a
}
