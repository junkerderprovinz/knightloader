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
// is a copy of. Nothing fails over to it automatically. That is deliberate and
// it is why the setting is off by default - an automatic switch to a second
// source is a decision about which of two files you end up with, and this build
// has no way to tell the user it made one.

import (
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
