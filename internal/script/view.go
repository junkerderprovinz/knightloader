package script

import "time"

// TaskView is the read-only shape of one download a script may see. It is
// this package's own struct rather than core.Task, like rules.Candidate, and
// the caller copies the fields in. Status is a plain string with
// core.Task.Status's JSON values ("done", "error", "running", ...).
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
	// NextTry is when an automatic retry is due, zero for none pending.
	// ClassifyTaskUpdate uses it; scripts never see it, so they only ever
	// run on a final failure.
	NextTry   time.Time `json:"-"`
	CreatedAt time.Time `json:"createdAt"`
}

// ProgressPct is Loaded/Size as a percentage clamped to 0-100, and 0 while
// Size is unknown, so scripts need not guard the division themselves.
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

// ClassifyTaskUpdate decides which trigger, if any, a task snapshot represents.
//
// TriggerTaskFailed fires only once NextTry is zero. internal/app's onUpdate
// sets NextTry to the next retry deadline before broadcasting a task that is
// still in Error, and clears it only when retries are exhausted or pointless
// (ReasonDiskFull), so a failure script runs once rather than per backoff
// attempt. A task handed to the next resolver, or whose account was
// unroutable, is back in Queued by then and never reaches this as a failure.
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

// QueueView is the read-only shape of the wait queue a script may see: the
// counters app.QueueCounters carries.
type QueueView struct {
	Files    int `json:"files"`
	Disabled int `json:"disabled"`
	Running  int `json:"running"`
}

// IsQueueIdle mirrors app_idle.go's queueIdleForAction: the queue is idle when
// every remaining file is switched off. A paused or held task still counts as
// work left.
func IsQueueIdle(v QueueView) bool {
	return v.Files == v.Disabled
}

// PackageView is what TriggerPackageDone carries: counts, not a verdict. The
// event fires when nothing is left to wait for, even if some files failed,
// because one dead link would otherwise stop the package from ever finishing.
// The script decides what the counts mean:
//
//	if (pkg.failed > 0) { notify(pkg.name + ": " + pkg.failed + " missing"); return; }
//
// The counts are not a partition of Files: Disabled overlaps the others (a
// file switched off after it finished is both), and Files also counts tasks
// in none of the buckets, such as a link still in the collector.
type PackageView struct {
	Name  string `json:"name"`
	Files int    `json:"files"`
	Done  int    `json:"done"`
	// Failed is files that settled as failed with no retry pending; a file
	// between backoff attempts is still pending and holds the event off.
	Failed int `json:"failed"`
	// Skipped is files the link filter is holding. They are counted but never
	// hold the event off, since they wait on a person, not on work.
	Skipped int `json:"skipped"`
	// Disabled is files the user switched off, treated like Skipped.
	Disabled int `json:"disabled"`
	// Bytes is what was downloaded, summed from Loaded rather than Size, so a
	// failed half-file counts what is on disk.
	Bytes int64 `json:"bytes"`
}

// ExtractView is what TriggerExtractDone carries: one finished unpacking,
// shaped like app.ExtractJob without the fields that only matter while it
// runs.
type ExtractView struct {
	JobID   string `json:"jobId"`
	TaskID  string `json:"taskId,omitempty"`
	Name    string `json:"name"`
	Dir     string `json:"dir"`
	Package string `json:"package,omitempty"`
	OK      bool   `json:"ok"`
	Error   string `json:"error,omitempty"`
	// Password reports that the failure was a missing or wrong archive
	// password, the one case fixed by typing something in and retrying.
	Password bool  `json:"password,omitempty"`
	Files    int   `json:"files"`
	Bytes    int64 `json:"bytes"`
	Nested   int   `json:"nested,omitempty"`
}

// ReconnectView is what TriggerReconnectDone carries. OK and Changed are
// separate so a script can tell a run that kept the same address
// (reconnect.ErrUnchanged) from one that could not reach the router, without
// parsing Error. From and To are empty when the run never read an address.
type ReconnectView struct {
	OK      bool   `json:"ok"`
	Changed bool   `json:"changed"`
	From    string `json:"from,omitempty"`
	To      string `json:"to,omitempty"`
	Error   string `json:"error,omitempty"`
	// Checks is how many times the address was polled after the method ran,
	// which shows whether a failed run gave up early or used its whole budget.
	Checks int `json:"checks"`
}

// AccountView is what TriggerAccountExpired carries. Account is "" for a
// service's default login and the account id for another one on the same
// service, as in resolver.SlotID, so a person with two keys knows which one
// lapsed.
type AccountView struct {
	Service string `json:"service"`
	Account string `json:"account,omitempty"`
	// Label is the name the user gave this account, if any: what a
	// notification should show. Scripts branch on Service and Account.
	Label string `json:"label,omitempty"`
	Tier  string `json:"tier,omitempty"`
	// Expiry is RFC3339, the same string app.AccountHealth.Expiry holds.
	Expiry string `json:"expiry,omitempty"`
}

// CaptchaView is what TriggerCaptchaPending carries: what is waiting and where,
// never the image, site key or context URL. A script cannot solve a captcha,
// so the material would only be a way to exfiltrate it.
type CaptchaView struct {
	ID     string `json:"id"`
	TaskID string `json:"taskId,omitempty"`
	Host   string `json:"host"`
	// Kind is captcha.Kind's string: "image", "click", "widget" or
	// "unsupported".
	Kind   string `json:"kind"`
	Prompt string `json:"prompt,omitempty"`
	// ExpiresAt is RFC3339, or "" when the challenge has no deadline.
	ExpiresAt string `json:"expiresAt,omitempty"`
}
