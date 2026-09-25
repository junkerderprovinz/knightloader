package app

// Mirrors: a second copy of a file the list already has, on another hoster.
//
// By default a mirror is dropped. With KeepMirrors it is staged as an ordinary
// task on hold, labelled with the download it copies. With MirrorFailover the
// parked copy is released when that download dies and takes over its folder,
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
// The sibling is put on hold rather than disabled: Hold means "not now", while
// Enabled is the user's own switch and nothing here writes it.
func (a *App) stageSibling(t *core.Task, m dedupe.Match) bool {
	if !a.keepsAsSibling(m) {
		return false
	}
	t.MirrorOf = m.Of.ID
	t.Hold = true
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

// mirrorRootLocked returns the id every copy of one file is filed under: the
// download the first sibling was staged against. Caller holds a.mu.
//
// It walks the chain because the mirror set may match a third copy against a
// sibling rather than the original. The walk is bounded since MirrorOf is a
// persisted column, and a cycle from a foreign store would otherwise hang the
// dispatcher with a.mu held.
func (a *App) mirrorRootLocked(t *core.Task) string {
	id := t.ID
	for i := 0; i < 20; i++ {
		cur := a.tasks[id]
		if cur == nil || cur.MirrorOf == "" {
			return id
		}
		id = cur.MirrorOf
	}
	return id
}

// parkedMirrorLocked picks the next copy of a dead task's file, or nil when the
// group has none left. Caller holds a.mu.
//
// A candidate is on hold and waiting in the collector or the queue: "start
// everything" moves a held sibling to StatusQueued, and only the dispatcher
// honours Hold. Disabled and skipped copies are left alone. Oldest first is
// paste order; the id breaks ties so the choice does not depend on map order.
func (a *App) parkedMirrorLocked(dead *core.Task) *core.Task {
	root := a.mirrorRootLocked(dead)
	var best *core.Task
	for id, c := range a.tasks {
		if id == dead.ID || c.MirrorOf == "" || !c.Hold {
			continue
		}
		if c.Status != core.StatusCollected && c.Status != core.StatusQueued {
			continue
		}
		if !c.Enabled || c.Skipped {
			continue
		}
		if a.mirrorRootLocked(c) != root {
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

// handOverToMirrorLocked releases the parked copy of a task that has just died
// and gives it that task's job. It returns the released sibling for the caller
// to save and broadcast, and the retry delay still to arm, which is zero after a
// handover so two transfers of one file never race. Caller holds a.mu.
//
// A handover happens when the retries are spent, or at once when the host says
// the file is gone (ReasonGone), in which case the retry already armed is taken
// back. A released sibling never parks again, so N copies allow at most N-1
// handovers. The original stays in StatusError with its reason and retry count,
// so the list still shows that the first hoster failed. The sibling starts with
// a full retry budget, since it is a different host.
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
	m.Hold = false
	m.Status = core.StatusQueued
	// A task on its way to a backend must not carry a verdict from before it
	// ran, as in startTasks.
	m.Error = ""
	m.Reason = core.ReasonUnknown
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
