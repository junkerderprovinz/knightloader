package app

// Background activity: work KnightLoader does on its own, such as a crawl, an
// availability recheck, the captcha poll or an auto-confirm pass. All kinds
// share one hub message ("activity") so the status strip renders whichever are
// active from one channel. The per-challenge captcha messages in
// app_captcha.go are separate.
//
// Each App's state lives in a package-level map keyed by *App, like the
// captcha and hoster-auth state.

import (
	"context"
	"strings"
	"sync"
)

// ActivityKind is one source the status strip renders. It is a fixed set with
// counters rather than free text, so a translated UI can render it.
type ActivityKind string

const (
	ActivityCrawl       ActivityKind = "crawl"
	ActivityLinkCheck   ActivityKind = "linkcheck"
	ActivityCaptcha     ActivityKind = "captcha"
	ActivityAutoConfirm ActivityKind = "autoconfirm"
	// ActivityContainer counts encrypted containers handed to the JD backend
	// and not yet collected. The api package publishes it through
	// SetContainerActivity, since its relay holds the count.
	ActivityContainer ActivityKind = "container"
)

// Activity is the hub's "activity" message: what one kind of background work
// is currently doing. For burst kinds Active never exceeds Total; gauge kinds
// (captcha, container) publish a live count with Active equal to Total.
type Activity struct {
	Kind   ActivityKind `json:"kind"`
	Active int          `json:"active"`
	Total  int          `json:"total"`
	// Cancellable is how many active units registered a stop handle, which
	// puts a stop button on the strip. It is derived from the same map
	// AbortActivity walks, so the two cannot disagree.
	Cancellable int `json:"cancellable"`
}

// activityState is one App's counters per kind, plus the stop handles of the
// runs that have one.
type activityState struct {
	mu     sync.Mutex
	active map[ActivityKind]int
	total  map[ActivityKind]int
	// cancels holds each cancellable run's stop handle per kind, keyed by an id
	// unique for the App's lifetime. Runs finish in any order, hence a map.
	cancels map[ActivityKind]map[uint64]context.CancelFunc
	next    uint64
}

var (
	activityMu  sync.Mutex
	activityReg = map[*App]*activityState{}
)

// activityStateFor returns this App's activity state, building it on first
// use.
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

// signalLocked returns one kind's current counters. Caller holds st.mu.
func (st *activityState) signalLocked(kind ActivityKind) Activity {
	return Activity{
		Kind:        kind,
		Active:      st.active[kind],
		Total:       st.total[kind],
		Cancellable: len(st.cancels[kind]),
	}
}

// beginActivity adds n units of kind. Overlapping callers add into the same
// counters. The broadcast happens under the lock so that concurrent updates
// reach clients in the order they were made; Hub.Broadcast never blocks.
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

// startActivityRun counts one cancellable unit of kind and returns the func
// that retires it. The returned func is meant to be deferred and only its first
// call counts, so a double call cannot retire an unrelated unit.
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
			// Handle and unit go together, or a broadcast in between would show
			// an active run with nothing to stop it.
			delete(st.cancels[kind], id)
			a.retireLocked(st, kind, 1)
		})
	}
}

// AbortActivity cancels every run of kind that registered a stop handle and
// returns how many that was. The handles stay until each run settles, so the
// stop button does not vanish while the work is still unwinding.
func (a *App) AbortActivity(kind ActivityKind) int {
	st := a.activityStateFor()
	st.mu.Lock()
	stops := make([]context.CancelFunc, 0, len(st.cancels[kind]))
	for _, c := range st.cancels[kind] {
		stops = append(stops, c)
	}
	st.mu.Unlock()

	// Outside the lock: the woken goroutine takes st.mu on its way out.
	for _, c := range stops {
		c()
	}
	return len(stops)
}

// KnownActivityKind parses a kind a caller names and refuses anything else, so
// a typo in a route is an error rather than a silent no-op.
func KnownActivityKind(s string) (ActivityKind, bool) {
	k := ActivityKind(strings.ToLower(strings.TrimSpace(s)))
	for _, known := range activityOrder {
		if k == known {
			return k, true
		}
	}
	return "", false
}

// endActivity retires n units of kind. When active reaches zero the total
// resets too, so the next burst counts from zero.
func (a *App) endActivity(kind ActivityKind, n int) {
	if n <= 0 {
		return
	}
	st := a.activityStateFor()
	st.mu.Lock()
	defer st.mu.Unlock()
	a.retireLocked(st, kind, n)
}

// retireLocked is endActivity with st.mu held, shared with startActivityRun's
// stop func so both settle a kind the same way.
func (a *App) retireLocked(st *activityState, kind ActivityKind, n int) {
	st.active[kind] -= n
	if st.active[kind] < 0 {
		st.active[kind] = 0
	}
	// Built before the reset, so the burst's last message reads "0 of N".
	sig := st.signalLocked(kind)
	if sig.Active == 0 {
		st.total[kind] = 0
	}
	a.Hub.Broadcast("activity", sig)
}

// setActivityGauge publishes a live count for a kind without a batch size.
func (a *App) setActivityGauge(kind ActivityKind, n int) {
	st := a.activityStateFor()
	st.mu.Lock()
	defer st.mu.Unlock()
	st.active[kind] = n
	st.total[kind] = n
	a.Hub.Broadcast("activity", st.signalLocked(kind))
}

// SetContainerActivity publishes how many handed-over containers are still
// waiting to be collected. The count lives in the api package's relay.
func (a *App) SetContainerActivity(n int) {
	a.setActivityGauge(ActivityContainer, n)
}

// ActivitySnapshot reports every kind's counters, idle ones included, for a
// client that just connected. The client replaces its whole map with it, so a
// missing kind would leave a stale entry behind.
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

// activityOrder is every kind ActivitySnapshot reports.
var activityOrder = []ActivityKind{ActivityCrawl, ActivityLinkCheck, ActivityCaptcha, ActivityAutoConfirm, ActivityContainer}
