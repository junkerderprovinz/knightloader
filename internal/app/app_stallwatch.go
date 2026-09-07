package app

// Standing still: a transfer this app still calls "running" that has not moved
// a byte for long enough that nobody is coming to fix it.
//
// A dead connection reports nothing. It does not fail, it does not time out on
// this side, and it does not stop being "running" - it simply stops
// delivering, keeps the slot it holds, and is still there in the morning with
// the three per cent it had at midnight. Every other failure in this app
// arrives as an error somebody classified (app_errors.go); this one arrives as
// silence, which is why it needed a watcher rather than a branch.
//
// WHAT A STALL IS NOT, and this is the part worth reading:
//
// It is not "no bytes". A free-mode download sitting on a captcha moves nothing
// for as long as it takes a human to answer, and it is one of the healthiest
// rows in the queue - something IS expected to happen and there is a person who
// can make it happen. Marking it would put a warning on the one row that is
// merely waiting for its owner, and RESTARTING it would throw away a challenge
// that has already been raised and ask the hoster for a fresh one, which is a
// worse position than the one it was in. So a task internal/captcha is holding
// (captchaWaitingLocked) is passed over here, and its clock is reset while it
// waits, so the ten minutes somebody spent away from the keyboard do not count
// towards the timeout the moment they come back and answer.
//
// The honest limit of that, written down because it will be tempting to claim
// more later: a hoster's own free-user countdown, a link being re-fetched and a
// reconnect in progress also move no bytes, and this watcher cannot tell any of
// them from a dead socket - the backends report all of them as "running, 0
// B/s" and nothing carries the difference out. That is why the timeout is
// configurable instead of a constant, why it cannot be set below a minute
// (settings.MinStallTimeout), and above all why the MARK on its own does
// nothing but say what is measurably true. Acting on it is a second switch,
// off by default, that somebody has to turn on for themselves.
//
// The state lives at package level keyed by the owning *App rather than as a
// field on App, the same trade app_captcha.go, app_hosterauth.go and
// app_accounts.go already document: app.go's struct is not this file's to grow.

import (
	"log"
	"slices"
	"sync"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/settings"
)

// stallCheckInterval is how often the watcher looks at what is running.
//
// It sets the resolution of the mark, not the timeout: the recorded moment
// bytes last moved can be up to one interval late, so five seconds keeps a
// duration shown on a row honest to within five seconds of the truth while
// costing one walk of the active set - a map with at most MaxConcurrent entries
// in it - per tick. The budget loop next door already ticks at three seconds
// against more work than this.
const stallCheckInterval = 5 * time.Second

// stallState is one App's stall bookkeeping.
type stallState struct {
	startOnce sync.Once
	// seen is the last byte count each running task was observed at, and when
	// it was first seen at that count. It is read and written ONLY from a pass
	// that holds a.mu, which is what makes a plain map safe here: the pass is
	// the single writer, and everything it compares against (a.active, a.tasks)
	// is under that same lock.
	seen map[string]stallSample
	// marked is the ids currently carrying a mark, which is what lets a pass
	// take one back without walking the whole task list.
	//
	// It is worth a second map for two reasons. A task can leave the running
	// set by paths this file does not own - a hand restart (RestartTasks,
	// app_queue.go) is the plain one - and a mark nobody takes off then sits on
	// a queued row for ever. And with the feature off, which is the default,
	// this map is empty, so the whole pass is two map lookups rather than a walk
	// of every task in the list every five seconds.
	marked map[string]bool
}

// stallSample is one reading: the byte count, and the moment the count was
// first seen. since is deliberately "when the bytes stopped" and not "when we
// noticed", because that is the number a row has to show.
type stallSample struct {
	loaded int64
	since  time.Time
}

var (
	stallMu  sync.Mutex
	stallReg = map[*App]*stallState{}
)

// stallStateFor returns this App's stall bookkeeping, building it on first use.
func (a *App) stallStateFor() *stallState {
	stallMu.Lock()
	defer stallMu.Unlock()
	st, ok := stallReg[a]
	if !ok {
		st = &stallState{seen: map[string]stallSample{}, marked: map[string]bool{}}
		stallReg[a] = st
	}
	return st
}

// ensureStallWatcher starts the watch loop exactly once per App.
//
// Called from dispatchLocked for the reason ensureCaptchaPoller is: this
// package's files own no start-up hook of their own, and dispatchLocked is the
// closest thing to "runs once at start-up and on nearly everything after" -
// the schedule runner's first Apply reaches it before a browser could have
// loaded the page. Idempotent and cheap after the first call.
func (a *App) ensureStallWatcher() {
	st := a.stallStateFor()
	st.startOnce.Do(func() { a.spawn(a.stallWatchLoop) })
}

// stallWatchLoop runs until a.ctx is done, which is what makes Close wait for
// it - see a.spawn's own contract. Everything it writes goes through
// publishTasks, the same helper every other spawned writer in this package
// uses.
func (a *App) stallWatchLoop() {
	tick := time.NewTicker(stallCheckInterval)
	defer tick.Stop()
	for {
		select {
		case <-a.ctx.Done():
			return
		case <-tick.C:
			a.stallPass(time.Now())
		}
	}
}

// stallPass is one look at everything running: mark what has stopped, clear
// what has started again, and restart what the settings say to restart.
//
// now is a parameter rather than read inside, so a test can drive a
// twenty-minute standstill in a millisecond instead of waiting for one.
func (a *App) stallPass(now time.Time) {
	a.mu.Lock()
	changed, restart := a.markStallsLocked(now)
	a.mu.Unlock()
	if len(changed) > 0 {
		a.publishTasks(changed)
	}
	// Off the lock: restartStalled talks to a backend, which is a network call
	// on every delegated one.
	for _, id := range restart {
		a.restartStalled(id)
	}
}

// markStallsLocked writes the mark onto what has stopped moving and takes it
// off what has started again, and reports which tasks the settings want
// restarted. Caller holds a.mu.
func (a *App) markStallsLocked(now time.Time) (changed []core.Task, restart []string) {
	st := a.stallStateFor()
	cfg := a.Settings.Get()
	timeout := time.Duration(cfg.StallTimeout) * time.Second
	// Every mark that has stopped applying comes off first, and this one loop
	// covers all four ways that happens: the transfer settled, somebody paused
	// it, somebody restarted it by hand (RestartTasks, which this file does not
	// own and cannot be asked to clear it), or the feature itself was switched
	// off. A reading left on a row after the thing that writes it stopped is the
	// last reading it ever took, sitting there with nothing to update it.
	for id := range st.marked {
		if timeout > 0 && a.active[id] {
			if t := a.tasks[id]; t != nil && t.Status == core.StatusRunning {
				continue
			}
		}
		delete(st.marked, id)
		if t := a.tasks[id]; t != nil && !t.StalledSince.IsZero() {
			t.StalledSince = time.Time{}
			changed = append(changed, *t)
		}
	}
	if timeout <= 0 {
		clear(st.seen)
		return changed, nil
	}
	// Anything that is no longer being driven has no standstill to measure -
	// it finished, failed, was paused or was deleted. Dropped rather than kept,
	// so this map is the size of the running set and not of the history.
	for id := range st.seen {
		if !a.active[id] {
			delete(st.seen, id)
		}
	}
	for id := range a.active {
		t := a.tasks[id]
		if t == nil || t.Status != core.StatusRunning {
			// Extracting, and the brief moment between a terminal update and the
			// slot being released, are not transfers that have stopped moving.
			continue
		}
		// The captcha exemption - see this file's own comment for why a link
		// waiting on a human is the healthiest row in the queue rather than the
		// worst one. The clock is RESET, not merely not read, so the wait does
		// not count towards a timeout the moment the challenge is answered.
		if a.captchaWaitingLocked(id) {
			st.seen[id] = stallSample{loaded: t.Loaded, since: now}
			if !t.StalledSince.IsZero() {
				t.StalledSince = time.Time{}
				delete(st.marked, id)
				changed = append(changed, *t)
			}
			continue
		}
		prev, ok := st.seen[id]
		if !ok || t.Loaded != prev.loaded {
			st.seen[id] = stallSample{loaded: t.Loaded, since: now}
			if !t.StalledSince.IsZero() {
				// Bytes again: whatever it was, it is over, and the mark goes
				// with it. This is the half that makes the mark self-clearing,
				// the same property core.Waiting has.
				t.StalledSince = time.Time{}
				delete(st.marked, id)
				changed = append(changed, *t)
			}
			continue
		}
		if now.Sub(prev.since) < timeout {
			continue
		}
		if t.StalledSince.IsZero() {
			// The moment the bytes stopped, not the moment the timeout expired,
			// so the row counts up from when it actually went quiet.
			t.StalledSince = prev.since
			st.marked[id] = true
			changed = append(changed, *t)
			log.Printf("task %s has moved no bytes for %s", id, now.Sub(prev.since).Truncate(time.Second))
		}
		if a.stallRestartDueLocked(t, cfg) {
			restart = append(restart, id)
		}
	}
	return changed, restart
}

// stallRestartDueLocked answers whether this marked task should be handed back
// to the queue automatically. Caller holds a.mu.
func (a *App) stallRestartDueLocked(t *core.Task, cfg settings.Settings) bool {
	if !cfg.StallRestart {
		return false
	}
	// NEVER A TORRENT, whatever the settings say. A torrent that has stopped
	// moving has found nobody to move bytes with, and the restart path below
	// deletes the backend's partial data before asking again - so restarting
	// hands the identical magnet to the identical swarm, minus everything it
	// had already fetched. The mark is still written, because "this torrent has
	// found nobody" is exactly what somebody needs to see; it is only the
	// automatic action that is wrong here.
	if t.Resolver == "torrent" || t.InfoHash != "" {
		return false
	}
	limit := cfg.StallMaxRestarts
	if limit <= 0 {
		limit = settings.DefaultStallRestarts
	}
	return t.StallRestarts < limit
}

// restartStalled hands one standing-still transfer back to the wait queue and
// starts it over.
//
// It goes down the same road RestartTasks does - clear the backend's own state,
// requeue, dispatch - rather than pausing and resuming, because a resume asks
// the connection that has stopped answering to carry on, which is the one thing
// already known not to work. The cost is the same as a hand restart's: the
// bytes fetched so far are dropped with the backend's partial file. That cost
// is why this is opt-in, why it is capped (settings.StallMaxRestarts) and why
// the mark alone never triggers it.
//
// The checks are made again under the lock. Between the pass that decided and
// this call the task may have been paused, deleted, finished on its own or
// started moving again, and restarting any of those would be this feature
// undoing somebody else's decision.
func (a *App) restartStalled(id string) {
	a.mu.Lock()
	t := a.tasks[id]
	if t == nil || !a.active[id] || t.StalledSince.IsZero() {
		a.mu.Unlock()
		return
	}
	stalledFor := time.Since(t.StalledSince).Truncate(time.Second)
	be := a.backendFor(t.Resolver)
	t.StallRestarts++
	t.StalledSince = time.Time{}
	t.Status = core.StatusQueued
	t.Speed = 0
	// Zeroed like RestartTasks does, and for the same reason: the backend is
	// about to be handed this link from the top, so a bar frozen at sixty per
	// cent would be describing an attempt that no longer exists. The backend's
	// first update writes the real number back.
	t.Loaded = 0
	t.Note = ""
	delete(a.active, id)
	delete(a.started, id) // dispatch hands it out fresh rather than resuming
	st := a.stallStateFor()
	delete(st.seen, id)
	delete(st.marked, id)
	restarts := t.StallRestarts
	a.mu.Unlock()

	be.Remove(id, true)

	a.mu.Lock()
	if a.tasks[id] != nil && !slices.Contains(a.queue, id) {
		a.queue = append(a.queue, id)
	}
	a.dispatchLocked()
	// Taken after the dispatch, for the reason StartTasks takes its copies
	// there: a copy from before it would say "queued" about a task this very
	// pass may have already started or refused.
	var c core.Task
	if live := a.tasks[id]; live != nil {
		c = *live
	}
	a.mu.Unlock()

	if c.ID == "" {
		return
	}
	log.Printf("task %s stood still for %s and was started again (restart %d)", id, stalledFor, restarts)
	a.publishTasks([]core.Task{c})
}
