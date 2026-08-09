package app

// The wait queue: what is in it, in which order, and the master switch that
// decides whether anything leaves it at all.

import (
	"sort"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/rules"
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

// --- Who an action is about -----------------------------------------------

// Selection names the tasks one queue action is about.
//
// Every verb below takes one, because every one of them is offered over forty
// rows as readily as over a single one. A route that can only take one id turns
// a selection into forty requests, forty store writes and forty broadcasts,
// which is slow enough to look broken and can fail halfway.
type Selection struct {
	// Ids are the tasks named outright, which is what a selection on screen is.
	Ids []string `json:"ids,omitempty"`
	// Package is a whole package by name.
	//
	// It is a pointer because the empty string is a legitimate package name — it
	// is the ungrouped one — and a plain string cannot tell that apart from "not
	// by package at all". It is worth having beside Ids: the ids a list can send
	// are the rows that survived its filter, and "send this package to the top"
	// has to carry the rows the filter hid with it, or the package arrives there
	// in pieces.
	Package *string `json:"package,omitempty"`
	// All widens the selection to every task the verb can touch.
	//
	// It has to be asked for. An empty request is a client that meant to name
	// something and did not, and quietly reading that as "all of them" is the
	// worst possible way to report the mistake — the queue rebuilds itself and
	// nothing says why.
	All bool `json:"all,omitempty"`
}

// pickLocked resolves a selection to live tasks, oldest first so that two calls
// naming the same set name it in the same order. keep drops the tasks a verb
// cannot act on; nil keeps everything. Caller holds a.mu.
func (a *App) pickLocked(sel Selection, keep func(*core.Task) bool) []*core.Task {
	seen := make(map[string]bool, len(sel.Ids))
	var out []*core.Task
	add := func(t *core.Task) {
		if t == nil || seen[t.ID] || (keep != nil && !keep(t)) {
			return
		}
		seen[t.ID] = true
		out = append(out, t)
	}
	if sel.All {
		for _, t := range a.tasks {
			add(t)
		}
	}
	for _, id := range sel.Ids {
		add(a.tasks[id])
	}
	if sel.Package != nil {
		for _, t := range a.tasks {
			if t.Package == *sel.Package {
				add(t)
			}
		}
	}
	sortByAge(out)
	return out
}

// idsOf is the answer every selection verb gives back: what was actually
// touched, so that nothing has to re-read the whole list to find out which.
func idsOf(in []*core.Task) []string {
	out := make([]string, 0, len(in))
	for _, t := range in {
		out = append(out, t.ID)
	}
	return out
}

// movable is a task that still has somewhere to go in the wait order. A
// finished or failed download has no place left in it, and letting a one-step
// move step over one would spend a press of the button on nothing the user can
// see.
func movable(t *core.Task) bool {
	return t.Status != core.StatusDone && t.Status != core.StatusError
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

// --- Priority ---------------------------------------------------------------

// The seven priorities, JDownloader's own set, so that the muscle memory of
// everybody arriving from it carries over.
//
// Priority is a pure interface enum. The only thing it does is order the wait
// queue: no backend reads it, it buys no bandwidth, it lifts no host limit, and
// a task at the highest priority still waits for a free slot exactly as one at
// the lowest does. Saying that out loud is the point — a control that looked
// like it made a download faster would be pressed for that, and it does not.
const (
	PriorityLowest  = -3
	PriorityLower   = -2
	PriorityLow     = -1
	PriorityDefault = 0
	PriorityHigh    = 1
	PriorityHigher  = 2
	PriorityHighest = 3
)

// PriorityChoice is one entry of the enum as the interface offers it: the value
// that goes on the task, and a stable id the browser translates.
//
// There is deliberately no label. The server does not know which of the shipped
// locales a given browser is showing, and two clients of one instance routinely
// differ — a translated sentence sent from here is the wrong language for one
// of them.
type PriorityChoice struct {
	ID    string `json:"id"`
	Value int    `json:"value"`
}

// Priorities is the list the menu is built from, highest first, so that an
// entry which exists is always a value the queue implements.
func Priorities() []PriorityChoice {
	return []PriorityChoice{
		{ID: "highest", Value: PriorityHighest},
		{ID: "higher", Value: PriorityHigher},
		{ID: "high", Value: PriorityHigh},
		{ID: "default", Value: PriorityDefault},
		{ID: "low", Value: PriorityLow},
		{ID: "lower", Value: PriorityLower},
		{ID: "lowest", Value: PriorityLowest},
	}
}

// clampPriority keeps a value inside the enum rather than refusing it: the
// queue can order any integer, so a client one version ahead is not worth a
// 400. The bound itself lives in internal/rules because a Packagizer rule
// writes this same field, and two bounds that disagree mean a rule can hand a
// task a priority the interface has no control able to undo.
func clampPriority(p int) int {
	if p < rules.PriorityMin {
		return rules.PriorityMin
	}
	if p > rules.PriorityMax {
		return rules.PriorityMax
	}
	return p
}

// SetPriority is the id-taking form the per-task routes call. Everything new
// goes through SetPriorityIn, which can also be handed a whole package.
func (a *App) SetPriority(ids []string, priority int) {
	a.SetPriorityIn(Selection{Ids: ids}, priority)
}

// SetPriorityIn puts a selection at one of the seven priorities. It takes
// effect immediately for everything not already downloading; a transfer in
// flight is never interrupted to honour an ordering decision, because the bytes
// it has already fetched are worth more than the order.
func (a *App) SetPriorityIn(sel Selection, priority int) []string {
	priority = clampPriority(priority)
	a.mu.Lock()
	chosen := a.pickLocked(sel, nil)
	// Which of them actually change band. A task carries its manual position
	// with it, and that position was a statement about the band it is leaving:
	// left as it stands, a link somebody sent to the bottom of "normal" arrives
	// at the TOP of "highest", ahead of links that were there all along, for a
	// reason nobody looking at the list could reconstruct.
	arrived := map[string]bool{}
	for _, t := range chosen {
		if t.Priority != priority && movable(t) {
			arrived[t.ID] = true
		}
		t.Priority = priority
	}
	// They join at the end of the band, keeping the order they had among
	// themselves. Last inside a higher band is still sooner than first inside a
	// lower one, which is what was asked for, and promoting a band somebody has
	// already ordered by hand must not scramble it.
	moved := a.renumberLocked(arrived, MoveBottom)
	copies := make([]core.Task, 0, len(chosen)+len(moved))
	named := make(map[string]bool, len(chosen))
	for _, t := range chosen {
		named[t.ID] = true
		copies = append(copies, *t) // after the renumbering, so the position is current
	}
	// The incumbents the arrivals pushed down: they changed too, and a browser
	// that never hears about it draws the old order until something else
	// happens to redraw the row.
	for _, c := range moved {
		if !named[c.ID] {
			copies = append(copies, c)
		}
	}
	a.dispatchLocked()
	a.mu.Unlock()
	a.saveAndBroadcast(copies)
	return idsOf(chosen)
}

// --- The manual order -------------------------------------------------------

// The four ways a selection changes place in the wait order. They are the whole
// vocabulary on purpose: anything finer is a drag-and-drop reorder, which has
// to arrive as one ordered list in one request — two browsers interleaving
// move-up and move-down produce an order neither of them asked for.
const (
	MoveTop    = "top"
	MoveUp     = "up"
	MoveDown   = "down"
	MoveBottom = "bottom"
)

// MoveTasks is the id-taking form the per-task routes call.
func (a *App) MoveTasks(ids []string, where string) {
	a.MoveIn(Selection{Ids: ids}, where)
}

// MoveIn changes where a selection sits in the wait order.
//
// The move stays inside the selection's own priority band, and that is a
// promise rather than a limitation. Priority outranks the manual position in
// the comparator, so a task lifted above a higher-priority one sorts straight
// back to where it was: the button would do nothing at all every few presses,
// which is far worse than a button that says what it can reach. Crossing a
// whole band is what the priority control is for.
//
// Positions are renumbered densely inside each band the move touches, which is
// what makes a single step expressible at all: the old scheme wrote min-1 and
// max+1, so it could say "before everything" and "after everything" and had no
// way to say "one place".
//
// The dense run is negative, ending at -1. A task nobody has moved carries
// position zero, so numbering the band from zero upwards would put every link
// pasted after a move ahead of the ones already ordered — a queue that reshuffles
// itself when you paste. Ending below zero leaves the untouched tasks where they
// belong: at the back, in the order they arrived. It is also why the numbers
// cannot drift: every renumbering lands in the same bounded run.
func (a *App) MoveIn(sel Selection, where string) []string {
	switch where {
	case MoveTop, MoveUp, MoveDown, MoveBottom:
	default:
		// Refused rather than defaulted. The old form read everything that was not
		// "top" as "bottom", so a typo in a client sent a selection to the end of
		// the queue and was answered with a 204.
		return nil
	}
	a.mu.Lock()
	chosen := a.pickLocked(sel, movable)
	if len(chosen) == 0 {
		a.mu.Unlock()
		return nil
	}
	want := make(map[string]bool, len(chosen))
	for _, t := range chosen {
		want[t.ID] = true
	}
	copies := a.renumberLocked(want, where)
	a.dispatchLocked()
	a.mu.Unlock()
	a.saveAndBroadcast(copies)
	return idsOf(chosen)
}

// renumberLocked applies one rearrangement to every band that holds a wanted
// task and hands back the tasks whose place actually changed, ready to publish.
//
// It is shared by the manual moves and by a priority change, because the two
// are the same operation seen from either side: something arrives in a band and
// the band has to come out with one unambiguous order. Caller holds a.mu.
func (a *App) renumberLocked(want map[string]bool, where string) []core.Task {
	if len(want) == 0 {
		return nil
	}
	var copies []core.Task
	for _, band := range a.bandsLocked() {
		if !reorder(band, want, where) {
			continue
		}
		for i, t := range band {
			pos := i - len(band)
			if t.Position == pos {
				continue
			}
			t.Position = pos
			copies = append(copies, *t)
		}
	}
	return copies
}

// bandsLocked groups the movable tasks into the runs the manual position
// actually orders: one band per forced/priority pair, each already in wait
// order. Two tasks in different bands are separated by something that outranks
// position, so shuffling positions between them changes nothing on screen and
// nothing in the dispatcher. Caller holds a.mu.
func (a *App) bandsLocked() [][]*core.Task {
	type key struct {
		forced   bool
		priority int
	}
	groups := map[key][]*core.Task{}
	for _, t := range a.tasks {
		if !movable(t) {
			continue
		}
		k := key{forced: t.Forced, priority: t.Priority}
		groups[k] = append(groups[k], t)
	}
	out := make([][]*core.Task, 0, len(groups))
	for _, g := range groups {
		sort.SliceStable(g, func(i, j int) bool {
			if g[i].Position != g[j].Position {
				return g[i].Position < g[j].Position
			}
			return g[i].CreatedAt.Before(g[j].CreatedAt)
		})
		out = append(out, g)
	}
	return out
}

// reorder rearranges one band in place and reports whether the band held any of
// the wanted tasks.
//
// A one-step move is a block move: a selected run slides past the one
// unselected task beside it, all of it together. Swapping each selected task
// with its neighbour instead would have two adjacent selected rows swap with
// each other and cancel out, so a selection of three would sit still while a
// selection of one moved.
func reorder(band []*core.Task, want map[string]bool, where string) bool {
	hit := false
	for _, t := range band {
		if want[t.ID] {
			hit = true
			break
		}
	}
	if !hit {
		return false
	}
	switch where {
	case MoveTop, MoveBottom:
		picked := make([]*core.Task, 0, len(band))
		rest := make([]*core.Task, 0, len(band))
		for _, t := range band {
			if want[t.ID] {
				picked = append(picked, t)
			} else {
				rest = append(rest, t)
			}
		}
		// Each half keeps the order it had: a selection sent to the top arrives in
		// the order the user is looking at, not reversed or shuffled.
		if where == MoveTop {
			copy(band, append(picked, rest...))
		} else {
			copy(band, append(rest, picked...))
		}
	case MoveUp:
		for i := 1; i < len(band); i++ {
			if want[band[i].ID] && !want[band[i-1].ID] {
				band[i-1], band[i] = band[i], band[i-1]
			}
		}
	case MoveDown:
		for i := len(band) - 2; i >= 0; i-- {
			if want[band[i].ID] && !want[band[i+1].ID] {
				band[i], band[i+1] = band[i+1], band[i]
			}
		}
	}
	return true
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

// --- The hard stop ------------------------------------------------------------

// StopAll is the other stop, and it is a different button from the master
// switch on purpose. SetHalted stops the dispatcher and lets the transfers in
// flight run to the end; this one stops them where they are.
//
// The order of the three steps below is the whole implementation, and getting
// it wrong is not a subtle bug:
//
//   - the halt is written FIRST, under the same lock the snapshot is taken
//     under. Pause frees a slot and dispatches, so against a queue that is not
//     yet halted this loop refills every slot it empties: the list settles with
//     as many downloads running as it started with, only different ones, and
//     "stop everything" looks like it did nothing;
//   - the ids are copied out rather than ranged over. Pause takes a.mu itself,
//     so ranging a.active here would deadlock on the first entry — and letting
//     go of the lock to range it instead is a data race, because completion
//     events write that same map from the backends' own goroutines while this
//     runs;
//   - the backends are told last, outside the lock, because Pause reaches the
//     network for a JD or a debrid task and the whole app must not wait behind
//     a box that is not answering.
//
// It answers with what it stopped, so the interface can say so without
// re-reading the list.
func (a *App) StopAll() []string {
	a.mu.Lock()
	a.manualHalt = true
	a.halted = true
	ids := make([]string, 0, len(a.active))
	for id := range a.active {
		ids = append(ids, id)
	}
	a.mu.Unlock()
	// Sorted only so the answer is stable; map order would make two identical
	// stops report their work in two different orders.
	sort.Strings(ids)
	for _, id := range ids {
		a.Pause(id)
	}
	a.Hub.Broadcast("queue", a.Queue())
	return ids
}

// StopCost is what the hard stop would cost, worked out before it is paid.
type StopCost struct {
	// Running is how many transfers would be stopped.
	Running int `json:"running"`
	// Losing names the transfers that cannot be picked up where they stopped, so
	// the dialog can point at the rows rather than quoting a number nobody can
	// place.
	Losing []string `json:"losing"`
	// Bytes is what those transfers have already written and would have to fetch
	// again. It is what has been loaded, never the announced size: a 40 GB
	// download that has written nothing loses nothing.
	Bytes int64 `json:"bytes"`
	// Unknown is how many transfers nobody has asked the resume question about.
	// It is counted apart and never folded into Losing, because "we do not know"
	// is a different sentence from "you will lose 4.2 GB", and telling the second
	// when the first is true is exactly how people learn to click straight
	// through the dialog.
	Unknown int `json:"unknown"`
	// UnknownBytes is what those have written — worth showing as a maybe, worth
	// never showing as a loss.
	UnknownBytes int64 `json:"unknownBytes"`
}

// StopCost reports what a hard stop right now would throw away.
//
// The three answers to "does this resume" are three answers here. A transfer
// that resumes cleanly costs nothing and is not counted at all; one that cannot
// is counted in Bytes; one nobody has asked is counted apart. The warning is
// the entire reason the hard stop is a separate button, and a warning that
// overstates itself once is a warning nobody reads again.
func (a *App) StopCost() StopCost {
	a.mu.Lock()
	defer a.mu.Unlock()
	cost := StopCost{Losing: []string{}}
	for id := range a.active {
		t := a.tasks[id]
		if t == nil {
			continue
		}
		cost.Running++
		switch {
		case t.Resumable == nil:
			cost.Unknown++
			cost.UnknownBytes += t.Loaded
		case !*t.Resumable:
			cost.Losing = append(cost.Losing, id)
			cost.Bytes += t.Loaded
		}
	}
	sort.Strings(cost.Losing)
	return cost
}

// --- Starting now, and switching off --------------------------------------

// ForceDownload starts a selection now: it goes to the front of the wait order,
// past every priority, and past the two flags that would otherwise hold it —
// a link switched off or parked cannot be "started now" and left off.
//
// Forced is deliberately not an eighth priority. It is the answer to
// "everything else can wait, fetch this one", and a forced link queued behind a
// high-priority package would be the one thing it was asked not to do.
//
// Two things it does not do, both because doing them quietly would be worse
// than not doing them. It does not lift the master switch: a stopped queue is a
// decision about the whole box, and a per-link button is not where that gets
// undone — the interface says so instead of starting nothing in silence. And it
// does not open a second door into the queue for a staged link: those go
// through StartTasks first, which is the one place the collector's own rules
// are applied.
func (a *App) ForceDownload(sel Selection) []string {
	a.mu.Lock()
	chosen := a.pickLocked(sel, movable)
	ids := idsOf(chosen)
	var staged []string
	for _, t := range chosen {
		if t.Status == core.StatusCollected {
			staged = append(staged, t.ID)
		}
	}
	a.mu.Unlock()

	if len(staged) > 0 {
		// Never with an empty list: to StartTasks that means "start everything in
		// the collector", which is emphatically not what forcing three links asks
		// for.
		a.StartTasks(staged)
	}

	a.mu.Lock()
	copies := make([]core.Task, 0, len(ids))
	for _, id := range ids {
		t := a.tasks[id]
		if t == nil {
			continue // removed while the collector pass ran
		}
		t.Forced = true
		t.Enabled = true
		t.Hold = false
		if t.Status == core.StatusPaused {
			// A paused transfer is put back in the queue rather than resumed
			// directly: the dispatcher is the only thing that knows whether this
			// backend has seen the task before, and a Resume sent to one that has
			// not starts nothing.
			t.Status = core.StatusQueued
			t.Speed = 0
			a.dequeueLocked(id)
			a.queue = append(a.queue, id)
		}
		copies = append(copies, *t)
	}
	a.dispatchLocked()
	a.mu.Unlock()
	a.saveAndBroadcast(copies)
	return ids
}

// SetEnabledIn is the bulk switch for disabled links: a selection, a whole
// package, or every link that is currently off.
//
// Only the links the switch would actually move are touched. "Switch everything
// back on" over a thousand-row list must not write and broadcast a thousand
// tasks that were already on — the store write is the cheap half; a thousand
// rows repainting in every open browser is the half people notice.
func (a *App) SetEnabledIn(sel Selection, enabled bool) []string {
	a.mu.Lock()
	ids := idsOf(a.pickLocked(sel, func(t *core.Task) bool { return t.Enabled != enabled }))
	a.mu.Unlock()
	// Through SetEnabled rather than around it: that is the one place the flag is
	// written and the dispatcher is driven afterwards, and a second path that
	// skipped the dispatch would make switching a link on look like it did
	// nothing until something unrelated happened to touch the queue.
	return a.SetEnabled(ids, enabled)
}

// --- The arithmetic under the list -----------------------------------------

// QueueCounters is what the list says about itself: how much work is left, how
// fast it is going and when it would be finished.
type QueueCounters struct {
	// Files is every file still owed, disabled links included. A link switched
	// off is still a file the user added; it is just not going to move, and
	// dropping it from the count would make the list shorter than the list.
	Files int `json:"files"`
	// Disabled is how many of those are switched off, so the interface can
	// explain why the file count and the byte total do not describe each other.
	Disabled int `json:"disabled"`
	Running  int `json:"running"`
	// Remaining is the bytes still to fetch, and disabled links are left out of
	// it: they are not going to be fetched, so counting them would put a number
	// in front of the user that no amount of waiting ever works off.
	//
	// A file whose size is not known yet contributes nothing rather than a guess.
	Remaining int64 `json:"remaining"`
	// Speed is what the same set is moving at. It leaves out a link that was
	// switched off while it was still running — a rare window, but one where
	// counting the speed and not the bytes would put the two halves of the ETA
	// out of step and quietly shorten it.
	Speed int64 `json:"speed"`
	// ETA is seconds, and nil rather than zero when there is no answer — nothing
	// is moving, or nothing is left. Zero would render as "done in a moment",
	// which is the one thing a stalled queue must not say.
	ETA *int64 `json:"eta"`
}

// Counters computes the figures under the list.
//
// Three exclusions, and each is a different reason. A finished or failed
// download is out of all of them: it is not owed any more. A link still in the
// collector is out too — it has not been added to the queue at all, and an ETA
// that counted links nobody has started would move whenever somebody pasted
// something. A disabled link stays in the file count and leaves the byte total
// and the ETA, which is the whole point of the switch.
//
// Held links are deliberately not excluded. A hold is a pause the user means to
// lift, so the bytes are still owed; dropping them would make the figure jump
// every time somebody parks a row for a minute.
func (a *App) Counters() QueueCounters {
	a.mu.Lock()
	defer a.mu.Unlock()
	var c QueueCounters
	for _, t := range a.tasks {
		switch t.Status {
		case core.StatusDone, core.StatusError, core.StatusCollected:
			continue
		}
		c.Files++
		if t.Status == core.StatusRunning {
			c.Running++
		}
		if !t.Enabled {
			c.Disabled++
			continue
		}
		c.Speed += t.Speed
		if t.Size > t.Loaded {
			c.Remaining += t.Size - t.Loaded
		}
	}
	if c.Speed > 0 && c.Remaining > 0 {
		eta := c.Remaining / c.Speed
		c.ETA = &eta
	}
	return c
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
