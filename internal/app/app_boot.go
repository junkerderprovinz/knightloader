package app

// What survives a restart: the state a stored task comes back in, the
// housekeeping that keeps the list from growing for ever, and the record of what
// was fetched that outlives the list itself.

import (
	"log"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/reclaim"
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
// A failed task is not mid-flight and is left exactly where it is, with one
// exception: the automatic retry it was waiting for died with the process. See
// the StatusError case.
//
// changed reports whether any of that was a change worth persisting.
func (a *App) reviveOnBoot(t *core.Task, resume string, queueWasLive bool) (changed, enqueue bool) {
	switch t.Status {
	case core.StatusExtracting:
		t.Status = core.StatusDone
		t.Speed = 0
		return true, false
	case core.StatusError:
		// A DEADLINE MUST NOT OUTLIVE THE PROCESS THAT WAS COUNTING TO IT.
		// NextTry is persisted; the time.AfterFunc that was going to honour it
		// was not. So after every container update each failed row carries a
		// moment that has usually already passed and that nothing will ever act
		// on: the list shows the "retrying automatically" mark, which is what
		// stops people acting on a row, for a retry that is never coming.
		//
		// Cleared rather than re-armed, and that is the whole decision. Re-arming
		// would restart every failed row at once on a boot whose deadlines all
		// went by while the box was off, against a resume policy the user chose
		// precisely to keep the queue quiet. Nothing is pending, so the row stops
		// claiming one. The spent count stays: it is what the backoff ladder
		// continues from when somebody presses restart, and it is a fact about
		// attempts that really were made.
		if t.NextTry.IsZero() {
			return false, false
		}
		t.NextTry = time.Time{}
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

// ReclaimReport is what one look at the disk found.
//
// Every candidate is in Findings, including the boring ones. A report that
// lists only its exceptions is a report nobody believes about the rest of the
// list, and the whole point of this pass is that somebody has to be able to
// disagree with it.
type ReclaimReport struct {
	// Trust is the tier the pass actually ran under, echoed back rather than
	// left for the caller to look up: the verdicts below cannot be read
	// without it.
	Trust string `json:"trust"`
	// Scanned is how many tasks were looked at, Settled how many of them came
	// out of it as finished downloads that never have to be fetched.
	Scanned int `json:"scanned"`
	Settled int `json:"settled"`
	// Findings are ordered by task id, so two runs over an unchanged list read
	// the same way round. The task list is a map and its iteration order is
	// deliberately random in Go.
	Findings []reclaim.Finding `json:"findings"`
	// Orphans are part files no task accounts for. They are reported and never
	// touched - see reclaim.Orphans.
	Orphans []reclaim.Orphan `json:"orphans"`
}

// Reclaim looks at what is already on the disk before anything is downloaded
// again, and settles the tasks whose files are provably already there.
//
// THE SITUATION IT IS FOR is a move to a new box, a restored backup, or a list
// somebody emptied and pasted back in. In all three the finished files are
// sitting in the download folder and nothing on this side has ever watched
// them arrive, so every one of them is fetched a second time over somebody's
// line. What counts as "already there" is internal/reclaim's decision and its
// Trust doc comment carries the reasoning; this function is the pass over the
// list and what is done with each verdict.
//
// IT IS A PRESS AND NOT A BOOT STEP, which is the one design decision here
// worth arguing with rather than reading past. The pass costs one stat per
// unfinished task; for every one of those that turns out to be the right
// length, a directory read and a parse of whatever sums files are in the
// folder (sumFromSiblingFile, the same lookup every finished download already
// pays for once); and for every one that finds a checksum, a full read of a
// file that may be tens of gigabytes. On a list of ten thousand that is a disk
// run in front of a queue that does not exist yet, and the person who would
// get it did not ask a question, they installed an update. Moving a box is a
// one-off event and it deserves a one-off press rather than a scan on every
// boot for ever afterwards; there is deliberately no setting that turns this
// into one, so no update can hand anybody a long disk run at start-up.
//
// IT STARTS NOTHING AND DELETES NOTHING. No dispatch afterwards, on purpose:
// pressing "look at the disk" must not start downloads, and settling a task as
// done frees no slot that another task was waiting on. Nothing is removed
// either, not the file a checksum has just proved is the wrong one and not an
// orphaned part file, because removing a row and deleting what was downloaded
// have been two different actions in this app since the day conflating them
// cost somebody their finished downloads (see applyRetention).
func (a *App) Reclaim() (ReclaimReport, error) {
	cfg := a.Settings.Get()
	trust := reclaim.ParseTrust(cfg.ReclaimTrust)
	rep := ReclaimReport{Trust: string(trust), Findings: []reclaim.Finding{}, Orphans: []reclaim.Orphan{}}

	// Read before the lock is taken, because it is a query against the same
	// store every task write goes through and this pass has no business
	// holding the task list while it runs.
	witness, err := a.reclaimWitness(trust)
	if err != nil {
		return rep, err
	}

	// Copies, and then the lock goes. Everything below stats and hashes files,
	// which is the one kind of work that must never happen under a.mu: a
	// checksum over a forty gigabyte file would hold the whole app still.
	a.mu.Lock()
	var candidates []core.Task
	claimed := map[string]bool{}
	dirs := map[string]bool{a.defaultDir(): true}
	for id, t := range a.tasks {
		dir := a.dirFor(t)
		dirs[dir] = true
		// Every task's part file, not only the candidates', because this set is
		// what keeps a running download's own part file from being reported as
		// an orphan.
		if t.Name != "" {
			claimed[reclaim.PartPath(dir, t.Name)] = true
		}
		if a.active[id] || !reclaimable(t) {
			continue
		}
		candidates = append(candidates, *t)
	}
	a.mu.Unlock()

	sort.Slice(candidates, func(i, j int) bool { return candidates[i].ID < candidates[j].ID })
	opts := reclaim.Options{Trust: trust, Sum: a.sumFromSiblingFile, Finished: witness}
	for i := range candidates {
		t := candidates[i]
		rep.Findings = append(rep.Findings, opts.Scan(reclaim.Request{
			TaskID:       t.ID,
			Dir:          a.dirFor(&t),
			Name:         t.Name,
			Size:         t.Size,
			ExpectedHash: t.ExpectedHash,
			Torrent:      isTorrentTask(&t),
		}))
	}
	rep.Scanned = len(rep.Findings)

	changed, settled := a.applyReclaim(rep.Findings)
	rep.Settled = settled
	// Off the lock, the same way dispatchLocked publishes what it turned down:
	// the store write and the broadcast must not happen under a.mu.
	a.publishTasks(changed)

	for dir := range dirs {
		found, err := reclaim.Orphans(dir, claimed)
		if err != nil {
			// Passed over without a word, because by far the most common
			// reason is that the folder is not there yet - true of the default
			// download folder on an install that has never finished anything,
			// and of a per-package subfolder for a package that has not
			// started. A log line per boot for that would be noise nobody can
			// act on, and there is nothing lost: a folder that does not exist
			// holds no orphans.
			continue
		}
		rep.Orphans = append(rep.Orphans, found...)
	}
	sort.Slice(rep.Orphans, func(i, j int) bool { return rep.Orphans[i].Path < rep.Orphans[j].Path })
	if rep.Settled > 0 {
		log.Printf("reclaim: %d of %d downloads were already on the disk and are marked finished without being fetched again",
			rep.Settled, rep.Scanned)
	}
	return rep, nil
}

// reclaimable reports whether a task is worth looking on the disk for.
//
// A RUNNING task is excluded by its caller (a.active) and by the status here,
// because the file underneath it is being written at this moment: hashing it
// would be hashing a moving target, and settling it as done would take a live
// transfer off the list.
//
// A COLLECTED task is scanned but never settled, and that split is deliberate.
// The collector is where a person decides what to download; a link that walked
// out of it on its own as "finished" has been confirmed by the app on their
// behalf. Telling them "eleven of these forty are already in your folder" is
// useful, deciding it for them is not - see applyReclaim, which acts on the
// verdicts for everything else and only reports these.
//
// A task fetched by headless JD is excluded outright: those bytes land on JD's
// own machine, so there is nothing here to look at and a verdict about this
// box's disk would be a verdict about the wrong disk. Same reasoning as
// keptItsProgress's own filesAreLocal check. A task whose name is still its URL
// never resolved and has no file name to look for.
func reclaimable(t *core.Task) bool {
	switch t.Status {
	case core.StatusDone, core.StatusRunning, core.StatusExtracting:
		return false
	}
	if !filesAreLocal(t) {
		return false
	}
	return t.Name != "" && t.Name != t.URL
}

// isTorrentTask reports whether this task's bytes are a swarm's business. The
// resolver id is the answer for anything staged through the ordinary path; the
// info hash catches a row read back out of the store, which is where the two
// could otherwise disagree.
func isTorrentTask(t *core.Task) bool { return t.Resolver == "torrent" || t.InfoHash != "" }

// applyReclaim writes the verdicts onto the live tasks and hands back copies of
// the ones that changed, for the caller to persist and broadcast off the lock,
// plus how many of them are downloads that now never have to happen.
//
// The count is kept apart from the length of the list on purpose: correcting a
// byte count is a change worth writing back and worth showing, and it is not a
// download saved. One number standing in for both would have a report claiming
// forty reclaimed files on a pass that only tidied forty progress bars.
//
// WHAT EACH VERDICT COSTS, which is the whole of the risk here:
//
//   - Complete settles the task as finished, so it is never fetched. That is
//     the only verdict that saves anything and the only one that can be
//     expensively wrong, which is why internal/reclaim will not reach it on
//     size alone unless the instance has been told to.
//   - Mismatch changes almost nothing on purpose. A checksum has proved the
//     file at that name is not this download, so the download must happen: the
//     task is left exactly where it was and only a byte count that was claiming
//     progress it does not have is cleared. The stale file is NOT deleted and
//     NOT renamed - the collision policy already owns what happens to a file in
//     the way, and it owns it at the moment the transfer starts rather than
//     minutes earlier from over here.
//   - Partial corrects the byte count and nothing else. Whether a beginning can
//     actually be continued is the backend's business: the FTP/SFTP backend
//     resumes from its own part file, and the embedded engine cannot, because
//     its download library renames around an existing file unconditionally
//     (gopeed v1.9.3's FetcherManager.AutoRename returns a literal true for
//     http, with no configuration behind it). Writing a true number down is
//     honest either way; promising a resume from here would not be.
//   - Recheck, Unproven and Absent change nothing at all. They are in the
//     report and that is the whole of their effect.
//
// Checksum is set only where a checksum was actually computed, matching
// verifyTask's own rule: the column says what verification found, and a tick
// that means "not checked" is worse than no tick. A Mismatch deliberately does
// not write "failed" into it either, because that column describes THIS task's
// own download and this task has not downloaded anything yet.
//
// The sentence goes on Note, which the store has no column for, so it reaches
// every open browser now and is gone after the next restart. That is the right
// lifetime for it: it explains a decision that was just taken in front of
// somebody, and the durable half of the answer is the task's own state plus
// the report this function feeds.
func (a *App) applyReclaim(findings []reclaim.Finding) (changed []core.Task, settled int) {
	now := time.Now()
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, f := range findings {
		t := a.tasks[f.TaskID]
		// Gone, restarted or picked up by the dispatcher while the files were
		// being hashed. The verdict is about a task that no longer exists in
		// the state it was measured in, and applying it anyway is how a live
		// download gets marked finished.
		if t == nil || a.active[f.TaskID] || !reclaimable(t) {
			continue
		}
		switch f.Verdict {
		case reclaim.Complete:
			if t.Status == core.StatusCollected {
				// Reported, not decided. See reclaimable.
				continue
			}
			t.Status = core.StatusDone
			t.Loaded = f.Bytes
			if t.Size <= 0 {
				t.Size = f.Bytes
			}
			t.Speed = 0
			t.Error = ""
			t.Reason = core.ReasonUnknown
			t.Waiting = core.WaitingNone
			t.Retries = 0
			t.NextTry = time.Time{}
			// With the two above, and for the same reason: the file is here, so
			// the budget that was being spent looking for it describes nothing.
			t.MaxTries = 0
			t.Note = f.Detail
			t.ChangedAt = now
			if f.Basis == reclaim.BasisChecksum {
				t.Checksum = "ok"
			}
			// Out of the wait queue, or the dispatcher would hand a finished
			// task to a backend on its next pass: the loop in dispatchLocked
			// reads the flags on a queued task, never its status.
			a.dequeueLocked(f.TaskID)
			// Same as any other settled download: a finished link stops
			// blocking its own re-add, because pasting it again is a
			// deliberate second attempt.
			a.forgetLinkLocked(t)
			settled++
		case reclaim.Mismatch:
			if t.Loaded == 0 {
				continue
			}
			t.Loaded = 0
			t.Note = f.Detail
			t.ChangedAt = now
		case reclaim.Partial:
			if t.Loaded == f.Bytes {
				continue
			}
			t.Loaded = f.Bytes
			t.Note = f.Detail
			t.ChangedAt = now
		default:
			continue
		}
		changed = append(changed, *t)
	}
	return changed, settled
}

// reclaimWitness is the record tier: everything this instance's own history
// says it has finished, as a set of name plus length.
//
// THE HISTORY IS THE RIGHT WITNESS for exactly the situations this pass is
// for. It is written by the store at the moment a download settles, one row
// per task, and it survives the list being cleared, trimmed or emptied - which
// is the entire reason that table exists. A moved box, a restored backup and
// an emptied list all carry the database, so all three still hold the note
// this app wrote itself when the last byte landed.
//
// It is a witness and not a proof, and the gap is worth stating: the history
// has no folder column, so a row can vouch for a file this instance finished
// somewhere else entirely. What closes most of that gap is that the candidate
// has to be sitting in THIS task's own download folder, under this task's own
// name, at exactly this task's own length, before the witness is ever
// consulted. What does not close is a second file that happens to share all
// three, and that is why a published checksum always outranks this and why the
// strict tier exists.
//
// Nil for the strict tier rather than a function that always says no, which is
// the difference between "do not ask" and "asked and got nothing" for
// reclaim.Options.
func (a *App) reclaimWitness(trust reclaim.Trust) (func(name string, size int64) bool, error) {
	if trust == reclaim.TrustChecksum {
		return nil, nil
	}
	entries, err := a.History(0)
	if err != nil {
		return nil, err
	}
	known := make(map[string]bool, len(entries))
	for _, e := range entries {
		if e.Name == "" || e.Size <= 0 {
			continue
		}
		known[witnessKey(e.Name, e.Size)] = true
	}
	return func(name string, size int64) bool { return known[witnessKey(name, size)] }, nil
}

// witnessKey folds the name to lower case, because the two sides of this
// comparison come from different places: one is what the store wrote when the
// download finished, the other is what a resolver just handed the collector.
// Windows and macOS would open both as the same file and refusing to match
// them would make the record tier useless on the platforms most of these
// instances run on.
func witnessKey(name string, size int64) string {
	return strings.ToLower(name) + "\x00" + strconv.FormatInt(size, 10)
}
