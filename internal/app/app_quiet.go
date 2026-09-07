package app

// Quiet mode: a second set of limits, one switch that puts them in force, and a
// timetable window that can do the same thing on a clock.
//
// Nothing here throttles or refuses anything itself. It decides ONE flag and ONE
// number: whether the second set is in force, and what the combined speed limit
// therefore is. The flag reaches the dispatcher through cfgInForceLocked, and the
// number reaches the three meters the way every other limit does, through
// a.limitInForce and applyBudget (app_budget.go). A quiet mode that pushed the
// engine, JD and yt-dlp itself would be three numbers to keep in step, and all
// three would be overwritten by the next budget tick three seconds later.

import (
	"time"

	"github.com/junkerderprovinz/knightloader/internal/schedule"
	"github.com/junkerderprovinz/knightloader/internal/settings"
)

// quietState is "what the human asked for" against "what is in force" - the same
// pair a.manualHalt and a.halted already are, and kept apart for the same reason:
// a timetable window writes the second one, so a mode the user switched on by
// hand would otherwise be switched off again by the end of a window that had
// nothing to do with it.
//
// One field on the App rather than three, following iconCache: the fields stay in
// the file that owns them.
type quietState struct {
	// manual is the switch, and it is the base the timetable is evaluated
	// against (scheduleBase).
	manual bool
	// inForce is what the queue is actually doing right now.
	inForce bool
	// baseSeen is the manual value scheduleBase last handed the runner. It exists
	// only so applySchedule can tell an answer computed BEFORE a press from one
	// computed after it - the identical marker a.scheduleBaseHalt is, closing the
	// identical race. See applySchedule.
	baseSeen bool
}

// WHAT WINS when the switch and the timetable disagree: a window beats the
// switch, and the switch decides the gaps between windows.
//
// This is the same answer the master switch already gives, and it is the same
// answer on purpose - one rule to learn about this box rather than two:
//
//   - the switch the user pressed is the BASE the timetable is evaluated against
//     (scheduleBase), so wherever no window covers now, it decides. A turtle
//     pressed at 03:00 is still pressed at noon;
//   - a window that says "quiet" wins while it is open, exactly as a pause window
//     wins over a queue the user has not halted. A window is a standing
//     instruction with a visible reason and an end, and letting an old press
//     override it here would mean overriding it again the next night, and every
//     night after, for a reason nobody could see on screen;
//   - pressing the turtle INSIDE a window still takes effect at once and holds
//     until the next boundary, because SetQuiet writes what is in force and
//     deliberately does not wake the schedule runner. That is the same escape
//     hatch SetHalted documents, and it is the half of the rule that matters
//     most: a button that visibly does nothing for the next six hours teaches
//     people the feature is broken, where one that is overruled at 06:00 teaches
//     them what the window is for.
//
// The rejected alternative was "the hand wins until it is pressed again", which
// reads well until the second night: the timetable would then be suppressed by a
// press made days ago that nobody remembers making, and there is nothing on
// screen that could explain why the window stopped working.

// speedInForce is the one rule about which of two speed limits applies. It is a
// function rather than two lines in two places so that applySchedule and
// SetQuiet cannot answer it differently.
//
// windowLimit is what the timetable arrived at, which is already the user's own
// figure wherever no speed window covers now - that is what schedule.At's base
// is for.
//
// Quiet mode's own cap REPLACES it rather than being the smaller of the two. A
// second set of limits is what the user is configuring, and a mode that silently
// kept the other number whenever that happened to be lower would make the figure
// on the quiet page one the box sometimes ignores: stored, shown back, and then
// not honoured by the thing it was set for. Zero on the quiet side is the one
// exception and it is not an exception to this rule - it means the field is not
// configured at all (settings.QuietLimits), so there is no second number to win.
func speedInForce(cfg settings.Settings, windowLimit int64, quiet bool) int64 {
	if !quiet {
		return windowLimit
	}
	return cfg.UnderQuiet(windowLimit).SpeedLimit
}

// cfgInForceLocked is what the DISPATCHER reads in place of a.Settings.Get():
// the user's own numbers with quiet mode's laid over them while it is in force.
//
// The substitution happens once, here, rather than at each place dispatchLocked
// reads a figure (cfg.MaxConcurrent, and the cfg maxPerHostFor is handed),
// because those two have to agree with each other - and because the next
// concurrency number added there would otherwise have to remember this mode
// exists.
//
// The speed limit is not decided here. That one is shared out between the three
// meters by applyBudget through a.limitInForce, and the dispatcher has never
// read it.
//
// NOT YET CALLED FROM dispatchLocked. Its one line lives in app_dispatch.go,
// which this wave hands to another agent, so it is written up rather than
// written in: until `cfg := a.Settings.Get()` at the top of dispatchLocked
// becomes `cfg := a.cfgInForceLocked()`, quiet mode moves the speed and not the
// slot count. Everything else about the mode is wired.
//
// Caller holds a.mu.
func (a *App) cfgInForceLocked() settings.Settings {
	cfg := a.Settings.Get()
	if !a.quiet.inForce {
		return cfg
	}
	// a.limitInForce is already the quiet answer where there is one, so handing
	// it back in changes nothing about speed. It is passed rather than left out
	// because UnderQuiet takes the limit that applies without the mode, and the
	// honest value for that here is whatever is in force.
	return cfg.UnderQuiet(a.limitInForce)
}

// SetQuiet turns quiet mode on or off by hand. It is the turtle button.
//
// Three things happen and the order is most of the implementation:
//
//  1. the limit is recomputed and written to a.limitInForce, the one number
//     applyBudget shares out between the engine, JD and yt-dlp;
//  2. the dispatcher runs, so a mode just switched OFF fills the slots it hands
//     back instead of waiting for the next event to notice. Switching it ON
//     never stops a transfer that is already running - the dispatcher hands
//     slots out and never takes them back, which is what this mode has to mean
//     if it is not to be the hard stop wearing a different icon;
//  3. the schedule runner is NOT woken. See the note above: waking it would
//     re-evaluate the timetable against the press that just happened and reverse
//     it a millisecond later, inside any window that says otherwise.
func (a *App) SetQuiet(on bool) {
	// Read before a.mu, like startTasks does, purely so the critical section
	// below stays about the queue's own state. Settings has its own lock.
	cfg := a.Settings.Get()
	now := time.Now()
	a.mu.Lock()
	a.quiet.manual = on
	// Written to what is in force as well as to the switch, exactly as SetHalted
	// writes manualHalt AND halted. The press is the answer until the next
	// boundary.
	a.quiet.inForce = on
	// The timetable is asked again rather than its last answer being kept on the
	// App to be read here. At is pure and nothing changes between two boundaries
	// - that is the promise the whole schedule package is built on, the one that
	// lets its runner sleep through the gap - so this arrives at exactly the
	// number the runner's last pass did, without a copy of it that can go stale.
	//
	// st.Quiet is deliberately not read. The base below carries the press so the
	// evaluation is honest about what it was given, but a window's answer wins at
	// boundaries and not at the moment somebody puts a finger on the button.
	st := schedule.Compile(cfg.Schedule).At(now, schedule.State{
		Paused: a.manualHalt,
		Limit:  cfg.SpeedLimit,
		Quiet:  on,
	})
	a.limitInForce = speedInForce(cfg, st.Limit, on)
	a.dispatchLocked()
	a.mu.Unlock()
	a.applyBudget()
	a.Hub.Broadcast("queue", a.Queue())
}
