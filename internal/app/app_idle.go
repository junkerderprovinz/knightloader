package app

// End-of-queue: once the wait queue has nothing enabled left to run, start or
// finish, an optional action fires after a cancellable countdown. See
// internal/idleaction for the state machine; this file is the seam between it
// and the app - what "idle" means here in terms of the task list, and what
// each action actually does.
//
// Wired from a new file rather than from app_queue.go or app_dispatch.go on
// purpose (build-plan.md's Wave 10B brief): both are complete, heavily
// tested, and 10A works in schedule-adjacent territory in this same file this
// wave. Nothing here calls into either of them - it reads the wait queue the
// same way any HTTP handler already does, through the existing Counters, and
// reacts to it on internal/idleaction.Controller's own timer rather than
// being invoked FROM their dispatch path.

import (
	"log"

	"github.com/junkerderprovinz/knightloader/internal/idleaction"
)

// queueIdleForAction reports whether the wait queue has nothing enabled left
// to run, start or finish - internal/idleaction.Controller polls this on its
// own schedule rather than being told about it from app_dispatch.go.
//
// Counters already draws exactly the line this needs: Files counts every
// task not yet done, failed, or still sitting unconfirmed in the collector,
// and subtracting Disabled is what keeps a link the user has deliberately
// switched off from pinning the idle action off forever - the everyday case
// this guards is somebody disabling two links they are not ready for while
// the other ten finish and the box is meant to go quiet. A manually paused or
// held task is NOT subtracted: both are "wait a bit", not "never", and either
// one still counts as work left to do, holding the action off exactly as a
// running download would.
//
// A SEEDING TORRENT (core.Task.Seeding == true) NEEDED NO EXCLUSION ADDED
// HERE, and that is a verified finding for this wave, not an oversight.
// docs/torrent-support.md's decision 4 ("a torrent that is only seeding does
// not count as owed work, or one perpetually-seeding torrent would
// permanently disable this whole feature") is already met, by construction:
// Seeding is a flag beside Status == core.StatusDone and never a status of
// its own (core.Task's own doc comment on the field - the same "flag, not a
// new core.Status" rule build-plan.md section 4 conflict 2 has held since
// Wave 1), and the switch inside Counters already treats StatusDone as not
// owed. A seeding task therefore never reaches c.Files at all, exactly like
// any other finished download - see TestSeedingTorrentDoesNotBlockTheIdleAction
// for this proven against a real App rather than left as a claim in a
// comment. The one change that WOULD have broken this silently is a second
// core.Status for "seeding", which is precisely the shape this rule already
// forbids.
//
// watchQueueIdleForScripts (internal/app/app_script.go, Wave 11) is a second,
// independent reader of this exact function, polling it for an unrelated
// reason (firing a script trigger rather than pausing the queue). It needed
// no change here either, for the same reason: one predicate, two readers,
// and neither has to know the other exists for both to read a seeding
// torrent correctly.
func (a *App) queueIdleForAction() bool {
	c := a.Counters()
	return c.Files == c.Disabled
}

// fireIdleAction carries out one action. It is the one place an Action value
// turns into something happening, so an action added later has exactly one
// switch to extend - see idleaction.Actions' own doc comment for why that
// list is only ActionNone and ActionPause today.
func (a *App) fireIdleAction(act idleaction.Action) {
	switch act {
	case idleaction.ActionPause:
		log.Printf("idle action: the queue has nothing left to do; pausing")
		a.SetHalted(true)
	default:
		// ActionNone never reaches here - Controller only fires when its
		// configured action is not ActionNone - so anything else landing
		// here is a config that has outrun this build's switch, not a
		// state to silently act on. Logged rather than ignored: the
		// countdown the user watched DID promise something would happen.
		log.Printf("idle action: %q is not an action this build knows how to carry out", string(act))
	}
}

// IdleActionState reports the countdown as it stands right now: the
// configuration, whether the queue is idle, and - if a countdown is running -
// the absolute instant it fires, so a reloaded page recomputes "N seconds
// left" from the same deadline the server is counting down to rather than
// restarting its own clock.
func (a *App) IdleActionState() idleaction.State {
	return a.idleAction.State()
}

// CancelIdleAction calls off a countdown in progress without turning the
// feature off - see idleaction.Controller.Cancel.
func (a *App) CancelIdleAction() {
	a.idleAction.Cancel()
}
