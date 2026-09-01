package app

// What survives a restart: the state a stored task comes back in, the
// housekeeping that keeps the list from growing for ever, and the record of what
// was fetched that outlives the list itself.

import (
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/settings"
	"github.com/junkerderprovinz/knightloader/internal/store"
)

// reviveOnBoot gives one stored task a state that is true now, and reports
// whether it should go back into the wait queue.
//
// EVERY ROW IN THE STORE BELONGED TO A PROCESS THAT IS GONE. A task the database
// calls "running" is not running: there is no transfer behind it, no backend
// holding it and no goroutine that will ever report on it, and leaving that
// status alone would put a row in the list that shows a speed for ever and can
// be neither paused nor resumed. So the mid-flight states are settled here, once,
// before anything else in this package can read them.
//
// The byte count is the part worth being careful about. A stored Loaded is a
// claim about a file, so it is kept exactly while that file is still there and
// cleared when it is not - a progress bar at 60 % of a download whose partial
// somebody deleted is a lie the user only discovers by pressing resume. It is
// NOT cleared merely because the transfer will start over: the bytes really are
// on the disk at the moment this runs, and zeroing a number that is true because
// of what will happen next is the other half of the same dishonesty.
//
// An interrupted extraction counts as done, because the download itself
// finished. That is also why it is written back to the store rather than only to
// the map: a task stuck at "extracting" would otherwise never be seen as
// finished by the history or by retention.
//
// changed reports whether any of that was a change worth persisting.
func (a *App) reviveOnBoot(t *core.Task, resume string, queueWasLive bool) (changed, enqueue bool) {
	switch t.Status {
	case core.StatusExtracting:
		t.Status = core.StatusDone
		t.Speed = 0
		return true, false
	case core.StatusRunning, core.StatusQueued:
	default:
		return false, false
	}

	was, loaded, speed := t.Status, t.Loaded, t.Speed
	t.Speed = 0
	if !a.keptItsProgress(t) {
		t.Loaded = 0
	}
	// Back into the wait queue either way, and the RESUME POLICY decides whether
	// the queue is running behind it - not whether the task is in it.
	//
	// "Never" used to scatter every task out of the queue as "paused", and the
	// queue then came up NOT halted: nothing was running, nothing could run, and
	// the master switch said the queue was live. Pressing play released a halt
	// that was not set and dispatched a queue that was empty, so the button did
	// nothing at all - on jdp's own instance, 19 paused rows and a switch
	// insisting everything was fine ("Die Start und Stopp buttons funktionieren
	// einfach nirgends! Es lädt auch nirgends was runter").
	//
	// Queued plus halted says the same thing honestly and is reversible in one
	// press: the rows wait, the switch says the queue is stopped, and play
	// starts them. "Never" still means nothing downloads until somebody says so,
	// which is the whole of what the setting promises. See holdOnBoot for the
	// halt itself, and StopBack for the same distinction under the stop button.
	t.Status = core.StatusQueued
	enqueue = true
	return t.Status != was || t.Loaded != loaded || t.Speed != speed, enqueue
}

// holdOnBoot reports whether the queue should come up stopped, given the resume
// policy and whether anything was actually in flight when the process ended.
//
// It is the other half of reviveOnBoot: that one puts the tasks back in the
// queue whatever the policy says, and this one decides whether the queue behind
// them is running. Splitting it that way is what makes the state reversible -
// a halted queue full of waiting rows takes one press to start, where rows
// scattered out of the queue took one press per row and no button offered it.
func holdOnBoot(resume string, queueWasLive bool) bool {
	if resume == settings.ResumeAll {
		return false
	}
	// "Only if it was running" is a statement about the QUEUE, not about one
	// row: if anything was in flight the queue was live, and everything that was
	// in it goes back into it. A task that was waiting for a slot when the power
	// went is not a task somebody paused.
	return !(resume == settings.ResumeRunning && queueWasLive)
}

// keptItsProgress reports whether the byte count a stored task carries still
// describes something that exists on disk.
//
// A task fetched by headless JD is downloaded on JD's own machine, so there is
// nothing here to look at and its count is left alone: JD is the authority on
// its own transfers, and clearing the number because this box cannot see the
// file would be inventing an answer. A task whose name is still its URL never
// resolved, so it has no file and no bytes either.
func (a *App) keptItsProgress(t *core.Task) bool {
	if t.Loaded <= 0 {
		return false
	}
	if !filesAreLocal(t) {
		return true
	}
	if t.Name == "" || t.Name == t.URL {
		return false
	}
	fi, err := os.Stat(filepath.Join(a.dirFor(t), t.Name))
	return err == nil && !fi.IsDir() && fi.Size() > 0
}

// upkeepInterval is how often the housekeeping runs. A minute, because
// everything it does is either idempotent or a cutoff measured in days: the
// interval decides how promptly a list is tidied, never what it is tidied down
// to.
const upkeepInterval = time.Minute

// upkeep is the housekeeping loop. It is the only goroutine this package starts
// for the life of the app, it stops when the app's context is cancelled, and
// Close waits for it - because everything it does writes to the store.
func (a *App) upkeep() {
	defer a.wg.Done()
	tick := time.NewTicker(upkeepInterval)
	defer tick.Stop()
	for {
		select {
		case <-a.ctx.Done():
			return
		case <-tick.C:
			a.sweep()
		}
	}
}

// sweep is one pass of the housekeeping: bring the finish times the app is
// holding into line with the ones the store recorded, trim the list, trim the
// history. In that order, because the first is what the second reads.
func (a *App) sweep() {
	a.reconcileFinishTimes()
	a.applyRetention()
	a.trimHistory()
	// Cheap on every ordinary tick (one map read, see hostRefreshAttempted)
	// and a real network round trip only once every hostRefreshInterval -
	// see refreshHostListsIfDue (app_accounts.go) for why this rides upkeep's
	// existing ticker instead of starting a second goroutine for it.
	a.refreshHostListsIfDue()
}

// reconcileFinishTimes copies the store's answer for "when did this finish"
// onto the tasks the app is holding.
//
// The stamp is written by the store, at the save that first carries a settled
// task (see store.stampFinish), and every caller in this package hands the store
// a COPY - deliberately, because a live task must never leave the lock. So the
// value lands on the copy that goes out to the browser and not on the task the
// list is built from, and a snapshot taken through the API would show an empty
// column for a download that finished a moment ago. This is the one pass that
// closes that gap.
//
// It broadcasts nothing. Every client already received the copy that carried the
// stamp; this is the app catching up with its own store, not news.
func (a *App) reconcileFinishTimes() {
	a.mu.Lock()
	var ask []string
	for id, t := range a.tasks {
		switch {
		case t.Status == core.StatusDone && t.FinishedAt.IsZero():
			ask = append(ask, id)
		case t.Status != core.StatusDone && !t.FinishedAt.IsZero():
			// The other half of the store's invariant: a task that has left the
			// done state - restarted by hand, handed to the next backend - must not
			// go on claiming a finish time, or retention would eventually reach a
			// download that is running.
			t.FinishedAt = time.Time{}
		}
	}
	a.mu.Unlock()
	if len(ask) == 0 {
		return
	}
	times, err := a.Store.FinishTimes(ask)
	if err != nil {
		log.Printf("could not read back when %d downloads finished: %v", len(ask), err)
		return
	}
	a.mu.Lock()
	for id, at := range times {
		// Re-checked under the lock: a task can have been restarted between the two
		// critical sections, and writing a finish time onto a running download is
		// exactly what the clearing branch above exists to undo.
		if t := a.tasks[id]; t != nil && t.Status == core.StatusDone && t.FinishedAt.IsZero() {
			t.FinishedAt = at
		}
	}
	a.mu.Unlock()
}

// applyRetention takes finished downloads off the LIST once they are older than
// the configured age.
//
// THE FILES ARE NEVER TOUCHED, and that is not a default here - it is the whole
// contract. Removing a row and deleting what was downloaded are two different
// actions in this app, because conflating them once already destroyed finished
// downloads on the ordinary "clear finished" path, and this is that same path
// running unattended on a timer. Nor is the record lost: the history keeps what
// was fetched, and retention does not read that table at all.
//
// Zero days keeps the list for ever, which is a choice the user can make and not
// the one they get by default.
func (a *App) applyRetention() {
	days := a.Settings.Get().KeepFinishedDays
	if days <= 0 {
		return
	}
	cutoff := time.Now().AddDate(0, 0, -days)
	old, err := a.Store.FinishedBefore(cutoff)
	if err != nil {
		log.Printf("could not work out which finished downloads have aged out: %v", err)
		return
	}
	if len(old) == 0 {
		return
	}
	// Through RemoveTasks, which is the one path that also unfiles the link from
	// the mirror set, clears the backend's state and frees a dispatch slot. false
	// is the argument this whole function exists to pass.
	removed := a.RemoveTasks(old, false)
	if len(removed) > 0 {
		log.Printf("retention: %d finished downloads older than %d days left the list; the files and the history are untouched",
			len(removed), days)
	}
}

// trimHistory caps the record. It is the only thing that ever deletes from the
// history, and it deletes the oldest entries rather than the ones belonging to
// tasks that have gone: a row here has no task any more by design.
func (a *App) trimHistory() {
	max := a.Settings.Get().HistoryMax
	if max <= 0 {
		return
	}
	n, err := a.Store.TrimHistory(max)
	if err != nil {
		log.Printf("could not trim the download history: %v", err)
		return
	}
	if n > 0 {
		log.Printf("download history trimmed to the newest %d entries (%d dropped)", max, n)
	}
}

// History reports what this instance has fetched, newest first. A limit of zero
// or less returns everything.
//
// It is read straight from the store and never from the task list, which is the
// entire reason the table exists: the answer has to survive the list being
// cleared, trimmed or emptied by a user who wanted a tidy screen.
func (a *App) History(limit int) ([]store.HistoryEntry, error) {
	return a.Store.History(limit)
}

// ClearHistory empties the record, which nothing else in the app is allowed to
// do as a side effect of tidying anything else.
func (a *App) ClearHistory() error {
	return a.Store.ClearHistory()
}
