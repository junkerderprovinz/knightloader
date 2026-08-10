package idleaction

import (
	"sync"
	"testing"
	"time"
)

// fakeClock is a controllable Now(), the same shape internal/schedule's own
// tests use a fakeClock for: it lets a countdown be walked past its deadline
// in one call instead of a real test sleeping DefaultDelaySeconds.
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

	// A second tick while still idle must not re-arm (which would push
	// FireAt further out and mean "just wait" quietly resets every clock the
	// interface is showing).
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

	// Time passing past the original deadline, while the queue is still idle,
	// must not fire - cancelling means "not now", and a tick that quietly
	// re-armed or fired anyway would make the button a lie.
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

// TestSwitchingActionOffMidCountdownDisarms pins the exact bug build-plan.md's
// Wave 10 review found and reproduced live: with the queue idle and a
// countdown already armed, switching Action to none neither the "queue went
// busy" case (idleNow is still true) nor the "arm" case (already armed)
// matches - so before this test's fix, the switch statement did nothing at
// all, c.action kept its stale pre-change value, and the fire check fired it
// anyway the moment the original deadline passed, despite the settings page
// reading the feature as off the whole time.
func TestSwitchingActionOffMidCountdownDisarms(t *testing.T) {
	h := newHarness(t)
	h.setConfig(Config{Action: ActionPause, DelaySeconds: 60})
	h.c.tick() // idle=false (harness default): the ordinary "was busy" tick everBusy needs
	h.setIdle(true)
	h.c.tick()
	if !h.c.State().Armed {
		t.Fatal("did not arm")
	}

	// Switched off mid-countdown, the queue still idle throughout - no
	// "went busy" transition to rely on.
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

	// Turning it back on, still within the same idle stretch, is a fresh
	// chance - settled was deliberately not forced true by the disarm above.
	h.setConfig(Config{Action: ActionPause, DelaySeconds: 30})
	h.c.tick()
	if !h.c.State().Armed {
		t.Fatal("did not re-arm after Action was turned back on within the same idle stretch")
	}
}

// TestArmsFromAConfigChangeWithNoInterveningBusyPeriod pins the exact bug an
// earlier "rising edge of idle" design had, and the fix for the bug the
// level-triggered replacement introduced in turn (build-plan.md's Wave 10
// review: arming on a boot-idle queue with no work ever having happened this
// run). On an ordinary boot the queue is usually already idle (nothing has
// been added yet) before anyone has configured an action, and that idle
// state can go on being true right up to and past the moment the feature is
// switched on - the harness below never calls setIdle(false) at all, on
// purpose. A plain tick must NOT arm in that state (everBusy is false and
// nothing forced it) - only an explicit Refresh, the same call ApplySettings
// makes on every settings save, may arm a queue that has been idle since the
// very first tick.
func TestArmsFromAConfigChangeWithNoInterveningBusyPeriod(t *testing.T) {
	h := newHarness(t)
	h.setIdle(true)
	h.c.tick() // idle from the very first tick, Action still the default (none)
	if h.c.State().Armed {
		t.Fatal("armed despite Action=none")
	}

	// The queue never went busy in between. A plain tick must still refuse
	// to arm even after the config change - it takes Refresh, exactly the
	// call ApplySettings makes, to prove this was a real settings save and
	// not just another poll of a queue that has been idle since boot.
	h.setConfig(Config{Action: ActionPause, DelaySeconds: 30})
	h.c.tick()
	if h.c.State().Armed {
		t.Fatal("armed from a plain tick with no Refresh - everBusy is false and nothing forced this arm")
	}

	// Refresh (build-plan.md's Wave 10B brief: "the countdown must survive a
	// page reload... fires even if nobody is watching the tab") is what
	// actually takes effect promptly here.
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

// TestDoesNotArmOnAnIdleBootWithAPersistedConfig is the review's own
// reproduction: a queue idle since Start, with Action already configured
// (as it would be after a restart carrying a saved setting forward), must
// not arm on an ordinary poll - only a real busy period or an explicit
// Refresh may unlock the first arm. Without this gate, every restart of an
// instance with the feature already on silently paused the queue a moment
// after boot, before the user had added anything.
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
	// A settings save mid-countdown is not this test's concern (arming
	// already captured the action and delay at the moment it armed); what
	// matters is that a save made BEFORE the next idle stretch is what that
	// stretch honours, not whatever was configured when the controller was
	// built.
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
	// Action stays ActionNone (Defaults()): idle is true, but nothing is
	// configured to happen about it. The interface still needs to know the
	// queue is idle - a settings page toggling the action on reads this
	// value to say "this would arm right now" rather than "nothing to see".
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
	// Close before Start must not hang - a boot that fails between
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

	// A real, running controller closes promptly - poll set tiny so this
	// test does not depend on defaultPoll's real two seconds.
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
		t.Fatal("Close did not return - the loop goroutine is stuck or was never actually started")
	}
	// The fake clock never advances past the five-second delay, so nothing
	// should have fired despite many polls at a millisecond each.
	select {
	case a := <-fired:
		t.Errorf("fired %q despite the clock never reaching the deadline", a)
	default:
	}
}
