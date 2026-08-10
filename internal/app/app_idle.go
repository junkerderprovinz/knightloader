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
