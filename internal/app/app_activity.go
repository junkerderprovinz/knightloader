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

import (
	"context"
	"strings"
	"sync"
)

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
	// ActivityContainer counts encrypted containers handed to the JD backend
	// and not yet collected. Unlike the four above it is published from the
	// api package (routes_containers.go owns the relay that knows the count),
	// which is what SetContainerActivity below exists for.
	ActivityContainer ActivityKind = "container"
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
	// Cancellable is how many of the active units registered a stop handle,
	// and it is what puts a stop button on a row of the status strip.
	//
	// A count rather than a bool because it is derived from the same map
	// AbortActivity walks, so the two can never disagree - and because "two of
	// the three crawls running can be called off" is a true sentence the
	// frontend is entitled to render however it likes.
	//
	// Not every kind can offer one. A captcha poll is a timer nobody is
	// waiting on, and an auto-confirm pass is over before a button could be
	// pressed; a page crawl is minutes of somebody else's server not
	// answering, which is why it is the kind this exists for.
	Cancellable int `json:"cancellable"`
}

// activityState is one App's counters, one active/total pair per kind, plus the
// stop handles of the runs that have one.
type activityState struct {
	mu     sync.Mutex
	active map[ActivityKind]int
	total  map[ActivityKind]int
	// cancels is the live stop handle of every cancellable run, per kind, keyed
	// by a number that is unique for the life of this App. A map and not a
	// slice because a run retires by deleting its own entry and runs do not
	// finish in the order they started.
	cancels map[ActivityKind]map[uint64]context.CancelFunc
	// next hands out those keys. It never wraps in any run this program will
	// ever have.
	next uint64
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
		st = &activityState{
			active:  map[ActivityKind]int{},
			total:   map[ActivityKind]int{},
			cancels: map[ActivityKind]map[uint64]context.CancelFunc{},
		}
		activityReg[a] = st
	}
	return st
}

// signalLocked is one kind's counters as they stand, for the broadcast. Called
// with st.mu held: every field it reads is guarded by that lock, and building
// the message inside the same critical section as the mutation is what keeps
// two concurrent callers' broadcasts from reaching a client in the opposite
// order from the changes that produced them - see beginActivity below.
func (st *activityState) signalLocked(kind ActivityKind) Activity {
	return Activity{
		Kind:        kind,
		Active:      st.active[kind],
		Total:       st.total[kind],
		Cancellable: len(st.cancels[kind]),
	}
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
	a.Hub.Broadcast("activity", st.signalLocked(kind))
}

// startActivityRun is beginActivity for ONE unit that can be called off, and
// returns the func that retires it again.
//
// It is deliberately not a second stream. A run appears on the status strip
// through the same counters and the same "activity" message every other kind
// uses; all this adds is the handle that AbortActivity pulls, published as
// Activity.Cancellable so the strip knows there is something to press.
//
// The returned func is safe to call more than once and is meant to be
// deferred; only the first call counts, because a run that retired twice would
// take a second, unrelated unit of the same kind down with it.
func (a *App) startActivityRun(kind ActivityKind, cancel context.CancelFunc) func() {
	st := a.activityStateFor()
	st.mu.Lock()
	st.next++
	id := st.next
	if st.cancels[kind] == nil {
		st.cancels[kind] = map[uint64]context.CancelFunc{}
	}
	st.cancels[kind][id] = cancel
	st.active[kind]++
	st.total[kind]++
	a.Hub.Broadcast("activity", st.signalLocked(kind))
	st.mu.Unlock()

	var once sync.Once
	return func() {
		once.Do(func() {
			st.mu.Lock()
			defer st.mu.Unlock()
			// The handle goes and the unit retires in ONE critical section. Two
			// sections would leave a window in which another goroutine's
			// broadcast reports a run that is still active with nothing left to
			// stop it, which on the strip is a row with a button that does
			// nothing.
			delete(st.cancels[kind], id)
			a.retireLocked(st, kind, 1)
		})
	}
}

// AbortActivity calls off every run of one kind that registered a stop handle,
// and reports how many that was.
//
// The handles are NOT removed here. A run deletes its own the moment it
// actually settles, which is the only point at which it is over; clearing them
// here would take the stop button off the strip while the work was still
// unwinding, and a person watching a row that goes on counting would press
// something else.
func (a *App) AbortActivity(kind ActivityKind) int {
	st := a.activityStateFor()
	st.mu.Lock()
	stops := make([]context.CancelFunc, 0, len(st.cancels[kind]))
	for _, c := range st.cancels[kind] {
		stops = append(stops, c)
	}
	st.mu.Unlock()

	// Called outside the lock. A CancelFunc does not run the cancelled work's
	// own cleanup itself, but the goroutine it wakes takes this very mutex on
	// its way out, and holding it while handing out cancellations is how that
	// becomes a deadlock the first time one of them settles quickly.
	for _, c := range stops {
		c()
	}
	return len(stops)
}

// KnownActivityKind turns a kind a caller names into one of the five, and
// refuses anything else - the same shape, for the same reason, as
// KnownOrigin (app_links.go): a route that acted on a free-text kind would
// silently do nothing for a typo instead of saying so.
func KnownActivityKind(s string) (ActivityKind, bool) {
	k := ActivityKind(strings.ToLower(strings.TrimSpace(s)))
	for _, known := range activityOrder {
		if k == known {
			return k, true
		}
	}
	return "", false
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
	a.retireLocked(st, kind, n)
}

// retireLocked is endActivity's body with the lock already held, so that the
// plain path and startActivityRun's stop func settle a kind by exactly the same
// rule. The second caller has to drop its handle and its unit together, and a
// second copy of this ordering is how one of them ends up publishing a total
// the other has already reset.
func (a *App) retireLocked(st *activityState, kind ActivityKind, n int) {
	st.active[kind] -= n
	if st.active[kind] < 0 {
		// A caller over-reporting its own completions is a bug worth seeing
		// on the strip as "0", never as a count that reads backwards.
		st.active[kind] = 0
	}
	// Built before the possible reset below, or the very broadcast that is
	// meant to show "0 of N" - the burst's own final word - would report
	// "0 of 0" instead, because Total had already been zeroed for the NEXT
	// burst before this one's last message was built.
	sig := st.signalLocked(kind)
	if sig.Active == 0 {
		st.total[kind] = 0
	}
	a.Hub.Broadcast("activity", sig)
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
	a.Hub.Broadcast("activity", st.signalLocked(kind))
}

// SetContainerActivity publishes how many handed-over containers are still
// waiting to be collected. Exported because the count lives in the api
// package's own relay (routes_containers.go) rather than in this one - every
// other kind is published by the app-package code that does the work, but a
// container's whole lifetime is that relay's map, and duplicating it here
// would mean two counts that can disagree.
//
// A gauge, not a burst: like captcha there is no fixed batch size for "how
// many are outstanding right now" to be a fraction of.
func (a *App) SetContainerActivity(n int) {
	a.setActivityGauge(ActivityContainer, n)
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
		out[i] = st.signalLocked(k)
	}
	return out
}

// activityOrder is every kind ActivitySnapshot reports, fixed so a snapshot
// is never missing one just because it has never fired on this App yet.
var activityOrder = []ActivityKind{ActivityCrawl, ActivityLinkCheck, ActivityCaptcha, ActivityAutoConfirm, ActivityContainer}
