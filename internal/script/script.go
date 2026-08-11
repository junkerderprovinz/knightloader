// Package script is KnightLoader's answer to JDownloader's Event Scripter: a
// goja (pure-Go ECMAScript 5.1(+)) VM host that runs a small, user-written
// JavaScript snippet against one of a fixed set of app events - a task
// finishing, a task settling as failed, the wait queue going idle, or a
// person pressing "run now" on a saved script (see Trigger). See
// docs/build-plan.md section 3's Wave 11 line and section 5's XL-package
// list ("internal/script host"), and docs/jd-feature-census.md's
// "Automation, rules and scripting" rows - search for "Event Scripter" and
// "Sandbox objects" - for what this replaces and for why the rest of JD's
// scripting surface is deliberately not here.
//
// # The sandbox boundary is the whole feature
//
// goja hands a script considerably more than a bare ECMAScript 5.1 standard
// library on its own: Object, Array, String, Math, JSON, RegExp, Date and
// the rest of ES5.1's built-ins, but also Promise, Proxy, Reflect, Symbol,
// Map/Set/WeakMap, BigInt, the typed-array family and globalThis - goja
// implements a substantial slice of ES6+ on top of its ES5.1 base (its own
// README calls itself "ECMAScript 5.1(+)"), and none of that is filtered out
// here. Plus eval and the Function constructor. None of the above is an
// escape - every one of them only constructs or operates on values already
// inside the same already-sandboxed Runtime, bound by the same Interrupt and
// the same globals as everything else: a Promise has nothing to schedule a
// callback from (goja implements no event loop - see its own README,
// "Where is setTimeout()"), and Proxy/Reflect/typed arrays never reach
// outside the Runtime that built them. There is no filesystem, no network,
// no process, no setTimeout/setInterval, no require or import of any kind,
// and none of that is a choice this package makes - it is everything a bare
// goja.Runtime simply does not have. Every capability beyond what goja
// itself already provides is a Go value this package chooses to hand the
// Runtime with Runtime.Set, and this is the complete list:
//
//   - task (object, present only when the execution is about one download):
//     read-only fields id, name, url, host, package, status, size, loaded,
//     speed, progressPct, error, reason, retries, priority, comment,
//     createdAt, taken from a TaskView the caller hands Fire or RunNow.
//     It is a snapshot copied at call time, never a live pointer into the
//     app's own task map - a script cannot make it go stale on purpose and
//     cannot reach the app's own copy by mutating this one, because the two
//     occupy unconnected memory.
//
//   - task.pause(), task.resume(), task.retry(), task.setPriority(n),
//     task.setComment(s): five closures already bound to the ONE task ID
//     this execution belongs to. This is the safety property to notice:
//     there is no function anywhere in this package's JS surface that TAKES
//     a task ID as a script-supplied argument, so a script cannot even
//     syntactically name a task other than its own. Actions (sandbox.go) is
//     the Go-level interface behind the five verbs, and it does take a
//     taskID string - the interface is reused by RunNow for an arbitrary
//     single task, so the flexibility belongs at that layer - but every
//     JS-visible binding this package builds closes over one fixed value
//     chosen in Go before the script sees a single line of it, never a
//     value the script can choose. See Actions' own doc comment for the one
//     existing hazard this design closes structurally rather than by
//     convention: RestartTasks (internal/app/app_queue.go) treats an EMPTY
//     id list as "every task", the exact bug class build-plan.md section 9
//     package 7 documents for the same method - a closure that can only
//     ever be called with the one non-empty ID it was built with cannot
//     hit that path by construction, where a script-supplied string could.
//
//   - queue (object, always present): files, disabled, running, idle - a
//     read-only QueueView snapshot, the same shape app.QueueCounters and
//     app_idle.go's queueIdleForAction already compute (see IsQueueIdle's
//     own doc comment for the exact formula it mirrors).
//
//   - trigger (object, always present): kind and firedAt, so a script
//     bound to one trigger and test-run via RunNow can tell it apart from
//     an ordinary firing, and a script not registered anywhere yet can
//     still be test-run sensibly.
//
//   - notify(message): posts a short string to every connected browser over
//     the same Hub a captcha prompt or an extraction result already uses
//     (Broadcaster, injected - see Host's own doc comment for why this
//     package never imports internal/hub directly). Rate-limited per Host,
//     never per script, so many scripts all bound to the same burst of
//     task completions cannot collectively flood the UI even though no one
//     of them individually looks abusive. This is the sandbox's one write
//     path to a human, and it is the only one.
//
//   - console.log(...), console.warn(...), console.error(...), log(...):
//     all five names append a line to THIS RUN's own output, capped at
//     maxLogLines/maxLogBytes and silently truncated past that. It is not
//     the application log and it touches no file - it exists only in the
//     Result this execution returns, which is how a person testing a script
//     sees what it printed.
//
// Nothing else is reachable. Each exclusion below was a real JD Event
// Scripter capability someone will eventually ask for back, so the reason
// it is missing is written down rather than left to be rediscovered:
//
//   - No File API (getPath/readFile/writeFile/deleteFile/getChecksum). A
//     script that can read or write arbitrary paths is a script that can
//     read the host's own secrets or overwrite the database out from under
//     the store - exactly the boundary Wave 10's file-streaming route
//     exists to hold on the HTTP side (build-plan.md section 9 package 20:
//     "the path-containment check plus the content-type allowlist are the
//     whole feature"), and a side door around it from inside a JS snippet
//     would make that work pointless.
//   - No HTTP API (getPage/postPage/getBrowser). A script that can make its
//     own outbound requests is a script that can exfiltrate whatever
//     task/queue data it can already see to any address it likes, from a
//     box that is often sitting on a home LAN with other things to protect.
//     build-plan.md section 9 package 9 makes the identical argument one
//     layer up the stack, for why a hoster's favicon is fetched
//     server-side rather than by the browser.
//   - No Process/shell API (callSync/callAsync/openURL/playWavAudio). Not
//     narrowable to "safe" at any grain finer than "absent": os/exec fed a
//     browser-edited string is arbitrary code execution on the host by
//     definition, sandbox or no sandbox around the JS that built the
//     string.
//   - No require(fileOrUrl) and no dynamic loading of any kind. A sandbox
//     whose code can fetch and run MORE code is not meaningfully a sandbox;
//     eval/Function survive only because, per the section above, they
//     cannot reach further than the Runtime they are already inside.
//   - No queue-wide actions: no pausing everything, no changing the global
//     speed limit, no starting a new download from a script-chosen URL.
//     Everything above is scoped to the one task a trigger fired for; a
//     blast radius of "the whole queue" or "fetch this URL as a new
//     download" is a materially bigger grant than "affect the download
//     this script is about". The one JD feature that legitimately wants
//     queue-wide scheduled actions (Settings > Scheduler) is a different,
//     time-based feature already covered by internal/schedule.
//   - No settings, account or credential access of any kind.
//   - No persistent property store (JD's getProperty/setProperty). Nothing
//     built on top of this package needs cross-run state yet, and adding it
//     is easier to design once, deliberately, than to retrofit after
//     scripts have quietly come to depend on whatever the first accidental
//     shape offered.
//
// # Every execution is bounded twice, for two different failure modes
//
// A goja.Runtime is built fresh per execution and thrown away afterwards -
// never reused, never shared between scripts or between two runs of the
// same script (see newRuntime in sandbox.go) - so no script can observe or
// corrupt another's globals, and no state a script sets on its own objects
// survives past the run that set it. Runtime.Interrupt, armed against the
// context execute is given (a timeout derived from the script's own
// TimeoutMS, clamped to [MinTimeout, MaxTimeout]) and checked by a small
// goroutine that exits the moment RunProgram returns, is what stops a
// script whose own JavaScript loops forever. Runtime.Interrupt's own doc
// comment states the one thing it cannot do: preempt time spent inside a
// host-provided Go function such as one of the closures above, which is
// exactly why every Actions implementation this package calls is required
// to be non-blocking - see Actions' own doc comment.
//
// Independently of Interrupt, the whole execution runs under a plain Go
// recover() (see execute in sandbox.go). goja converts a Go panic(v
// goja.Value) into a catchable JS exception on its own - the documented
// pattern this package's own bindings rely on to signal a failed action
// back to the script - but anything else panicking, whether a bug in this
// package's own bindings or a goja internal path this package has not
// exercised, must never be allowed to cross a goroutine boundary
// unrecovered: an unrecovered panic on any goroutine kills the whole
// process, taking down every other download in it, not only the one the
// misbehaving script was about.
//
// Two things this does NOT bound, stated rather than left to be discovered
// the hard way: goja has no built-in memory ceiling, so one huge
// synchronous allocation (new Array(1e9)) can cost real memory before the
// timeout goroutine ever gets a chance to call Interrupt -
// Runtime.SetMaxCallStackSize keeps unbounded recursion from being one
// route to that outcome, but a single explicit large allocation is a known,
// accepted residual risk shared by every embedded-VM-without-a-custom-
// allocator design, goja included. And CPU spent inside a host closure -
// the paragraph above - is not bounded by Interrupt either; it is bounded
// only by that closure being written to return quickly, which is a
// property of Actions' implementation, not of this package.
//
// # Every goroutine this package starts is tracked
//
// Host owns its own ctx/cancel/WaitGroup pair and its own spawn - the same
// shape internal/idleaction.Controller and internal/schedule.Runner already
// use for a background subsystem with its own lifecycle (see host.go). It
// is deliberately not a.spawn: that method is unexported on *app.App and
// belongs to a different package, and this package does not import
// internal/app at all (see below). Whoever constructs a Host is expected to
// call its Close from the same App.Close that already closes sched and
// idleAction, so a running script cannot outlive the app that started it. A
// small fixed pool of worker goroutines drains a bounded queue; Fire never
// blocks its caller, the same promise Hub.Broadcast already makes and for
// the same reason - the cost of firing a trigger must not depend on how
// long the slowest currently-running script takes.
//
// # Wiring this in is deliberately not this package's job
//
// This package imports nothing from internal/app, internal/core or
// internal/hub. Actions and Broadcaster (sandbox.go) are small interfaces
// it declares; *app.App and *hub.Hub can satisfy them structurally, with no
// changes required on their side, the same way TaskView and QueueView
// (view.go) are this package's own plain structs rather than core.Task or
// app.QueueCounters - see internal/rules.Candidate for the identical
// decoupling, done for the identical reason, already established elsewhere
// in this tree. Nothing in this package touches internal/app, internal/api
// or web/; the handful of lines that construct a Host, feed it Fire calls
// at the existing task/queue broadcast sites, and Close it alongside sched
// and idleAction belong to whoever wires this wave together next, and are
// spelled out in full in this wave's own report rather than guessed at
// here.
package script

import (
	"time"
)

// Trigger identifies which real app event a script is bound to. The set is
// fixed and small on purpose, the same reasoning app_activity.go's
// ActivityKind already gives for its own four values: a typed, closed list
// is what lets every layer above this one (storage, the registry index, a
// future settings UI) exhaustively switch on it instead of rendering
// whatever free-text string happened to be saved.
type Trigger string

const (
	// TriggerTaskDone fires once a task settles as done - see
	// ClassifyTaskUpdate for the exact condition, grounded in
	// internal/app/app_dispatch.go's onUpdate and its single "task" broadcast
	// per settled state.
	TriggerTaskDone Trigger = "task.done"
	// TriggerTaskFailed fires once a task settles as failed with no
	// automatic retry pending - see ClassifyTaskUpdate. A task mid-retry
	// (NextTry set) does not fire this; only the final word does, so a
	// script bound to it is not run once per backoff attempt.
	TriggerTaskFailed Trigger = "task.failed"
	// TriggerQueueIdle fires when the wait queue has nothing enabled left to
	// run, start or finish - see IsQueueIdle, the same formula
	// app_idle.go's queueIdleForAction already computes for the end-of-queue
	// action.
	TriggerQueueIdle Trigger = "queue.idle"
	// TriggerOnDemand is a person running a script directly - the "run now"
	// path RunNow serves, and the trigger 11B's user action buttons bind to.
	// It carries a task when the button lives on a task's own row, and none
	// when it lives on a global toolbar - both are valid, and task is simply
	// absent from the sandbox in the second case.
	TriggerOnDemand Trigger = "manual"
)

// Valid reports whether t is one of the triggers this build knows about.
// Used by Store validation so a row saved by a later build - or hand-edited
// - cannot silently register against a trigger this one never fires.
func (t Trigger) Valid() bool {
	switch t {
	case TriggerTaskDone, TriggerTaskFailed, TriggerQueueIdle, TriggerOnDemand:
		return true
	default:
		return false
	}
}

// AllTriggers lists every trigger this build fires, for whoever builds a
// GET /api/scripts/triggers (or similar) route: the trigger vocabulary a
// script editor offers should come from the registry that actually fires
// them, never a hand-copied list that quietly drifts the day a fifth
// trigger is added here and the picker is not - the exact reasoning
// web/src/lib/scripts.ts's own fetchScriptTriggers already assumes of
// whatever answers that route. Returns a fresh slice on every call, so a
// caller is free to sort or mutate it.
func AllTriggers() []Trigger {
	return []Trigger{TriggerTaskDone, TriggerTaskFailed, TriggerQueueIdle, TriggerOnDemand}
}

// Timeout bounds, in one place so Store validation and the default a script
// gets without an opinion of its own agree with each other.
const (
	// DefaultTimeout is what a script gets when it does not set TimeoutMS.
	DefaultTimeout = 5 * time.Second
	// MinTimeout is the shortest a script may ask for. Below this the
	// interrupt goroutine's own overhead starts to matter more than the
	// budget it is enforcing.
	MinTimeout = 100 * time.Millisecond
	// MaxTimeout is the longest a script may ask for, automatic or manual.
	// This is automation glue between real events, not a batch job - thirty
	// seconds is already generous for "read a few fields and call one or
	// two actions", and a worker held that long is a worker forty other
	// scripts queued behind it are waiting on (see host.go's workerCount).
	MaxTimeout = 30 * time.Second
)

// Source size bounds. Generous for what this feature is - a short
// automation snippet, not an uploaded application - and cheap insurance
// against a pathological paste (a whole file dropped into the editor by
// mistake) rather than a considered attack surface; the timeout and the
// sandbox above are what actually keep a script honest once it runs.
const (
	MaxCodeBytes = 64 * 1024
	MaxNameBytes = 200
)

// Script is one saved automation entry: a name, the trigger it is bound to,
// its source, and whether it currently runs at all.
type Script struct {
	ID      string  `json:"id"`
	Name    string  `json:"name"`
	Trigger Trigger `json:"trigger"`
	Code    string  `json:"code"`
	// Enabled is the master switch for automatic firing. A disabled script
	// is left out of the trigger index entirely (see Host.rebuildIndex) but
	// is still reachable by ID for RunNow - a person testing a script they
	// have not turned on yet is the ordinary case, not an edge one.
	Enabled bool `json:"enabled"`
	// TimeoutMS overrides DefaultTimeout for this one script when non-zero,
	// clamped to [MinTimeout, MaxTimeout] - a script may ask for LESS time
	// than the default, never more than the hard ceiling.
	TimeoutMS int       `json:"timeoutMs,omitempty"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// Result is what one execution produced, returned by RunNow and broadcast
// (in the smaller Event shape) after every automatic firing.
type Result struct {
	ScriptID   string    `json:"scriptId"`
	Name       string    `json:"name"`
	Trigger    Trigger   `json:"trigger"`
	TaskID     string    `json:"taskId,omitempty"`
	StartedAt  time.Time `json:"startedAt"`
	DurationMS int64     `json:"durationMs"`
	// Output is what the script printed via log()/console.*, oldest first,
	// capped - see maxLogLines/maxLogBytes in sandbox.go.
	Output []string `json:"output,omitempty"`
	OK     bool     `json:"ok"`
	// Error is set when the script threw, failed to compile, or was
	// interrupted - see TimedOut for which of those it was.
	Error    string `json:"error,omitempty"`
	TimedOut bool   `json:"timedOut,omitempty"`
}

// Event is the hub message this package broadcasts - message type "script",
// the same free-form-string-keyed Hub every other subsystem already
// broadcasts on (see internal/app/app_activity.go's own doc comment for the
// precedent: one message kind carrying a Kind discriminator, rather than a
// new message type per source). Kind is "notify" for a script's own
// notify(message) call, or "result" for a completed run's summary.
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

// clampTimeout applies MinTimeout/MaxTimeout to a script's own TimeoutMS,
// or to DefaultTimeout when it did not set one.
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
