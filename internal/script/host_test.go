package script

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeActions records every call it receives, guarded by a mutex since
// Host runs scripts on its own worker goroutines while a test asserts from
// the main one.
type fakeActions struct {
	mu         sync.Mutex
	paused     []string
	resumed    []string
	retried    []string
	priorities map[string]int
	comments   map[string]string
	retryErr   error
	retryPanic any // if non-nil, Retry panics with this instead of returning
}

func newFakeActions() *fakeActions {
	return &fakeActions{priorities: map[string]int{}, comments: map[string]string{}}
}

func (f *fakeActions) Pause(id string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.paused = append(f.paused, id)
}
func (f *fakeActions) Resume(id string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.resumed = append(f.resumed, id)
}
func (f *fakeActions) Retry(id string) error {
	f.mu.Lock()
	p := f.retryPanic
	err := f.retryErr
	f.mu.Unlock()
	if p != nil {
		panic(p)
	}
	if err != nil {
		return err
	}
	f.mu.Lock()
	f.retried = append(f.retried, id)
	f.mu.Unlock()
	return nil
}
func (f *fakeActions) SetPriority(id string, p int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.priorities[id] = p
}
func (f *fakeActions) SetComment(id, text string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.comments[id] = text
}

func (f *fakeActions) pausedIDs() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.paused...)
}

// fakeHub records every broadcast "script" event and offers a channel a
// test can select on, so tests wait for the actual event rather than
// sleeping and hoping a worker got there first.
type fakeHub struct {
	mu     sync.Mutex
	events []Event
	ch     chan Event
}

func newFakeHub() *fakeHub {
	return &fakeHub{ch: make(chan Event, 64)}
}

func (f *fakeHub) Broadcast(typ string, data any) {
	if typ != "script" {
		return
	}
	ev, ok := data.(Event)
	if !ok {
		return
	}
	f.mu.Lock()
	f.events = append(f.events, ev)
	f.mu.Unlock()
	select {
	case f.ch <- ev:
	default:
	}
}

// waitEvent blocks for the next event matching pred, or fails the test
// after a generous but bounded wait - this suite never relies on a script
// actually taking close to MaxTimeout, so a few seconds is already a huge
// margin over the slowest legitimate case.
func waitEvent(t *testing.T, hub *fakeHub, pred func(Event) bool) Event {
	t.Helper()
	deadline := time.After(5 * time.Second)
	for {
		select {
		case ev := <-hub.ch:
			if pred(ev) {
				return ev
			}
		case <-deadline:
			t.Fatalf("timed out waiting for a matching script event")
			return Event{}
		}
	}
}

func newTestHost(t *testing.T, actions Actions, hub Broadcaster) *Host {
	t.Helper()
	h, err := NewHost(Options{DataDir: t.TempDir(), Actions: actions, Hub: hub})
	if err != nil {
		t.Fatalf("NewHost: %v", err)
	}
	t.Cleanup(func() { _ = h.Close() })
	return h
}

func TestNewHost_RequiresActionsAndHub(t *testing.T) {
	dir := t.TempDir()
	if _, err := NewHost(Options{DataDir: dir, Hub: newFakeHub()}); err == nil {
		t.Fatal("NewHost without Actions should error")
	}
	if _, err := NewHost(Options{DataDir: dir, Actions: newFakeActions()}); err == nil {
		t.Fatal("NewHost without Hub should error")
	}
}

func TestHost_FireRunsMatchingEnabledScript(t *testing.T) {
	actions, hub := newFakeActions(), newFakeHub()
	h := newTestHost(t, actions, hub)

	s, err := h.SaveScript(Script{Name: "on-done", Trigger: TriggerTaskDone, Code: "notify('done ran');", Enabled: true})
	if err != nil {
		t.Fatalf("SaveScript: %v", err)
	}

	h.Fire(TriggerTaskDone, &TaskView{ID: "t1"}, QueueView{})

	ev := waitEvent(t, hub, func(e Event) bool { return e.Kind == "notify" })
	if ev.Message != "done ran" {
		t.Fatalf("notify message = %q, want %q", ev.Message, "done ran")
	}
	res := waitEvent(t, hub, func(e Event) bool { return e.Kind == "result" })
	if !res.OK || res.ScriptID != s.ID || res.TaskID != "t1" {
		t.Fatalf("result event = %+v, want OK result for %q/t1", res, s.ID)
	}
}

func TestHost_FireIgnoresDisabledScript(t *testing.T) {
	actions, hub := newFakeActions(), newFakeHub()
	h := newTestHost(t, actions, hub)
	if _, err := h.SaveScript(Script{Name: "off", Trigger: TriggerTaskDone, Code: "notify('should not run');", Enabled: false}); err != nil {
		t.Fatalf("SaveScript: %v", err)
	}

	h.Fire(TriggerTaskDone, &TaskView{ID: "t1"}, QueueView{})

	select {
	case ev := <-hub.ch:
		t.Fatalf("a disabled script fired: %+v", ev)
	case <-time.After(200 * time.Millisecond):
	}
}

func TestHost_FireIgnoresNonMatchingTrigger(t *testing.T) {
	actions, hub := newFakeActions(), newFakeHub()
	h := newTestHost(t, actions, hub)
	if _, err := h.SaveScript(Script{Name: "on-fail", Trigger: TriggerTaskFailed, Code: "notify('wrong trigger');", Enabled: true}); err != nil {
		t.Fatalf("SaveScript: %v", err)
	}

	h.Fire(TriggerTaskDone, &TaskView{ID: "t1"}, QueueView{})

	select {
	case ev := <-hub.ch:
		t.Fatalf("a script bound to a different trigger fired: %+v", ev)
	case <-time.After(200 * time.Millisecond):
	}
}

func TestHost_QueueIdleFiresWithNoTask(t *testing.T) {
	actions, hub := newFakeActions(), newFakeHub()
	h := newTestHost(t, actions, hub)
	if _, err := h.SaveScript(Script{
		Name: "on-idle", Trigger: TriggerQueueIdle, Enabled: true,
		Code: "notify('idle:' + queue.idle + ':' + (typeof task));",
	}); err != nil {
		t.Fatalf("SaveScript: %v", err)
	}

	h.Fire(TriggerQueueIdle, nil, QueueView{Files: 2, Disabled: 2})

	ev := waitEvent(t, hub, func(e Event) bool { return e.Kind == "notify" })
	if ev.Message != "idle:true:undefined" {
		t.Fatalf("notify message = %q, want %q (task must be absent when Fire is given no TaskView)", ev.Message, "idle:true:undefined")
	}
}

func TestHost_TaskActionsScopedToFiringTask(t *testing.T) {
	actions, hub := newFakeActions(), newFakeHub()
	h := newTestHost(t, actions, hub)
	if _, err := h.SaveScript(Script{
		Name: "act", Trigger: TriggerTaskDone, Enabled: true,
		Code: `task.pause(); task.resume(); task.retry(); task.setPriority(2); task.setComment("noted"); notify('acted');`,
	}); err != nil {
		t.Fatalf("SaveScript: %v", err)
	}

	h.Fire(TriggerTaskDone, &TaskView{ID: "task-42"}, QueueView{})
	waitEvent(t, hub, func(e Event) bool { return e.Kind == "notify" })
	res := waitEvent(t, hub, func(e Event) bool { return e.Kind == "result" })
	if !res.OK {
		t.Fatalf("result = %+v, want OK", res)
	}

	if got := actions.pausedIDs(); len(got) != 1 || got[0] != "task-42" {
		t.Fatalf("Pause calls = %v, want exactly [task-42]", got)
	}
	actions.mu.Lock()
	defer actions.mu.Unlock()
	if len(actions.resumed) != 1 || actions.resumed[0] != "task-42" {
		t.Fatalf("Resume calls = %v, want exactly [task-42]", actions.resumed)
	}
	if len(actions.retried) != 1 || actions.retried[0] != "task-42" {
		t.Fatalf("Retry calls = %v, want exactly [task-42]", actions.retried)
	}
	if actions.priorities["task-42"] != 2 {
		t.Fatalf("SetPriority(task-42) = %d, want 2", actions.priorities["task-42"])
	}
	if actions.comments["task-42"] != "noted" {
		t.Fatalf("SetComment(task-42) = %q, want %q", actions.comments["task-42"], "noted")
	}
}

// TestHost_ScriptCannotNameAnotherTask is the concrete check behind the
// package doc comment's central safety claim: there is no function in the
// JS surface that takes a task ID as an argument, so a script cannot even
// try to act on a task other than the one it was fired for. If a future
// change ever added such a function, this test would start failing the
// moment a script actually called it with a foreign ID.
func TestHost_ScriptCannotNameAnotherTask(t *testing.T) {
	actions, hub := newFakeActions(), newFakeHub()
	h := newTestHost(t, actions, hub)
	if _, err := h.SaveScript(Script{
		Name: "probe", Trigger: TriggerTaskDone, Enabled: true,
		Code: `
			var reached = false;
			if (typeof pause === "function") { pause("someone-elses-task"); reached = true; }
			if (typeof globalThis !== "undefined" && typeof globalThis.pauseTask === "function") { reached = true; }
			notify(reached ? "escaped" : "contained");
		`,
	}); err != nil {
		t.Fatalf("SaveScript: %v", err)
	}

	h.Fire(TriggerTaskDone, &TaskView{ID: "own-task"}, QueueView{})
	ev := waitEvent(t, hub, func(e Event) bool { return e.Kind == "notify" })
	if ev.Message != "contained" {
		t.Fatalf("notify message = %q, want %q - a global ID-taking action function is reachable", ev.Message, "contained")
	}
	if got := actions.pausedIDs(); len(got) != 0 {
		t.Fatalf("Pause was called with %v, want no calls at all", got)
	}
}

func TestHost_NoAmbientCapabilities(t *testing.T) {
	actions, hub := newFakeActions(), newFakeHub()
	h := newTestHost(t, actions, hub)
	if _, err := h.SaveScript(Script{
		Name: "probe-globals", Trigger: TriggerOnDemand, Enabled: true,
		Code: `
			var names = ["require", "process", "fetch", "XMLHttpRequest", "Buffer", "setTimeout", "setInterval", "importScripts", "readFile", "writeFile", "callSync", "callAsync", "openURL"];
			var found = [];
			for (var i = 0; i < names.length; i++) {
				if (typeof this[names[i]] !== "undefined") { found.push(names[i]); }
			}
			notify(found.length === 0 ? "clean" : "leaked:" + found.join(","));
		`,
	}); err != nil {
		t.Fatalf("SaveScript: %v", err)
	}
	res, err := h.RunNow(context.Background(), mustID(t, h, "probe-globals"), nil, QueueView{})
	if err != nil {
		t.Fatalf("RunNow: %v", err)
	}
	if !res.OK {
		t.Fatalf("result = %+v, want OK", res)
	}
	ev := waitEvent(t, hub, func(e Event) bool { return e.Kind == "notify" })
	if ev.Message != "clean" {
		t.Fatalf("notify message = %q, want %q", ev.Message, "clean")
	}
}

func TestHost_TimeoutInterruptsRunawayScript(t *testing.T) {
	actions, hub := newFakeActions(), newFakeHub()
	h := newTestHost(t, actions, hub)
	if _, err := h.SaveScript(Script{
		Name: "loop", Trigger: TriggerTaskDone, Enabled: true, TimeoutMS: int(MinTimeout.Milliseconds()),
		Code: "while (true) {}",
	}); err != nil {
		t.Fatalf("SaveScript: %v", err)
	}

	start := time.Now()
	h.Fire(TriggerTaskDone, &TaskView{ID: "t1"}, QueueView{})
	res := waitEvent(t, hub, func(e Event) bool { return e.Kind == "result" })
	elapsed := time.Since(start)

	if res.OK {
		t.Fatalf("a runaway script reported OK: %+v", res)
	}
	if elapsed > 3*time.Second {
		t.Fatalf("an infinite loop with a %v timeout took %v to be stopped", MinTimeout, elapsed)
	}
}

func TestHost_ThrownExceptionReportsErrorNotTimeout(t *testing.T) {
	actions, hub := newFakeActions(), newFakeHub()
	h := newTestHost(t, actions, hub)
	if _, err := h.SaveScript(Script{
		Name: "thrower", Trigger: TriggerTaskDone, Enabled: true,
		Code: `throw new Error("deliberate");`,
	}); err != nil {
		t.Fatalf("SaveScript: %v", err)
	}

	h.Fire(TriggerTaskDone, &TaskView{ID: "t1"}, QueueView{})
	res := waitEvent(t, hub, func(e Event) bool { return e.Kind == "result" })
	if res.OK {
		t.Fatal("a thrown exception should not report OK")
	}
	if !strings.Contains(res.Error, "deliberate") {
		t.Fatalf("error = %q, want it to mention the thrown message", res.Error)
	}
}

// TestHost_PanicInActionsIsRecovered is the concrete check behind the
// package doc comment's "bounded twice" section: a raw Go panic (not
// panic(goja.Value)) from inside an Actions implementation - a real bug, or
// a not-yet-Value-wrapped error - must be caught by execute's own
// recover() and turned into a failed Result, never allowed to escape the
// worker goroutine and take the whole test binary (and in production, the
// whole app) down with it.
func TestHost_PanicInActionsIsRecovered(t *testing.T) {
	actions := newFakeActions()
	actions.retryPanic = "boom: not a goja.Value"
	hub := newFakeHub()
	h := newTestHost(t, actions, hub)
	if _, err := h.SaveScript(Script{
		Name: "panics", Trigger: TriggerTaskDone, Enabled: true,
		Code: `task.retry();`,
	}); err != nil {
		t.Fatalf("SaveScript: %v", err)
	}

	h.Fire(TriggerTaskDone, &TaskView{ID: "t1"}, QueueView{})
	res := waitEvent(t, hub, func(e Event) bool { return e.Kind == "result" })
	if res.OK {
		t.Fatal("a panicking action should not report OK")
	}
	if !strings.Contains(res.Error, "internal error") {
		t.Fatalf("error = %q, want it to be reported as an internal error", res.Error)
	}
}

func TestHost_RunNowIgnoresEnabledFlag(t *testing.T) {
	actions, hub := newFakeActions(), newFakeHub()
	h := newTestHost(t, actions, hub)
	s, err := h.SaveScript(Script{Name: "draft", Trigger: TriggerTaskDone, Enabled: false, Code: "notify('drafted');"})
	if err != nil {
		t.Fatalf("SaveScript: %v", err)
	}

	res, err := h.RunNow(context.Background(), s.ID, &TaskView{ID: "t9"}, QueueView{})
	if err != nil {
		t.Fatalf("RunNow: %v", err)
	}
	if !res.OK || res.Trigger != TriggerOnDemand {
		t.Fatalf("RunNow result = %+v, want an OK manual run", res)
	}
}

func TestHost_RunNowUnknownScriptErrors(t *testing.T) {
	h := newTestHost(t, newFakeActions(), newFakeHub())
	if _, err := h.RunNow(context.Background(), "no-such-id", nil, QueueView{}); err == nil {
		t.Fatal("RunNow of an unknown script id should error")
	}
}

func TestHost_RunNowRespectsCallerCancellation(t *testing.T) {
	h := newTestHost(t, newFakeActions(), newFakeHub())
	s, err := h.SaveScript(Script{
		Name: "long", Trigger: TriggerOnDemand, Enabled: true, TimeoutMS: int(MaxTimeout.Milliseconds()),
		Code: "while (true) {}",
	})
	if err != nil {
		t.Fatalf("SaveScript: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()

	start := time.Now()
	res, err := h.RunNow(ctx, s.ID, nil, QueueView{})
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("RunNow: %v", err)
	}
	if res.OK {
		t.Fatal("a script cancelled through the caller's context should not report OK")
	}
	if elapsed > 3*time.Second {
		t.Fatalf("RunNow took %v to honour a 150ms caller context, want well under its own %v timeout", elapsed, MaxTimeout)
	}
}

// TestHost_CloseInterruptsRunningScript proves Close's cancellation reaches
// an in-flight script directly, rather than Close only ever waiting out
// whatever timeout the script itself was given - here the script's own
// budget is the full MaxTimeout, and Close must still return in a small
// fraction of it.
func TestHost_CloseInterruptsRunningScript(t *testing.T) {
	actions, hub := newFakeActions(), newFakeHub()
	h, err := NewHost(Options{DataDir: t.TempDir(), Actions: actions, Hub: hub})
	if err != nil {
		t.Fatalf("NewHost: %v", err)
	}
	if _, err := h.SaveScript(Script{
		Name: "forever", Trigger: TriggerTaskDone, Enabled: true, TimeoutMS: int(MaxTimeout.Milliseconds()),
		Code: `notify('started'); while (true) {}`,
	}); err != nil {
		t.Fatalf("SaveScript: %v", err)
	}

	h.Fire(TriggerTaskDone, &TaskView{ID: "t1"}, QueueView{})
	waitEvent(t, hub, func(e Event) bool { return e.Kind == "notify" && e.Message == "started" })

	start := time.Now()
	if err := h.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if elapsed := time.Since(start); elapsed > 3*time.Second {
		t.Fatalf("Close took %v to interrupt a script whose own timeout was %v", elapsed, MaxTimeout)
	}
}

// TestHost_CloseWaitsForAndInterruptsARunNowScript is the RunNow half of
// TestHost_CloseInterruptsRunningScript above: a script started through
// RunNow (an HTTP handler's "test run" path, not the worker pool) used to
// run entirely untracked - Close() returned immediately while it kept
// going, free to call h.actions and h.hub.Broadcast against a host that had
// already torn down. Proves both halves of the fix: Close does not return
// before the RunNow call itself has returned, and it does so quickly
// rather than waiting out the script's own MaxTimeout budget.
func TestHost_CloseWaitsForAndInterruptsARunNowScript(t *testing.T) {
	actions, hub := newFakeActions(), newFakeHub()
	h, err := NewHost(Options{DataDir: t.TempDir(), Actions: actions, Hub: hub})
	if err != nil {
		t.Fatalf("NewHost: %v", err)
	}
	s, err := h.SaveScript(Script{
		Name: "forever", Trigger: TriggerOnDemand, Enabled: true, TimeoutMS: int(MaxTimeout.Milliseconds()),
		Code: `notify('started'); while (true) {}`,
	})
	if err != nil {
		t.Fatalf("SaveScript: %v", err)
	}

	runReturned := make(chan struct{})
	go func() {
		defer close(runReturned)
		// context.Background(), deliberately: nothing on the caller's own
		// side ever cancels this - only Close (via the merged host ctx)
		// may.
		_, _ = h.RunNow(context.Background(), s.ID, nil, QueueView{})
	}()
	waitEvent(t, hub, func(e Event) bool { return e.Kind == "notify" && e.Message == "started" })

	start := time.Now()
	if err := h.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	elapsed := time.Since(start)
	if elapsed > 3*time.Second {
		t.Fatalf("Close took %v to interrupt a RunNow script whose own timeout was %v", elapsed, MaxTimeout)
	}
	select {
	case <-runReturned:
	default:
		t.Fatal("Close returned before the RunNow call it should have waited for did")
	}
}

// TestHost_FireNeverBlocksCaller fires far more jobs than the queue can
// hold, into a trigger backed by a script that never returns on its own -
// the worst case for a caller that blocked. Fire must still return quickly
// every single time, the same guarantee Hub.Broadcast documents for
// itself.
func TestHost_FireNeverBlocksCaller(t *testing.T) {
	actions, hub := newFakeActions(), newFakeHub()
	h := newTestHost(t, actions, hub)
	if _, err := h.SaveScript(Script{
		Name: "sink", Trigger: TriggerTaskDone, Enabled: true, TimeoutMS: int(MinTimeout.Milliseconds()),
		Code: "while (true) {}",
	}); err != nil {
		t.Fatalf("SaveScript: %v", err)
	}

	const attempts = fireQueueDepth*2 + workerCount + 10
	done := make(chan struct{})
	go func() {
		for i := 0; i < attempts; i++ {
			h.Fire(TriggerTaskDone, &TaskView{ID: "t1"}, QueueView{})
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatalf("%d Fire calls did not return within 2s; Fire must never block on worker throughput", attempts)
	}
}

func TestHost_NotifyIsRateLimited(t *testing.T) {
	actions, hub := newFakeActions(), newFakeHub()
	h := newTestHost(t, actions, hub)
	if _, err := h.SaveScript(Script{
		Name: "spam", Trigger: TriggerOnDemand, Enabled: true,
		Code: `for (var i = 0; i < 50; i++) { notify("msg" + i); }`,
	}); err != nil {
		t.Fatalf("SaveScript: %v", err)
	}

	res, err := h.RunNow(context.Background(), mustID(t, h, "spam"), nil, QueueView{})
	if err != nil {
		t.Fatalf("RunNow: %v", err)
	}
	if !res.OK {
		t.Fatalf("result = %+v, want OK (notify must not make the script itself fail when rate-limited)", res)
	}

	hub.mu.Lock()
	notifyCount := 0
	for _, ev := range hub.events {
		if ev.Kind == "notify" {
			notifyCount++
		}
	}
	hub.mu.Unlock()
	if notifyCount >= 50 {
		t.Fatalf("got %d notify events out of 50 calls, want the shared rate limit to have dropped most of them", notifyCount)
	}
}

func TestHost_RebuildIndexSkipsScriptsThatDoNotCompile(t *testing.T) {
	actions, hub := newFakeActions(), newFakeHub()
	h := newTestHost(t, actions, hub)

	// A save() call can never leave a broken row behind (validate always
	// compiles first) - the only way one reaches rebuildIndex is a store
	// mutated outside this package's own API, which is exactly what this
	// reaches into st.byID directly to simulate.
	h.st.mu.Lock()
	h.st.byID["broken"] = Script{ID: "broken", Name: "broken", Trigger: TriggerTaskDone, Enabled: true, Code: "function( {"}
	h.st.mu.Unlock()
	h.rebuildIndex()

	if _, err := h.SaveScript(Script{Name: "good", Trigger: TriggerTaskDone, Enabled: true, Code: "notify('good');"}); err != nil {
		t.Fatalf("SaveScript: %v", err)
	}

	h.Fire(TriggerTaskDone, &TaskView{ID: "t1"}, QueueView{})
	ev := waitEvent(t, hub, func(e Event) bool { return e.Kind == "notify" })
	if ev.Message != "good" {
		t.Fatalf("notify message = %q, want %q - a broken sibling script must not take the trigger down", ev.Message, "good")
	}
}

func TestHost_LogCaptureViaConsoleAndLog(t *testing.T) {
	h := newTestHost(t, newFakeActions(), newFakeHub())
	s, err := h.SaveScript(Script{
		Name: "logger", Trigger: TriggerOnDemand, Enabled: true,
		Code: `log("a"); console.log("b", 2); console.warn("c"); console.error("d");`,
	})
	if err != nil {
		t.Fatalf("SaveScript: %v", err)
	}

	res, err := h.RunNow(context.Background(), s.ID, nil, QueueView{})
	if err != nil {
		t.Fatalf("RunNow: %v", err)
	}
	if !res.OK {
		t.Fatalf("result = %+v, want OK", res)
	}
	want := []string{"a", "b 2", "c", "d"}
	if len(res.Output) != len(want) {
		t.Fatalf("Output = %v, want %v", res.Output, want)
	}
	for i, line := range want {
		if res.Output[i] != line {
			t.Fatalf("Output[%d] = %q, want %q (full: %v)", i, res.Output[i], line, res.Output)
		}
	}
}

func TestHost_LogOutputIsCapped(t *testing.T) {
	h := newTestHost(t, newFakeActions(), newFakeHub())
	s, err := h.SaveScript(Script{
		Name: "spammy-logger", Trigger: TriggerOnDemand, Enabled: true,
		Code: fmt.Sprintf(`for (var i = 0; i < %d; i++) { log("line " + i); }`, maxLogLines+50),
	})
	if err != nil {
		t.Fatalf("SaveScript: %v", err)
	}

	res, err := h.RunNow(context.Background(), s.ID, nil, QueueView{})
	if err != nil {
		t.Fatalf("RunNow: %v", err)
	}
	if !res.OK {
		t.Fatalf("result = %+v, want OK - logging past the cap must not fail the script", res)
	}
	if len(res.Output) != maxLogLines {
		t.Fatalf("Output has %d lines, want exactly maxLogLines=%d", len(res.Output), maxLogLines)
	}
	if res.Output[0] != "line 0" {
		t.Fatalf("Output[0] = %q, want the earliest lines kept, not the latest", res.Output[0])
	}
}

// TestHost_MissingArgumentIsACatchableTypeError proves rt.NewTypeError
// produces an ordinary catchable JS exception - a script that guards its
// own call keeps running, and an uncaught one settles the run as a failure
// rather than a Go-level crash, exactly like any other thrown error (see
// TestHost_ThrownExceptionReportsErrorNotTimeout).
func TestHost_MissingArgumentIsACatchableTypeError(t *testing.T) {
	h := newTestHost(t, newFakeActions(), newFakeHub())

	caught, err := h.SaveScript(Script{
		Name: "guarded", Trigger: TriggerOnDemand, Enabled: true,
		Code: `try { notify(); } catch (e) { notify("caught:" + e.name); }`,
	})
	if err != nil {
		t.Fatalf("SaveScript: %v", err)
	}
	res, err := h.RunNow(context.Background(), caught.ID, nil, QueueView{})
	if err != nil {
		t.Fatalf("RunNow: %v", err)
	}
	if !res.OK {
		t.Fatalf("a caught TypeError should not fail the run: %+v", res)
	}

	uncaught, err := h.SaveScript(Script{
		Name: "unguarded", Trigger: TriggerOnDemand, Enabled: true,
		Code: `notify();`,
	})
	if err != nil {
		t.Fatalf("SaveScript: %v", err)
	}
	res, err = h.RunNow(context.Background(), uncaught.ID, nil, QueueView{})
	if err != nil {
		t.Fatalf("RunNow: %v", err)
	}
	if res.OK {
		t.Fatal("an uncaught TypeError should fail the run")
	}
	if !strings.Contains(res.Error, "message") {
		t.Fatalf("error = %q, want it to mention what notify() needed", res.Error)
	}
}

func mustID(t *testing.T, h *Host, name string) string {
	t.Helper()
	for _, s := range h.ListScripts() {
		if s.Name == name {
			return s.ID
		}
	}
	t.Fatalf("no saved script named %q", name)
	return ""
}
