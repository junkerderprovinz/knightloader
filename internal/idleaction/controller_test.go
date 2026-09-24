package idleaction

import (
	"sync"
	"testing"
	"time"
)

// fakeClock is a controllable Now, so a countdown can be walked past its
// deadline in one call instead of a test sleeping DefaultDelaySeconds.
type fakeClock struct {
	mu  sync.Mutex
	now time.Time
}

func newFakeClock() *fakeClock { return &fakeClock{now: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)} }

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *fakeClock) advance(d time.Duration) {
	c.mu.Lock()
	c.now = c.now.Add(d)
	c.mu.Unlock()
}

// harness wires a Controller against fakes for every dependency, with the
// idle reading and the fired actions both settable/observable so a test can
// drive the state machine one tick at a time.
type harness struct {
	c     *Controller
	clock *fakeClock

	mu      sync.Mutex
	cfg     Config
	idle    bool
	fired   []Action
	changes int
}

func newHarness(t *testing.T) *harness {
	h := &harness{cfg: Defaults(), clock: newFakeClock()}
	c, err := NewController(Options{
		Config: func() Config {
			h.mu.Lock()
			defer h.mu.Unlock()
			return h.cfg
		},
		Idle: func() bool {
			h.mu.Lock()
			defer h.mu.Unlock()
			return h.idle
		},
		Fire: func(a Action) {
			h.mu.Lock()
			h.fired = append(h.fired, a)
			h.mu.Unlock()
		},
		OnChange: func() {
			h.mu.Lock()
			h.changes++
			h.mu.Unlock()
		},
		Clock: h.clock,
	})
	if err != nil {
		t.Fatalf("NewController: %v", err)
	}
	h.c = c
	return h
}

func (h *harness) setIdle(v bool) {
	h.mu.Lock()
	h.idle = v
	h.mu.Unlock()
}

func (h *harness) setConfig(c Config) {
	h.mu.Lock()
	h.cfg = c
	h.mu.Unlock()
}

func (h *harness) firedActions() []Action {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := make([]Action, len(h.fired))
	copy(out, h.fired)
	return out
}

func (h *harness) changeCount() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.changes
}

func TestArmsOnlyOnTheRisingEdgeOfIdle(t *testing.T) {
	h := newHarness(t)
	h.setConfig(Config{Action: ActionPause, DelaySeconds: 60})

	h.c.tick() // idle=false at construction: no arm
	if h.c.State().Armed {
		t.Fatal("armed while the queue is not idle")
	}

	h.setIdle(true)
	h.c.tick()
	st := h.c.State()
	if !st.Armed {
		t.Fatal("did not arm on the rising edge of idle")
	}
	if st.Action != ActionPause {
		t.Errorf("Action = %q, want %q", st.Action, ActionPause)
	}
	if st.FireAt == nil {
		t.Fatal("FireAt is nil while armed")
	}
	if want := h.clock.Now().Add(60 * time.Second); !st.FireAt.Equal(want) {
		t.Errorf("FireAt = %v, want %v", *st.FireAt, want)
	}

	// A second tick while still idle must not re-arm: that would push FireAt
	// further out and reset every clock the interface is showing.
	fireAtBefore := *st.FireAt
	h.c.tick()
	st2 := h.c.State()
	if !st2.Armed || !st2.FireAt.Equal(fireAtBefore) {
		t.Errorf("a later tick while idle changed the countdown: got %v, want unchanged %v", st2.FireAt, fireAtBefore)
	}
}

func TestDoesNotArmWhenActionIsNone(t *testing.T) {
	h := newHarness(t)
	h.setConfig(Config{Action: ActionNone, DelaySeconds: 60})
	h.setIdle(true)
	h.c.tick()
	if h.c.State().Armed {
		t.Fatal("armed despite Action=none")
	}
}

func TestFiresWhenTheCountdownElapses(t *testing.T) {
	h := newHarness(t)
	h.setConfig(Config{Action: ActionPause, DelaySeconds: 10})
	h.c.tick() // idle=false (harness default): the ordinary "was busy" tick everBusy needs
	h.setIdle(true)
	h.c.tick()
	if !h.c.State().Armed {
		t.Fatal("did not arm")
	}

	h.clock.advance(9 * time.Second)
	h.c.tick()
	if len(h.firedActions()) != 0 {
		t.Fatal("fired before the delay elapsed")
	}
	if !h.c.State().Armed {
		t.Fatal("disarmed before the delay elapsed")
	}

	h.clock.advance(1 * time.Second)
	h.c.tick()
	fired := h.firedActions()
	if len(fired) != 1 || fired[0] != ActionPause {
		t.Fatalf("fired = %v, want exactly one ActionPause", fired)
	}
	if h.c.State().Armed {
		t.Fatal("still armed after firing")
	}
}

func TestDoesNotReFireOrReArmWhileTheSameIdleStretchContinues(t *testing.T) {
	h := newHarness(t)
	h.setConfig(Config{Action: ActionPause, DelaySeconds: 10})
	h.c.tick() // idle=false (harness default): the ordinary "was busy" tick everBusy needs
	h.setIdle(true)
	h.c.tick()
	h.clock.advance(10 * time.Second)
	h.c.tick() // fires once

	for i := 0; i < 5; i++ {
		h.clock.advance(time.Minute)
		h.c.tick()
	}
	if fired := h.firedActions(); len(fired) != 1 {
		t.Fatalf("fired %d times across a continuous idle stretch, want exactly 1: %v", len(fired), fired)
	}

	// The queue getting something to do again, and then going idle a second
	// time, is what earns a fresh countdown.
	h.setIdle(false)
	h.c.tick()
	h.setIdle(true)
	h.c.tick()
	if !h.c.State().Armed {
		t.Fatal("did not arm on the second idle stretch")
	}
	h.clock.advance(10 * time.Second)
	h.c.tick()
	if fired := h.firedActions(); len(fired) != 2 {
		t.Fatalf("fired %d times across two idle stretches, want exactly 2: %v", len(fired), fired)
	}
}

func TestCancelDisarmsAndSuppressesTheRestOfTheStretch(t *testing.T) {
	h := newHarness(t)
	h.setConfig(Config{Action: ActionPause, DelaySeconds: 10})
	h.c.tick() // idle=false (harness default): the ordinary "was busy" tick everBusy needs
	h.setIdle(true)
	h.c.tick()
	if !h.c.State().Armed {
		t.Fatal("did not arm")
	}

	h.c.Cancel()
	if h.c.State().Armed {
		t.Fatal("still armed after Cancel")
	}

	// Time passing the original deadline while the queue is still idle must
	// not fire: cancelling means "not now", and a tick that re-armed or fired
	// anyway would make the button a lie.
	h.clock.advance(time.Minute)
	h.c.tick()
	if fired := h.firedActions(); len(fired) != 0 {
		t.Fatalf("fired after Cancel: %v", fired)
	}
	if h.c.State().Armed {
		t.Fatal("re-armed on its own after Cancel, within the same idle stretch")
	}

	// A fresh idle stretch is a fresh chance.
	h.setIdle(false)
	h.c.tick()
	h.setIdle(true)
	h.c.tick()
	if !h.c.State().Armed {
		t.Fatal("did not arm on the next idle stretch after a Cancel")
	}
}

func TestCancelIsANoOpWhenNothingIsArmed(t *testing.T) {
	h := newHarness(t)
	h.c.Cancel() // must not panic
	if h.changeCount() != 0 {
		t.Errorf("OnChange fired for a Cancel that changed nothing")
	}
}

func TestBecomingBusyAgainDisarmsAWaitingCountdown(t *testing.T) {
	h := newHarness(t)
	h.setConfig(Config{Action: ActionPause, DelaySeconds: 60})
	h.c.tick() // idle=false (harness default): the ordinary "was busy" tick everBusy needs
	h.setIdle(true)
	h.c.tick()
	if !h.c.State().Armed {
		t.Fatal("did not arm")
	}

	// Something to do again before the countdown reached zero: acting on it
	// would be acting on an idle stretch that has already ended.
	h.setIdle(false)
	h.c.tick()
	if h.c.State().Armed {
		t.Fatal("stayed armed after the queue had something to do again")
	}
	if fired := h.firedActions(); len(fired) != 0 {
		t.Fatalf("fired despite the queue going busy first: %v", fired)
	}
}

// With the queue idle and a countdown armed, switching Action to none matches
// neither the "queue went busy" case (idleNow is still true) nor the arm case
// (already armed). Without its own case the stale action fires the moment the
// original deadline passes, while the settings page reads the feature as off.
func TestSwitchingActionOffMidCountdownDisarms(t *testing.T) {
	h := newHarness(t)
	h.setConfig(Config{Action: ActionPause, DelaySeconds: 60})
	h.c.tick() // idle=false (harness default): the ordinary "was busy" tick everBusy needs
	h.setIdle(true)
	h.c.tick()
	if !h.c.State().Armed {
		t.Fatal("did not arm")
	}

	// Switched off mid-countdown, the queue still idle throughout, so there
	// is no "went busy" transition to rely on.
	h.setConfig(Config{Action: ActionNone, DelaySeconds: 60})
	h.c.tick()
	if h.c.State().Armed {
		t.Fatal("still armed after Action was switched to none")
	}

	// The original deadline passing must not fire the stale action.
	h.clock.advance(time.Minute)
	h.c.tick()
	if fired := h.firedActions(); len(fired) != 0 {
		t.Fatalf("fired the stale action after being switched off: %v", fired)
	}

	// Turning it back on within the same idle stretch is a fresh chance: the
	// disarm above does not set settled.
	h.setConfig(Config{Action: ActionPause, DelaySeconds: 30})
	h.c.tick()
	if !h.c.State().Armed {
		t.Fatal("did not re-arm after Action was turned back on within the same idle stretch")
	}
}

// On an ordinary boot the queue is idle before anyone has configured an
// action, and stays idle past the moment the feature is switched on, so the
// harness below never calls setIdle(false). A plain tick must not arm in that
// state; only an explicit Refresh, the call ApplySettings makes on every
// settings save, may arm a queue that has been idle since the first tick.
func TestArmsFromAConfigChangeWithNoInterveningBusyPeriod(t *testing.T) {
	h := newHarness(t)
	h.setIdle(true)
	h.c.tick() // idle from the very first tick, Action still the default (none)
	if h.c.State().Armed {
		t.Fatal("armed despite Action=none")
	}

	// The queue never went busy in between, so a plain tick still refuses to
	// arm after the config change: it takes Refresh to tell a real settings
	// save from another poll of a queue that has been idle since boot.
	h.setConfig(Config{Action: ActionPause, DelaySeconds: 30})
	h.c.tick()
	if h.c.State().Armed {
		t.Fatal("armed from a plain tick with no Refresh; everBusy is false and nothing forced this arm")
	}

	// Refresh is what takes effect promptly here.
	h.c.Refresh()
	h.c.tick()
	st := h.c.State()
	if !st.Armed {
		t.Fatal("did not arm from Refresh while continuously idle since the first tick")
	}
	if st.Action != ActionPause {
		t.Errorf("Action = %q, want %q", st.Action, ActionPause)
	}
}

// A queue idle since Start with Action already configured, as after a restart
// carrying a saved setting forward, must not arm on an ordinary poll: only a
// real busy period or an explicit Refresh unlocks the first arm. Without that
// gate, a restart with the feature on pauses the queue a moment after boot.
func TestDoesNotArmOnAnIdleBootWithAPersistedConfig(t *testing.T) {
	h := newHarness(t)
	h.setConfig(Config{Action: ActionPause, DelaySeconds: 60}) // as if loaded from a previous session
	h.setIdle(true)                                            // as if nothing has been queued yet this run

	for i := 0; i < 5; i++ {
		h.c.tick()
		if h.c.State().Armed {
			t.Fatalf("armed on tick %d of a boot-idle queue with no busy period and no Refresh", i)
		}
	}
	if fired := h.firedActions(); len(fired) != 0 {
		t.Fatalf("fired despite never arming: %v", fired)
	}

	// A real busy period unlocks ordinary arming from here on.
	h.setIdle(false)
	h.c.tick()
	h.setIdle(true)
	h.c.tick()
	if !h.c.State().Armed {
		t.Fatal("did not arm after an observed busy period")
	}
}

func TestConfigIsReadFreshEveryTick(t *testing.T) {
	// A save made before the next idle stretch is what that stretch honours,
	// not whatever was configured when the controller was built. A save
	// mid-countdown is a different question: arming captured the action and
	// the delay already.
	h := newHarness(t)
	h.setConfig(Config{Action: ActionNone, DelaySeconds: 60})
	h.setIdle(true)
	h.c.tick()
	if h.c.State().Armed {
		t.Fatal("armed despite Action=none")
	}

	h.setIdle(false)
	h.c.tick()
	h.setConfig(Config{Action: ActionPause, DelaySeconds: 5})
	h.setIdle(true)
	h.c.tick()
	st := h.c.State()
	if !st.Armed || st.Action != ActionPause {
		t.Fatalf("did not pick up the config saved before this idle stretch: %+v", st)
	}
}

func TestOnChangeFiresExactlyOnArmAndOnFire(t *testing.T) {
	h := newHarness(t)
	h.setConfig(Config{Action: ActionPause, DelaySeconds: 10})

	h.c.tick() // idle=false: no change
	h.c.tick() // still idle=false: no change
	if n := h.changeCount(); n != 0 {
		t.Fatalf("OnChange fired %d times before anything changed", n)
	}

	h.setIdle(true)
	h.c.tick() // arms: one change
	if n := h.changeCount(); n != 1 {
		t.Fatalf("OnChange fired %d times arming, want 1", n)
	}

	h.c.tick() // still armed, still idle: no change
	if n := h.changeCount(); n != 1 {
		t.Fatalf("OnChange fired on an unchanged tick: %d", n)
	}

	h.clock.advance(10 * time.Second)
	h.c.tick() // fires: one more change
	if n := h.changeCount(); n != 2 {
		t.Fatalf("OnChange fired %d times after arm+fire, want 2", n)
	}
}

func TestStateReportsIdleEvenWhenNothingIsArmed(t *testing.T) {
	h := newHarness(t)
	h.setIdle(true)
	// Action stays ActionNone: idle is true, but nothing is configured to
	// happen about it. The settings page reads Idle to say "this would arm
	// right now" when the action is toggled on.
	st := h.c.State()
	if !st.Idle {
		t.Error("State().Idle is false while Idle() reports true")
	}
	if st.Armed {
		t.Error("armed with no action configured")
	}
}

func TestNewControllerRequiresItsCallbacks(t *testing.T) {
	full := Options{
		Config: func() Config { return Defaults() },
		Idle:   func() bool { return false },
		Fire:   func(Action) {},
	}

	missingConfig := full
	missingConfig.Config = nil
	if _, err := NewController(missingConfig); err == nil {
		t.Error("NewController accepted a nil Config")
	}

	missingIdle := full
	missingIdle.Idle = nil
	if _, err := NewController(missingIdle); err == nil {
		t.Error("NewController accepted a nil Idle")
	}

	missingFire := full
	missingFire.Fire = nil
	if _, err := NewController(missingFire); err == nil {
		t.Error("NewController accepted a nil Fire")
	}

	if _, err := NewController(full); err != nil {
		t.Errorf("NewController rejected a fully populated Options: %v", err)
	}
}

func TestStartAndCloseLifecycle(t *testing.T) {
	// Close before Start must not hang: a boot that fails between
	// NewController and Start still runs a deferred Close.
	c, err := NewController(Options{
		Config: func() Config { return Defaults() },
		Idle:   func() bool { return false },
		Fire:   func(Action) {},
	})
	if err != nil {
		t.Fatalf("NewController: %v", err)
	}
	if err := c.Close(); err != nil {
		t.Errorf("Close before Start: %v", err)
	}

	// Start after Close is a documented no-op, not a panic.
	c.Start()

	// A running controller closes promptly, with poll set small so this test
	// does not wait out defaultPoll.
	fired := make(chan Action, 1)
	c2, err := NewController(Options{
		Config: func() Config { return Config{Action: ActionPause, DelaySeconds: 5} },
		Idle:   func() bool { return true },
		Fire:   func(a Action) { fired <- a },
		Clock:  newFakeClock(),
		Poll:   time.Millisecond,
	})
	if err != nil {
		t.Fatalf("NewController: %v", err)
	}
	c2.Start()
	c2.Start() // twice is a no-op, must not start a second loop
	done := make(chan error, 1)
	go func() { done <- c2.Close() }()
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("Close: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Close did not return; the loop goroutine is stuck or was never started")
	}
	// The fake clock never advances past the five-second delay, so nothing
	// should have fired despite many polls at a millisecond each.
	select {
	case a := <-fired:
		t.Errorf("fired %q despite the clock never reaching the deadline", a)
	default:
	}
}

// A second save can land while a tick is reading the configuration of the
// first. Its Refresh must still arm on the next tick, or the action it
// configured waits for a poll a minute away.
func TestARefreshDuringATickArmsOnTheNextOne(t *testing.T) {
	h := newHarness(t)
	h.idle = true
	h.cfg = Config{Action: ActionNone}
	saved := false
	h.c.cfg = func() Config {
		h.mu.Lock()
		cfg := h.cfg
		if !saved {
			saved = true
			h.cfg = Config{Action: ActionPause, DelaySeconds: 5}
			h.mu.Unlock()
			h.c.Refresh()
			return cfg
		}
		h.mu.Unlock()
		return cfg
	}

	h.c.Refresh()
	h.c.tick()
	h.c.tick()
	if !h.c.State().Armed {
		t.Fatal("the action saved during a tick never armed")
	}
}
