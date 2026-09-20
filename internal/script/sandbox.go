package script

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/dop251/goja"
)

// Actions is the set of app operations a script may call, always with a
// taskID fixed in Go before the script runs. *app.App satisfies it without
// importing this package.
//
// Every implementation must return promptly (an in-memory, lock-guarded
// update, no network, disk or waiting on another goroutine), because
// Runtime.Interrupt cannot preempt a script inside a Go call; a blocking
// method would defeat the timeout.
//
// Retry must treat an empty taskID as retrying nothing. RestartTasks in
// internal/app reads an empty list as every errored task, and although this
// package never binds an empty taskID, implementations should enforce it too.
type Actions interface {
	// Pause pauses the given task. A no-op if it is not running or waiting.
	Pause(taskID string)
	// Resume resumes a paused task. A no-op otherwise.
	Resume(taskID string)
	// Retry re-queues one failed task from scratch. See the empty-taskID
	// requirement above.
	Retry(taskID string) error
	// SetPriority sets one task's queue priority. The implementation clamps
	// it, as it does for a value from the HTTP API, so there is one clamp.
	SetPriority(taskID string, priority int)
	// SetComment overwrites the note on one task's row.
	SetComment(taskID string, text string)
}

// Broadcaster carries notify() messages and run summaries to connected
// browsers. *hub.Hub satisfies it without this package importing it.
type Broadcaster interface {
	Broadcast(typ string, data any)
}

// Output capture bounds for one execution's log and console calls, so a
// console.log in a tight loop cannot grow Result.Output much before the
// timeout ends the loop.
const (
	maxLogLines = 200
	maxLogBytes = 16 * 1024
	maxLineLen  = 2000
)

// maxCallStackFrames bounds goja's interpreter call stack, which otherwise
// defaults to math.MaxInt32.
const maxCallStackFrames = 512

// execCtx is the Go side of one execution's sandbox: what newRuntime needs to
// build the globals, plus the buffer log and console write into. runOne builds
// a fresh one per execution.
type execCtx struct {
	actions Actions
	notify  func(message string) bool

	trigger Trigger
	firedAt time.Time

	// taskID is empty exactly when firing.Task is nil, so no closure is ever
	// bound to an empty taskID (see Actions).
	taskID string
	// firing is the whole event this execution is about, payloads included.
	firing Firing

	output   []string
	outBytes int
}

// appendLog records one already-joined line, dropping anything past
// maxLogLines or maxLogBytes.
func (e *execCtx) appendLog(line string) {
	if len(e.output) >= maxLogLines || e.outBytes >= maxLogBytes {
		return
	}
	if len(line) > maxLineLen {
		line = line[:maxLineLen] + "…(truncated)"
	}
	e.output = append(e.output, line)
	e.outBytes += len(line)
}

// newRuntime builds one fresh, single-use goja.Runtime with exactly the
// globals the package documentation lists. A Runtime is not goroutine-safe
// and is never shared.
func newRuntime(e *execCtx) (*goja.Runtime, error) {
	rt := goja.New()
	// JS property names follow the json tags, lowerCamelCase like every other
	// wire shape here.
	rt.SetFieldNameMapper(goja.TagFieldNameMapper("json", true))
	rt.SetMaxCallStackSize(maxCallStackFrames)

	logFn := func(call goja.FunctionCall) goja.Value {
		parts := make([]string, 0, len(call.Arguments))
		for _, a := range call.Arguments {
			parts = append(parts, a.String())
		}
		e.appendLog(strings.Join(parts, " "))
		return goja.Undefined()
	}
	if err := rt.Set("log", logFn); err != nil {
		return nil, err
	}
	console := map[string]any{"log": logFn, "warn": logFn, "error": logFn}
	if err := rt.Set("console", console); err != nil {
		return nil, err
	}

	notifyFn := func(call goja.FunctionCall) goja.Value {
		if len(call.Arguments) == 0 {
			panic(rt.NewTypeError("notify(message) needs a message"))
		}
		e.notify(call.Argument(0).String())
		return goja.Undefined()
	}
	if err := rt.Set("notify", notifyFn); err != nil {
		return nil, err
	}

	if err := rt.Set("trigger", map[string]any{
		"kind":    string(e.trigger),
		"firedAt": e.firedAt.UTC().Format(time.RFC3339),
	}); err != nil {
		return nil, err
	}

	if err := rt.Set("queue", map[string]any{
		"files":    e.firing.Queue.Files,
		"disabled": e.firing.Queue.Disabled,
		"running":  e.firing.Queue.Running,
		"idle":     IsQueueIdle(e.firing.Queue),
	}); err != nil {
		return nil, err
	}

	if e.firing.Task != nil {
		if err := rt.Set("task", taskGlobal(rt, e)); err != nil {
			return nil, err
		}
	}

	// One global per event payload, bound only when this firing carries it,
	// so `typeof pkg` tells a script whether it has one. The package global
	// is "pkg" because scripts compile in strict mode, where `package` is a
	// reserved word no script could reference.
	for name, payload := range map[string]any{
		"pkg":        packageGlobal(e.firing.Package),
		"extraction": extractGlobal(e.firing.Extract),
		"reconnect":  reconnectGlobal(e.firing.Reconnect),
		"account":    accountGlobal(e.firing.Account),
		"captcha":    captchaGlobal(e.firing.Captcha),
	} {
		if payload == nil {
			continue
		}
		if err := rt.Set(name, payload); err != nil {
			return nil, err
		}
	}

	return rt, nil
}

// packageGlobal is the "pkg" global for TriggerPackageDone, or nil when this
// firing carries no package.
func packageGlobal(v *PackageView) any {
	if v == nil {
		return nil
	}
	return map[string]any{
		"name":     v.Name,
		"files":    v.Files,
		"done":     v.Done,
		"failed":   v.Failed,
		"skipped":  v.Skipped,
		"disabled": v.Disabled,
		"bytes":    v.Bytes,
	}
}

// extractGlobal is the "extraction" global for TriggerExtractDone.
func extractGlobal(v *ExtractView) any {
	if v == nil {
		return nil
	}
	return map[string]any{
		"id":       v.JobID,
		"taskId":   v.TaskID,
		"name":     v.Name,
		"dir":      v.Dir,
		"package":  v.Package,
		"ok":       v.OK,
		"error":    v.Error,
		"password": v.Password,
		"files":    v.Files,
		"bytes":    v.Bytes,
		"nested":   v.Nested,
	}
}

// reconnectGlobal is the "reconnect" global for TriggerReconnectDone.
func reconnectGlobal(v *ReconnectView) any {
	if v == nil {
		return nil
	}
	return map[string]any{
		"ok":      v.OK,
		"changed": v.Changed,
		"from":    v.From,
		"to":      v.To,
		"error":   v.Error,
		"checks":  v.Checks,
	}
}

// accountGlobal is the "account" global for TriggerAccountExpired.
func accountGlobal(v *AccountView) any {
	if v == nil {
		return nil
	}
	return map[string]any{
		"service": v.Service,
		"account": v.Account,
		"label":   v.Label,
		"tier":    v.Tier,
		"expiry":  v.Expiry,
	}
}

// captchaGlobal is the "captcha" global for TriggerCaptchaPending.
func captchaGlobal(v *CaptchaView) any {
	if v == nil {
		return nil
	}
	return map[string]any{
		"id":        v.ID,
		"taskId":    v.TaskID,
		"host":      v.Host,
		"kind":      v.Kind,
		"prompt":    v.Prompt,
		"expiresAt": v.ExpiresAt,
	}
}

// taskGlobal builds the "task" global: the read-only task fields plus five
// closures bound to e.taskID, so no script can name another task.
func taskGlobal(rt *goja.Runtime, e *execCtx) map[string]any {
	t := e.firing.Task
	taskID := e.taskID
	actions := e.actions
	return map[string]any{
		"id":          t.ID,
		"name":        t.Name,
		"url":         t.URL,
		"host":        t.Host,
		"package":     t.Package,
		"status":      t.Status,
		"size":        t.Size,
		"loaded":      t.Loaded,
		"speed":       t.Speed,
		"progressPct": t.ProgressPct(),
		"error":       t.Error,
		"reason":      t.Reason,
		"retries":     t.Retries,
		"priority":    t.Priority,
		"comment":     t.Comment,
		"createdAt":   t.CreatedAt.UTC().Format(time.RFC3339),

		"pause": func(goja.FunctionCall) goja.Value {
			actions.Pause(taskID)
			return goja.Undefined()
		},
		"resume": func(goja.FunctionCall) goja.Value {
			actions.Resume(taskID)
			return goja.Undefined()
		},
		"retry": func(goja.FunctionCall) goja.Value {
			if err := actions.Retry(taskID); err != nil {
				panic(rt.ToValue(err.Error()))
			}
			return goja.Undefined()
		},
		"setPriority": func(call goja.FunctionCall) goja.Value {
			if len(call.Arguments) == 0 {
				panic(rt.NewTypeError("setPriority(n) needs a number"))
			}
			// ToInteger turns NaN into 0 and saturates out-of-range values;
			// the Actions implementation clamps the rest.
			actions.SetPriority(taskID, int(call.Argument(0).ToInteger()))
			return goja.Undefined()
		},
		"setComment": func(call goja.FunctionCall) goja.Value {
			if len(call.Arguments) == 0 {
				panic(rt.NewTypeError("setComment(text) needs a string"))
			}
			actions.SetComment(taskID, call.Argument(0).String())
			return goja.Undefined()
		},
	}
}

// runOutcome is execute's result: what the script printed, whether it failed,
// and whether the failure was a timeout.
type runOutcome struct {
	output   []string
	err      error
	timedOut bool
}

// execute runs one compiled program against the sandbox globals e describes,
// until it finishes or the timeout interrupts it, with recover as a backstop.
func execute(ctx context.Context, prog *goja.Program, timeout time.Duration, e *execCtx) (outcome runOutcome) {
	defer func() {
		if r := recover(); r != nil {
			// Anything goja does not turn into a *goja.Exception, such as a
			// bug in the bindings. An unrecovered panic would kill the whole
			// process.
			outcome = runOutcome{output: e.output, err: fmt.Errorf("script: internal error: %v", r)}
		}
	}()

	rt, err := newRuntime(e)
	if err != nil {
		return runOutcome{output: e.output, err: fmt.Errorf("script: building sandbox: %w", err)}
	}

	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	// stopWatch ends the watcher goroutine on every return path. It touches
	// only this Runtime, so it needs no tracking by Close.
	stopWatch := make(chan struct{})
	defer close(stopWatch)
	go func() {
		select {
		case <-runCtx.Done():
			rt.Interrupt(runCtx.Err())
		case <-stopWatch:
		}
	}()

	_, runErr := rt.RunProgram(prog)
	if runErr != nil {
		// *goja.InterruptedError unwraps to the error passed to Interrupt, so
		// this holds only for a timeout, never for a thrown JS exception.
		timedOut := errors.Is(runErr, context.DeadlineExceeded)
		return runOutcome{output: e.output, err: fmt.Errorf("script: %w", runErr), timedOut: timedOut}
	}
	return runOutcome{output: e.output}
}
