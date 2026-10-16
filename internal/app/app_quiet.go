package app

// Quiet mode: a second set of limits, a switch that puts them in force, and
// timetable windows that do the same on a clock. Nothing here throttles
// anything. It decides whether the second set applies and what speed limit
// follows; the flag reaches the dispatcher through cfgInForceLocked and the
// number reaches the meters through a.limitInForce and applyBudget, which would
// overwrite anything pushed to a backend directly.
//
// The switch and the timetable resolve as they do for the master switch. The
// switch is the base the timetable is evaluated against (scheduleBase), so it
// decides where no window covers, and a quiet window wins while it is open. A
// press inside a window holds until the next boundary, because SetQuiet does
// not wake the runner; letting it override windows would suppress the timetable
// every night for a reason nobody can see.

import (
	"log"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/schedule"
	"github.com/junkerderprovinz/knightloader/internal/settings"
)

// quietState separates what the user asked for from what is in force, like
// a.manualHalt and a.halted, so the end of a window does not switch off a mode
// the user turned on by hand.
type quietState struct {
	// manual is the switch, and the base the timetable is evaluated against
	// (scheduleBase).
	manual bool
	// inForce is what the queue is doing right now.
	inForce bool
	// baseSeen is the manual value scheduleBase last handed the runner, so
	// applySchedule can tell an answer computed before a press from one computed
	// after it. It closes the same race a.scheduleBaseHalt does.
	baseSeen bool
	// playback is the media server watcher's answer: something that counts is
	// playing (app_playback.go).
	playback bool
	// waived is a press that switched the mode off while playback held it. It
	// holds until that playback ends, as a press inside a window holds until
	// the window's boundary; without it the button would do nothing for as
	// long as the film runs.
	waived bool
}

// on is whether the second set of limits applies: by the switch, a window, or
// a stream nobody waived.
func (q quietState) on() bool { return q.inForce || q.byPlayback() }

// byPlayback is whether a stream holds the mode, whatever the switch says.
func (q quietState) byPlayback() bool { return q.playback && !q.waived }

// speedInForce decides which of the two speed limits applies, in one place so
// applySchedule and SetQuiet cannot disagree. windowLimit is the timetable's
// answer, which is the user's own figure where no speed window covers now.
//
// Quiet mode's cap replaces that limit rather than taking the smaller of the
// two, so the figure on the quiet page is always the one honoured. A quiet cap
// of zero means unconfigured (settings.QuietLimits).
func speedInForce(cfg settings.Settings, windowLimit int64, quiet bool) int64 {
	if !quiet {
		return windowLimit
	}
	return cfg.UnderQuiet(windowLimit).SpeedLimit
}

// cfgInForceLocked is what the dispatcher reads in place of a.Settings.Get():
// the user's settings with quiet mode's laid over them while it is in force.
// Substituting once here keeps every concurrency figure the dispatcher reads in
// agreement. The speed limit is shared out by applyBudget instead. Caller holds
// a.mu.
func (a *App) cfgInForceLocked() settings.Settings {
	cfg := a.Settings.Get()
	if !a.quiet.on() {
		return cfg
	}
	// a.limitInForce already carries the quiet answer, so passing it changes
	// nothing about speed; UnderQuiet just needs the limit that applies.
	return cfg.UnderQuiet(a.limitInForce)
}

// SetQuiet turns quiet mode on or off by hand; it is the turtle button.
//
// It recomputes a.limitInForce for applyBudget, then runs the dispatcher so a
// mode switched off fills its freed slots at once. Switching it on never stops a
// running transfer: the dispatcher hands slots out and never takes them back.
// The schedule runner is not woken, since it would re-evaluate the timetable and
// reverse the press inside any window that says otherwise.
//
// Switching it off while a stream holds it waives that stream until it ends,
// and lets the unpacking queue go with it.
func (a *App) SetQuiet(on bool) {
	// Settings has its own lock; reading it first keeps the critical section
	// about the queue.
	cfg := a.Settings.Get()
	a.mu.Lock()
	a.quiet.manual = on
	// Written to what is in force too, as SetHalted writes both manualHalt and
	// halted: the press holds until the next boundary.
	a.quiet.inForce = on
	a.quiet.waived = !on && a.quiet.playback
	a.settleQuietLocked(cfg)
	a.mu.Unlock()
	a.afterQuietChange()
}

// settleQuietLocked puts what the quiet state now says into the limit and the
// slots, for a press or a stream starting or ending. Caller holds a.mu.
func (a *App) settleQuietLocked(cfg settings.Settings) {
	// At is pure and nothing changes between boundaries, so asking again gives
	// the runner's last answer without keeping a copy that could go stale.
	// st.Quiet is not read: a window wins at boundaries, not at a press.
	st := a.sched.Suspension().At(schedule.Compile(cfg.Schedule), time.Now(), schedule.State{
		Paused: a.manualHalt,
		Limit:  cfg.SpeedLimit,
		Quiet:  a.quiet.manual,
	})
	a.limitInForce = speedInForce(cfg, st.Limit, a.quiet.on())
	a.dispatchLocked()
	a.resumeExtractionsLocked()
}

// applyQuiet is a save's half of quiet mode. The timetable runner applies only
// when its answer changes, and a new quiet figure does not change it, so the
// limit in force is worked out again here. The watcher asks again at once, so
// switching playback off releases the queue now rather than at the next tick,
// and a cleared unpacking pause lets the waiting archives go.
func (a *App) applyQuiet(cfg settings.Settings) {
	if a.Playback != nil {
		a.Playback.Wake()
	}
	a.mu.Lock()
	a.settleQuietLocked(cfg)
	a.mu.Unlock()
	a.afterQuietChange()
}

// afterQuietChange is the work outside a.mu once the quiet state moved: the
// meters, the torrents' upload cap, and the tabs.
func (a *App) afterQuietChange() {
	a.applyBudget()
	if err := a.pushTorrentConfig(); err != nil {
		log.Printf("quiet mode's upload limit not applied: %v", err)
	}
	a.publishHeldExtractions()
	a.Hub.Broadcast("queue", a.Queue())
}
