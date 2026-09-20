package app

// End-of-queue: once the queue has nothing enabled left to run, an optional
// action fires after a cancellable countdown. internal/idleaction holds the
// state machine; this file defines idle in terms of the task list and carries
// out the actions.

import (
	"log"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/idleaction"
)

// idleActionPoll overrides internal/idleaction's own poll interval when it is
// not zero. Tests push it out of the way, so that a check that ApplySettings
// arms the countdown cannot pass because of the poll.
var idleActionPoll time.Duration

// queueIdleForAction reports whether the queue has nothing enabled left to run,
// start or finish. Disabled links are subtracted so they cannot hold the
// action off for ever, while paused and held tasks still count as work left.
// A seeding torrent is StatusDone with a flag beside it, so Counters already
// leaves it out.
func (a *App) queueIdleForAction() bool {
	c := a.Counters()
	return c.Files == c.Disabled
}

// fireIdleAction carries out one action. It runs on the controller's goroutine,
// which must not block because Close waits for it, so every branch either
// returns at once or hands off. RequestExit is a non-blocking send.
func (a *App) fireIdleAction(act idleaction.Action) {
	switch act {
	case idleaction.ActionPause:
		log.Printf("idle action: the queue has nothing left to do; pausing")
		a.SetHalted(true)
	case idleaction.ActionQuit:
		// The same exit POST /api/system/quit uses, so the countdown has no
		// shutdown path of its own.
		if a.RequestExit == nil {
			// Actions are not filtered by capability, so a settings.json holding
			// "quit" can reach a build that cannot quit. The countdown promised
			// something, so the run is recorded.
			a.recordIdleRun(IdleRun{Action: act, Problem: idleaction.ProblemNotSupported})
			return
		}
		log.Printf("idle action: the queue has nothing left to do; quitting")
		if !a.RequestExit(false) {
			// What was asked for is happening anyway, so this is a clean run.
			log.Printf("idle action: a shutdown was already in progress")
		}
		a.recordIdleRun(IdleRun{Action: act})
	case idleaction.ActionCommand:
		a.runIdleCommand(a.Settings.Get().IdleAction.Command)
	case idleaction.ActionSuspend:
		if a.RequestSuspend == nil {
			// The container build, and every test.
			a.recordIdleRun(IdleRun{Action: act, Problem: idleaction.ProblemNotSupported})
			return
		}
		log.Printf("idle action: the queue has nothing left to do; asking the system to sleep")
		a.runIdleSuspend()
	default:
		// The controller never fires ActionNone, so this is a configuration
		// newer than the build.
		log.Printf("idle action: %q is not an action this build knows how to carry out", string(act))
	}
}

// IdleActionState reports the configuration, whether the queue is idle, the
// instant a running countdown fires (so a reloaded page shows the same
// deadline) and what the last action did. The stored command line is redacted
// because this document goes to the browser.
func (a *App) IdleActionState() IdleState {
	st := a.idleAction.State()
	st.Config = st.Config.Redacted()
	out := IdleState{State: st}
	if run, ok := a.LastIdleRun(); ok {
		out.LastRun = &run
	}
	return out
}

// CancelIdleAction calls off a running countdown without turning the feature
// off.
func (a *App) CancelIdleAction() {
	a.idleAction.Cancel()
}
