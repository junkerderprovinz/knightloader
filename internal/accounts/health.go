package accounts

// health.go: a real state machine for one account's reachability, kept apart
// from Credential (accounts.go) on purpose - health is never a secret, and it
// changes on its own, from what a download attempt reports back, not from
// what a person typed into a form.
//
// Before this, a bad key's only trace was the TASK it happened to be
// attached to: core.Task.Reason on that one task, set by app_errors.go's
// classify() and read by nothing at the account level. So a revoked API key
// kept being handed the next queued link, and the next after that, forever -
// every attempt failed the same way and nothing ever rolled that up into "stop
// trying this one." HealthState is that roll-up, and Tracker is where it is
// kept: persisted, so a restart does not forget a key is dead, and separate
// from any one task, so forty tasks sharing a bad key see one verdict instead
// of forty independent failures.
//
// This package stays provider-agnostic - it knows core.Reason (the taxonomy
// every backend's failure already collapses to) and nothing about AllDebrid,
// Real-Debrid or TorBox specifically. Turning "this looked like an auth
// failure" into "this service's own error code says the key is dead" needs
// to know which service sent it, and that knowledge already lives in
// internal/app/app_accounts.go's checkCredential - see
// internal/app/app_health.go's refineState for where it is applied.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/core"
)

// HealthState is where one account currently stands. Only HealthOK routes
// normally; every other value means dispatch should pass this account over -
// see app_dispatch.go's resolverForTaskLocked, which skips a non-OK account
// without touching the resolver registry (docs/build-plan.md package 14, row
// 2: a benched account stays registered, or every task that could only ever
// have gone there hard-fails the moment it is skipped instead of held).
type HealthState string

const (
	// HealthOK is the default for an account nothing has ever reported
	// anything bad about, and what a struggling account returns to the
	// moment it proves itself again (ReportSuccess). It is also what the
	// zero Record reads as - see Tracker.Get - so an account that has never
	// failed needs no entry in the store at all.
	HealthOK HealthState = "ok"
	// HealthInvalid is a credential the service rejected outright: a wrong
	// or revoked key, a login that does not authenticate, an account banned
	// outright. The only fix is a person entering a different credential -
	// see Tracker.Reset, which SetAccountCredential (app_accounts.go) calls
	// on every save for exactly that reason. Never set on a guess: see
	// ClassifyReason and row 4 of package 14 - a global outage must not read
	// as this.
	HealthInvalid HealthState = "invalid"
	// HealthExpired is a credential that authenticates fine but whose
	// premium access has lapsed or was never bought - the service is
	// explicitly saying "pay or renew", not "this key is wrong." Distinct
	// from HealthInvalid because the fix is different, and telling a
	// perfectly valid key to be regenerated is advice that goes nowhere.
	HealthExpired HealthState = "expired"
	// HealthTempDisabled is everything shaped like it might clear up on its
	// own: a rate limit, the service answering 5xx, a network failure
	// reaching it, or an auth-shaped rejection with nothing more specific
	// behind it. It is the ONLY state that carries a BenchedUntil and the
	// only one a probe is ever scheduled against (see Tracker.ReportFailure
	// and app_health.go's scheduleProbe) - and it is the required default
	// whenever a failure cannot be told apart from a global outage (row 4).
	HealthTempDisabled HealthState = "temp_disabled"
	// HealthError is a specific, named condition the service reported that
	// is neither of the above: geo/IP-blocked, an account locked pending
	// support. Real and diagnosable, but a new key does not fix it, waiting
	// does not fix it, and staying registered-but-skipped is still the
	// right routing answer - so it exists as its own value rather than
	// being folded into HealthInvalid or HealthTempDisabled, either of
	// which would tell the user the wrong next step.
	HealthError HealthState = "error"
)

// Usable reports whether an account in this state should still be routed to.
// Only OK is, and so is the zero value - an id nothing has ever reported
// anything about is exactly as usable as one that has actively proven itself.
func (s HealthState) Usable() bool {
	return s == HealthOK || s == ""
}

// ClassifyReason turns a task's already-computed core.Reason into the
// generic account-health verdict that failure implies BEFORE anything
// service-specific is applied - see app_health.go's refineState for the
// layer that knows AllDebrid's and Real-Debrid's own error vocabularies and
// may promote this into HealthInvalid or HealthExpired. This layer never
// produces either of those on its own: the taxonomy it reads is shared by
// every backend in the app, so a wrong guess here would be wrong for all of
// them at once.
//
// applicable is false for a failure that says something about the LINK or
// about this machine, never about whether the account itself still works -
// core.ReasonGone (a dead link), core.ReasonUnsupported (nothing claims it),
// core.ReasonCaptcha (needs a human), core.ReasonDiskFull and
// core.ReasonCancelled (both local). Reporting any of those to the account
// would bench a perfectly good key over one dead link, holding back every
// OTHER link routed through it.
func ClassifyReason(reason core.Reason) (state HealthState, applicable bool) {
	switch reason {
	case core.ReasonGone, core.ReasonUnsupported, core.ReasonCaptcha,
		core.ReasonDiskFull, core.ReasonCancelled:
		return "", false
	case core.ReasonAuth, core.ReasonLimit, core.ReasonUnavailable,
		core.ReasonNetwork, core.ReasonUnknown:
		// Row 4, spelled out here because it is the one rule the whole
		// package answers to: a rate limit, a down service, an unreachable
		// host and an auth rejection with nothing more specific behind it
		// are indistinguishable from here, and TempDisabled is the only one
		// of the four states that is safe to be wrong about - it costs one
		// extra probe at worst. Guessing HealthInvalid instead does not
		// self-correct; a person has to notice and re-enter a key that was
		// fine all along.
		return HealthTempDisabled, true
	default:
		// A reason this switch does not know about yet. Silence, not a
		// guess: a new core.Reason added later must be a deliberate
		// decision about whether it says anything about the account, not
		// something that starts benching accounts because it happened to
		// fall through.
		return "", false
	}
}

// Record is what is known about one account's health right now.
type Record struct {
	State HealthState `json:"state"`
	// Detail is the last failure's sentence (or the probe's), kept for
	// whatever eventually shows this on the accounts page - the same role
	// core.Task.Error plays for a download.
	Detail string `json:"detail,omitempty"`
	// BenchedUntil is when the one scheduled probe fires. Set only while
	// State is HealthTempDisabled - zero for every other state, including
	// one that started as HealthTempDisabled and has since cleared or been
	// superseded.
	BenchedUntil time.Time `json:"benchedUntil,omitempty"`
	// CheckedAt is when this Record was last written - by a real failure, a
	// successful download, a manual test, or the bench-expiry probe.
	CheckedAt time.Time `json:"checkedAt,omitempty"`
	// BenchCount is how many HealthTempDisabled episodes have run
	// back-to-back with no intervening success. It is what the bench
	// duration grows from (see app_health.go's benchDelay) - a key that has
	// been dead for a day should not still be probed every fifteen minutes.
	// ReportSuccess resets it to zero.
	BenchCount int `json:"benchCount,omitempty"`
}

// key mirrors accountKey (accounts.go) and metaKey (app_accounts.go): the
// default account is the bare service id, a named one is service+NUL+account.
// Kept as its own copy rather than shared, because accountKey strips a stray
// NUL a caller might type and every caller reaching this package has already
// had that chance to matter upstream of it - see accountKey's own comment for
// why the stripping is load-bearing there and would only be redundant here.
func key(service, account string) string {
	if account == "" {
		return service
	}
	return service + "\x00" + account
}

// Tracker persists a Record per (service, account). It is the health
// equivalent of Store (accounts.go): its own small, unencrypted JSON file
// beside accounts.json, because health is never a secret and has no business
// inside the sealed store - the same reasoning app_accounts.go's acctMeta
// already applies to Enabled and Label.
type Tracker struct {
	path string

	mu   sync.Mutex
	data map[string]Record
}

// OpenTracker loads (or initialises) the health store rooted at dir - the
// same data directory accounts.Open and app.acctMetaPath already use. It
// never fails: a missing or unreadable file reads the same as "nothing has
// ever been recorded," exactly as loadAcctMetaLocked treats account_meta.json
// - health is diagnostic, and a health file this app cannot parse must not
// be able to stop it from starting.
func OpenTracker(dir string) *Tracker {
	t := &Tracker{path: filepath.Join(dir, "account_health.json"), data: map[string]Record{}}
	if b, err := os.ReadFile(t.path); err == nil {
		_ = json.Unmarshal(b, &t.data)
	}
	return t
}

// Get returns what is known about one account, or the zero Record (State
// HealthOK, nothing else set) if nothing has ever been reported about it.
func (t *Tracker) Get(service, account string) Record {
	t.mu.Lock()
	defer t.mu.Unlock()
	r := t.data[key(service, account)]
	if r.State == "" {
		r.State = HealthOK
	}
	return r
}

// Usable is Get(...).State.Usable(), spelled out so a routing call site does
// not need to know Record's shape at all - the one question app_dispatch.go
// actually asks.
func (t *Tracker) Usable(service, account string) bool {
	return t.Get(service, account).State.Usable()
}

// ReportSuccess clears an account back to HealthOK. A download that actually
// went through is the strongest signal there is, stronger than any probe, so
// it wins outright: an account benched five minutes ago that just unlocked a
// link for real has nothing left to prove, whatever the clock says.
func (t *Tracker) ReportSuccess(service, account string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	k := key(service, account)
	if r, ok := t.data[k]; !ok || r.State == HealthOK {
		return // nothing to clear, and nothing worth persisting for it either
	}
	t.data[k] = Record{State: HealthOK, CheckedAt: time.Now()}
	t.flushLocked()
}

// ReportFailure records one failure against (service, account). benchFor is
// the caller's chosen bench duration for a FRESH transition into
// HealthTempDisabled (see app_health.go's benchDelay) - this type has no
// business owning backoff policy, only recording what it is told to.
//
// started reports whether THIS call is the one that put the account into
// HealthTempDisabled for a new bench episode: true only on the
// not-already-benched -> HealthTempDisabled edge, false for every call that
// lands on an account already benched (piling another failure onto an
// existing bench neither pushes BenchedUntil out nor counts as a second
// episode). This is what lets the caller schedule EXACTLY ONE probe per
// bench (row 3): under concurrent callers - forty tasks failing within the
// same millisecond, all sharing one dead key - mu serialises them, so only
// the very first one ever observes the edge and every other one sees the
// account already benched and gets started=false.
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

// Reset drops whatever was recorded for (service, account), back to a clean
// slate. SetAccountCredential (app_accounts.go) calls this on every save -
// clearing a credential drops a stale verdict that must not resurface if the
// slot is configured again later (the same reasoning deleteAccountMeta
// already applies to Enabled/Label), and saving a NEW one means the key
// deserves its own trial rather than inheriting the old one's history.
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

// flushLocked persists the whole map. Caller holds mu.
func (t *Tracker) flushLocked() {
	b, err := json.MarshalIndent(t.data, "", "  ")
	if err != nil {
		return
	}
	_ = os.WriteFile(t.path, b, 0o600)
}
