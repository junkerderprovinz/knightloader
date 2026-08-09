package app

// The wait queue: what is in it, in which order, and the master switch that
// decides whether anything leaves it at all.

import (
	"sort"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/schedule"
)

// StartTasks moves collected tasks into the download queue and dispatches them.
// An empty id list starts every collected task.
func (a *App) StartTasks(ids []string) {
	want := map[string]bool{}
	for _, id := range ids {
		want[id] = true
	}
	all := len(ids) == 0
	a.mu.Lock()
	var toStart []*core.Task
	for id, t := range a.tasks {
		// Skipped is the holding area, and it is why the flag is on the task
		// rather than a note kept somewhere else: "start everything" reaches every
		// collected link, and a filtered one has to be out of that reach without
		// being out of the record. Restore is the only way it starts.
		if t.Status == core.StatusCollected && !t.Skipped && (all || want[id]) {
			toStart = append(toStart, t)
		}
	}
	sort.Slice(toStart, func(i, j int) bool { return toStart[i].CreatedAt.Before(toStart[j].CreatedAt) })
	for _, t := range toStart {
		t.Status = core.StatusQueued
		t.Error = ""
		// The typed reason goes with the sentence, everywhere and always. Left
		// standing it outlives what produced it, and the interface would advise
		// about a dead link while the task is running again.
		t.Reason = core.ReasonUnknown
		t.Speed = 0
		a.queue = append(a.queue, t.ID)
	}
	a.dispatchLocked()
	// Snapshotted after dispatching, never before. Dispatch settles the tasks it
	// refuses — a filtered link, a taken destination — and a copy taken above
	// would carry "queued" with the error cleared, which is exactly what would
	// then be written over the refusal in the store and on screen.
	copies := make([]core.Task, 0, len(toStart))
	for _, t := range toStart {
		copies = append(copies, *t)
	}
	a.mu.Unlock()
	for i := range copies {
		c := copies[i]
		_ = a.Store.Save(&c)
		a.Hub.Broadcast("task", &c)
	}
}

// RestartTasks re-runs finished or errored tasks from scratch: their backend
// state is cleared and they re-enter the download queue. Empty ids = every
// errored task.
func (a *App) RestartTasks(ids []string) {
	want := map[string]bool{}
	for _, id := range ids {
		want[id] = true
	}
	all := len(ids) == 0
	a.mu.Lock()
	type reset struct {
		id string
		be backend
	}
	var targets []reset
	for id, t := range a.tasks {
		restartable := t.Status == core.StatusError || (t.Status == core.StatusDone && !all)
		if restartable && (all || want[id]) {
			targets = append(targets, reset{id, a.backendFor(t.Resolver)})
			t.Status = core.StatusQueued
			t.Error = ""
			t.Reason = core.ReasonUnknown
			t.Loaded = 0
			t.Speed = 0
			delete(a.active, id)
			delete(a.started, id) // dispatch will hand it to the backend fresh
		}
	}
	a.mu.Unlock()

	// Clear any leftover backend state before re-queuing.
	for _, r := range targets {
		r.be.Remove(r.id, true)
	}

	a.mu.Lock()
	var live []*core.Task
	for _, r := range targets {
		if t := a.tasks[r.id]; t != nil {
			a.queue = append(a.queue, r.id)
			// Filed again: settling took it out of the mirror set, and a task that
			// is live once more has to block a second copy of its own link.
			a.dupes.Add(linkEntry(t))
			live = append(live, t)
		}
	}
	a.dispatchLocked()
	// After dispatching, for the same reason as in StartTasks: a copy taken
	// before it would write "queued, no error" over a task dispatch just refused.
	copies := make([]core.Task, 0, len(live))
	for _, t := range live {
		copies = append(copies, *t)
	}
	a.mu.Unlock()
	for i := range copies {
		c := copies[i]
		_ = a.Store.Save(&c)
		a.Hub.Broadcast("task", &c)
	}
}

// sortQueueLocked puts the wait queue in the order the user asked for: higher
// priority first, then the manual position, then oldest first. Caller holds a.mu.
func (a *App) sortQueueLocked() {
	sort.SliceStable(a.queue, func(i, j int) bool {
		x, y := a.tasks[a.queue[i]], a.tasks[a.queue[j]]
		if x == nil || y == nil {
			return y == nil && x != nil
		}
		// Forced outranks priority, because it is not a priority: it is the answer
		// to "everything else can wait, fetch this one", and a forced link sitting
		// behind a high-priority package would be the one thing it was asked not to
		// do. It only reorders the queue — a forced link still waits for a slot,
		// since starting past MaxConcurrent is a separate decision with per-host and
		// per-account caps behind it.
		if x.Forced != y.Forced {
			return x.Forced
		}
		if x.Priority != y.Priority {
			return x.Priority > y.Priority
		}
		if x.Position != y.Position {
			return x.Position < y.Position
		}
		return x.CreatedAt.Before(y.CreatedAt)
	})
}

// SetPriority lifts or drops tasks in the wait queue. Higher runs first; the
// change takes effect immediately for anything not already downloading.
func (a *App) SetPriority(ids []string, priority int) {
	if priority < -2 {
		priority = -2
	}
	if priority > 2 {
		priority = 2
	}
	a.mu.Lock()
	var copies []core.Task
	for _, id := range ids {
		if t := a.tasks[id]; t != nil {
			t.Priority = priority
			copies = append(copies, *t)
		}
	}
	a.dispatchLocked()
	a.mu.Unlock()
	a.saveAndBroadcast(copies)
}

// MoveTasks reorders the queue by hand. where is "top" or "bottom"; everything
// keeps its priority, so a moved task still waits behind higher-priority work.
func (a *App) MoveTasks(ids []string, where string) {
	a.mu.Lock()
	min, max := 0, 0
	for _, t := range a.tasks {
		if t.Position < min {
			min = t.Position
		}
		if t.Position > max {
			max = t.Position
		}
	}
	var copies []core.Task
	for i, id := range ids {
		t := a.tasks[id]
		if t == nil {
			continue
		}
		if where == "top" {
			t.Position = min - len(ids) + i
		} else {
			t.Position = max + 1 + i
		}
		copies = append(copies, *t)
	}
	a.dispatchLocked()
	a.mu.Unlock()
	a.saveAndBroadcast(copies)
}

// QueueState is the master switch and the stop mark, as the UI sees them.
type QueueState struct {
	Halted   bool   `json:"halted"`
	StopMark string `json:"stopMark,omitempty"`
	// Running is how many downloads are actually in flight, which is what makes
	// "halted" legible: halted with three running means three still finishing.
	Running int `json:"running"`
}

// Queue reports the master switch.
func (a *App) Queue() QueueState {
	a.mu.Lock()
	defer a.mu.Unlock()
	return QueueState{Halted: a.halted, StopMark: a.stopMark, Running: len(a.active)}
}

// SetHalted stops or resumes handing queued tasks to a backend. Halting leaves
// what is already downloading alone, because killing a transfer mid-file throws
// away work the user did not ask to lose.
func (a *App) SetHalted(halted bool) {
	a.mu.Lock()
	// Recorded as the manual switch as well as the effective one. The schedule
	// evaluates against the manual flag, so a stop made by hand survives the end
	// of a window instead of being lifted by it — and the runner is deliberately
	// not woken, so a manual release inside a pause window holds until the next
	// boundary rather than being reversed a millisecond later.
	a.manualHalt = halted
	a.halted = halted
	if !halted {
		// Resuming clears the stop mark: it has served its purpose, and leaving
		// it armed would halt the queue again at the next finished download for
		// a reason nobody would connect to a click made minutes ago.
		a.stopMark = ""
		a.dispatchLocked()
	}
	a.mu.Unlock()
	a.Hub.Broadcast("queue", a.Queue())
}

// SetStopMark arms the queue to halt once this task finishes. An empty id
// disarms it.
func (a *App) SetStopMark(id string) {
	a.mu.Lock()
	if id == "" || a.tasks[id] != nil {
		a.stopMark = id
	}
	a.mu.Unlock()
	a.Hub.Broadcast("queue", a.Queue())
}

// dequeueLocked removes id from the wait queue. Caller holds a.mu.
func (a *App) dequeueLocked(id string) {
	for i, q := range a.queue {
		if q == id {
			a.queue = append(a.queue[:i], a.queue[i+1:]...)
			return
		}
	}
}

// scheduleBase is what the queue does when no window applies: the halt the user
// set by hand and the speed limit they configured. It is read fresh on every
// pass of the runner, so a stop made during a pause window is still in force
// when that window ends rather than being lifted along with it.
func (a *App) scheduleBase() schedule.State {
	a.mu.Lock()
	paused := a.manualHalt
	a.mu.Unlock()
	return schedule.State{Paused: paused, Limit: a.Settings.Get().SpeedLimit}
}

// applySchedule puts the state the timetable arrived at into effect. It runs on
// the runner's own goroutine and only when the answer changed, so it does the
// cheap work and hands the slow work off.
//
// It writes the halt flag and never the stop mark. The mark is the user's own
// "finish this, then stop", and clearing it at the end of a nightly window would
// throw away an instruction nobody could connect to anything they did.
func (a *App) applySchedule(st schedule.State) {
	a.mu.Lock()
	a.halted = st.Paused
	if !st.Paused {
		a.dispatchLocked()
	}
	a.mu.Unlock()
	a.Throttle.Set(st.Limit)
	// JD lives on its own box and is told over the network, so it is pushed off
	// this goroutine: a slow or unreachable JD must not delay the next boundary.
	go a.pushJDSpeedLimit(st.Limit)
	a.Hub.Broadcast("queue", a.Queue())
}

// ScheduleState is the timetable, what it says right now, and when that changes.
type ScheduleState struct {
	Entries []schedule.Entry `json:"entries"`
	State   schedule.State   `json:"state"`
	// Next is nil when the answer never changes again, which is what an empty
	// timetable has. A UI can then say "throttled until 06:00" instead of showing
	// a table the user has to read themselves.
	Next *time.Time `json:"next"`
}

// ScheduleState reports the timetable and the state it currently implies. The
// schedule is recompiled for the read rather than borrowed from the runner: it
// is a handful of rows, and a getter on the runner would be state two goroutines
// could disagree about.
func (a *App) ScheduleState() ScheduleState {
	entries := a.Settings.Get().Schedule
	s := schedule.Compile(entries)
	base := a.scheduleBase()
	now := time.Now()
	out := ScheduleState{Entries: entries, State: s.At(now, base)}
	if n, ok := s.Next(now, base); ok {
		out.Next = &n
	}
	if out.Entries == nil {
		out.Entries = []schedule.Entry{}
	}
	return out
}
