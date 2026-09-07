package app

import (
	"testing"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/schedule"
	"github.com/junkerderprovinz/knightloader/internal/settings"
)

const (
	quietDaytime = 8 << 20   // the speed limit on the settings page
	quietWindow  = 2 << 20   // what a nightly limit window says instead
	quietTurtle  = 512 << 10 // what quiet mode says
)

// newTurtleApp builds an app with a known set of loud and quiet numbers, and with
// its schedule runner stopped.
//
// Stopping the runner is what makes these tests deterministic rather than a coin
// toss, for the reason TestScheduleCannotUndoAHardStop spells out: the base
// marker these tests drive by hand has exactly one reader in production, and a
// live loop taking that reading between two lines of a test body is a second one.
func newTurtleApp(t *testing.T, s settings.Settings) *App {
	t.Helper()
	a, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { a.Close() })
	if err := a.sched.Close(); err != nil {
		t.Fatal(err)
	}
	s.DownloadDir = t.TempDir()
	if _, err := a.ApplySettings(s); err != nil {
		t.Fatal(err)
	}
	return a
}

func loudAndQuiet() settings.Settings {
	return settings.Settings{
		MaxConcurrent: 4,
		MaxPerHost:    4,
		SpeedLimit:    quietDaytime,
		Quiet:         settings.QuietLimits{SpeedLimit: quietTurtle, MaxConcurrent: 1},
	}
}

func limitInForce(t *testing.T, a *App) int64 {
	t.Helper()
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.limitInForce
}

// allDay is a timetable covering every minute of every day with one action. Two
// rows rather than one because a window may not start and end on the same minute,
// and "00:00 to 00:00" is refused rather than read as a whole day.
func allDay(action schedule.Action) []schedule.Entry {
	days := []time.Weekday{time.Sunday, time.Monday, time.Tuesday, time.Wednesday, time.Thursday, time.Friday, time.Saturday}
	return []schedule.Entry{
		{Days: days, Start: "00:00", End: "12:00", Action: action},
		{Days: days, Start: "12:00", End: "00:00", Action: action},
	}
}

// tick is the two steps the schedule runner takes at every boundary, in the order
// it takes them: read the base, evaluate the timetable against it, apply. Written
// out rather than hidden behind Runner because these tests are about what happens
// BETWEEN those steps.
func tick(a *App) {
	a.applySchedule(schedule.Compile(a.Settings.Get().Schedule).At(time.Now(), a.scheduleBase()))
}

// TestQuietModeSurvivesAScheduleBoundary is the trap this mode is most likely to
// fall into, and the reason its limit is written where it is.
//
// a.limitInForce is the ONE number applyBudget shares out between the engine, JD
// and yt-dlp. The schedule writes it at every boundary. A quiet mode that pushed
// its limit anywhere else - straight onto the throttle, or onto the three meters
// - would work perfectly until the next window edge and then be undone without a
// word: the box would go back to full speed in the middle of the night, with the
// turtle still lit on screen.
func TestQuietModeSurvivesAScheduleBoundary(t *testing.T) {
	a := newTurtleApp(t, loudAndQuiet())

	a.SetQuiet(true)
	if got := limitInForce(t, a); got != quietTurtle {
		t.Fatalf("the limit in force is %d after the press, want the quiet %d", got, quietTurtle)
	}

	// A boundary that has nothing to say about quiet mode. This is what the
	// runner hands applySchedule at every edge of every unrelated window, and on
	// an empty timetable it is every wake-up there is.
	tick(a)

	if got := limitInForce(t, a); got != quietTurtle {
		t.Errorf("the limit in force is %d after a boundary, want the quiet %d: the schedule wrote over the turtle", got, quietTurtle)
	}
	if !a.Queue().Quiet {
		t.Error("quiet mode switched itself off at a boundary that never mentioned it")
	}
}

// TestQuietWindowWinsOverTheSwitch is the precedence question answered out loud,
// in the direction app_quiet.go's note commits to: a window beats the switch
// while it is open, and hands the switch its answer back at the end.
//
// The opposite rule ("the hand wins until it is pressed again") reads well until
// the second night, when a press nobody remembers making suppresses the timetable
// with nothing on screen to explain it.
func TestQuietWindowWinsOverTheSwitch(t *testing.T) {
	s := loudAndQuiet()
	s.Schedule = allDay(schedule.ActionQuiet)
	a := newTurtleApp(t, s)

	// The switch is off. The window is open. The window wins.
	tick(a)
	if !a.Queue().Quiet {
		t.Fatal("a quiet window did not put the mode in force")
	}
	if got := limitInForce(t, a); got != quietTurtle {
		t.Errorf("the limit in force is %d inside a quiet window, want %d", got, quietTurtle)
	}

	// The window ends - here, by being taken out of the timetable, which is the
	// same input applySchedule sees at a closing edge.
	s.Schedule = nil
	if _, err := a.ApplySettings(s); err != nil {
		t.Fatal(err)
	}
	tick(a)
	if a.Queue().Quiet {
		t.Error("the mode stayed in force after its window closed, with nothing holding it on")
	}
	if got := limitInForce(t, a); got != quietDaytime {
		t.Errorf("the limit in force is %d after the window closed, want the ordinary %d", got, quietDaytime)
	}
}

// TestQuietPressInsideAWindowHoldsUntilTheNextBoundary is the other half of that
// rule, and the half a user actually touches. Pressing the turtle off inside a
// quiet window has to DO something: SetQuiet writes what is in force and
// deliberately does not wake the runner, so the press holds until the next
// boundary rather than being reversed a millisecond later.
//
// Without it the button would sit there visibly dead for the length of the
// window, which is how people learn a feature is broken.
func TestQuietPressInsideAWindowHoldsUntilTheNextBoundary(t *testing.T) {
	s := loudAndQuiet()
	s.Schedule = allDay(schedule.ActionQuiet)
	a := newTurtleApp(t, s)

	tick(a)
	if !a.Queue().Quiet {
		t.Fatal("fixture broken: the window should have put the mode in force")
	}

	a.SetQuiet(false)
	if a.Queue().Quiet {
		t.Error("the turtle pressed inside a quiet window did nothing at all")
	}
	if got := limitInForce(t, a); got != quietDaytime {
		t.Errorf("the limit in force is %d after switching the mode off, want the ordinary %d", got, quietDaytime)
	}

	// And the window takes it back at the next boundary, which is the whole point
	// of it being a window.
	tick(a)
	if !a.Queue().Quiet {
		t.Error("the window did not reassert itself at the next boundary")
	}
}

// TestQuietCannotBeUndoneByAStaleBase is the hard stop's race, on the new flag.
//
// The runner reads the base under the lock, DROPS it, evaluates the timetable,
// and only then applies the answer. A press that lands in that gap is about to be
// overwritten by a reading taken before it happened. The interleaving is written
// out by hand rather than raced for, for the reason
// TestScheduleCannotUndoAHardStop gives: a test that reproduces it by chance goes
// on being a flake in the other direction.
func TestQuietCannotBeUndoneByAStaleBase(t *testing.T) {
	a := newTurtleApp(t, loudAndQuiet())

	// 1. The runner reads the base. Nothing has been pressed, so it reads "loud".
	stale := a.scheduleBase()
	if stale.Quiet {
		t.Fatal("fixture broken: the base should read as loud before the press")
	}

	// 2. The press lands while the runner is between its read and its apply.
	a.SetQuiet(true)

	// 3. The runner applies the answer it computed from the stale base.
	a.applySchedule(stale)

	if !a.Queue().Quiet {
		t.Error("the schedule cleared a quiet mode the user had just switched on by hand")
	}
	if got := limitInForce(t, a); got != quietTurtle {
		t.Errorf("the limit in force is %d, want the quiet %d: the stale answer took the limit with it", got, quietTurtle)
	}
}

// TestQuietCutsTheSlotCount covers the other half of the mode: the numbers the
// DISPATCHER reads. Both have to move together - a quiet mode that halves the
// speed and leaves eight transfers running is eight transfers crawling, which is
// slower to finish and no quieter on the line.
func TestQuietCutsTheSlotCount(t *testing.T) {
	a := newTurtleApp(t, loudAndQuiet())

	cfg := func() settings.Settings {
		a.mu.Lock()
		defer a.mu.Unlock()
		return a.cfgInForceLocked()
	}

	if got := cfg(); got.MaxConcurrent != 4 || got.MaxPerHost != 4 {
		t.Fatalf("before the press: %d concurrent, %d per host, want the user's own 4 and 4", got.MaxConcurrent, got.MaxPerHost)
	}

	a.SetQuiet(true)
	got := cfg()
	if got.MaxConcurrent != 1 {
		t.Errorf("MaxConcurrent in force = %d, want the quiet 1", got.MaxConcurrent)
	}
	// Pulled down with it. Left at 4 it would be a per-host figure the dispatcher
	// can never reach, and the two numbers together would read as "one at a time,
	// four per host".
	if got.MaxPerHost != 1 {
		t.Errorf("MaxPerHost in force = %d, want it pulled down to the quiet 1", got.MaxPerHost)
	}

	a.SetQuiet(false)
	if got := cfg(); got.MaxConcurrent != 4 || got.MaxPerHost != 4 {
		t.Errorf("after switching off: %d concurrent, %d per host, want the user's own 4 and 4 back", got.MaxConcurrent, got.MaxPerHost)
	}
}

// TestQuietSpeedBeatsALimitWindow settles the case where both have an opinion
// about speed. Quiet mode replaces the window's figure rather than taking the
// smaller of the two: a mode that sometimes ignored the number on its own page,
// depending on the time of day, is a setting stored, shown back, and then not
// honoured.
func TestQuietSpeedBeatsALimitWindow(t *testing.T) {
	s := loudAndQuiet()
	s.Schedule = allDay(schedule.ActionLimit)
	for i := range s.Schedule {
		s.Schedule[i].Limit = quietWindow
	}
	a := newTurtleApp(t, s)

	tick(a)
	if got := limitInForce(t, a); got != quietWindow {
		t.Fatalf("fixture broken: the limit in force is %d, want the window's %d", got, quietWindow)
	}

	a.SetQuiet(true)
	if got := limitInForce(t, a); got != quietTurtle {
		t.Errorf("the limit in force is %d with the turtle on inside a limit window, want the quiet %d", got, quietTurtle)
	}
	// And it stays that way across the boundary, which is where writing it past
	// applySchedule would have shown up.
	tick(a)
	if got := limitInForce(t, a); got != quietTurtle {
		t.Errorf("the limit in force is %d after a boundary, want the quiet %d", got, quietTurtle)
	}
}

// TestQuietWithNoSpeedOfItsOwnLeavesTheWindowAlone is the zero case, and it is
// the one that decides whether this mode is safe to ship. Zero in the quiet
// figures means "not configured", never "unlimited" - a turtle whose speed field
// is empty and which therefore takes the brakes OFF, inside a nightly window
// somebody set precisely to keep them on, is the one failure it can never have.
func TestQuietWithNoSpeedOfItsOwnLeavesTheWindowAlone(t *testing.T) {
	s := loudAndQuiet()
	s.Quiet.SpeedLimit = 0
	s.Schedule = allDay(schedule.ActionLimit)
	for i := range s.Schedule {
		s.Schedule[i].Limit = quietWindow
	}
	a := newTurtleApp(t, s)

	tick(a)
	a.SetQuiet(true)
	if got := limitInForce(t, a); got != quietWindow {
		t.Errorf("the limit in force is %d, want the window's own %d left standing", got, quietWindow)
	}
	// The slot count still moves. Configuring one half of the mode and not the
	// other has to be possible, or the speed field is effectively mandatory.
	a.mu.Lock()
	concurrent := a.cfgInForceLocked().MaxConcurrent
	a.mu.Unlock()
	if concurrent != 1 {
		t.Errorf("MaxConcurrent in force = %d, want the quiet 1: the half that IS configured still applies", concurrent)
	}
}
