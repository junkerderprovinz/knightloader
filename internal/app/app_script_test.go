package app

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/idleaction"
	"github.com/junkerderprovinz/knightloader/internal/script"
)

// A task settling as done in onUpdate reaches a task.done script through the
// bus, and the script's setComment reaches the task through scriptActions. The
// script runs on the host's worker pool, hence the polling.
func TestScriptFiresOnTaskDone(t *testing.T) {
	a, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()

	if _, err := a.Scripts.SaveScript(script.Script{
		Name:    "mark done",
		Trigger: script.TriggerTaskDone,
		Enabled: true,
		Code:    `task.setComment("done-by-script")`,
	}); err != nil {
		t.Fatal(err)
	}

	task := &core.Task{ID: "1", URL: "https://host.example/f.bin", Resolver: "direct", Status: core.StatusRunning}
	a.mu.Lock()
	a.tasks[task.ID] = task
	a.active[task.ID] = true
	a.started[task.ID] = true
	a.mu.Unlock()

	a.onUpdate(task.ID, core.Update{Status: core.StatusDone, Size: 10, Loaded: 10})

	waitFor(t, "the task.done script set the comment", func() bool {
		a.mu.Lock()
		defer a.mu.Unlock()
		return task.Comment == "done-by-script"
	})
}

// A failure with an automatic retry pending must not run a task.failed script.
// This goes through onUpdate, since ClassifyTaskUpdate relies on NextTry being
// set before the broadcast.
func TestScriptDoesNotFireOnTaskFailedWithRetryPending(t *testing.T) {
	t.Parallel()
	a, err := newApp(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()

	if _, err := a.Scripts.SaveScript(script.Script{
		Name:    "mark failed",
		Trigger: script.TriggerTaskFailed,
		Enabled: true,
		Code:    `task.setComment("failed-by-script")`,
	}); err != nil {
		t.Fatal(err)
	}

	task := &core.Task{ID: "1", URL: "https://host.example/f.bin", Resolver: "direct", Status: core.StatusRunning}
	a.mu.Lock()
	a.tasks[task.ID] = task
	a.active[task.ID] = true
	a.started[task.ID] = true
	a.mu.Unlock()

	// A transient failure: MaxRetries defaults above zero, so this settles
	// with NextTry armed.
	a.onUpdate(task.ID, core.Update{Status: core.StatusError, Err: "connection reset"})

	// An absence cannot be polled for, so wait well past the time the worker
	// pool needs to pick a job up.
	time.Sleep(300 * time.Millisecond)
	a.mu.Lock()
	comment := task.Comment
	nextTry := task.NextTry
	a.mu.Unlock()
	if nextTry.IsZero() {
		t.Fatal("test setup: task settled with NextTry cleared, so this is not exercising the retry-pending case at all")
	}
	if comment == "failed-by-script" {
		t.Error("a task.failed script ran while a retry was still pending")
	}
}

// The on-demand path end to end: ScriptTask builds the view as the run route
// does, and the script's setPriority lands on the real task. RunNow is
// synchronous.
func TestRunNowThroughApp(t *testing.T) {
	a, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()

	task := &core.Task{ID: "1", URL: "https://host.example/f.bin", Resolver: "direct", Status: core.StatusQueued, Priority: 0}
	a.mu.Lock()
	a.tasks[task.ID] = task
	a.mu.Unlock()

	saved, err := a.Scripts.SaveScript(script.Script{
		Name:    "bump priority",
		Trigger: script.TriggerOnDemand,
		Enabled: false,
		Code:    `task.setPriority(2)`,
	})
	if err != nil {
		t.Fatal(err)
	}

	tv, ok := a.ScriptTask(task.ID)
	if !ok {
		t.Fatal("ScriptTask did not find the task just inserted")
	}
	res, err := a.Scripts.RunNow(context.Background(), saved.ID, &tv, a.ScriptQueue())
	if err != nil {
		t.Fatal(err)
	}
	if !res.OK {
		t.Fatalf("run result = %+v, want ok", res)
	}

	a.mu.Lock()
	priority := task.Priority
	a.mu.Unlock()
	if priority != 2 {
		t.Errorf("task priority = %d, want 2 (RunNow's script.setPriority did not reach the real task)", priority)
	}
}

// RestartTasks reads an empty slice as every errored task, so Retry must not
// forward an empty id.
func TestScriptActionsRetryRefusesEmptyTaskID(t *testing.T) {
	a, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()

	errored := &core.Task{ID: "1", Status: core.StatusError}
	a.mu.Lock()
	a.tasks[errored.ID] = errored
	a.mu.Unlock()

	if err := (scriptActions{a}).Retry(""); err == nil {
		t.Error("Retry(\"\") = nil error, want a refusal")
	}
	a.mu.Lock()
	status := errored.Status
	a.mu.Unlock()
	if status != core.StatusError {
		t.Errorf("an empty-taskID Retry touched an unrelated errored task: status = %q", status)
	}
}

// queue.idle fires for a queue idle from the start, with no idle action
// configured. The broadcast is observed through a fake Hub connection.
//
// It polls with its own deadline because scriptIdlePoll is 2s, which leaves too
// little of waitFor's 3s on a loaded machine.
func TestWatchQueueIdleForScriptsFiresOnce(t *testing.T) {
	t.Parallel()
	a, err := newApp(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()

	// With ActionNone the controller never fires, so any firing comes from
	// watchQueueIdleForScripts.
	if a.Settings.Get().IdleAction.Action != idleaction.ActionNone {
		t.Fatal("test setup: expected no idle action configured by default")
	}

	fc := &activityFakeConn{}
	a.Hub.Add(fc)

	if _, err := a.Scripts.SaveScript(script.Script{
		Name:    "notify idle",
		Trigger: script.TriggerQueueIdle,
		Enabled: true,
		Code:    `notify("idle")`,
	}); err != nil {
		t.Fatal(err)
	}

	deadline := time.Now().Add(7 * time.Second)
	for {
		for _, raw := range fc.snapshot() {
			var env struct {
				Type string       `json:"type"`
				Data script.Event `json:"data"`
			}
			if json.Unmarshal(raw, &env) == nil && env.Type == "script" && env.Data.Kind == "notify" {
				return
			}
		}
		if time.Now().After(deadline) {
			t.Fatal("no script notify() broadcast arrived; watchQueueIdleForScripts did not fire")
		}
		time.Sleep(20 * time.Millisecond)
	}
}
