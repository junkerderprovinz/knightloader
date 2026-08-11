package script

import "time"

// TaskView is the read-only shape of one download a script may see. It is
// this package's own struct, not core.Task, for the reason spelled out in
// the package doc comment's "wiring this in" section: internal/rules faces
// the identical problem (a leaf package that needs to look at a task
// without depending on internal/core) and already solves it the same way
// with rules.Candidate. Whoever builds a TaskView from a real *core.Task is
// a one-line-per-field copy at the call site, not a conversion this package
// performs, since only the caller (eventually internal/app) can reach both
// types.
//
// Status is a plain string mirroring core.Task.Status's own JSON encoding
// ("done", "error", "running", ...) rather than core.Status, for the same
// decoupling reason. statusDone/statusError below are this package's own
// unexported copies of the two values ClassifyTaskUpdate branches on, kept
// as named constants rather than sprinkled string literals.
type TaskView struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	URL      string `json:"url"`
	Host     string `json:"host"`
	Package  string `json:"package"`
	Status   string `json:"status"`
	Size     int64  `json:"size"`
	Loaded   int64  `json:"loaded"`
	Speed    int64  `json:"speed"`
	Error    string `json:"error"`
	Reason   string `json:"reason"`
	Retries  int    `json:"retries"`
	Priority int    `json:"priority"`
	Comment  string `json:"comment"`
	// NextTry is when an automatic retry is due, zero for none pending. It
	// is the classification input that tells a settled failure apart from
	// one about to be retried - see ClassifyTaskUpdate - and is deliberately
	// not exposed to a script (sandbox.go's task object has no nextTry
	// field): a script sees "failed" or does not run at all, never a
	// half-decided state to build its own retry logic around.
	NextTry   time.Time `json:"-"`
	CreatedAt time.Time `json:"createdAt"`
}

// ProgressPct is Loaded/Size as a 0-100 percentage, 0 when Size is not yet
// known - the one derived convenience field the sandbox adds on top of a
// plain field copy, because "loaded / size * 100, clamped" is exactly the
// kind of arithmetic a script would otherwise get subtly wrong once (a
// divide by zero on a task whose size is still unknown) and then work
// around badly.
func (v TaskView) ProgressPct() float64 {
	if v.Size <= 0 {
		return 0
	}
	pct := float64(v.Loaded) / float64(v.Size) * 100
	switch {
	case pct < 0:
		return 0
	case pct > 100:
		return 100
	default:
		return pct
	}
}

const (
	statusDone  = "done"
	statusError = "error"
)

// ClassifyTaskUpdate decides which trigger, if any, a task snapshot
// represents right now. It is a pure function over TaskView precisely so it
// can be unit-tested with plain struct literals and so the one subtle rule
// it encodes is checked in one place rather than re-derived at every call
// site:
//
// TriggerTaskFailed fires only once NextTry is zero - internal/app's own
// onUpdate (app_dispatch.go) sets t.NextTry to the next retry deadline
// BEFORE broadcasting "task" with Status still Error, and only zeroes it
// once retries are exhausted or the failure is one nothing retries helps
// with (ReasonDiskFull). Firing on every Status==Error broadcast would run
// a "notify me when a download fails" script once per backoff attempt
// instead of once, on the failure that is actually final - see
// app_dispatch.go's own comment: "left settled where the error is on
// screen". A task that hands itself to the next resolver in the fallback
// chain, or whose account turned out to be unroutable, is put back to
// StatusQueued before that same broadcast, not left at StatusError - so
// neither of those paths reaches this function as a failure at all, without
// this function needing to know either mechanism exists.
func ClassifyTaskUpdate(v TaskView) (Trigger, bool) {
	switch v.Status {
	case statusDone:
		return TriggerTaskDone, true
	case statusError:
		if v.NextTry.IsZero() {
			return TriggerTaskFailed, true
		}
	}
	return "", false
}

// QueueView is the read-only shape of the wait queue a script may see - the
// same three counters app.QueueCounters already carries (Files, Disabled,
// Running), copied rather than imported for the reason TaskView's own doc
// comment gives.
type QueueView struct {
	Files    int `json:"files"`
	Disabled int `json:"disabled"`
	Running  int `json:"running"`
}

// IsQueueIdle mirrors app_idle.go's queueIdleForAction exactly: the queue
// has nothing left to do when every remaining file is one the user has
// switched off. A manually paused or held task is NOT idle by this
// definition, on purpose, matching queueIdleForAction's own comment - both
// mean "wait a bit", not "never", and either one still counts as work left
// to do.
func IsQueueIdle(v QueueView) bool {
	return v.Files == v.Disabled
}
