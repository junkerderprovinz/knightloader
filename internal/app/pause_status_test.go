package app

import (
	"slices"
	"testing"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/core"
)

// A stop the app commanded is recorded by the app: the engine does not report a
// status of its own for it.
func TestPauseWritesStatusForARunningTask(t *testing.T) {
	a := newCaptchaTestApp(t)
	task := putTask(t, a, core.Task{
		URL: "https://host.example/big.bin", Name: "big.bin",
		Status: core.StatusRunning, Enabled: true,
	})
	id := task.ID

	a.mu.Lock()
	a.active[id] = true
	a.mu.Unlock()

	a.Pause(id)

	deadline := time.Now().Add(2 * time.Second)
	for {
		a.mu.Lock()
		got := a.tasks[id].Status
		a.mu.Unlock()
		if got == core.StatusPaused {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("status after Pause = %q, want %q", got, core.StatusPaused)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// Pausing a row that finished before the click arrived leaves it finished, so
// a later resume cannot send it back through a fresh start over its own file.
func TestPauseLeavesAFinishedTaskDone(t *testing.T) {
	a := newCaptchaTestApp(t)
	task := putTask(t, a, core.Task{
		URL: "https://host.example/done.bin", Name: "done.bin",
		Status: core.StatusDone, Enabled: true,
	})

	a.Pause(task.ID)
	a.Resume(task.ID)

	a.mu.Lock()
	defer a.mu.Unlock()
	if got := a.tasks[task.ID].Status; got != core.StatusDone {
		t.Errorf("status after pause and resume = %q, want %q", got, core.StatusDone)
	}
	if slices.Contains(a.queue, task.ID) || a.active[task.ID] {
		t.Error("a finished task went back into the queue")
	}
}

// Resume only takes paused tasks; a failed one goes through restart, which
// clears its error and attempts first.
func TestResumeIgnoresAFailedTask(t *testing.T) {
	a := newCaptchaTestApp(t)
	task := putTask(t, a, core.Task{
		URL: "https://host.example/bad.bin", Name: "bad.bin",
		Status: core.StatusError, Error: "gone", Enabled: true,
	})

	a.Resume(task.ID)

	a.mu.Lock()
	defer a.mu.Unlock()
	if got := a.tasks[task.ID].Status; got != core.StatusError {
		t.Errorf("status after resume = %q, want %q", got, core.StatusError)
	}
}
