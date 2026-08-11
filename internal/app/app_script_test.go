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

// TestScriptFiresOnTaskDone proves the actual wiring point in
// app_dispatch.go's onUpdate: a task settling as done reaches a real,
// enabled task.done script through Scripts.Fire, and that script's
// task.setComment(...) call reaches the real task through scriptActions -
// not just that internal/script can run a script in isolation, which its
// own package tests already cover. Polled through the package's own
// waitFor (rules_wiring_test.go): Fire's own worker pool runs the script on
// a different goroutine, so there is nothing to read synchronously right
// after onUpdate returns.
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

// TestScriptDoesNotFireOnTaskFailedWithRetryPending is the app-level half of
// script.ClassifyTaskUpdate's own doc comment: a failure that still has an
// automatic retry pending (NextTry set) must not run a task.failed script at
// all, only the settled failure does - proven here against the real
// onUpdate path rather than the pure function alone, so a future change to
// onUpdate's own ordering (NextTry set before the broadcast this wiring
// hangs off) cannot silently break the contract ClassifyTaskUpdate assumes.
func TestScriptDoesNotFireOnTaskFailedWithRetryPending(t *testing.T) {
	a, err := New(t.TempDir())
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

	// A transient failure: MaxRetries defaults > 0, so this settles with
	// NextTry armed rather than cleared, which is exactly the state
	// ClassifyTaskUpdate must refuse to fire on.
	a.onUpdate(task.ID, core.Update{Status: core.StatusError, Err: "connection reset"})

	// There is nothing to poll FOR here (the absence of a firing), so this
	// waits out a window generous next to Fire's own worker pool picking a
	// job up, then asserts the comment never arrived - a real, if
	// necessarily time-bounded, negative check.
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

// TestRunNowThroughApp is the manual/on-demand path end to end: build the
// TaskView through ScriptTask (the same call routes_scripts.go's run route
// makes), run through Scripts.RunNow, and confirm the script's
// task.setPriority call landed on the real task via scriptActions. RunNow
// runs synchronously, so there is nothing to poll for here.
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

// TestScriptActionsRetryRefusesEmptyTaskID is scriptActions' own defensive
// line, independent of internal/script's own bindings never constructing an
// empty-taskID closure in the first place - see Retry's own doc comment for
// why RestartTasks(nil) treating an empty slice as "every errored task"
// makes this the one Actions method that cannot simply forward its
// argument.
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

// TestWatchQueueIdleForScriptsFiresOnce confirms the independent poller
// fires TriggerQueueIdle for a queue that is idle from the start - unlike
// idleaction.Controller's own everBusy gate, this trigger has no reason to
// withhold that first firing (see watchQueueIdleForScripts' own doc
// comment) - and that a script bound to it works with NO idle-pause action
// configured at all, the exact coupling this wiring has to avoid. Observed
// through a fake Hub connection (activityFakeConn, app_activity_test.go's
// own type - same package, so it is reused rather than redeclared) the same
// way that file's own tests observe a broadcast, since Fire's own worker
// pool runs on its own goroutine with nothing else this test could block on.
//
// Polls with its own deadline rather than the package's shared waitFor:
// scriptIdlePoll is 2s and this needs to survive at least one full tick plus
// worker-pool and writer-goroutine scheduling on top, more room than
// waitFor's fixed 3s budget reliably leaves on a loaded machine.
func TestWatchQueueIdleForScriptsFiresOnce(t *testing.T) {
	a, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()

	// Confirmed unconfigured: the default idle-action is ActionNone, so
	// idleaction.Controller's own Fire callback never runs, and this
	// firing can only be coming from watchQueueIdleForScripts.
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
