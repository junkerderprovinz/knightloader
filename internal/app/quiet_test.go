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

// newTurtleApp builds an app with a known set of loud and quiet numbers and its
// schedule runner stopped. The base marker these tests drive by hand has one
// reader in production, and a live loop taking that reading between two lines of
// a test body would be a second one.
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

// tick is what the schedule runner does at a boundary: read the base, evaluate
// the timetable against it, apply. Written out rather than hidden behind Runner
// because these tests are about what happens between those steps.
func tick(a *App) {
	a.applySchedule(schedule.Compile(a.Settings.Get().Schedule).At(time.Now(), a.scheduleBase()))
}

// a.limitInForce is the number applyBudget shares out between the engine, JD and
// yt-dlp, and the schedule writes it at every boundary. A quiet mode that pushed
// its limit straight onto the throttle or the three meters would be undone at
// the next window edge, with the turtle still lit on screen.
func TestQuietModeSurvivesAScheduleBoundary(t *testing.T) {
	a := newTurtleApp(t, loudAndQuiet())

	a.SetQuiet(true)
	if got := limitInForce(t, a); got != quietTurtle {
		t.Fatalf("the limit in force is %d after the press, want the quiet %d", got, quietTurtle)
	}

	// A boundary that says nothing about quiet mode, which is what the runner
	// hands applySchedule at the edge of every unrelated window.
	tick(a)

	if got := limitInForce(t, a); got != quietTurtle {
		t.Errorf("the limit in force is %d after a boundary, want the quiet %d: the schedule wrote over the turtle", got, quietTurtle)
	}
	if !a.Queue().Quiet {
		t.Error("quiet mode switched itself off at a boundary that never mentioned it")
	}
}

// A window beats the switch while it is open and hands the switch its answer
// back at the end. The opposite rule would let a press nobody remembers making
// suppress the timetable every night.
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

	// The window ends by being taken out of the timetable, which is the input
	// applySchedule sees at a closing edge.
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

// Pressing the turtle off inside a quiet window has to do something, or the
// button sits dead for the length of the window. SetQuiet writes what is in
// force and does not wake the runner, so the press holds until the next
// boundary instead of being reversed a millisecond later.
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

	// And the window takes it back at the next boundary.
	tick(a)
	if !a.Queue().Quiet {
		t.Error("the window did not reassert itself at the next boundary")
	}
}

// The hard stop's race, on the quiet flag. The runner reads the base under the
// lock, drops it, evaluates the timetable and only then applies the answer, so a
// press landing in that gap would be overwritten by a reading taken before it.
// The interleaving is written out by hand, as in TestScheduleCannotUndoAHardStop,
// since a test that reproduces it by chance is a flake in the other direction.
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

// The other half of the mode, the numbers the dispatcher reads. A quiet mode
// that halves the speed and leaves eight transfers running gives eight crawling
// transfers, which is slower to finish and no quieter on the line.
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
	// Pulled down with it. Left at 4 it would be a per-host figure the
	// dispatcher can never reach.
	if got.MaxPerHost != 1 {
		t.Errorf("MaxPerHost in force = %d, want it pulled down to the quiet 1", got.MaxPerHost)
	}

	a.SetQuiet(false)
	if got := cfg(); got.MaxConcurrent != 4 || got.MaxPerHost != 4 {
		t.Errorf("after switching off: %d concurrent, %d per host, want the user's own 4 and 4 back", got.MaxConcurrent, got.MaxPerHost)
	}
}

// Where both have an opinion about speed, quiet mode replaces the window's
// figure rather than taking the smaller of the two, so the number on its own
// page is not ignored depending on the time of day.
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
	// And it stays that way across the boundary, where writing it past
	// applySchedule would show up.
	tick(a)
	if got := limitInForce(t, a); got != quietTurtle {
		t.Errorf("the limit in force is %d after a boundary, want the quiet %d", got, quietTurtle)
	}
}

// Zero in the quiet figures means "not configured", never "unlimited": a turtle
// with an empty speed field must not take the brakes off inside a nightly window
// somebody set to keep them on.
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
	// The slot count still moves: configuring one half of the mode and not the
	// other is allowed, or the speed field would be mandatory.
	a.mu.Lock()
	concurrent := a.cfgInForceLocked().MaxConcurrent
	a.mu.Unlock()
	if concurrent != 1 {
		t.Errorf("MaxConcurrent in force = %d, want the quiet 1 from the half that is configured", concurrent)
	}
}
