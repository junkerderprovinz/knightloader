package script

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/dop251/goja"
)

// Actions is the narrow set of app operations a script may call, always
// resolved to a taskID Go already fixed before the script ran a single line
// - see the package doc comment's task.pause()/etc. entry for why nothing
// in this package's JS surface can hand a script the ability to choose that
// ID itself. This interface only exists so *app.App can satisfy it
// structurally without internal/app importing this package or being told
// its method set has to change - see the package doc's "wiring this in"
// section.
//
// Every implementation MUST be non-blocking, or effectively so (an
// in-memory, lock-guarded update - no network call, no disk write on the
// hot path, no waiting on another goroutine). Runtime.Interrupt cannot
// preempt time spent inside a native Go call once a script has entered one
// (see Runtime.Interrupt's own doc comment, and the package doc's "bounded
// twice" section) - a blocking Actions method is a hole in the timeout this
// whole package exists to enforce, not a slow path a caller can wait out.
//
// Retry MUST treat an empty taskID as a request to retry NOTHING, never as
// "every task" - see internal/app's RestartTasks(ids []string), which
// treats an empty slice as "every errored task" (build-plan.md section 9
// package 7 documents the exact hazard). This package's own bindings never
// construct a closure over an empty taskID (see newRuntime), so this
// requirement is a second, independent line of defence for whatever other
// caller Actions gains later - implementations should still enforce it
// rather than lean on this package alone doing so.
type Actions interface {
	// Pause pauses the given task. A no-op if it is not running or waiting.
	Pause(taskID string)
	// Resume resumes a paused task. A no-op otherwise.
	Resume(taskID string)
	// Retry re-queues one failed task from scratch. See the empty-taskID
	// requirement above.
	Retry(taskID string) error
	// SetPriority sets one task's queue priority. Implementations own
	// clamping to whatever range the app allows (internal/rules.PriorityMin/
	// Max today) - this package passes the script's number through
	// unvalidated, the same way a hand-typed value from the HTTP API
	// already does, so there is exactly one clamp to keep in sync rather
	// than two.
	SetPriority(taskID string, priority int)
	// SetComment overwrites the note on one task's row.
	SetComment(taskID string, text string)
}

// Broadcaster is the one write path this package has to a connected human -
// notify() - and the channel it reports a completed run on. *hub.Hub
// already satisfies this with no changes; the interface exists so this
// package does not need to import internal/hub to declare what it needs
// from it (see the package doc's "wiring this in" section).
type Broadcaster interface {
	Broadcast(typ string, data any)
}

// Output capture bounds for one execution's log()/console.* calls - see the
// package doc comment's console.log entry. Generous enough for ordinary
// debugging, small enough that a script calling console.log in a tight loop
// cannot grow Result.Output into a real memory concern before the timeout
// catches the loop itself.
const (
	maxLogLines = 200
	maxLogBytes = 16 * 1024
	maxLineLen  = 2000
)

// maxCallStackFrames bounds goja's OWN interpreter call stack
// (Runtime.SetMaxCallStackSize), which defaults to math.MaxInt32 - see the
// package doc comment's "bounded twice" section for what this does and does
// not close. A few hundred frames is already deep for hand-written
// automation glue; it is not a tuning knob anyone is expected to hit.
const maxCallStackFrames = 512

// execCtx is the Go side of one execution's sandbox: everything newRuntime
// needs to build the globals for it, plus the buffer log()/console.* write
// into. Built fresh per execution by runOne (host.go), never reused.
type execCtx struct {
	actions Actions
	notify  func(message string) bool

	trigger Trigger
	firedAt time.Time

	// taskID is "" and task is nil together, always - see newRuntime, which
	// is the one place that decides whether the "task" global exists at
	// all. Never set taskID without task, or task without a non-empty
	// taskID: the empty-taskID hazard Actions' own doc comment describes is
	// exactly what keeping these two in lockstep prevents.
	taskID string
	task   *TaskView
	queue  QueueView

	output   []string
	outBytes int
}

// appendLog records one already-joined line, silently dropping anything
// past maxLogLines/maxLogBytes - see the constants' own comment for why
// this is a real bound and not a formality.
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

// newRuntime builds one fresh, single-use goja.Runtime and wires in exactly
// the globals the package doc comment enumerates - nothing else. Called
// once per execution (see execute) and discarded after; a goja.Runtime is
// explicitly not goroutine-safe and this package never shares one across
// two calls, so there is no pooling to reason about.
func newRuntime(e *execCtx) (*goja.Runtime, error) {
	rt := goja.New()
	// TagFieldNameMapper mirrors the json tags already on TaskView/QueueView
	// into idiomatic lowerCamelCase JS property names, matching every other
	// wire shape in this codebase rather than exposing Go's own
	// capitalised field names.
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
		"files":    e.queue.Files,
		"disabled": e.queue.Disabled,
		"running":  e.queue.Running,
		"idle":     IsQueueIdle(e.queue),
	}); err != nil {
		return nil, err
	}

	if e.task != nil {
		if err := rt.Set("task", taskGlobal(rt, e)); err != nil {
			return nil, err
		}
	}

	return rt, nil
}

// taskGlobal builds the map[string]any bound as the "task" global: the
// read-only fields from e.task, plus five closures each bound to e.taskID -
// see the package doc comment's task.pause() entry and Actions' own doc
// comment for why that binding, not an ID-taking function, is the whole of
// the safety property.
func taskGlobal(rt *goja.Runtime, e *execCtx) map[string]any {
	t := e.task
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
			// ToInteger follows ECMAScript's own ToInteger conversion: NaN
			// becomes 0 and out-of-range values saturate, so a script
			// passing a non-numeric or absurd argument cannot panic this
			// binding - it can only ask for a priority the Actions
			// implementation's own clamp (see Actions' doc comment) will
			// reduce to something sane.
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

// runOutcome is execute's result: what the script printed, whether it
// failed, and whether a failure was specifically a timeout - kept as one
// struct rather than three-plus return values so a caller cannot transpose
// them.
type runOutcome struct {
	output   []string
	err      error
	timedOut bool
}

// execute runs one already-compiled program to completion or until timeout,
// whichever comes first, against the sandbox globals e describes. It is the
// one place both bounding mechanisms the package doc comment's "bounded
// twice" section describes meet: Interrupt via ctx, and recover() as the
// backstop under it.
func execute(ctx context.Context, prog *goja.Program, timeout time.Duration, e *execCtx) (outcome runOutcome) {
	defer func() {
		if r := recover(); r != nil {
			// Backstop for anything goja's own panic(Value) handling does
			// not turn into a clean, already-returned *goja.Exception - a
			// bug in this file's own bindings, or a goja internal path
			// this package has not exercised. See the package doc
			// comment's "bounded twice" section: this must never propagate,
			// because an unrecovered panic on any goroutine kills the
			// whole process, not just this one script.
			outcome = runOutcome{output: e.output, err: fmt.Errorf("script: internal error: %v", r)}
		}
	}()

	rt, err := newRuntime(e)
	if err != nil {
		return runOutcome{output: e.output, err: fmt.Errorf("script: building sandbox: %w", err)}
	}

	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	// stopWatch bounds this goroutine's own lifetime to this function's:
	// it is closed on every return path (the defer immediately below,
	// which runs before the recover() defer above unwinds further) and it
	// touches nothing but the Runtime built two lines up and these two
	// local channels - not App-owned state, not Host-owned state - so it is
	// not the bare-goroutine hazard a.spawn's own doc comment warns
	// against; there is nothing here for a future Close to wait on that
	// this function does not already wait on itself by returning.
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
		// errors.Is walks *goja.InterruptedError's own Unwrap (confirmed in
		// goja's runtime.go: it returns e.iface.(error) when the value
		// passed to Interrupt was one) straight through to the exact
		// sentinel context.WithTimeout produces, so this is true only when
		// RunProgram failed BECAUSE the clock ran out - never for an
		// ordinary thrown JS exception, which is a *goja.Exception and does
		// not unwrap to this at all.
		timedOut := errors.Is(runErr, context.DeadlineExceeded)
		return runOutcome{output: e.output, err: fmt.Errorf("script: %w", runErr), timedOut: timedOut}
	}
	return runOutcome{output: e.output}
}
