package schedule

import (
	"sync"
	"testing"
	"time"
)

// fakeClock stands in for the wall clock so the runner can be walked through a
// night of boundaries in microseconds. slept records what the loop asked to wait
// for, which is the only way to see from outside that it sleeps to the next
// change rather than polling.
type fakeClock struct {
	mu    sync.Mutex
	now   time.Time
	fire  chan time.Time
	slept chan time.Duration
}

func newFakeClock(now time.Time) *fakeClock {
	return &fakeClock{now: now, fire: make(chan time.Time, 1), slept: make(chan time.Duration, 8)}
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *fakeClock) After(d time.Duration) (<-chan time.Time, func()) {
	c.slept <- d
	return c.fire, func() {}
}

// advance moves the clock and releases the pending wait, the way a real timer
// firing would.
func (c *fakeClock) advance(to time.Time) {
	c.mu.Lock()
	c.now = to
	c.mu.Unlock()
	c.fire <- to
}

func (c *fakeClock) waited(t *testing.T) time.Duration {
	t.Helper()
	select {
	case d := <-c.slept:
		return d
	case <-time.After(2 * time.Second):
		t.Fatal("the runner never went to sleep")
		return 0
	}
}

// TestRunnerWakesAtTheNextChange: the loop applies the current state, then
// wakes exactly at the edge where it changes when that comes before the next
// recheck, and otherwise rechecks without applying anything.
func TestRunnerWakesAtTheNextChange(t *testing.T) {
	entries := []Entry{{Days: everyDay(), Start: "22:00", End: "06:00", Action: ActionLimit, Limit: 1000}}
	clock := newFakeClock(ts(2, 21, 59).Add(30 * time.Second))
	applied := make(chan State, 8)

	r, err := NewRunner(Options{
		Entries: entries,
		Apply:   func(s State) { applied <- s },
		Base:    func() State { return State{Limit: 5000} },
		Clock:   clock,
	})
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}
	r.Start()

	// Boot inside no window: the user's own limit is applied straight away, so a
	// restart does not leave the queue running on whatever the last state was.
	if got := <-applied; got != (State{Limit: 5000}) {
		t.Errorf("first apply = %+v, want the base state", got)
	}
	if got := clock.waited(t); got != 30*time.Second {
		t.Errorf("waited %s, want 30s; the window opens at 22:00", got)
	}

	clock.advance(ts(2, 22, 0))
	if got := <-applied; got != (State{Limit: 1000}) {
		t.Errorf("apply at the opening edge = %+v, want the window's limit", got)
	}
	if got := clock.waited(t); got != recheck {
		t.Errorf("waited %s, want the recheck interval; the window closes at 06:00", got)
	}

	clock.advance(ts(2, 22, 1))
	if got := clock.waited(t); got != recheck {
		t.Errorf("waited %s after a recheck, want the recheck interval again", got)
	}

	// Re-installing the same timetable must wake the loop (a saved settings page
	// might have changed anything) but must not repeat an unchanged state at the
	// engine and the UI.
	r.Set(entries)
	if got := clock.waited(t); got != recheck {
		t.Errorf("waited %s after Set, want the recheck interval", got)
	}
	if err := r.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if n := len(applied); n != 0 {
		t.Errorf("%d extra applies, want none: the state never changed", n)
	}
}

// TestRunnerSetAppliesImmediately: a timetable saved at 22:05 must take effect at
// 22:05. Waiting for the next boundary would mean the setting the user just
// pressed save on appears to do nothing.
func TestRunnerSetAppliesImmediately(t *testing.T) {
	clock := newFakeClock(ts(2, 22, 5))
	applied := make(chan State, 8)

	r, err := NewRunner(Options{
		Apply: func(s State) { applied <- s },
		Clock: clock,
	})
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}
	defer r.Close()
	r.Start()

	if got := <-applied; got != (State{}) {
		t.Errorf("first apply = %+v, want the empty base of an empty timetable", got)
	}
	r.Set([]Entry{{Days: everyDay(), Start: "22:00", End: "06:00", Action: ActionPause}})
	if got := <-applied; got != (State{Paused: true}) {
		t.Errorf("apply after Set = %+v, want the queue paused", got)
	}
}

// TestRunnerRechecksAnEmptyTimetableQuietly: with nothing scheduled the loop
// still reads the clock once per recheck, and never repeats the unchanged state
// at the engine.
func TestRunnerRechecksAnEmptyTimetableQuietly(t *testing.T) {
	clock := newFakeClock(ts(2, 12, 0))
	applied := make(chan State, 8)

	r, err := NewRunner(Options{Apply: func(s State) { applied <- s }, Clock: clock})
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}
	r.Start()
	<-applied

	for _, m := range []int{1, 2, 3} {
		if got := clock.waited(t); got != recheck {
			t.Fatalf("waited %s, want the recheck interval", got)
		}
		clock.advance(ts(2, 12, m))
	}
	clock.waited(t)
	if err := r.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if n := len(applied); n != 0 {
		t.Errorf("%d applies after the first, want none: nothing changed", n)
	}
}

// sleepyClock keeps the wall clock apart from the clock timers run on, the way
// a suspended laptop does: suspend moves only the wall clock, and run moves
// both and fires each timer whose time has come.
type sleepyClock struct {
	mu   sync.Mutex
	wall time.Time
	mono time.Duration

	armed chan sleepyTimer
	// pending is the timer the loop is waiting on. Only the test goroutine
	// touches it.
	pending *sleepyTimer
}

type sleepyTimer struct {
	at   time.Duration
	fire chan time.Time
}

func newSleepyClock(wall time.Time) *sleepyClock {
	return &sleepyClock{wall: wall, armed: make(chan sleepyTimer, 1)}
}

func (c *sleepyClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.wall
}

func (c *sleepyClock) After(d time.Duration) (<-chan time.Time, func()) {
	c.mu.Lock()
	tm := sleepyTimer{at: c.mono + d, fire: make(chan time.Time, 1)}
	c.mu.Unlock()
	c.armed <- tm
	return tm.fire, func() {}
}

func (c *sleepyClock) suspend(d time.Duration) {
	c.mu.Lock()
	c.wall = c.wall.Add(d)
	c.mu.Unlock()
}

// run lets d pass on both clocks. It waits for the loop to arm each timer
// before moving on, so everything the loop applies within d has been applied
// when it returns.
func (c *sleepyClock) run(t *testing.T, d time.Duration) {
	t.Helper()
	c.mu.Lock()
	until := c.mono + d
	c.mu.Unlock()
	for {
		if c.pending == nil {
			select {
			case tm := <-c.armed:
				c.pending = &tm
			case <-time.After(2 * time.Second):
				t.Fatal("the runner never went to sleep")
			}
		}
		tm := *c.pending
		c.mu.Lock()
		if tm.at > until {
			c.wall = c.wall.Add(until - c.mono)
			c.mono = until
			c.mu.Unlock()
			return
		}
		c.wall = c.wall.Add(tm.at - c.mono)
		c.mono = tm.at
		now := c.wall
		c.mu.Unlock()
		c.pending = nil
		tm.fire <- now
	}
}

type stampedState struct {
	at    time.Time
	state State
}

// applyLog records every Apply with the wall clock it happened at.
type applyLog struct {
	mu   sync.Mutex
	list []stampedState
}

func (l *applyLog) add(at time.Time, s State) {
	l.mu.Lock()
	l.list = append(l.list, stampedState{at, s})
	l.mu.Unlock()
}

func (l *applyLog) last() stampedState {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.list[len(l.list)-1]
}

// TestRunnerCatchesUpAfterTheMachineSlept: the computer sleeps from 12:00 to
// 20:00 inside an 08:00 to 18:00 pause. A wait armed for the 18:00 edge still
// has six hours to go on wake, and the queue has to run again within the
// recheck instead of at two in the morning.
func TestRunnerCatchesUpAfterTheMachineSlept(t *testing.T) {
	clock := newSleepyClock(ts(2, 12, 0))
	var applied applyLog
	r, err := NewRunner(Options{
		Entries: []Entry{{Days: everyDay(), Start: "08:00", End: "18:00", Action: ActionPause}},
		Apply:   func(s State) { applied.add(clock.Now(), s) },
		Clock:   clock,
	})
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}
	defer r.Close()
	r.Start()

	clock.run(t, 0)
	clock.suspend(8 * time.Hour)
	clock.run(t, time.Minute)

	if got := applied.last(); got.state != (State{}) {
		t.Errorf("last apply = %+v at %s, want the queue running again after the wake", got.state, got.at.Format("15:04"))
	}
}

// TestRunnerServesAWindowAHaltByHandMadeReal: with a pause from 08:00 to 18:00
// and a resume from 01:00 to 05:00, the resume window changes nothing at 19:00,
// so the loop has no reason to stop there. A halt by hand at 22:00 does not
// wake the loop, and the resume window has to release the queue at 01:00 all
// the same.
func TestRunnerServesAWindowAHaltByHandMadeReal(t *testing.T) {
	clock := newSleepyClock(ts(2, 19, 0))
	var applied applyLog
	var mu sync.Mutex
	base := State{}
	r, err := NewRunner(Options{
		Entries: []Entry{
			{Days: everyDay(), Start: "08:00", End: "18:00", Action: ActionPause},
			{Days: everyDay(), Start: "01:00", End: "05:00", Action: ActionResume},
		},
		Apply: func(s State) { applied.add(clock.Now(), s) },
		Base: func() State {
			mu.Lock()
			defer mu.Unlock()
			return base
		},
		Clock: clock,
	})
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}
	defer r.Close()
	r.Start()

	clock.run(t, 3*time.Hour)
	mu.Lock()
	base = State{Paused: true}
	mu.Unlock()
	clock.run(t, 3*time.Hour)

	got := applied.last()
	if got.state != (State{}) || !got.at.Equal(ts(3, 1, 0)) {
		t.Errorf("last apply = %+v at %s, want the queue released at 01:00", got.state, got.at.Format("15:04"))
	}
}

// TestRunnerCloseWaitsForApply: Close is what the caller uses before tearing down
// whatever Apply talks to, so it must not return while a call is still inside it.
func TestRunnerCloseWaitsForApply(t *testing.T) {
	clock := newFakeClock(ts(2, 12, 0))
	release := make(chan struct{})
	var mu sync.Mutex
	inside := false

	r, err := NewRunner(Options{
		Apply: func(State) {
			mu.Lock()
			inside = true
			mu.Unlock()
			<-release
			mu.Lock()
			inside = false
			mu.Unlock()
		},
		Clock: clock,
	})
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}
	r.Start()

	// Wait for Apply to be entered, then let it finish and close.
	for {
		mu.Lock()
		in := inside
		mu.Unlock()
		if in {
			break
		}
		time.Sleep(time.Millisecond)
	}
	close(release)
	if err := r.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if inside {
		t.Error("Close returned while Apply was still running")
	}
}

// TestNewRunnerNeedsASink: a runner with nowhere to put its answer looks exactly
// like a schedule that does not work, so it is refused at construction rather
// than at three in the morning.
func TestNewRunnerNeedsASink(t *testing.T) {
	if _, err := NewRunner(Options{}); err == nil {
		t.Fatal("NewRunner accepted Options with no Apply")
	}
}

// TestRunnerCloseWithoutStart must not block: the app can fail to boot between
// building the runner and starting it.
func TestRunnerCloseWithoutStart(t *testing.T) {
	r, err := NewRunner(Options{Apply: func(State) {}})
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}
	done := make(chan struct{})
	go func() {
		_ = r.Close()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Close blocked on a runner that was never started")
	}
}

// TestRunnerStartAfterCloseDoesNothing is the same boot failure seen from the
// other end: a Close that has already run is the caller saying the engine Apply
// talks to is going away, so a Start that arrives afterwards must not bring the
// loop up and call into it.
func TestRunnerStartAfterCloseDoesNothing(t *testing.T) {
	clock := newFakeClock(ts(2, 12, 0))
	applied := make(chan State, 4)

	r, err := NewRunner(Options{
		Entries: []Entry{{Days: everyDay(), Start: "00:00", End: "23:00", Action: ActionPause}},
		Apply:   func(s State) { applied <- s },
		Clock:   clock,
	})
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}
	if err := r.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	r.Start()

	select {
	case s := <-applied:
		t.Fatalf("Apply was called with %+v after Close had returned", s)
	case <-time.After(50 * time.Millisecond):
	}
	// A second Close must still not block, whatever Start did with the goroutine.
	if err := r.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}
}
