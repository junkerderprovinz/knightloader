package app

// The wait queue: what is in it, in which order, and the master switch that
// decides whether anything leaves it at all.

import (
	"errors"
	"fmt"
	"slices"
	"sort"
	"sync"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/dedupe"
	"github.com/junkerderprovinz/knightloader/internal/rules"
	"github.com/junkerderprovinz/knightloader/internal/schedule"
)

// StartResult is what a start actually did, so the interface can say why
// nothing moved: the queue was halted, the named tasks were held by a filter
// or disabled, or the ids matched nothing.
type StartResult struct {
	// Started is how many tasks left the collector for the queue.
	Started int `json:"started"`
	// Skipped is how many named tasks a link-filter rule is holding back. Only
	// Restore releases them.
	Skipped int `json:"skipped"`
	// Disabled is how many were passed over because their own switch is off.
	// It is separate from Skipped because the cure differs: turn the switch
	// back on rather than Restore.
	Disabled int `json:"disabled"`
	// Released reports that this start lifted a halt the user had set by hand,
	// so the interface can flip the master switch without waiting for a poll.
	Released bool `json:"released"`
	// Blocked reports that a schedule window is pausing the queue: the tasks
	// are queued and wait for the window to end. It is a separate flag because
	// the interface says a different sentence for it.
	Blocked bool `json:"blocked"`
}

// StartTasks moves collected tasks into the download queue and dispatches them.
// An empty id list starts every collected task.
//
// This is the entrance for automation (auto-confirm, a watch folder, a forced
// selection), and it never touches the master switch; otherwise a link arriving
// from the browser extension would restart a stopped queue.
// StartTasksByHand is the one that releases a halt.
func (a *App) StartTasks(ids []string) StartResult {
	return a.startTasks(ids, false)
}

// StartTasksByHand is StartTasks for a start somebody pressed: it also releases
// a halt they set by hand, since pressing start on a queue they stopped asks
// for exactly that. A schedule window is never overridden, only reported.
func (a *App) StartTasksByHand(ids []string) StartResult {
	return a.startTasks(ids, true)
}

func (a *App) startTasks(ids []string, byHand bool) StartResult {
	want := map[string]bool{}
	for _, id := range ids {
		want[id] = true
	}
	all := len(ids) == 0
	// Settings has its own lock; reading it first keeps the critical section
	// about a.tasks.
	cfg := a.Settings.Get()
	addAtTop := cfg.AddAtTop
	// The timetable's answer without the manual switch, which tells a halt the
	// user set from a configured window.
	scheduledPause := a.sched.Suspension().At(schedule.Compile(cfg.Schedule), time.Now(), schedule.State{Limit: cfg.SpeedLimit}).Paused
	var out StartResult
	a.mu.Lock()
	var toStart []*core.Task
	for id, t := range a.tasks {
		if t.Status != core.StatusCollected || !(all || want[id]) {
			continue
		}
		if t.Skipped {
			// Held by the filter: counted so the answer can say so, never
			// started. Only Restore releases it.
			out.Skipped++
			continue
		}
		if t.VariantOff {
			// Out of view by the host's preset, not by anybody's switch, so
			// it is not counted as disabled either.
			continue
		}
		// The user's own switch holds for a start by id as well as for "start
		// everything".
		if !t.Enabled {
			out.Disabled++
			continue
		}
		toStart = append(toStart, t)
	}
	// toStart was filled from a map, so equal stamps need a tiebreak that
	// does not change from one run to the next.
	sortByAge(toStart)
	for _, t := range toStart {
		t.Status = core.StatusQueued
		// A confirmed link has no countdown pending.
		t.ConfirmDue = time.Time{}
		t.Error = ""
		// The reason goes with the error sentence, or the interface would
		// advise about a dead link while the task runs again.
		t.Reason = core.ReasonUnknown
		t.Speed = 0
		a.queue = append(a.queue, t.ID)
	}
	out.Started = len(toStart)
	// A start by hand releases a halt the user set by hand; otherwise the tasks
	// would sit queued behind a.halted with nothing said. A schedule window is
	// never overridden, since it would have to be overridden again every night;
	// that case is reported through Blocked.
	if len(toStart) > 0 && a.halted {
		if byHand && a.manualHalt && !scheduledPause {
			a.manualHalt = false
			a.halted = false
			// As in SetHalted: a stop mark left armed would halt the queue
			// again at the next finished download.
			a.stopMark = ""
			out.Released = true
		} else {
			out.Blocked = true
		}
	}
	// AddAtTop: a batch leaving the collector plays next, using the same
	// renumbering as a manual "move to top". Only the tasks that just changed
	// status are moved, never the raw ids, so an unknown id cannot renumber a
	// band.
	var moved []core.Task
	if addAtTop && len(toStart) > 0 {
		atTop := make(map[string]bool, len(toStart))
		for _, t := range toStart {
			atTop[t.ID] = true
		}
		moved = a.renumberLocked(atTop, MoveTop)
	}
	a.dispatchLocked()
	// Copied after dispatching: dispatch settles the tasks it refuses (a
	// filtered link, a taken destination), and an earlier copy would write
	// "queued" over the refusal.
	named := make(map[string]bool, len(toStart))
	copies := make([]core.Task, 0, len(toStart)+len(moved))
	for _, t := range toStart {
		named[t.ID] = true
		copies = append(copies, *t)
	}
	// The tasks AddAtTop pushed down changed position too.
	for _, c := range moved {
		if !named[c.ID] {
			copies = append(copies, c)
		}
	}
	// A link whose last shown row just left the collector takes its set-aside
	// rows along out of the list.
	started := map[string]bool{}
	for _, t := range toStart {
		if t.Variant != "" {
			started[t.URL] = true
		}
	}
	var stranded []string
	if len(started) > 0 {
		stranded = a.strandedVariantRowsLocked(started, nil)
	}
	a.mu.Unlock()
	for i := range copies {
		c := copies[i]
		_ = a.Store.Save(&c)
		a.Hub.Broadcast("task", &c)
	}
	for _, id := range stranded {
		a.removeTask(id, false)
	}
	if len(toStart) > 0 {
		// Links left the collector, which may be all a countdown was waiting
		// for.
		a.wakeAutoConfirm()
	}
	// The master switch moved, so every surface watching it (other browsers,
	// the extension, the phone) is told. Only on a change, to avoid redrawing
	// it on every start.
	if out.Released {
		a.Hub.Broadcast("queue", a.Queue())
	}
	return out
}

// RestartTasks re-runs finished or errored tasks from scratch: backend state
// and resolver are cleared, and they re-enter the queue to be routed again.
// Empty ids means every errored task.
func (a *App) RestartTasks(ids []string) { a.RestartTasksIn(ids, nil) }

// RestartTasksIn is RestartTasks narrowed to failures with the given reasons, so
// dead links, spent allowances and a full disk need not be retried together.
//
// An empty reason list means every cause. core.ReasonUnknown ("") is a valid
// entry: unclassified failures are a group of their own. With both ids and
// reasons, the two intersect, so picking a cause never widens a selection.
func (a *App) RestartTasksIn(ids []string, reasons []core.Reason) {
	want := map[string]bool{}
	for _, id := range ids {
		want[id] = true
	}
	wantReason := map[core.Reason]bool{}
	for _, r := range reasons {
		wantReason[r] = true
	}
	byReason := len(wantReason) > 0
	all := len(ids) == 0
	a.mu.Lock()
	type reset struct {
		id string
		be backend
	}
	var targets []reset
	for id, t := range a.tasks {
		restartable := t.Status == core.StatusError || (t.Status == core.StatusDone && !all)
		if byReason && !wantReason[t.Reason] {
			continue
		}
		if restartable && (all || want[id]) {
			targets = append(targets, reset{id, a.backendFor(t.Resolver)})
			t.Status = core.StatusQueued
			t.Error = ""
			t.Reason = core.ReasonUnknown
			t.Loaded = 0
			t.Speed = 0
			// The file is fetched again, and how its archive was last unpacked
			// says nothing about the new one.
			t.Unpack = core.UnpackNone
			// This is the retry the failure was waiting for.
			t.NextTry = time.Time{}
			// Routing is decided again as well. What may have changed since the
			// failure (a key entered, an account added, a debrid service back
			// from its cool-down) is a routing change, and keeping the failed
			// backend would fail the same way. An empty resolver goes through
			// resolverForTaskLocked's search; a pin still decides.
			t.Resolver = ""
			t.Mode = core.ModeUnknown
			delete(a.active, id)
			delete(a.started, id) // dispatch will hand it to the backend fresh
			delete(a.fellBack, id)
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
			// Settling took it out of the mirror set; live again, it must block
			// a second copy of its link.
			a.dupes.Add(linkEntry(t))
			live = append(live, t)
		}
	}
	a.dispatchLocked()
	// Copied after dispatching, as in startTasks.
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

// UndoWindow is how long a removed selection can still be brought back. It is
// reported to the client with the token, so the message never outlives the bin.
const UndoWindow = 30 * time.Second

// binned is one removed task plus whether it was still waiting for a slot,
// which Status cannot say once the task is gone; a queued row restored without
// its place in a.queue would wait forever.
type binned struct {
	task   core.Task
	queued bool
}

// bin is one removal, kept whole: after a partial undo the missing rows would
// look as if they had been removed on purpose.
type bin struct {
	app   *App
	tasks []binned
}

// bins holds the removals that can still be taken back, keyed by the token the
// client was handed. An entry is written by RemoveTasksUndoable, read once by
// UndoRemove, and dropped by the clock or at shutdown, whichever comes first.
var bins sync.Map // token -> *bin

// RemoveTasksUndoable is RemoveTasks for a removal pressed by hand: it removes
// the same rows and keeps a copy for UndoWindow.
//
// With deleteFiles there is no token. The bytes are gone, and an undo that only
// brought the row back would restart the transfer from zero.
//
// The bin lives in memory only. A restart is not an undo, and a deletion coming
// back after an update would be a nasty surprise.
func (a *App) RemoveTasksUndoable(ids []string, deleteFiles bool) (removed []string, token string) {
	// The set-aside rows RemoveTasks takes along go into the bin too, so an
	// undo brings back the whole link.
	aside := a.strandedBy(ids)
	// Copied before the removal takes the tasks out of a.tasks.
	a.mu.Lock()
	inQueue := make(map[string]bool, len(a.queue))
	for _, id := range a.queue {
		inQueue[id] = true
	}
	kept := make(map[string]binned, len(ids)+len(aside))
	for _, id := range slices.Concat(ids, aside) {
		if t := a.tasks[id]; t != nil {
			kept[id] = binned{task: *t, queued: inQueue[id]}
		}
	}
	a.mu.Unlock()

	removed = a.RemoveTasks(ids, deleteFiles)
	if deleteFiles || len(removed) == 0 {
		return removed, ""
	}
	// Only what the removal really took, in case another caller removed a row
	// in between.
	b := &bin{app: a, tasks: make([]binned, 0, len(removed)+len(aside))}
	a.mu.Lock()
	for _, id := range slices.Concat(removed, aside) {
		if e, ok := kept[id]; ok && a.tasks[id] == nil {
			b.tasks = append(b.tasks, e)
		}
	}
	a.mu.Unlock()
	if len(b.tasks) == 0 {
		return removed, ""
	}
	token = newID()
	bins.Store(token, b)
	// Not a.spawn: this goroutine writes nothing Close waits for, and a spawn
	// refused during shutdown would leave the bin to never expire. Waiting on
	// a.ctx drops the bin at shutdown too.
	go func() {
		timer := time.NewTimer(UndoWindow)
		defer timer.Stop()
		select {
		case <-timer.C:
		case <-a.ctx.Done():
		}
		bins.Delete(token)
	}()
	return removed, token
}

// UndoRemove puts back what one removal took and reports which rows came back.
// An unknown or expired token restores nothing and is not an error; expiring is
// a token's normal end.
func (a *App) UndoRemove(token string) []string {
	v, ok := bins.Load(token)
	b, _ := v.(*bin)
	// A browser may sit in front of several instances (routes_federation.go),
	// so a token issued by another instance is not found here.
	if !ok || b == nil || b.app != a {
		return nil
	}
	bins.Delete(token)

	a.mu.Lock()
	var live []*core.Task
	for i := range b.tasks {
		e := b.tasks[i]
		// Never two rows for one id: a live download must not be replaced by
		// a copy of a removed one.
		if a.tasks[e.task.ID] != nil {
			continue
		}
		t := e.task
		enqueue := e.queued
		if t.Status == core.StatusRunning || t.Status == core.StatusExtracting {
			// The backend was told to forget the task, so it comes back as
			// waiting, as reviveOnBoot does after a restart.
			t.Status = core.StatusQueued
			enqueue = true
		}
		// Loaded is kept, as in reviveOnBoot: it describes a file, and a removal
		// that kept the files left the partial on disk. Speed described a
		// transfer, and there is none.
		t.Speed = 0
		a.tasks[t.ID] = &t
		// Filed again, but never over a link pasted since the removal, or the
		// mirror set would let a third copy past.
		if m := a.dupes.Check(dedupe.Entry{URL: t.URL}); m.Verdict != dedupe.Duplicate {
			a.dupes.Add(linkEntry(&t))
		}
		if enqueue {
			a.queue = append(a.queue, t.ID)
		}
		live = append(live, &t)
	}
	// Priority and position came back with the copies, so restore the order
	// rather than leaving restored rows at the end.
	a.sortQueueLocked()
	a.dispatchLocked()
	// Copied after dispatching, as in startTasks.
	copies := make([]core.Task, 0, len(live))
	for _, t := range live {
		copies = append(copies, *t)
	}
	a.mu.Unlock()
	back := make([]string, 0, len(copies))
	for i := range copies {
		c := copies[i]
		// A set-aside row came back with its link but is not a row anybody
		// sees, so it is not counted as one.
		if !c.VariantOff {
			back = append(back, c.ID)
		}
		_ = a.Store.Save(&c)
		a.Hub.Broadcast("task", &c)
	}
	return back
}

// Selection names the tasks one queue action is about, so a verb over many
// rows is one request rather than one per row.
type Selection struct {
	// Ids are the tasks named outright.
	Ids []string `json:"ids,omitempty"`
	// Package is a whole package by name, including rows a list filter hides.
	// It is a pointer because "" is a real package (the ungrouped one).
	Package *string `json:"package,omitempty"`
	// All widens the selection to every task the verb can touch. It must be
	// asked for; an empty request is not read as "all".
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

// idsOf is what every selection verb returns: the ids it actually touched.
func idsOf(in []*core.Task) []string {
	out := make([]string, 0, len(in))
	for _, t := range in {
		out = append(out, t.ID)
	}
	return out
}

// movable reports whether a task still has a place in the wait order. A
// one-step move must not spend itself on a finished or failed download.
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
		// Forced outranks priority: it means "fetch this one first". Here it
		// only reorders; dispatchLocked is what starts it past the ordinary
		// limits, in the forced pool.
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

// The seven priorities, JDownloader's set. Priority only orders the wait queue:
// no backend reads it, it buys no bandwidth, and the highest priority still
// waits for a free slot.
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
// that goes on the task, and a stable id the browser translates. There is no
// label, since clients of one instance can use different languages.
type PriorityChoice struct {
	ID    string `json:"id"`
	Value int    `json:"value"`
}

// Priorities is the list the menu is built from, highest first.
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

// clampPriority keeps a value inside the enum rather than refusing it, since
// the queue can order any integer. The bound lives in internal/rules because a
// Packagizer rule writes the same field.
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

// SetPriorityIn puts a selection at one of the seven priorities. It applies at
// once to everything not yet downloading; a running transfer is never
// interrupted for an ordering decision.
func (a *App) SetPriorityIn(sel Selection, priority int) []string {
	priority = clampPriority(priority)
	a.mu.Lock()
	chosen := a.pickLocked(sel, nil)
	// The tasks that change band. Their manual position described the old
	// band and must not carry over.
	arrived := map[string]bool{}
	for _, t := range chosen {
		if t.Priority != priority && movable(t) {
			arrived[t.ID] = true
		}
		t.Priority = priority
	}
	// They join the end of the new band in their existing order, leaving a
	// hand-ordered band intact.
	moved := a.renumberLocked(arrived, MoveBottom)
	copies := make([]core.Task, 0, len(chosen)+len(moved))
	named := make(map[string]bool, len(chosen))
	for _, t := range chosen {
		named[t.ID] = true
		copies = append(copies, *t) // after the renumbering, so the position is current
	}
	// The tasks the arrivals pushed down changed position too.
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

// The four relative moves in the wait order. Anything finer is a drag-and-drop
// reorder, which arrives as one ordered list (ReorderBand) so two browsers
// cannot interleave steps.
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
// The move stays inside the selection's priority band: priority outranks
// position in the comparator, so crossing a band is the priority control's job.
//
// Positions are renumbered densely within each touched band, which is what
// makes a single step possible. The run is negative and ends at -1: unmoved
// tasks carry position zero, so links pasted later stay behind the ordered
// ones in arrival order, and the numbers cannot drift.
func (a *App) MoveIn(sel Selection, where string) []string {
	switch where {
	case MoveTop, MoveUp, MoveDown, MoveBottom:
	default:
		// Refused rather than defaulted, so a client typo does not move
		// anything.
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
// task and returns the tasks whose place changed, ready to publish. Manual
// moves and priority changes share it. Caller holds a.mu.
func (a *App) renumberLocked(want map[string]bool, where string) []core.Task {
	if len(want) == 0 {
		return nil
	}
	var copies []core.Task
	for _, band := range a.bandsLocked() {
		if !reorder(band, want, where) {
			continue
		}
		copies = append(copies, renumberBand(band)...)
	}
	return copies
}

// renumberBand writes dense positions for one band in the slice's current
// order and returns the tasks whose position changed. renumberLocked and
// ReorderBand share it. The run ends at -1 for the reason given at MoveIn.
// Caller holds a.mu.
func renumberBand(band []*core.Task) []core.Task {
	var copies []core.Task
	for i, t := range band {
		pos := i - len(band)
		if t.Position == pos {
			continue
		}
		t.Position = pos
		copies = append(copies, *t)
	}
	return copies
}

// bandsLocked groups the movable tasks into the runs the manual position
// orders: one band per forced and priority pair, each in wait order. Between
// bands something outranks position, so positions only matter within one.
// Caller holds a.mu.
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
// A one-step move is a block move: a selected run slides past the unselected
// task beside it. Swapping each selected task with its neighbour would make
// adjacent selected rows swap with each other and cancel out.
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
		// Each half keeps its order, so a selection arrives as it was shown.
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

// ReorderBand applies the exact order a drag arrived with to tasks in one
// priority band, as one list in one pass under the lock, so two browsers'
// drags cannot interleave.
//
// ids may be a subset of the band: those tasks take that order within the
// slots they already occupy, and every other task stays put. A band spans the
// collector and the download list, while each screen shows only part of it,
// so a drag inside one list names only some of the band.
//
// Refused with a reason: an unknown id, one listed twice, one that is not
// movable, and ids from more than one band.
func (a *App) ReorderBand(ids []string) ([]string, error) {
	// The route refuses an empty list too, but tasks[0] below needs this.
	if len(ids) == 0 {
		return nil, errors.New("this needs at least one task id")
	}
	a.mu.Lock()
	seen := make(map[string]bool, len(ids))
	tasks := make([]*core.Task, 0, len(ids))
	for _, id := range ids {
		if seen[id] {
			a.mu.Unlock()
			return nil, fmt.Errorf("id %q is listed twice", id)
		}
		seen[id] = true
		t := a.tasks[id]
		if t == nil {
			a.mu.Unlock()
			return nil, fmt.Errorf("no such task: %s", id)
		}
		if !movable(t) {
			a.mu.Unlock()
			return nil, fmt.Errorf("task %s is not in the wait queue", id)
		}
		tasks = append(tasks, t)
	}

	// All in one band: the pairing bandsLocked groups by.
	first := tasks[0]
	for _, t := range tasks[1:] {
		if t.Priority != first.Priority || t.Forced != first.Forced {
			a.mu.Unlock()
			return nil, errors.New("ids span more than one band")
		}
	}

	var band []*core.Task
	for _, b := range a.bandsLocked() {
		if b[0].Forced == first.Forced && b[0].Priority == first.Priority {
			band = b
			break
		}
	}
	// The caller's order, placed into the slots those tasks already hold:
	// unnamed tasks never move.
	ordered := make([]*core.Task, 0, len(band))
	next := 0
	for _, t := range band {
		if seen[t.ID] {
			ordered = append(ordered, tasks[next])
			next++
			continue
		}
		ordered = append(ordered, t)
	}
	if next != len(tasks) {
		// Should not happen after the checks above, but renumbering a band that
		// lost a task would corrupt its positions.
		a.mu.Unlock()
		return nil, fmt.Errorf("only %d of %d ids belong to this band", next, len(tasks))
	}

	copies := renumberBand(ordered)
	a.dispatchLocked()
	a.mu.Unlock()
	a.saveAndBroadcast(copies)
	return idsOf(tasks), nil
}

// QueueState is the master switch and the stop mark, as the UI sees them.
type QueueState struct {
	Halted   bool   `json:"halted"`
	StopMark string `json:"stopMark,omitempty"`
	// Running is how many downloads are actually in flight, which is what makes
	// "halted" legible: halted with three running means three still finishing.
	Running int `json:"running"`
	// Quiet is whether the second set of limits is in force (app_quiet.go),
	// whether by the switch or a timetable window. ScheduleState.State.Quiet
	// says whether the timetable did it.
	Quiet bool `json:"quiet"`
}

// Queue reports the master switch.
func (a *App) Queue() QueueState {
	a.mu.Lock()
	defer a.mu.Unlock()
	return QueueState{Halted: a.halted, StopMark: a.stopMark, Running: len(a.active), Quiet: a.quiet.inForce}
}

// SetHalted stops or resumes handing queued tasks to a backend. Halting leaves
// what is already downloading alone, because killing a transfer mid-file throws
// away work the user did not ask to lose.
func (a *App) SetHalted(halted bool) {
	a.mu.Lock()
	// Recorded as the manual switch too, which the schedule evaluates against,
	// so a stop by hand survives the end of a window. The runner is not woken,
	// so a release inside a pause window holds until the next boundary.
	a.manualHalt = halted
	a.halted = halted
	// The switch is the newer word on the whole queue, so a link still
	// waiting on "Start now" waits for it too.
	clear(a.startNow)
	if !halted {
		// A stop mark left armed would halt the queue again at the next
		// finished download.
		a.stopMark = ""
	}
	// Dispatched even when halting, so every waiting row is told why it waits.
	// dispatchLocked starts nothing while halted.
	a.dispatchLocked()
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

// StopAll is the hard stop. SetHalted lets running transfers finish; this
// stops them where they are. The order of the steps matters:
//
//   - the halt is written first, under the lock the snapshot is taken under,
//     or each stopped task's freed slot would be refilled at once;
//   - the ids are copied out, since StopBack takes a.mu and completion events
//     write a.active from backend goroutines;
//   - the backends are told last, outside the lock, since stopping a JD or
//     debrid task goes over the network.
//
// It returns the ids it stopped.
func (a *App) StopAll() []string {
	a.mu.Lock()
	a.manualHalt = true
	a.halted = true
	clear(a.startNow)
	ids := make([]string, 0, len(a.active))
	for id := range a.active {
		ids = append(ids, id)
	}
	a.mu.Unlock()
	// Sorted so the answer is stable.
	sort.Strings(ids)
	for _, id := range ids {
		// StopBack rather than Pause, so the task returns to the wait queue and
		// play resumes it.
		a.StopBack(id)
	}
	a.Hub.Broadcast("queue", a.Queue())
	return ids
}

// StopCost is what the hard stop would cost, worked out before it is paid.
type StopCost struct {
	// Running is how many transfers would be stopped.
	Running int `json:"running"`
	// Losing names the transfers that cannot resume where they stopped, so the
	// dialog can point at the rows.
	Losing []string `json:"losing"`
	// Bytes is what those transfers have already written and would fetch again:
	// loaded bytes, not the announced size.
	Bytes int64 `json:"bytes"`
	// Unknown is how many transfers have not been asked whether they resume.
	// They are kept out of Losing so the dialog never overstates the loss.
	Unknown int `json:"unknown"`
	// UnknownBytes is what those have written: a possible loss, not a certain
	// one.
	UnknownBytes int64 `json:"unknownBytes"`
}

// StopCost reports what a hard stop right now would throw away. A transfer
// that resumes costs nothing, one that cannot counts in Bytes, and one nobody
// has asked counts apart.
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

// ForceDownload starts a selection now: it goes to the front of the wait order,
// past every priority, and its disabled and hold flags are cleared. Forced is
// not an eighth priority; a forced link must never wait behind a high-priority
// package.
//
// On a stopped queue the selection starts all the same and nothing else does,
// as JDownloader's forced start works: the master switch is a decision about
// the whole box and stays where it is (see App.startNow). Staged links go
// through StartTasks, where the collector's rules apply. How the flag counts
// against the limits once the download runs is said where dispatchLocked
// counts the slots.
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
		// Never with an empty list, which StartTasks reads as everything in the
		// collector.
		a.StartTasks(staged)
	}

	a.mu.Lock()
	var forced []*core.Task
	for _, id := range ids {
		t := a.tasks[id]
		if t == nil {
			continue // removed while the collector pass ran
		}
		t.Forced = true
		t.Enabled = true
		t.Hold = false
		if t.Status == core.StatusPaused {
			// Requeued rather than resumed: only the dispatcher knows whether the
			// backend has seen the task, and a Resume to one that has not starts
			// nothing.
			t.Status = core.StatusQueued
			t.Speed = 0
			a.dequeueLocked(id)
			a.queue = append(a.queue, id)
		}
		if t.Status == core.StatusQueued {
			a.startNow[id] = true
		}
		forced = append(forced, t)
	}
	a.dispatchLocked()
	// Copied after dispatching, as in startTasks.
	copies := make([]core.Task, 0, len(forced))
	for _, t := range forced {
		copies = append(copies, *t)
	}
	a.mu.Unlock()
	a.saveAndBroadcast(copies)
	return ids
}

// SetEnabledIn is the bulk switch for disabled links: a selection, a whole
// package, or every link that is currently off. Only links the switch would
// change are touched, so a large list is not rewritten and rebroadcast.
func (a *App) SetEnabledIn(sel Selection, enabled bool) []string {
	a.mu.Lock()
	ids := idsOf(a.pickLocked(sel, func(t *core.Task) bool { return t.Enabled != enabled }))
	a.mu.Unlock()
	// SetEnabled writes the flag and then dispatches.
	return a.SetEnabled(ids, enabled)
}

// QueueCounters is what the list says about itself: how much work is left, how
// fast it is going and when it would be finished.
type QueueCounters struct {
	// Files is every file still owed, disabled links included.
	Files int `json:"files"`
	// Disabled is how many of those are switched off, so the interface can
	// explain why the file count and byte total differ.
	Disabled int `json:"disabled"`
	Running  int `json:"running"`
	// Remaining is the bytes still to fetch, excluding disabled links, which
	// will not be fetched. A file of unknown size contributes nothing.
	Remaining int64 `json:"remaining"`
	// Speed is what the same set is moving at, so a link disabled while
	// running does not shorten the ETA.
	Speed int64 `json:"speed"`
	// ETA is seconds, and nil when nothing is moving or nothing is left; zero
	// would read as "done in a moment".
	ETA *int64 `json:"eta"`
	// Captchas is how many challenges GET /api/captcha would list. Each one
	// holds a download up, and a view of several instances reads the number
	// here rather than downloading every picture to count them.
	Captchas int `json:"captchas"`
}

// Counters computes the figures under the list.
//
// Finished and failed downloads are excluded, as are links still in the
// collector, which would make the ETA move on every paste. A disabled link
// counts as a file but not in the bytes or the ETA. Held links count fully,
// since a hold is a pause the user means to lift.
func (a *App) Counters() QueueCounters {
	c := QueueCounters{Captchas: len(a.captchaStateFor().store.List())}
	a.mu.Lock()
	defer a.mu.Unlock()
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

// dequeueLocked removes id from the wait queue, and with it a press of "Start
// now" the link was waiting on: paused, resumed or removed, it waits for the
// queue like any other. Caller holds a.mu.
func (a *App) dequeueLocked(id string) {
	delete(a.startNow, id)
	for i, q := range a.queue {
		if q == id {
			a.queue = append(a.queue[:i], a.queue[i+1:]...)
			return
		}
	}
}

// scheduleBase is what the queue does when no window applies: the halt the user
// set by hand and the configured speed limit. It is read on every pass, so a
// stop made during a pause window survives the window's end.
func (a *App) scheduleBase() schedule.State {
	a.mu.Lock()
	paused := a.manualHalt
	// Remembered so applySchedule can spot a stale answer.
	a.scheduleBaseHalt = paused
	// Quiet mode is a base like the halt, remembered for the same reason.
	quiet := a.quiet.manual
	a.quiet.baseSeen = quiet
	a.mu.Unlock()
	return schedule.State{Paused: paused, Limit: a.Settings.Get().SpeedLimit, Quiet: quiet}
}

// applySchedule puts the timetable's answer into effect. It runs on the
// runner's goroutine and only when the answer changed, so the slow work is
// handed off.
//
// It writes the halt flag and never the stop mark, the user's own "finish this,
// then stop", which a window ending must not throw away.
func (a *App) applySchedule(st schedule.State) {
	// Settings has its own lock; read it before taking a.mu.
	cfg := a.Settings.Get()
	a.mu.Lock()
	// The runner reads the base, releases the lock and evaluates before calling
	// this, so a change in that gap (StopAll setting the halt) would be
	// overwritten by a stale answer and its freed slot handed out
	// (TestStopAllHaltsBeforeItFreesASlot). Only an answer the timetable left
	// equal to the base is corrected; a window that says pause or run still
	// wins.
	paused := st.Paused
	if paused == a.scheduleBaseHalt && a.manualHalt != a.scheduleBaseHalt {
		paused = a.manualHalt
	}
	a.halted = paused
	// The same correction for a quiet press landing in the gap (see
	// app_quiet.go for why a window wins).
	quiet := st.Quiet
	if quiet == a.quiet.baseSeen && a.quiet.manual != a.quiet.baseSeen {
		quiet = a.quiet.manual
	}
	a.quiet.inForce = quiet
	// Set in the same critical section as the flag and before the dispatch:
	// quiet mode is one decision about slots (cfgInForceLocked) and speed, and
	// a dispatch between the two would see half of it. a.limitInForce is what
	// applyBudget shares out, so the quiet limit has to be written here.
	limit := speedInForce(cfg, st.Limit, quiet)
	a.limitInForce = limit
	// Always dispatched, so rows held by a pause window are told why, including
	// at boot, where this is the first dispatch.
	a.dispatchLocked()
	a.mu.Unlock()
	// applyBudget shares the limit in force between the three meters
	// (app_budget.go), which is why it was recorded above first.
	a.applyBudget()
	// JD is told over the network, so off this goroutine; a.spawn so Close
	// waits for it, since it reads a.jd. The limit in force, so JD honours quiet
	// mode without waiting for the next budget tick.
	a.spawn(func() { a.pushJDSpeedLimit(limit) })
	a.Hub.Broadcast("queue", a.Queue())
}

// ScheduleState is the timetable, what it says right now, and when that changes.
type ScheduleState struct {
	Entries []schedule.Entry `json:"entries"`
	State   schedule.State   `json:"state"`
	// Next is when the answer changes, so a UI can say "throttled until
	// 06:00"; nil when it never changes, as with an empty timetable.
	Next *time.Time `json:"next"`
	// Suspended says the timetable is set aside (SuspendSchedule), and
	// SuspendedUntil when it applies again by itself; nil while it waits to be
	// lifted by hand.
	Suspended      bool       `json:"suspended"`
	SuspendedUntil *time.Time `json:"suspendedUntil,omitempty"`
}

// ScheduleState reports the timetable and the state it currently implies. The
// few rows are recompiled for the read rather than shared with the runner's
// goroutine.
func (a *App) ScheduleState() ScheduleState {
	entries := a.Settings.Get().Schedule
	s := schedule.Compile(entries)
	base := a.scheduleBase()
	now := time.Now()
	sp := a.sched.Suspension()
	out := ScheduleState{Entries: entries, State: sp.At(s, now, base)}
	if n, ok := sp.Next(s, now, base); ok {
		out.Next = &n
	}
	if sp.Covers(now) {
		out.Suspended = true
		if !sp.Until.IsZero() {
			until := sp.Until
			out.SuspendedUntil = &until
		}
	}
	if out.Entries == nil {
		out.Entries = []schedule.Entry{}
	}
	return out
}
