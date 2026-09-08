package app

// The volume allowance, driven at the three places it acts: the period
// arithmetic that decides which downloads count, the dispatch pass that
// declines to START one, and the read of the speed limit that slows the rest of
// the period down.
//
// Every figure comes out of a history this file writes itself. A test that
// waited for real downloads would only ever say something on a machine that had
// already spent an allowance, which is to say never.

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/settings"
)

const (
	volHost     = "vol.example"
	volResolver = "volbe"
)

// volumeApp wires one host to a fake backend, so a dispatch pass ends in a
// channel rather than on somebody's server. The disk guard's own reserve is
// switched off: nothing in this file is about free space, and the default
// half-gigabyte would otherwise decide some of these passes.
func volumeApp(t *testing.T, mutate func(*settings.Settings)) (*App, *capBackend) {
	t.Helper()
	a := newQueueApp(t)
	s := settings.Defaults()
	s.MaxConcurrent, s.MaxPerHost = 4, 4
	s.DownloadDir = t.TempDir()
	s.Crawl = false
	s.DiskReserve, s.DiskLowSpace, s.DiskCriticalSpace = 0, 0, 0
	mutate(&s)
	if _, err := a.ApplySettings(s); err != nil {
		t.Fatal(err)
	}
	be := &capBackend{got: make(chan int, 8)}
	a.bmu.Lock()
	a.debrid[volResolver] = be
	a.bmu.Unlock()
	a.Registry.Register(hostResolver{id: volResolver, host: volHost})
	return a, be
}

// fetched files one finished download in the history, dated now, which is
// inside whatever period is running whatever reset day the test chose.
func fetched(t *testing.T, a *App, id string, size int64) {
	t.Helper()
	now := time.Now()
	task := core.Task{
		ID: id, URL: "https://" + volHost + "/" + id + ".bin", Name: id + ".bin",
		Host: volHost, Resolver: volResolver, Size: size,
		Status: core.StatusDone, CreatedAt: now.Add(-time.Minute), FinishedAt: now,
	}
	if err := a.Store.Save(&task); err != nil {
		t.Fatal(err)
	}
}

// queueVol stages one queued task, the way the dispatcher expects to find one.
func queueVol(a *App, id string, size int64) {
	a.mu.Lock()
	a.tasks[id] = &core.Task{
		ID: id, URL: "https://" + volHost + "/" + id + ".bin", Name: id + ".bin",
		Resolver: volResolver, Status: core.StatusQueued, Enabled: true, Size: size,
	}
	a.queue = append(a.queue, id)
	a.mu.Unlock()
}

// TestAResetDayNeverRollsIntoTheNextMonth is the arithmetic trap this whole
// file's periods rest on. time.Date(2026, February, 31, ...) is not an error and
// it is not the 28th: it is 3 March, silently normalised. A reset day of 31 that
// went through it would start February's period in March, so the period would
// overlap the next one and the counter would be summing a window that has not
// begun.
func TestAResetDayNeverRollsIntoTheNextMonth(t *testing.T) {
	day := func(y int, m time.Month, d int) time.Time {
		return time.Date(y, m, d, 0, 0, 0, 0, time.Local)
	}
	cases := []struct {
		name      string
		now       time.Time
		resetDay  int
		wantStart time.Time
		wantEnd   time.Time
	}{
		{"the 31st in a short month", day(2026, time.March, 5).Add(9 * time.Hour), 31,
			day(2026, time.February, 28), day(2026, time.March, 31)},
		{"the 31st in a leap February", day(2024, time.March, 5).Add(9 * time.Hour), 31,
			day(2024, time.February, 29), day(2024, time.March, 31)},
		{"the 30th in a short month", day(2026, time.March, 5), 30,
			day(2026, time.February, 28), day(2026, time.March, 30)},
		{"the 29th in a non-leap February", day(2026, time.March, 5), 29,
			day(2026, time.February, 28), day(2026, time.March, 29)},
		{"the 29th in a leap February", day(2024, time.March, 5), 29,
			day(2024, time.February, 29), day(2024, time.March, 29)},
		{"still inside February, so the period opened in January", day(2026, time.February, 15), 31,
			day(2026, time.January, 31), day(2026, time.February, 28)},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			start := capPeriodStart(c.now, c.resetDay)
			if !start.Equal(c.wantStart) {
				t.Errorf("period started %v, want %v", start, c.wantStart)
			}
			end := capPeriodEnd(start, c.resetDay)
			if !end.Equal(c.wantEnd) {
				t.Errorf("period ends %v, want %v", end, c.wantEnd)
			}
			// The property underneath both numbers, and the one a counter is
			// wrong without: the window has to contain the moment it was
			// worked out for.
			if c.now.Before(start) || !c.now.Before(end) {
				t.Errorf("%v is not inside [%v, %v)", c.now, start, end)
			}
		})
	}
}

// TestThePeriodRunsFromTheChosenDayAndRollsTheYear covers the ordinary case the
// clamp above is the exception to, including December, where the previous
// month is in another year.
func TestThePeriodRunsFromTheChosenDayAndRollsTheYear(t *testing.T) {
	day := func(y int, m time.Month, d int) time.Time {
		return time.Date(y, m, d, 0, 0, 0, 0, time.Local)
	}
	// The minute before the boundary still belongs to the period that opened
	// last month.
	before := day(2026, time.June, 20).Add(-time.Minute)
	if got := capPeriodStart(before, 20); !got.Equal(day(2026, time.May, 20)) {
		t.Errorf("a minute before the reset the period started %v, want %v", got, day(2026, time.May, 20))
	}
	// And the boundary itself opens the new one, which is what makes the window
	// half-open at both ends of the same instant.
	if got := capPeriodStart(day(2026, time.June, 20), 20); !got.Equal(day(2026, time.June, 20)) {
		t.Errorf("on the reset day the period started %v, want the day itself", got)
	}
	if got := capPeriodStart(day(2026, time.January, 5), 20); !got.Equal(day(2025, time.December, 20)) {
		t.Errorf("in January the period started %v, want %v", got, day(2025, time.December, 20))
	}
}

// TestTheResetDayIsClampedHereAsWellAsInTheSettings is the second half of a
// guard that is only useful twice. The settings store cleans what is SAVED, and
// this arithmetic is also handed documents nobody saved: a test's literal, a
// settings.json edited by hand, an install that upgraded into the key with a
// zero in it.
func TestTheResetDayIsClampedHereAsWellAsInTheSettings(t *testing.T) {
	now := time.Date(2026, time.March, 15, 12, 0, 0, 0, time.Local)
	if got, want := capPeriodStart(now, 0), capPeriodStart(now, settings.DefaultVolumeCapResetDay); !got.Equal(want) {
		t.Errorf("a reset day of 0 started the period at %v, want the default day's %v", got, want)
	}
	if got, want := capPeriodStart(now, 99), capPeriodStart(now, 31); !got.Equal(want) {
		t.Errorf("a reset day of 99 started the period at %v, want the 31st's %v", got, want)
	}
}

// TestACapThatIsReachedHoldsTheQueueAndSaysWhy is the pause action, and the
// reason on the row is half of it: "all slots busy" would be a true sentence
// about the wrong problem, and it sends the reader to raise MaxConcurrent,
// which downloads not one byte less.
func TestACapThatIsReachedHoldsTheQueueAndSaysWhy(t *testing.T) {
	a, _ := volumeApp(t, func(s *settings.Settings) {
		s.VolumeCap = 1000
		s.VolumeCapAction = settings.VolumeCapPause
	})
	fetched(t, a, "spent", 1200)
	if u := a.volumeCapPass(); !u.Reached {
		t.Fatalf("1200 bytes against a cap of 1000 reads as not reached: %+v", u)
	}
	queueVol(a, "next", 0)

	running, waiting := dispatchNow(a)
	if running["next"] {
		t.Error("a download started after the volume cap was reached")
	}
	if waiting["next"] != core.WaitingVolume {
		t.Errorf("next waits with %q, want %q", waiting["next"], core.WaitingVolume)
	}
}

// TestReportingOnlyCountsAndHoldsNothing is the default, and it is the default
// for the reason the two disk thresholds ship at zero: nobody's queue changes
// because they installed an update.
func TestReportingOnlyCountsAndHoldsNothing(t *testing.T) {
	a, _ := volumeApp(t, func(s *settings.Settings) {
		s.VolumeCap = 1000
		s.VolumeCapAction = settings.VolumeCapReport
	})
	fetched(t, a, "spent", 5000)
	if u := a.volumeCapPass(); u.Used != 5000 {
		t.Fatalf("used = %d, want the 5000 in the history", u.Used)
	}
	queueVol(a, "next", 0)

	running, _ := dispatchNow(a)
	if !running["next"] {
		t.Error("the queue was held back although the cap was only set to report; the counter is the whole feature at that setting")
	}
}

// TestNoCapIsNoOpinionEvenWithAnActionSet pins the off state. There is
// deliberately no "off" action, so a cap of 0 is the only thing that says
// "never hold anything back" - and an action left over from a month when there
// was a cap must not act on its own.
func TestNoCapIsNoOpinionEvenWithAnActionSet(t *testing.T) {
	a, _ := volumeApp(t, func(s *settings.Settings) {
		s.VolumeCap = 0
		s.VolumeCapAction = settings.VolumeCapPause
	})
	fetched(t, a, "lots", 9_000_000)
	if u := a.volumeCapPass(); u.Reached {
		t.Fatalf("a cap of 0 reads as reached: %+v", u)
	}
	queueVol(a, "next", 0)

	running, _ := dispatchNow(a)
	if !running["next"] {
		t.Error("the queue was held back with no cap set at all; 0 is a real answer and it means no cap, never 'off means stop'")
	}
}

// TestAFinishIsCountedBeforeTheNextTaskStarts is the whole reason the settle
// path counts at all.
//
// The order of events on a finish is: the counter is told, the dispatcher hands
// out the freed slot, and only then does the store write the history row. A
// counter that waited for its next query would still read zero at the one
// moment it is asked, and the batch that download paid for would start.
func TestAFinishIsCountedBeforeTheNextTaskStarts(t *testing.T) {
	a, _ := volumeApp(t, func(s *settings.Settings) {
		s.VolumeCap = 1000
		s.VolumeCapAction = settings.VolumeCapPause
	})
	// One pass over an empty history, so the counter has an opinion and it is
	// "nothing has been downloaded this period".
	if u := a.volumeCapPass(); u.Used != 0 || u.Reached {
		t.Fatalf("a fresh history reads as %+v, want nothing used", u)
	}

	a.mu.Lock()
	a.tasks["big"] = &core.Task{
		ID: "big", URL: "https://" + volHost + "/big.bin", Name: "big.bin",
		Resolver: volResolver, Status: core.StatusRunning, Enabled: true, Size: 1500, Loaded: 1500,
	}
	a.active["big"] = true
	a.started["big"] = true
	a.mu.Unlock()
	queueVol(a, "next", 0)

	a.onUpdate("big", core.Update{Status: core.StatusDone, Size: 1500, Loaded: 1500})

	a.mu.Lock()
	started := a.active["next"]
	waiting := a.tasks["next"].Waiting
	a.mu.Unlock()
	if started {
		t.Error("the next download started in the very pass the finish freed the slot in: the cap was reached and the counter had not been told yet")
	}
	if waiting != core.WaitingVolume {
		t.Errorf("next waits with %q, want %q", waiting, core.WaitingVolume)
	}
}

// TestTheSameFinishIsNotCountedTwice keeps the fast path honest against a
// backend that reports a terminal status more than once. The history's own rule
// is one row per task, so a second report adds nothing there and must add
// nothing here either.
func TestTheSameFinishIsNotCountedTwice(t *testing.T) {
	a, _ := volumeApp(t, func(s *settings.Settings) { s.VolumeCap = 10_000 })
	a.volumeCapPass()

	task := &core.Task{ID: "twice", Size: 400}
	a.mu.Lock()
	a.volumeCapRecordLocked(task)
	a.volumeCapRecordLocked(task)
	a.mu.Unlock()

	used, ok := a.volumeCapUsed()
	if !ok || used != 400 {
		t.Errorf("used = %d (known %v) after the same download settled twice, want 400", used, ok)
	}
}

// TestTheThrottleTakesTheSmallerLimit is the throttle action, folded into the
// one read of the limit in force rather than written anywhere. Zero on either
// side is "no limit here", so the other one wins outright.
func TestTheThrottleTakesTheSmallerLimit(t *testing.T) {
	a, _ := volumeApp(t, func(s *settings.Settings) {
		s.VolumeCap = 1000
		s.VolumeCapAction = settings.VolumeCapThrottle
		s.VolumeCapThrottle = 500_000
	})
	fetched(t, a, "spent", 1200)
	a.volumeCapPass()

	if got := a.volumeCapLimit(0); got != 500_000 {
		t.Errorf("with no other limit the capped speed is %d, want the 500000 that was configured", got)
	}
	if got := a.volumeCapLimit(100_000); got != 100_000 {
		t.Errorf("a window asking for 100000 came out as %d; a limit BESIDE the others never raises one of them", got)
	}
	if got := a.volumeCapLimit(900_000); got != 500_000 {
		t.Errorf("a window asking for 900000 came out as %d, want the cap's slower 500000", got)
	}
}

// TestTheThrottleDoesNothingUntilTheCapIsReached is the other half: an
// allowance that has not been spent must not slow anything down, or the setting
// is a speed limit wearing a cap's name.
func TestTheThrottleDoesNothingUntilTheCapIsReached(t *testing.T) {
	a, _ := volumeApp(t, func(s *settings.Settings) {
		s.VolumeCap = 10_000
		s.VolumeCapAction = settings.VolumeCapThrottle
		s.VolumeCapThrottle = 500_000
	})
	fetched(t, a, "some", 400)
	a.volumeCapPass()

	if got := a.volumeCapLimit(0); got != 0 {
		t.Errorf("limit = %d well under the cap, want 0: nothing is capped yet and 0 is unlimited", got)
	}
	if got := a.volumeCapLimit(900_000); got != 900_000 {
		t.Errorf("limit = %d well under the cap, want the 900000 that was already in force", got)
	}
}

// TestThePauseActionDoesNotAlsoThrottle keeps the two acting values apart. They
// are alternatives on one Tabs, and an install that chose to hold the queue
// back would otherwise silently also be running at a speed it never set - which
// with the field left at 0 is not even a speed.
func TestThePauseActionDoesNotAlsoThrottle(t *testing.T) {
	a, _ := volumeApp(t, func(s *settings.Settings) {
		s.VolumeCap = 1000
		s.VolumeCapAction = settings.VolumeCapPause
		s.VolumeCapThrottle = 500_000
	})
	fetched(t, a, "spent", 1200)
	a.volumeCapPass()

	if got := a.volumeCapLimit(0); got != 0 {
		t.Errorf("limit = %d with the action set to pause, want the limit left alone", got)
	}
}

// TestTheCappedSpeedReachesTheEngine is the WIRING, which the three tests above
// say nothing about: they call the fold directly, and a fold nothing calls is a
// throttle that never throttles. This one goes the whole way, from a spent
// allowance to the number the engine's own limiter is actually set to.
//
// Nothing is downloading, so the share-out hands each meter the whole limit -
// see shareOut's own rule 2 - which is what makes the engine's limiter readable
// as the figure that was decided.
func TestTheCappedSpeedReachesTheEngine(t *testing.T) {
	a, _ := volumeApp(t, func(s *settings.Settings) {
		s.SpeedLimit = 0
		s.VolumeCap = 1000
		s.VolumeCapAction = settings.VolumeCapThrottle
		s.VolumeCapThrottle = 500_000
	})
	fetched(t, a, "spent", 1200)
	a.volumeCapPass()
	a.applyBudget()

	if got := a.Throttle.Limit(); got != 500_000 {
		t.Errorf("the engine is limited to %d with the allowance spent, want the 500000 the capped speed asks for", got)
	}
}

// TestClearingTheHistoryClearsTheCounter writes down the decision rather than
// repairing it. The counter is a reading of the download history and of nothing
// else, so the button that empties the history empties it too. The alternative -
// a private total kept beside the table - would be the one figure in the app
// that cannot be checked against anything, and it would survive a restore from
// backup disagreeing with the record.
func TestClearingTheHistoryClearsTheCounter(t *testing.T) {
	a, _ := volumeApp(t, func(s *settings.Settings) {
		s.VolumeCap = 1000
		s.VolumeCapAction = settings.VolumeCapPause
	})
	fetched(t, a, "spent", 1200)
	if u := a.volumeCapPass(); !u.Reached {
		t.Fatalf("the cap is not reached with 1200 of 1000 used: %+v", u)
	}
	if err := a.ClearHistory(); err != nil {
		t.Fatal(err)
	}
	u := a.volumeCapPass()
	if u.Used != 0 || u.Reached {
		t.Errorf("after the history was emptied the counter reads %+v, want nothing used and nothing held back", u)
	}
}

// TestTheCounterIsPushedWhenItChanges is what makes the chip in the status bar a
// step function rather than a poll. The bar is hidden on every page but the
// downloads list and hidden rather than unmounted, so anything it polled would
// go on polling for a widget nobody can see.
func TestTheCounterIsPushedWhenItChanges(t *testing.T) {
	a, _ := volumeApp(t, func(s *settings.Settings) {
		s.VolumeCap = 1000
		s.VolumeCapAction = settings.VolumeCapReport
	})
	fc := &activityFakeConn{}
	a.Hub.Add(fc)
	fetched(t, a, "spent", 700)
	a.volumeCapPass()

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		for _, raw := range fc.snapshot() {
			var env struct {
				Type string      `json:"type"`
				Data VolumeUsage `json:"data"`
			}
			if json.Unmarshal(raw, &env) != nil || env.Type != "volume" {
				continue
			}
			if env.Data.Used != 700 || env.Data.Cap != 1000 {
				t.Fatalf("the pushed counter says %+v, want 700 of 1000", env.Data)
			}
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Error("no volume message reached a connected client, so the counter only ever moves when something asks for it")
}
