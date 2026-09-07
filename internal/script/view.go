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

// PackageView is what TriggerPackageDone carries: which package finished and
// how it went. It is COUNTS AND NOT A VERDICT, and that is the whole design
// decision behind this event.
//
// "The package is done" is not the same claim as "every file in it worked".
// A package holding one dead link would never finish under the stricter
// reading, so the trigger that exists to say "the last part has landed,
// go and unpack it" would be exactly the trigger a broken link switches off
// forever. So the event fires when there is nothing left to WAIT for, and
// the counts below let the script decide what it thinks of that:
//
//	if (pkg.failed > 0) { notify(pkg.name + ": " + pkg.failed + " missing"); return; }
//
// Done + Failed + Skipped + Disabled does not have to add up to Files, and
// reading it as a partition is the one mistake to avoid: Disabled overlaps
// the other three (a file the user switched off after it had already
// finished is both), and Files counts every task in the package including
// ones in none of the four buckets, such as a link still sitting in the
// collector at the moment a different package member settled.
type PackageView struct {
	Name  string `json:"name"`
	Files int    `json:"files"`
	Done  int    `json:"done"`
	// Failed is files that settled as failed with no retry pending - the
	// same "final word only" reading ClassifyTaskUpdate applies to
	// task.failed, so a file merely between two backoff attempts counts as
	// pending and holds the whole event off rather than being reported as
	// lost.
	Failed int `json:"failed"`
	// Skipped is files the link filter is holding. They are counted so a
	// script can say "and four links were filtered out", and they never hold
	// the event off: a held link is not work in progress, it is a decision
	// waiting for a person who may never make it.
	Skipped int `json:"skipped"`
	// Disabled is files the user switched off. Same treatment as Skipped and
	// for the same reason app_idle.go's queueIdleForAction subtracts them:
	// "not right now" from the person who owns the queue is not owed work.
	Disabled int `json:"disabled"`
	// Bytes is what the package actually pulled down, summed over every file
	// in it - Loaded and not Size, so a package with a failed half-file
	// reports what is on the disk rather than what was advertised.
	Bytes int64 `json:"bytes"`
}

// ExtractView is what TriggerExtractDone carries: one finished unpacking, in
// the shape app.ExtractJob already publishes to the browser, minus the
// fields that only mean something while it is still running (Archive, Depth,
// the timestamps). A script asking "what came out of that archive" wants
// Files and Bytes; a script asking "why did it not work" wants Error and
// Password, which is the one failure with an obvious next step.
type ExtractView struct {
	JobID   string `json:"jobId"`
	TaskID  string `json:"taskId,omitempty"`
	Name    string `json:"name"`
	Dir     string `json:"dir"`
	Package string `json:"package,omitempty"`
	OK      bool   `json:"ok"`
	Error   string `json:"error,omitempty"`
	// Password is the failure being a missing or wrong archive password
	// rather than a broken archive - the one case where typing something in
	// and pressing start again fixes it.
	Password bool  `json:"password,omitempty"`
	Files    int   `json:"files"`
	Bytes    int64 `json:"bytes"`
	Nested   int   `json:"nested,omitempty"`
}

// ReconnectView is what TriggerReconnectDone carries. OK and Changed are two
// separate answers on purpose: internal/reconnect's ErrUnchanged means the
// run did everything it was told to and the address stayed put, which is a
// working configuration that achieved nothing - a different problem from a
// run that could not reach the router at all, and one a script should be
// able to tell apart without parsing Error.
//
// From and To are plain strings rather than netip.Addr for the decoupling
// reason TaskView's own doc comment gives, and empty when the run never got
// far enough to read an address.
type ReconnectView struct {
	OK      bool   `json:"ok"`
	Changed bool   `json:"changed"`
	From    string `json:"from,omitempty"`
	To      string `json:"to,omitempty"`
	Error   string `json:"error,omitempty"`
	// Checks is how many times the address was polled after the method ran,
	// which is the one number that says whether a failed run gave up
	// immediately or waited out its whole budget.
	Checks int `json:"checks"`
}

// AccountView is what TriggerAccountExpired carries. Account is "" for a
// service's default login and the account id for a second one on the same
// service, which is the same distinction resolver.SlotID draws - a person
// with two AllDebrid keys needs to know WHICH one lapsed, and "alldebrid"
// alone does not say.
type AccountView struct {
	Service string `json:"service"`
	Account string `json:"account,omitempty"`
	// Label is the name the user gave this account, empty when they never
	// gave one. It is what a notification should read out; Service and
	// Account are what a script should branch on.
	Label string `json:"label,omitempty"`
	Tier  string `json:"tier,omitempty"`
	// Expiry is RFC3339, matching app.AccountHealth.Expiry exactly - the
	// same string the accounts page already shows, not a re-formatted one.
	Expiry string `json:"expiry,omitempty"`
}

// CaptchaView is what TriggerCaptchaPending carries: enough to say what is
// waiting and where, never the challenge payload itself. The image data, the
// site key and the context URL are all deliberately absent - a script cannot
// answer a captcha (there is no such Action, and the package doc comment's
// "no HTTP API" entry says why one solved by a script's own outbound request
// is not a thing this sandbox offers), so handing it the material to try
// would only be an exfiltration route with no legitimate use behind it.
type CaptchaView struct {
	ID     string `json:"id"`
	TaskID string `json:"taskId,omitempty"`
	Host   string `json:"host"`
	// Kind is captcha.Kind's own string ("image", "click", "widget",
	// "unsupported"), copied rather than imported for the reason TaskView's
	// doc comment gives.
	Kind   string `json:"kind"`
	Prompt string `json:"prompt,omitempty"`
	// ExpiresAt is RFC3339, or "" when the challenge carries no deadline.
	// It is the field a "you have two minutes" notification needs.
	ExpiresAt string `json:"expiresAt,omitempty"`
}
