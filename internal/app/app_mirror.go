package app

// The second copy of a file the list already has.
//
// A mirror is the same release on a second hoster, and until this file existed
// the app did one thing with it: dropped it, leaving a line in an in-memory
// trace that the next restart cleared. That is the right default - two copies of
// one file is two downloads of one file - but it throws away the only thing that
// helps when the first hoster turns out to be dead, and it is the reason
// core.Task.MirrorOf was a column nothing ever wrote.
//
// So, when the user asks for it, the mirror is kept instead: staged as an
// ordinary task, parked so nothing starts it, and labelled with the download it
// is a copy of.
//
// The rest of the file is the step that was missing: what happens to that
// parked copy when the download it mirrors finally dies. The sibling is
// released and takes the dead task's job over - its folder, its package, its
// priority - because those are the three things somebody set for the file
// rather than for the link, and a copy that lands in the wrong folder under the
// wrong package is a spare tyre bolted to the wrong axle.
//
// It has a switch OF ITS OWN (settings.MirrorFailover) rather than riding on
// KeepMirrors, and it is off as well, because the two are not the same size of
// decision. Keeping a mirror costs a row in a list. Releasing one starts a
// transfer from a hoster the user did not pick, at whatever speed that hoster
// gives them, possibly from an account they do not have - and this build has no
// way to ask them first. That stays a decision.

import (
	"time"

	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/dedupe"
)

// keepsAsSibling reports whether a match is one the user asked to keep.
//
// A DUPLICATE IS NEVER KEPT, whatever the setting says. The same URL twice is a
// fact rather than a guess - it is the one verdict that needs no policy - and
// staging it a second time would put two rows in the list pointing at the same
// bytes on the same hoster, which is not a mirror of anything.
func (a *App) keepsAsSibling(m dedupe.Match) bool {
	return m.Verdict == dedupe.Mirror && a.Settings.Get().KeepMirrors
}

// stageSibling stages a link the mirror set folded away, as a second copy of the
// task it matched. It reports whether it did, so the caller can fall back to
// recording the link as skipped.
//
// The sibling is put on HOLD rather than switched off. Both flags keep the
// dispatcher away from it, but Hold is the one that means "not now" instead of
// "the user does not want this link", and lifting it is exactly the gesture
// somebody makes when the copy they were downloading dies. Enabled is the user's
// own switch and nothing here is entitled to write it.
func (a *App) stageSibling(t *core.Task, m dedupe.Match) bool {
	if !a.keepsAsSibling(m) {
		return false
	}
	t.MirrorOf = m.Of.ID
	t.Hold = true
	a.putSibling(t)
	return true
}

// putSibling stages the sibling.
//
// It is the one insert in this package that does not go through put, and the
// reason is put's first line: put refuses a link the mirror set already covers,
// which is precisely what this link is. Everything else it does happens here in
// the same order and under the same lock - the fresh id, the entry in the mirror
// set, the store write, the broadcast - and the entry matters most of the three:
// without it a third paste of this same URL would be staged as yet another
// sibling of the original instead of being recognised as the duplicate it is.
func (a *App) putSibling(t *core.Task) {
	a.mu.Lock()
	if t.ID == "" {
		t.ID = a.freshIDLocked()
	}
	a.tasks[t.ID] = t
	a.dupes.Add(linkEntry(t))
	c := *t
	a.mu.Unlock()
	_ = a.Store.Save(&c)
	a.Hub.Broadcast("task", &c)
}

// mirrorCanHelp reports whether a second hoster could plausibly get past a
// failure. Like addressMayHelp in app_errors.go it can only ever veto, never
// cause: anything unclassified answers yes, so a taxonomy that grows a name
// tomorrow does not silently switch the handover off.
//
// Two vetoes, and both are cases where the failure was never the link's fault.
// A full disk is the same disk for every hoster there has ever been, so the copy
// runs into the identical wall and buries the one failure somebody could have
// fixed under a second one. A cancelled run is this side standing the task down
// - a shutdown, a task taken away underneath the attempt - and a shutdown that
// starts a fresh transfer at a hoster nobody picked is the worst imaginable
// moment to make that choice for them.
//
// The five backend-named causes (core.ReasonBotCheck and its neighbours) are
// deliberately NOT vetoed here, which is the opposite of what addressMayHelp
// and retryCannotHelp decided about the same five - so it is written down
// rather than left to the fallthrough. A mirror is a different SITE, and not
// one of the five is a fact about the file: another host has not decided this
// address is a robot, does not put this video behind that channel's
// membership, may serve this region, and may hand over something yt-dlp can
// still read. Those are exactly the failures a second source exists for.
func mirrorCanHelp(r core.Reason) bool {
	switch r {
	case core.ReasonDiskFull, core.ReasonCancelled:
		return false
	}
	return true
}

// mirrorRootLocked answers the id every copy of one file is filed under: the
// download the first sibling was staged against. Caller holds a.mu.
//
// It walks rather than reading MirrorOf once, because the field says which entry
// the mirror set matched, not which task the group began with. Paste a third
// copy and the set may well answer with the SIBLING - putSibling files it there
// on purpose - so the third task points at the second, the second at the first,
// and a one-step read would put the two halves of one group under two different
// roots and lose the tail of the chain.
//
// Bounded rather than "until MirrorOf is empty". Nothing here can write a cycle
// - a match always names a row that was already in the set - but MirrorOf is a
// persisted column, and a loop arriving from a store some other build wrote
// would hang the dispatcher with a.mu held rather than merely misfile a link.
// Twenty hops is far beyond any list of copies of one release.
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

// parkedMirrorLocked picks the copy of a dead task's file that is next in line,
// or nil when the group has none left. Caller holds a.mu.
//
// A candidate is a sibling that is still parked and has not run: on hold, and
// waiting in the collector or in the queue. Those two states rather than
// StatusCollected alone, because "start everything" reaches a held sibling and
// moves it to StatusQueued - startTasks does not consult Hold, the dispatcher
// does (WaitingHold), which is what TestAKeptMirrorIsNotDispatched pins. Read as
// collected-only, the handover would find nothing for any user who has ever
// pressed start with the collector full, which is most of them.
//
// The rest of what the condition rules out it rules out by construction: the
// task that just died (settled, not waiting), any earlier hop of the same chain
// (released, so no longer on hold), and a copy the user switched off or the link
// filter parked - Enabled and Skipped are answers somebody already gave, and
// nothing here is entitled to overrule either.
//
// Oldest first, which is the order they were pasted and therefore the order a
// person working through the links by hand would try them. The tie-break on the
// id is not cosmetic: a.tasks is a map, so without it the choice between two
// copies staged in the same millisecond would come out of Go's map seed and
// differ from run to run.
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
// and hands it that task's job. It answers the sibling it released, for the
// caller to save and broadcast, and the retry delay still to be armed - zero
// whenever a handover happened, because the handover REPLACES the attempts that
// are left rather than running beside them. Two transfers of one file racing
// each other is the exact outcome the whole mirror set exists to prevent, and
// starting one here would be this file causing it. Caller holds a.mu.
//
// WHEN. Two moments count as "the source is finished" and no others. The
// ordinary one is the backoff being spent: retryIn is zero, every attempt the
// user allowed has been made, and the task is settled where somebody would
// otherwise read the error and paste the mirror by hand. The other is the host
// answering that the file is not there (ReasonGone). Every remaining attempt
// would ask the same host about the same missing file, so the handover takes
// their place, and the arming that already happened is taken back - left
// standing, the row would keep counting an attempt this download is never going
// to make, and show a "retrying automatically" mark that stops people acting on
// it while the copy is already running.
//
// WHAT IF THE MIRROR DIES TOO. It is an ordinary task, so it settles through
// this same function and looks for another parked copy of the same file. So yes,
// there is a chain, and it runs for as long as copies are left. It ends because
// every hop CONSUMES one: a released sibling has its hold cleared and is never
// parked again, so a group of N pasted copies allows at most N-1 handovers and
// then the last failure simply stands. It cannot go backwards either, because a
// task that already failed is not a candidate - only a collected one on hold is.
//
// WHAT HAPPENS TO THE ORIGINAL. It stays exactly where it is: StatusError, with
// its sentence, its reason and its own retry count intact. It is not removed and
// it is not repurposed. Somebody coming back to a finished download has to be
// able to see that the first hoster died and that the file in the folder came
// from the second one; a list that tidies the failure away tells them a story in
// which nothing went wrong, and the next time that hoster dies they have no
// reason to suspect it.
//
// DOES IT COUNT AS A RETRY. No, on either side. The dead task's counter is not
// advanced - it records what THAT link spent, and a different URL being tried is
// not another go at this one - and the sibling starts on zero with the full
// budget, because it is a different host that has never been asked. That is not
// a way around MaxRetries: the pool of copies is finite and one hop shorter
// every time, so the budget can be handed out only as often as the user pasted
// mirrors.
func (a *App) handOverToMirrorLocked(dead *core.Task, retryIn time.Duration) (*core.Task, time.Duration) {
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
	// The three fields that belong to the FILE rather than to the link, which is
	// why they are copied and the rest of the dead task is not: the folder and the
	// package are where the user expects this file to appear, and the priority is
	// how badly they wanted it. A sibling pasted in the same breath usually agrees
	// with all three, but a Packagizer rule or a hand-typed folder on the original
	// row does not reach the parked copy, and the point of a spare is that it
	// lands where the original was going.
	m.Dir, m.Package, m.Priority = dead.Dir, dead.Package, dead.Priority
	m.Hold = false
	m.Status = core.StatusQueued
	// Cleared with the sentence they belong to, exactly as startTasks does it: a
	// task on its way to a backend must not carry a verdict from before it ran.
	m.Error = ""
	m.Reason = core.ReasonUnknown
	m.Speed = 0
	// Dequeued before it is queued, ForceDownload's own move: a sibling that a
	// "start everything" already pushed into the wait queue is in it TWICE
	// otherwise, and the second entry is a second Start for one download the
	// moment a slot frees up.
	a.dequeueLocked(m.ID)
	a.queue = append(a.queue, m.ID)
	a.dispatchLocked()
	// Copied after the dispatch and not before, for startTasks' own reason: the
	// dispatcher settles what it turns down, and a copy taken a line earlier would
	// carry "queued" over a refusal in the store and on every open page.
	c := *m
	return &c, 0
}
