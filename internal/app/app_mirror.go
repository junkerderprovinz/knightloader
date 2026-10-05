package app

// Mirrors: a second copy of a file the list already has, on another hoster.
//
// By default a mirror is dropped. With KeepMirrors it is staged as an ordinary
// task, disabled, labelled with the download it copies. With MirrorFailover the
// parked copy is enabled when that download dies and takes over its folder,
// package and priority. Failover has its own switch, off by default, because it
// starts a transfer from a hoster the user did not pick.

import (
	"time"

	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/dedupe"
)

// keepsAsSibling reports whether a match is one the user asked to keep. A
// duplicate (the same URL twice) is never kept, whatever the setting: it would
// be a second row for the same bytes on the same hoster.
func (a *App) keepsAsSibling(m dedupe.Match) bool {
	return m.Verdict == dedupe.Mirror && a.Settings.Get().KeepMirrors
}

// stageSibling stages a link the mirror set folded away as a second copy of the
// task it matched, and reports whether it did so the caller can record the link
// as skipped otherwise.
//
// The sibling is disabled, the one way a link is parked, so the queue does not
// fetch the same file twice. Enabling it by hand starts it like any other link.
func (a *App) stageSibling(t *core.Task, m dedupe.Match) bool {
	if !a.keepsAsSibling(m) {
		return false
	}
	t.MirrorOf = m.Of.ID
	t.Enabled = false
	a.putSibling(t)
	return true
}

// putSibling stages the sibling. It bypasses put, which refuses a link the
// mirror set already covers, and otherwise does the same steps in the same
// order. The mirror set entry matters most: without it a third paste of this URL
// would become another sibling instead of a duplicate.
func (a *App) putSibling(t *core.Task) {
	a.mu.Lock()
	if t.ID == "" {
		t.ID = a.freshIDLocked()
	}
	a.tasks[t.ID] = t
	a.dupes.Add(linkEntry(t))
	c := a.copyLocked(t)
	a.mu.Unlock()
	a.publish(&c)
}

// mirrorCanHelp reports whether a second hoster could plausibly get past a
// failure. Like addressMayHelp it only vetoes, so an unclassified reason
// answers yes.
//
// A full disk is the same disk for every hoster, and a cancelled run is this
// side standing the task down, often at shutdown. The backend-specific causes
// (core.ReasonBotCheck and its neighbours) are not vetoed, unlike in
// addressMayHelp and retryCannotHelp: they are facts about a site, not the file,
// and another site is exactly what a mirror offers.
func mirrorCanHelp(r core.Reason) bool {
	switch r {
	case core.ReasonDiskFull, core.ReasonCancelled:
		return false
	}
	return true
}

// mirrorRoot returns the id every copy of one file is filed under: the download
// the first sibling was staged against.
//
// It walks the chain because the mirror set may match a third copy against a
// sibling rather than the original. The walk is bounded since MirrorOf is a
// persisted column, and a cycle from a foreign store would otherwise hang the
// dispatcher with a.mu held.
func mirrorRoot(tasks map[string]*core.Task, id string) string {
	for i := 0; i < 20; i++ {
		cur := tasks[id]
		if cur == nil || cur.MirrorOf == "" {
			return id
		}
		id = cur.MirrorOf
	}
	return id
}

// MirrorRoots maps every kept copy to the task its group is filed under, so a
// reader can count the copies of one file as one file.
func MirrorRoots(tasks []*core.Task) map[string]string {
	byID := make(map[string]*core.Task, len(tasks))
	for _, t := range tasks {
		byID[t.ID] = t
	}
	out := map[string]string{}
	for _, t := range tasks {
		if t.MirrorOf != "" {
			out[t.ID] = mirrorRoot(byID, t.ID)
		}
	}
	return out
}

// HandedOver returns the failed tasks whose file another copy has taken on: an
// enabled mirror in the same group that is on its way or finished. Once every
// such copy has failed too, nothing is left to carry the file and no failure
// is handed over.
func HandedOver(tasks []*core.Task) map[string]bool {
	byID := make(map[string]*core.Task, len(tasks))
	for _, t := range tasks {
		byID[t.ID] = t
	}
	carriers := map[string][]string{}
	for _, c := range tasks {
		if c.MirrorOf == "" || !c.Enabled || c.Skipped || c.Status == core.StatusCollected {
			continue
		}
		if c.Status == core.StatusError && c.NextTry.IsZero() {
			continue
		}
		root := mirrorRoot(byID, c.ID)
		carriers[root] = append(carriers[root], c.ID)
	}
	out := map[string]bool{}
	for _, t := range tasks {
		if t.Status != core.StatusError {
			continue
		}
		for _, id := range carriers[mirrorRoot(byID, t.ID)] {
			if id != t.ID {
				out[t.ID] = true
			}
		}
	}
	return out
}

// parkedMirrorLocked picks the next copy of a dead task's file, or nil when the
// group has none left. Caller holds a.mu.
//
// A candidate is disabled and waiting in the collector or the queue. A copy
// somebody enabled is on its way already, and a skipped one was rejected.
// Oldest first is paste order; the id breaks ties so the choice does not depend
// on map order.
func (a *App) parkedMirrorLocked(dead *core.Task) *core.Task {
	root := mirrorRoot(a.tasks, dead.ID)
	var best *core.Task
	for id, c := range a.tasks {
		if id == dead.ID || c.MirrorOf == "" || c.Enabled || c.Skipped {
			continue
		}
		if c.Status != core.StatusCollected && c.Status != core.StatusQueued {
			continue
		}
		if mirrorRoot(a.tasks, id) != root {
			continue
		}
		switch {
		case best == nil, c.CreatedAt.Before(best.CreatedAt):
			best = c
		case c.CreatedAt.Equal(best.CreatedAt) && c.ID < best.ID:
			best = c
		}
	}
	return best
}

// handOverToMirrorLocked enables the parked copy of a task that has just died
// and gives it that task's job. It returns the enabled sibling for the caller
// to save and broadcast, and the retry delay still to arm, which is zero after a
// handover so two transfers of one file never race. Caller holds a.mu.
//
// A handover happens when the retries are spent, or at once when the host says
// the file is gone (ReasonGone), in which case the retry already armed is taken
// back. A sibling that took over stays enabled and is never picked again, so N
// copies allow at most N-1 handovers. The original stays in StatusError with
// its reason and retry count, so the list still shows that the first hoster
// failed. The sibling starts with a full retry budget, since it is a different
// host.
func (a *App) handOverToMirrorLocked(dead *core.Task, retryIn time.Duration) (*taskCopy, time.Duration) {
	if !a.Settings.Get().MirrorFailover || !mirrorCanHelp(dead.Reason) {
		return nil, retryIn
	}
	if retryIn > 0 && dead.Reason != core.ReasonGone {
		return nil, retryIn
	}
	m := a.parkedMirrorLocked(dead)
	if m == nil {
		return nil, retryIn
	}
	if retryIn > 0 {
		dead.Retries--
		dead.NextTry = time.Time{}
	}
	// Folder, package and priority belong to the file rather than the link, and
	// a rule or hand edit on the original row never reached the parked copy.
	m.Dir, m.Package, m.Priority = dead.Dir, dead.Package, dead.Priority
	m.Enabled = true
	m.Status = core.StatusQueued
	// A task on its way to a backend must not carry a verdict from before it
	// ran, as in startTasks.
	m.ClearFailure()
	m.Speed = 0
	// A sibling already in the wait queue would otherwise be in it twice and be
	// started twice.
	a.dequeueLocked(m.ID)
	a.queue = append(a.queue, m.ID)
	a.dispatchLocked()
	// Copied after the dispatch, which may settle a refusal onto the task.
	c := a.copyLocked(m)
	return &c, 0
}
