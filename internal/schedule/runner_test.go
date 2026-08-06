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

// TestRunnerSleepsToTheNextChange is the end of the argument the pure evaluator
// makes: the loop applies the current state, then waits exactly as long as the
// state stays that way. A runner that polled would ask for a fixed short
// interval here instead of eight hours.
func TestRunnerSleepsToTheNextChange(t *testing.T) {
	entries := []Entry{{Days: everyDay(), Start: "22:00", End: "06:00", Action: ActionLimit, Limit: 1000}}
	clock := newFakeClock(ts(2, 21, 0))
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
	if got := clock.waited(t); got != time.Hour {
		t.Errorf("waited %s, want 1h — the window opens at 22:00", got)
	}

	clock.advance(ts(2, 22, 0))
	if got := <-applied; got != (State{Limit: 1000}) {
		t.Errorf("apply at the opening edge = %+v, want the window's limit", got)
	}
	if got := clock.waited(t); got != 8*time.Hour {
		t.Errorf("waited %s, want 8h — the window closes at 06:00 the next morning", got)
	}

	// Re-installing the same timetable must wake the loop (a saved settings page
	// might have changed anything) but must not repeat an unchanged state at the
	// engine and the UI.
	r.Set(entries)
	if got := clock.waited(t); got != 8*time.Hour {
		t.Errorf("waited %s after Set, want the same 8h", got)
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

// TestRunnerWaitsForeverWithNothingToDo: an empty timetable has no next change,
// so the loop must park on Set and Close instead of asking for a timer it would
// only have to re-arm.
func TestRunnerWaitsForeverWithNothingToDo(t *testing.T) {
	clock := newFakeClock(ts(2, 12, 0))
	applied := make(chan State, 8)

	r, err := NewRunner(Options{Apply: func(s State) { applied <- s }, Clock: clock})
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}
	r.Start()
	<-applied

	select {
	case d := <-clock.slept:
		t.Fatalf("the runner armed a timer for %s with nothing to wake up for", d)
	case <-time.After(50 * time.Millisecond):
	}
	if err := r.Close(); err != nil {
		t.Fatalf("Close: %v", err)
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
