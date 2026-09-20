package accounts

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/core"
)

// HealthState is where one account currently stands. Only HealthOK routes
// normally; any other state makes dispatch pass the account over while it
// stays registered, so tasks that can only go there are held rather than
// failed.
type HealthState string

const (
	// HealthOK is the default, and what the zero Record reads as, so an
	// account that has never failed needs no stored entry.
	HealthOK HealthState = "ok"
	// HealthInvalid is a credential the service rejected outright. Only a new
	// credential fixes it, so it is never set on a guess.
	HealthInvalid HealthState = "invalid"
	// HealthExpired is a credential that authenticates but whose premium
	// access has lapsed. The fix is renewing, not a new key.
	HealthExpired HealthState = "expired"
	// HealthTempDisabled covers failures that may clear on their own: a rate
	// limit, a 5xx, a network error, or an auth rejection with nothing more
	// specific. It is the only state with a BenchedUntil and a scheduled
	// probe, and the default whenever a failure could be a global outage.
	HealthTempDisabled HealthState = "temp_disabled"
	// HealthError is a specific condition that neither a new key nor waiting
	// fixes, such as an IP block or an account locked pending support.
	HealthError HealthState = "error"
)

// Usable reports whether an account in this state should still be routed to.
func (s HealthState) Usable() bool {
	return s == HealthOK || s == ""
}

// ClassifyReason turns a task's core.Reason into the generic health verdict
// it implies, before any service-specific refinement. It never returns
// HealthInvalid or HealthExpired, since core.Reason is shared by every
// backend. applicable is false for a failure about the link or this machine,
// which says nothing about the account.
func ClassifyReason(reason core.Reason) (state HealthState, applicable bool) {
	switch reason {
	case core.ReasonGone, core.ReasonUnsupported, core.ReasonCaptcha,
		core.ReasonDiskFull, core.ReasonCancelled:
		return "", false
	case core.ReasonAuth, core.ReasonLimit, core.ReasonUnavailable,
		core.ReasonNetwork, core.ReasonUnknown:
		// These cannot be told apart from an outage here. A wrong
		// TempDisabled costs one extra probe; a wrong Invalid needs a person
		// to re-enter a key that was fine.
		return HealthTempDisabled, true
	default:
		// A new core.Reason has to be classified on purpose before it can
		// bench accounts.
		return "", false
	}
}

// Record is what is known about one account's health.
type Record struct {
	State HealthState `json:"state"`
	// Detail is the last failure's (or probe's) message.
	Detail string `json:"detail,omitempty"`
	// BenchedUntil is when the scheduled probe fires. It is set only while
	// State is HealthTempDisabled.
	BenchedUntil time.Time `json:"benchedUntil,omitempty"`
	CheckedAt    time.Time `json:"checkedAt,omitempty"`
	// BenchCount counts consecutive HealthTempDisabled episodes without a
	// success in between; the bench duration grows with it.
	BenchCount int `json:"benchCount,omitempty"`
}

// key mirrors accountKey without stripping NULs, which callers of this
// package have already been through.
func key(service, account string) string {
	if account == "" {
		return service
	}
	return service + "\x00" + account
}

// Tracker persists a Record per (service, account) in its own unencrypted
// JSON file beside accounts.json, since health is never a secret.
type Tracker struct {
	path string

	mu   sync.Mutex
	data map[string]Record
}

// OpenTracker loads (or initialises) the health store rooted at dir. A
// missing or unreadable file reads as empty, so a broken health file cannot
// stop the app from starting.
func OpenTracker(dir string) *Tracker {
	t := &Tracker{path: filepath.Join(dir, "account_health.json"), data: map[string]Record{}}
	if b, err := os.ReadFile(t.path); err == nil {
		_ = json.Unmarshal(b, &t.data)
	}
	return t
}

// Get returns what is known about one account, or a Record with State
// HealthOK if nothing has been reported.
func (t *Tracker) Get(service, account string) Record {
	t.mu.Lock()
	defer t.mu.Unlock()
	r := t.data[key(service, account)]
	if r.State == "" {
		r.State = HealthOK
	}
	return r
}

// Usable reports whether one account should still be routed to.
func (t *Tracker) Usable(service, account string) bool {
	return t.Get(service, account).State.Usable()
}

// ReportSuccess clears an account back to HealthOK. A download that went
// through outranks any probe, whatever the bench clock says.
func (t *Tracker) ReportSuccess(service, account string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	k := key(service, account)
	if r, ok := t.data[k]; !ok || r.State == HealthOK {
		return
	}
	t.data[k] = Record{State: HealthOK, CheckedAt: time.Now()}
	t.flushLocked()
}

// ReportFailure records one failure. benchFor is the bench duration to use
// when this call moves the account into HealthTempDisabled; started reports
// whether it did. A failure on an account that is already benched neither
// extends the bench nor starts a new episode, so under concurrent failures
// exactly one caller sees started and schedules the probe.
func (t *Tracker) ReportFailure(service, account string, state HealthState, detail string, benchFor time.Duration) (rec Record, started bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	k := key(service, account)
	prev := t.data[k]
	now := time.Now()
	next := Record{State: state, Detail: detail, CheckedAt: now}
	if state == HealthTempDisabled {
		started = prev.State != HealthTempDisabled
		if started {
			next.BenchedUntil = now.Add(benchFor)
			next.BenchCount = prev.BenchCount + 1
		} else {
			next.BenchedUntil = prev.BenchedUntil
			next.BenchCount = prev.BenchCount
		}
	}
	t.data[k] = next
	t.flushLocked()
	return next, started
}

// Reset drops whatever was recorded for an account. It is called whenever a
// credential is saved, so a new key starts without the old one's history.
func (t *Tracker) Reset(service, account string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	k := key(service, account)
	if _, ok := t.data[k]; !ok {
		return
	}
	delete(t.data, k)
	t.flushLocked()
}

func (t *Tracker) flushLocked() {
	b, err := json.MarshalIndent(t.data, "", "  ")
	if err != nil {
		return
	}
	_ = os.WriteFile(t.path, b, 0o600)
}
