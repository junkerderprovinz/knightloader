package app

import (
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
