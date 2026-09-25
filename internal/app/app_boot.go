package app

// What survives a restart: the state a stored task comes back in, the
// housekeeping that keeps the list from growing for ever, and the history that
// outlives the list.

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

// reviveOnBoot gives one stored task a state that is true after the restart
// and reports whether it changed and whether it goes back into the wait queue.
// Every stored row belonged to a process that is gone, so nothing is really
// running.
//
// Loaded is kept only while the partial file still exists; a stale progress
// bar would only be discovered on resume. An interrupted extraction counts as
// done, since the download finished, and is written back so history and
// retention see it.
func (a *App) reviveOnBoot(t *core.Task, resume string, queueWasLive bool) (changed, enqueue bool) {
	switch t.Status {
	case core.StatusExtracting:
		t.Status = core.StatusDone
		t.Speed = 0
		return true, false
	case core.StatusError:
		// NextTry was persisted but the timer behind it died with the process,
		// so the row would show a retry that never comes. It is cleared rather
		// than re-armed, so a boot does not restart every failed row at once;
		// the spent retry count stays for the backoff to continue from.
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
	// Always back into the queue; the resume policy decides only whether the
	// queue itself comes up halted (see holdOnBoot), so one press on play
	// starts everything.
	t.Status = core.StatusQueued
	enqueue = true
	return t.Status != was || t.Loaded != loaded || t.Speed != speed, enqueue
}

// holdOnBoot reports whether the queue should come up halted, given the resume
// policy and whether anything was in flight when the process ended.
func holdOnBoot(resume string, queueWasLive bool) bool {
	if resume == settings.ResumeAll {
		return false
	}
	// "Only if it was running" is about the queue as a whole: if anything was
	// in flight, everything that was queued resumes too.
	return !(resume == settings.ResumeRunning && queueWasLive)
}

// keptItsProgress reports whether a stored task's byte count still describes a
// file on disk. JD downloads live on JD's machine and keep their count; a task
// whose name is still its URL never resolved and has no file.
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

// upkeepInterval is how often housekeeping runs. Everything it does is
// idempotent or a cutoff in days, so the interval only affects promptness.
const upkeepInterval = time.Minute

// upkeep is the housekeeping loop. It stops when the app's context is
// cancelled, and Close waits for it because it writes to the store.
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

// sweep is one housekeeping pass. Finish times are reconciled first because
// retention reads them.
func (a *App) sweep() {
	a.reconcileFinishTimes()
	a.applyRetention()
	a.trimHistory()
	// Both of these are cheap on an ordinary tick. The maintenance pass only
	// starts work on its own goroutine, because Close waits for upkeep.
	a.refreshHostListsIfDue()
	a.runDBMaintenanceIfDue()
}

// reconcileFinishTimes copies the store's finish times onto the live tasks.
// The store stamps the copy it is given on save, never the live task, so
// without this the API would show no finish time for a fresh download. It
// broadcasts nothing, since clients already received the stamped copy.
func (a *App) reconcileFinishTimes() {
	a.mu.Lock()
	var ask []string
	for id, t := range a.tasks {
		switch {
		case t.Status == core.StatusDone && t.FinishedAt.IsZero():
			ask = append(ask, id)
		case t.Status != core.StatusDone && !t.FinishedAt.IsZero():
			// A task that left the done state loses its finish time, or
			// retention could reach a running download.
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
		// Re-checked, since the task may have been restarted in between.
		if t := a.tasks[id]; t != nil && t.Status == core.StatusDone && t.FinishedAt.IsZero() {
			t.FinishedAt = at
		}
	}
	a.mu.Unlock()
}

// applyRetention removes finished downloads older than the configured age from
// the list. It never touches the files or the history. Zero days keeps
// everything.
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
	removed := a.RemoveTasks(old, false)
	if len(removed) > 0 {
		log.Printf("retention: %d finished downloads older than %d days left the list; the files and the history are untouched",
			len(removed), days)
	}
}

// trimHistory caps the history at HistoryMax, dropping the oldest entries. It
// is the only thing that deletes from the history.
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

// History reports what this instance has fetched, newest first; a limit of
// zero or less returns everything. It reads the store, not the task list, so
// it survives the list being cleared.
func (a *App) History(limit int) ([]store.HistoryEntry, error) {
	return a.Store.History(limit)
}

// ClearHistory empties the download history.
func (a *App) ClearHistory() error {
	return a.Store.ClearHistory()
}

// ReclaimReport is what one look at the disk found. Every candidate is listed,
// not only the exceptions, so the user can disagree with any verdict.
type ReclaimReport struct {
	// Trust is the tier the pass ran under, needed to read the verdicts.
	Trust string `json:"trust"`
	// Scanned is how many tasks were looked at, Settled how many became
	// finished downloads.
	Scanned int `json:"scanned"`
	Settled int `json:"settled"`
	// Findings are ordered by task id, so runs over an unchanged list compare.
	Findings []reclaim.Finding `json:"findings"`
	// Orphans are part files no task accounts for. They are reported, never
	// touched.
	Orphans []reclaim.Orphan `json:"orphans"`
}

// Reclaim settles tasks whose files are provably already on disk, for a move
// to a new box, a restored backup or a list that was emptied and pasted again.
// internal/reclaim decides what counts as proof.
//
// It runs on request, never at boot: it can hash many large files, and an
// update must not bring a long disk scan with it. It starts no downloads and
// deletes nothing.
func (a *App) Reclaim() (ReclaimReport, error) {
	cfg := a.Settings.Get()
	trust := reclaim.ParseTrust(cfg.ReclaimTrust)
	rep := ReclaimReport{Trust: string(trust), Findings: []reclaim.Finding{}, Orphans: []reclaim.Orphan{}}

	// A store query, so it runs before a.mu is taken.
	witness, err := a.reclaimWitness(trust)
	if err != nil {
		return rep, err
	}

	// Copies only; the stat and hash work below must not run under a.mu.
	a.mu.Lock()
	var candidates []core.Task
	claimed := map[string]bool{}
	dirs := map[string]bool{a.defaultDir(): true}
	for id, t := range a.tasks {
		dir := a.dirFor(t)
		dirs[dir] = true
		// Every task's part file, so a running download's is not an orphan.
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
	a.publishTasks(changed)

	for dir := range dirs {
		found, err := reclaim.Orphans(dir, claimed)
		if err != nil {
			// Usually a folder that does not exist yet, which holds no orphans.
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

// reclaimable reports whether a task is worth looking for on disk. Running
// tasks are excluded because their file is still being written, JD tasks
// because their files are on another machine. Collected tasks are scanned but
// never settled (see applyReclaim): the collector is where the user decides.
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

// isTorrentTask reports whether a task is a torrent. The info hash covers rows
// read back from the store.
func isTorrentTask(t *core.Task) bool { return t.Resolver == "torrent" || t.InfoHash != "" }

// applyReclaim applies the verdicts to the live tasks and returns copies of
// the changed ones, for the caller to persist off the lock, plus how many were
// settled as finished. Correcting a byte count is a change but not a saved
// download, hence the separate count.
//
//   - Complete settles the task as finished. It is the only verdict that can
//     be expensively wrong, so internal/reclaim reaches it on size alone only
//     when the trust setting allows.
//   - Mismatch means a checksum proved the file is something else. Only a
//     false byte count is cleared; the file is left for the collision policy
//     to handle when the download starts.
//   - Partial corrects the byte count only. Whether a partial can be resumed
//     is the backend's business (the embedded engine always renames around an
//     existing file).
//   - Recheck, Unproven and Absent change nothing.
//
// Checksum is set only when a checksum was actually computed. Note carries the
// explanation to open browsers and is not stored.
func (a *App) applyReclaim(findings []reclaim.Finding) (changed []taskCopy, settled int) {
	now := time.Now()
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, f := range findings {
		t := a.tasks[f.TaskID]
		// Skip tasks that changed state while the files were hashed.
		if t == nil || a.active[f.TaskID] || !reclaimable(t) {
			continue
		}
		switch f.Verdict {
		case reclaim.Complete:
			if t.Status == core.StatusCollected {
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
			t.MaxTries = 0
			t.Note = f.Detail
			t.ChangedAt = now
			if f.Basis == reclaim.BasisChecksum {
				t.Checksum = "ok"
			}
			// dispatchLocked reads a queued task's flags, not its status, so a
			// finished task must leave the queue.
			a.dequeueLocked(f.TaskID)
			// A finished link does not block its own re-add.
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
		changed = append(changed, a.copyLocked(t))
	}
	return changed, settled
}

// reclaimWitness returns the record tier's witness: the name and size of
// everything this instance's history says it finished. The history survives a
// cleared list, a moved box and a restored backup. It has no folder column, so
// it only counts once the file already matches this task's folder, name and
// length; a published checksum always outranks it. The strict tier gets nil,
// meaning "do not ask".
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

// witnessKey lower-cases the name, since Windows and macOS treat names that
// differ only in case as the same file.
func witnessKey(name string, size int64) string {
	return strings.ToLower(name) + "\x00" + strconv.FormatInt(size, 10)
}
