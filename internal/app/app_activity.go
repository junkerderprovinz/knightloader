package app

// Ambient background activity: work KnightLoader does on its own, with
// nobody necessarily watching a progress bar for it - a crawl following a
// pasted page, an availability recheck, the captcha poll loop, an
// unattended auto-confirm pass. One hub message kind ("activity") carries
// all four, because the frontend status strip renders "whichever kinds are
// currently active" off one channel rather than subscribing to one signal
// per source - see build-plan.md section 3's Wave 9 table (9A) and section
// 8's own Wave 9 note: "one hub message {kind, active, total}, with [every
// source] publishing into that same channel rather than inventing their
// own."
//
// app_captcha.go's pollCaptchasOnce already owns "captcha"/"captchaResolved"
// for the per-challenge prompt modal (id, host, taskID, reason - a status
// strip has no use for any of that); this file adds a SEPARATE, aggregate
// signal alongside those broadcasts, never in place of them.
//
// Kept at package level and keyed by the owning *App, not as a field on App
// (app.go) - the same trade captchaState (app_captcha.go), hosterAuth
// (app_hosterauth.go) and accountHealthState (app_accounts.go) already
// document: app.go's struct is not this wave's file to grow, and a
// package-level map gives the same per-instance guarantee without touching
// it. Production runs exactly one App for the life of the process; a test
// suite that builds many discards each one quickly enough that the
// accumulated entries cost nothing that matters.

import "sync"

// ActivityKind is one of the four sources a status strip renders. Fixed and
// small on purpose, per the build-plan note quoted above: a typed job with
// counters, not a free-text status line a translated UI could not render
// without guessing what it means.
type ActivityKind string

const (
	ActivityCrawl       ActivityKind = "crawl"
	ActivityLinkCheck   ActivityKind = "linkcheck"
	ActivityCaptcha     ActivityKind = "captcha"
	ActivityAutoConfirm ActivityKind = "autoconfirm"
)

// Activity is the hub's "activity" message: what one kind of ambient work is
// doing right now.
//
// Active never exceeds Total for a burst kind (crawl, linkcheck,
// autoconfirm) - see beginActivity/endActivity. captcha is published
// through setActivityGauge instead, with Active and Total always equal:
// there is no fixed batch size for "how many challenges are outstanding
// right now" to be a fraction of, only a live count.
type Activity struct {
	Kind   ActivityKind `json:"kind"`
	Active int          `json:"active"`
	Total  int          `json:"total"`
}

// activityState is one App's counters, one active/total pair per kind.
type activityState struct {
	mu     sync.Mutex
	active map[ActivityKind]int
	total  map[ActivityKind]int
}

var (
	activityMu  sync.Mutex
	activityReg = map[*App]*activityState{}
)

// activityStateFor returns this App's activity wiring, building it on first
// use - the same lazy-registry shape captchaStateFor (app_captcha.go) and
// hosterAuth (app_hosterauth.go) already use, for the identical reason.
func (a *App) activityStateFor() *activityState {
	activityMu.Lock()
	defer activityMu.Unlock()
	st, ok := activityReg[a]
	if !ok {
		st = &activityState{active: map[ActivityKind]int{}, total: map[ActivityKind]int{}}
		activityReg[a] = st
	}
	return st
}

// beginActivity adds n units of kind, just discovered - the start of a
// burst, or more work joining one already running. Two overlapping callers
// (two browsers both pressing "recheck all") add into the same shared
// counters rather than each owning their own, which is why this takes a
// delta rather than setting an absolute value.
//
// The state mutation and the broadcast happen under the same lock,
// deliberately: Hub.Broadcast never blocks on a client write (its own doc
// comment), so holding a small per-App mutex across it is cheap, and it is
// what keeps two concurrent callers of the same kind from having their
// broadcasts reach a client in the opposite order from the mutations that
// produced them - the same reason pollCaptchasOnce (app_captcha.go) holds
// its own pollMu across every broadcast in one pass rather than releasing
// between them.
func (a *App) beginActivity(kind ActivityKind, n int) {
	if n <= 0 {
		return
	}
	st := a.activityStateFor()
	st.mu.Lock()
	defer st.mu.Unlock()
	st.active[kind] += n
	st.total[kind] += n
	a.Hub.Broadcast("activity", Activity{Kind: kind, Active: st.active[kind], Total: st.total[kind]})
}

// endActivity retires n units of kind that beginActivity previously counted.
//
// The moment active reaches zero, total resets with it. A status strip only
// ever renders a kind while Active>0 - see the frontend's own
// components/StatusStrip.tsx - so what Total meant to the burst that just
// finished has nowhere left to be read, and the next burst is entitled to
// start counting from zero rather than from whatever an unrelated earlier
// one left behind.
func (a *App) endActivity(kind ActivityKind, n int) {
	if n <= 0 {
		return
	}
	st := a.activityStateFor()
	st.mu.Lock()
	defer st.mu.Unlock()
	st.active[kind] -= n
	if st.active[kind] < 0 {
		// A caller over-reporting its own completions is a bug worth seeing
		// on the strip as "0", never as a count that reads backwards.
		st.active[kind] = 0
	}
	// Read before the possible reset below, or the very broadcast that is
	// meant to show "0 of N" - the burst's own final word - would report
	// "0 of 0" instead, because Total had already been zeroed for the NEXT
	// burst before this one's last message was built.
	active, total := st.active[kind], st.total[kind]
	if active == 0 {
		st.total[kind] = 0
	}
	a.Hub.Broadcast("activity", Activity{Kind: kind, Active: active, Total: total})
}

// setActivityGauge publishes a live count for a kind with no fixed batch
// size to be a fraction of - captcha's own "how many are outstanding right
// now", never a countdown from a known total. Active and Total are
// deliberately published equal; see Activity's own doc comment.
func (a *App) setActivityGauge(kind ActivityKind, n int) {
	st := a.activityStateFor()
	st.mu.Lock()
	defer st.mu.Unlock()
	st.active[kind] = n
	st.total[kind] = n
	a.Hub.Broadcast("activity", Activity{Kind: kind, Active: n, Total: n})
}

// ActivitySnapshot reports every kind's current counters, including the ones
// sitting idle at zero. Sent once to a client that just (re)connected - see
// serveWS - so a browser that was disconnected mid-burst starts from the
// truth instead of from whatever its last "activity" broadcast happened to
// say. All four kinds are included even at zero, not only the active ones:
// the frontend replaces its whole map with this one shot (the same "snapshot
// clears, single-kind messages merge" split a.Tasks() already uses), and a
// kind missing from that replacement would leave a stale entry with no way
// to ever clear it.
func (a *App) ActivitySnapshot() []Activity {
	st := a.activityStateFor()
	st.mu.Lock()
	defer st.mu.Unlock()
	out := make([]Activity, len(activityOrder))
	for i, k := range activityOrder {
		out[i] = Activity{Kind: k, Active: st.active[k], Total: st.total[k]}
	}
	return out
}

// activityOrder is every kind ActivitySnapshot reports, fixed so a snapshot
// is never missing one just because it has never fired on this App yet.
var activityOrder = []ActivityKind{ActivityCrawl, ActivityLinkCheck, ActivityCaptcha, ActivityAutoConfirm}
