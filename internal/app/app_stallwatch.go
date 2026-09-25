package app

// Stall detection: a transfer still "running" that has moved no bytes for too
// long. A dead connection does not fail or time out on this side; it keeps its
// slot and reports 0 B/s, so it needs a watcher rather than an error branch.
//
// A task waiting on a captcha is passed over and its clock reset, since
// restarting it would throw away a challenge already raised. A hoster's
// countdown, a link refresh or a reconnect also move no bytes, and the backends
// report them exactly like a dead socket. That is why the timeout is
// configurable with a floor (settings.MinStallTimeout) and why the mark only
// states what is measurable.
//
// What follows the mark is Engine.Reconnect, not the router reconnect above:
// the engine drops the transfer's connections and asks for the rest of the
// file on new ones, which costs nothing it has fetched and keeps the slot. It repeats once per timeout while
// the transfer stands still. Restarting from the top is a separate switch, off
// by default, and takes over from the second timeout on.
//
// The state is package-level and keyed by *App, like captchaState.

import (
	"log"
	"slices"
	"sync"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/settings"
)

// stallCheckInterval is how often the watcher looks at what is running. It sets
// the resolution of the mark, not the timeout.
const stallCheckInterval = 5 * time.Second

// stallState is one App's stall bookkeeping.
type stallState struct {
	startOnce sync.Once
	// seen is the last byte count each running task was observed at, and when
	// it was first seen at that count. Only a pass holding a.mu touches it.
	seen map[string]stallSample
	// marked is the ids currently carrying a mark. A task can leave the running
	// set by paths this file does not own (RestartTasks), and with the feature
	// off the map is empty, so clearing marks never walks every task.
	marked map[string]bool
}

// stallSample is one reading. since is when the bytes stopped, not when the
// watcher noticed, because that is what a row shows. reconnected is when the
// watcher last reconnected this standstill, and the next timeout counts from
// there.
type stallSample struct {
	loaded      int64
	since       time.Time
	reconnected time.Time
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

// ensureStallWatcher starts the watch loop once per App. It is called from
// dispatchLocked, like ensureCaptchaPoller, which runs early at start-up and
// on nearly everything after.
func (a *App) ensureStallWatcher() {
	st := a.stallStateFor()
	st.startOnce.Do(func() { a.spawn(a.stallWatchLoop) })
}

// stallWatchLoop runs until a.ctx is done, so Close waits for it.
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
// what has started again, and reconnect or restart what the settings say to.
// now is a parameter so a test can drive a long standstill instantly.
func (a *App) stallPass(now time.Time) {
	a.mu.Lock()
	changed, reconnect, restart := a.markStallsLocked(now)
	a.mu.Unlock()
	if len(changed) > 0 {
		a.publishTasks(changed)
	}
	// Off the lock: both talk to the backend. Not App.Pause and App.Resume,
	// which would give the slot away and send the task through the queue.
	for _, id := range reconnect {
		if a.Engine.Reconnect(a.torrentFiles.engineIDFor(id)) {
			log.Printf("task %s moved no bytes, so its connections were opened again", id)
		}
	}
	for _, id := range restart {
		a.restartStalled(id)
	}
}

// markStallsLocked writes the mark onto what has stopped moving and takes it
// off what has started again, and reports which tasks the settings want
// reconnected and which restarted. Caller holds a.mu.
func (a *App) markStallsLocked(now time.Time) (changed []taskCopy, reconnect, restart []string) {
	st := a.stallStateFor()
	cfg := a.Settings.Get()
	timeout := time.Duration(cfg.StallTimeout) * time.Second
	// Marks that no longer apply come off first: the transfer settled, was
	// paused or restarted by hand, or the feature was switched off.
	for id := range st.marked {
		if timeout > 0 && a.active[id] {
			if t := a.tasks[id]; t != nil && t.Status == core.StatusRunning {
				continue
			}
		}
		delete(st.marked, id)
		if t := a.tasks[id]; t != nil && !t.StalledSince.IsZero() {
			t.StalledSince = time.Time{}
			changed = append(changed, a.copyLocked(t))
		}
	}
	if timeout <= 0 {
		clear(st.seen)
		return changed, nil, nil
	}
	// Keep seen the size of the running set.
	for id := range st.seen {
		if !a.active[id] {
			delete(st.seen, id)
		}
	}
	for id := range a.active {
		t := a.tasks[id]
		if t == nil || t.Status != core.StatusRunning {
			// Extracting, or between a terminal update and the slot's release.
			continue
		}
		// A task waiting on a captcha has its clock reset, so the wait does not
		// count once the challenge is answered. So does a torrent a debrid
		// service is still fetching: nothing comes here until it is done, which
		// for a torrent it has not cached can take hours.
		if a.captchaWaitingLocked(id) || t.Remote != nil {
			st.seen[id] = stallSample{loaded: t.Loaded, since: now}
			if !t.StalledSince.IsZero() {
				t.StalledSince = time.Time{}
				delete(st.marked, id)
				changed = append(changed, a.copyLocked(t))
			}
			continue
		}
		prev, ok := st.seen[id]
		if !ok || t.Loaded != prev.loaded {
			st.seen[id] = stallSample{loaded: t.Loaded, since: now}
			if !t.StalledSince.IsZero() {
				// Bytes again, so the mark clears itself.
				t.StalledSince = time.Time{}
				delete(st.marked, id)
				changed = append(changed, a.copyLocked(t))
			}
			continue
		}
		due := prev.since
		if prev.reconnected.After(due) {
			due = prev.reconnected
		}
		if now.Sub(due) < timeout {
			continue
		}
		if t.StalledSince.IsZero() {
			// When the bytes stopped, so the row counts from the real moment.
			t.StalledSince = prev.since
			st.marked[id] = true
			changed = append(changed, a.copyLocked(t))
			log.Printf("task %s has moved no bytes for %s", id, now.Sub(prev.since).Truncate(time.Second))
		}
		// A reconnect keeps the bytes, so it comes first. A standstill it did
		// not end is restarted when the restart is switched on.
		restartDue := a.stallRestartDueLocked(t, cfg)
		switch {
		case cfg.StallReconnect && a.Engine.Reconnectable(a.torrentFiles.engineIDFor(id)) && (prev.reconnected.IsZero() || !restartDue):
			reconnect = append(reconnect, id)
			prev.reconnected = now
			st.seen[id] = prev
		case restartDue:
			restart = append(restart, id)
		}
	}
	return changed, reconnect, restart
}

// stallRestartDueLocked reports whether this marked task should be handed back
// to the queue automatically. Caller holds a.mu.
func (a *App) stallRestartDueLocked(t *core.Task, cfg settings.Settings) bool {
	if !cfg.StallRestart {
		return false
	}
	// Never a torrent: a stalled torrent has no peers, and the restart deletes
	// the partial data before handing the same magnet to the same swarm. Nor a
	// download imported from a debrid account, whose files come one after
	// another like a torrent's and would all be deleted. The mark is still
	// written.
	if t.Resolver == "torrent" || t.InfoHash != "" || importedTask(t) {
		return false
	}
	limit := cfg.StallMaxRestarts
	if limit <= 0 {
		limit = settings.DefaultStallRestarts
	}
	return t.StallRestarts < limit
}

// restartStalled hands one stalled transfer back to the wait queue and starts
// it over.
//
// It restarts the way RestartTasks does. The bytes fetched so far are dropped,
// which is why this is opt-in, waits for a reconnect to have failed where one
// is possible, and is capped by settings.StallMaxRestarts. The checks are
// repeated under the lock because the task may have been paused, deleted,
// finished or started moving since the pass decided.
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
	// The backend starts from the top; its first update writes the real figure.
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
	// Copied after the dispatch, which may already have started or refused it.
	var c taskCopy
	if live := a.tasks[id]; live != nil {
		c = a.copyLocked(live)
	}
	a.mu.Unlock()

	if c.ID == "" {
		return
	}
	log.Printf("task %s stood still for %s and was started again (restart %d)", id, stalledFor, restarts)
	a.publish(&c)
}
