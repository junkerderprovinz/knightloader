// Package script runs small user-written JavaScript snippets on app events, in
// the manner of JDownloader's Event Scripter. It hosts a goja VM
// (ECMAScript 5.1 with much of ES6) and binds each script to one Trigger: a
// task finishing or failing, the queue going idle, a link arriving, a package
// finishing, an archive unpacking, a checksum mismatch, a reconnect, an
// account lapsing, a captcha waiting, or a person pressing "run now".
//
// Events arrive on a Bus (bus.go). internal/app publishes a Firing and the
// Host subscribes in NewHost, so another consumer of the same events
// subscribes too instead of being called beside every publishing site.
//
// # The sandbox
//
// goja provides the ES built-ins, including Promise, Proxy, Reflect, typed
// arrays, eval and the Function constructor. None of them reach outside the
// Runtime: goja has no event loop, filesystem, network, process, timers or
// module loading. Everything beyond that is set on the Runtime by this
// package, and this is the complete list:
//
//   - task (only when the execution is about one download): a read-only copy
//     of a TaskView, never a pointer into the app's task map.
//   - task.pause(), task.resume(), task.retry(), task.setPriority(n) and
//     task.setComment(s): closures bound in Go to that one task's ID. No
//     binding takes a task ID from the script, so a script cannot name
//     another task, and none can pass the empty ID that RestartTasks reads
//     as "every task".
//   - queue: a read-only QueueView (files, disabled, running, idle).
//   - trigger: kind and firedAt, so a RunNow test run can tell itself apart.
//   - pkg, extraction, reconnect, account, captcha: read-only views present
//     only for the trigger that carries them. "pkg" because `package` is
//     reserved in strict mode. None adds an action.
//   - notify(message): a short message to every connected browser through
//     the injected Broadcaster, rate-limited per Host rather than per script
//     so a burst of scripts cannot flood the UI.
//   - console.log, console.warn, console.error and log: lines appended to
//     this run's Result, capped at maxLogLines/maxLogBytes.
//
// Left out on purpose: file access (it would reach the host's secrets and the
// database), HTTP (it could exfiltrate task data from inside a home LAN),
// processes (arbitrary code execution; starting a program on an event is
// internal/eventprog, set up in the settings rather than from a script),
// loading further code, queue-wide actions or new downloads
// (internal/schedule covers timed queue actions), settings and credentials,
// and a persistent property store.
//
// # Limits
//
// Every execution gets a fresh Runtime that is thrown away afterwards, so no
// script sees another's globals. Runtime.Interrupt, armed against a timeout
// from TimeoutMS clamped to [MinTimeout, MaxTimeout], stops a script that
// loops forever. It cannot preempt a host Go function, which is why every
// Actions implementation must not block. The execution also runs under
// recover: goja turns a panic(goja.Value) into a JS exception, but any other
// panic would otherwise kill the whole process.
//
// goja has no memory ceiling. SetMaxCallStackSize stops unbounded recursion,
// but one large allocation such as new Array(1e9) is an accepted risk.
//
// # Goroutines and wiring
//
// Host owns its context, cancel and WaitGroup, like internal/schedule.Runner,
// and its Close belongs in App.Close so no script outlives the app. A small
// worker pool drains a bounded queue and firing never blocks the caller.
//
// This package imports nothing from internal/app, internal/core or
// internal/hub: Actions and Broadcaster are interfaces *app.App and *hub.Hub
// satisfy, and the views are this package's own structs, as with
// internal/rules.Candidate. internal/app/app_script.go does the wiring.
package script

import (
	"time"
)

// Trigger identifies which app event a script is bound to. The set is closed
// so storage, the registry index and the editor can switch on it exhaustively.
type Trigger string

const (
	// TriggerTaskDone fires once a task settles as done (see
	// ClassifyTaskUpdate).
	TriggerTaskDone Trigger = "task.done"
	// TriggerTaskFailed fires once a task settles as failed with no automatic
	// retry pending, so a script is not run once per backoff attempt.
	TriggerTaskFailed Trigger = "task.failed"
	// TriggerQueueIdle fires when the wait queue has nothing enabled left to
	// run, start or finish (see IsQueueIdle).
	TriggerQueueIdle Trigger = "queue.idle"
	// TriggerOnDemand is a person running a script directly through RunNow or
	// a user action button. It carries a task when the button sits on a task's
	// row and none on a global toolbar.
	TriggerOnDemand Trigger = "manual"
)

// The events that arrive through the bus.
const (
	// TriggerLinkAdded fires once per link that entered the list. A link the
	// filter refused sits in the holding area and does not fire it, nor does
	// a link merged into an existing one.
	TriggerLinkAdded Trigger = "link.added"
	// TriggerPackageDone fires when a package has nothing left to wait for,
	// unlike task.done, which fires per file. See PackageView and
	// packageComplete in internal/app.
	TriggerPackageDone Trigger = "package.done"
	// TriggerExtractDone fires when an unpacking ends on its own, successfully
	// or not; Extract.OK says which, so a script that only wants successes
	// starts with `if (!extraction.ok) return;`. A cancelled extraction does
	// not fire it.
	TriggerExtractDone Trigger = "extract.done"
	// TriggerChecksumFailed fires when a finished file's hash did not match.
	// A file with no checksum, or one that could not be read, is unverified
	// rather than wrong and does not fire it.
	TriggerChecksumFailed Trigger = "checksum.failed"
	// TriggerReconnectDone fires after every reconnect run, automatic or by
	// hand. Reconnect.OK and Reconnect.Changed are separate, since a run that
	// kept the same address worked but achieved nothing.
	TriggerReconnectDone Trigger = "reconnect.done"
	// TriggerAccountExpired fires once when the account-health sweep sees an
	// expiry pass, not on every sweep (see fireAccountExpiry in internal/app).
	TriggerAccountExpired Trigger = "account.expired"
	// TriggerCaptchaPending fires once per new challenge, not on every poll
	// while it waits for an answer.
	TriggerCaptchaPending Trigger = "captcha.pending"
)

// Valid reports whether t is one of the triggers this build knows about, so a
// row saved by a later build or edited by hand cannot register against a
// trigger that never fires.
func (t Trigger) Valid() bool {
	switch t {
	case TriggerTaskDone, TriggerTaskFailed, TriggerQueueIdle, TriggerOnDemand,
		TriggerLinkAdded, TriggerPackageDone, TriggerExtractDone, TriggerChecksumFailed,
		TriggerReconnectDone, TriggerAccountExpired, TriggerCaptchaPending:
		return true
	default:
		return false
	}
}

// AllTriggers lists every trigger this build fires, for the script editor's
// trigger picker, which renders them in this order. It returns a fresh slice.
func AllTriggers() []Trigger {
	return []Trigger{
		TriggerTaskDone, TriggerTaskFailed, TriggerQueueIdle, TriggerOnDemand,
		TriggerLinkAdded, TriggerPackageDone, TriggerExtractDone, TriggerChecksumFailed,
		TriggerReconnectDone, TriggerAccountExpired, TriggerCaptchaPending,
	}
}

// Timeout bounds, shared by Store validation and the default.
const (
	// DefaultTimeout is what a script gets when it does not set TimeoutMS.
	DefaultTimeout = 5 * time.Second
	// MinTimeout is the shortest a script may ask for; below it the interrupt
	// goroutine's overhead dominates.
	MinTimeout = 100 * time.Millisecond
	// MaxTimeout is the longest a script may ask for. A worker held longer
	// delays every script queued behind it (see workerCount).
	MaxTimeout = 30 * time.Second
)

// Source size bounds, against a whole file pasted into the editor by mistake.
const (
	MaxCodeBytes = 64 * 1024
	MaxNameBytes = 200
)

// Script is one saved automation entry.
type Script struct {
	ID      string  `json:"id"`
	Name    string  `json:"name"`
	Trigger Trigger `json:"trigger"`
	Code    string  `json:"code"`
	// Enabled switches automatic firing. A disabled script is left out of the
	// trigger index but can still be run with RunNow, which is how a new
	// script is tested.
	Enabled bool `json:"enabled"`
	// TimeoutMS overrides DefaultTimeout when non-zero, clamped to
	// [MinTimeout, MaxTimeout].
	TimeoutMS int       `json:"timeoutMs,omitempty"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// Result is what one execution produced, returned by RunNow and broadcast as
// an Event after every automatic firing.
type Result struct {
	ScriptID   string    `json:"scriptId"`
	Name       string    `json:"name"`
	Trigger    Trigger   `json:"trigger"`
	TaskID     string    `json:"taskId,omitempty"`
	StartedAt  time.Time `json:"startedAt"`
	DurationMS int64     `json:"durationMs"`
	// Output is what the script printed through log() and console, oldest
	// first, capped (see maxLogLines and maxLogBytes).
	Output []string `json:"output,omitempty"`
	OK     bool     `json:"ok"`
	// Error is set when the script threw, failed to compile or was
	// interrupted; TimedOut says whether it was the last.
	Error    string `json:"error,omitempty"`
	TimedOut bool   `json:"timedOut,omitempty"`
}

// Event is the hub message this package broadcasts under the message type
// "script". Kind is "notify" for a script's notify(message) call or "result"
// for a completed run's summary.
type Event struct {
	Kind       string  `json:"kind"`
	ScriptID   string  `json:"scriptId,omitempty"`
	Name       string  `json:"name,omitempty"`
	Message    string  `json:"message,omitempty"`
	Trigger    Trigger `json:"trigger,omitempty"`
	TaskID     string  `json:"taskId,omitempty"`
	OK         bool    `json:"ok,omitempty"`
	Error      string  `json:"error,omitempty"`
	DurationMS int64   `json:"durationMs,omitempty"`
}

// clampTimeout applies MinTimeout and MaxTimeout to a script's TimeoutMS, or
// returns DefaultTimeout when it is unset.
func clampTimeout(d time.Duration) time.Duration {
	if d <= 0 {
		return DefaultTimeout
	}
	if d < MinTimeout {
		return MinTimeout
	}
	if d > MaxTimeout {
		return MaxTimeout
	}
	return d
}
