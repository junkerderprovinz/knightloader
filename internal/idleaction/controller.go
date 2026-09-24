package idleaction

// Controller watches the queue and, once Config says to, carries out an
// action after a cancellable countdown.

import (
	"errors"
	"sync"
	"time"
)

// Clock is the time source Controller reads, injected so a test can drive a
// countdown without waiting for one. Only Now is needed: a countdown is
// checked against the wall clock on every poll rather than slept for its exact
// length.
type Clock interface {
	Now() time.Time
}

type systemClock struct{}

func (systemClock) Now() time.Time { return time.Now() }

// State is the countdown as it stands, everything a client needs to draw it
// without polling faster than the controller itself does.
type State struct {
	Config Config `json:"config"`
	// Idle is whether the queue has nothing left to do, read fresh for every
	// State call rather than from the last poll.
	Idle bool `json:"idle"`
	// Armed is whether a countdown is currently running.
	Armed bool `json:"armed"`
	// Action is which one is armed. Empty when Armed is false.
	Action Action `json:"action,omitempty"`
	// FireAt is the absolute instant the action fires, nil when nothing is
	// armed. Absolute rather than "seconds left", so a client that reloads
	// the page, or was asleep for a few seconds, draws the deadline the
	// server is counting down to instead of restarting its own clock from a
	// number that was stale on arrival.
	FireAt *time.Time `json:"fireAt,omitempty"`
}

// defaultPoll is how often the controller re-checks Idle while nothing is
// armed, and how often it checks a running countdown against the clock. Short
// next to any delay worth a cancel button, so the gap between the queue going
// idle and the countdown starting is not one a person would notice.
const defaultPoll = 2 * time.Second

// Options configures a Controller.
type Options struct {
	// Config reports the current configuration, read fresh on every pass so a
	// saved settings page takes effect without anything being restarted.
	Config func() Config
	// Idle reports whether the queue has nothing left to do right now.
	Idle func() bool
	// Fire performs the action once a countdown reaches zero. It runs on the
	// controller's own goroutine, so it must not block for long.
	Fire func(Action)
	// OnChange is called, on the controller's own goroutine or on whichever
	// goroutine calls Cancel, whenever Armed changes - just armed, just
	// disarmed, cancelled or fired. Optional; nil means nobody is told and a
	// caller has to poll State instead.
	OnChange func()
	// Clock defaults to the wall clock.
	Clock Clock
	// Poll defaults to defaultPoll.
	Poll time.Duration
}

// Controller owns exactly one goroutine, started by Start and stopped by
// Close, the shape internal/schedule.Runner uses: a loop that reacts to more
// than a fixed timetable needs somewhere to hold state between wake-ups.
type Controller struct {
	cfg      func() Config
	idle     func() bool
	fire     func(Action)
	onChange func()
	clock    Clock
	poll     time.Duration

	// wake lets Cancel and Refresh ask for an immediate re-evaluation instead
	// of waiting up to poll for the next tick. Buffered by one and sent to
	// without blocking: two requests arriving before the loop gets to the
	// first collapse into one wake-up, which loses nothing because tick
	// re-reads current state anyway.
	wake chan struct{}
	stop chan struct{}
	done chan struct{}

	startOnce sync.Once
	closeOnce sync.Once

	mu      sync.Mutex
	started bool
	// settled is true once the current idle stretch has been acted on, fired
	// or cancelled, so it is not re-armed on every remaining tick of the same
	// stretch. It is cleared the moment the queue has something to do again,
	// which makes the next idle stretch a fresh chance.
	//
	// Together with armed this is most of the state machine, and there is no
	// "was idle last tick" flag: idle with nothing armed and the stretch not
	// settled is a reason to arm, whether idle just became true or a settings
	// save turned the feature on while the queue already had nothing to do
	// (see Refresh).
	//
	// everBusy separates a queue this Controller has seen busy at least once,
	// which may arm on an ordinary poll, from one that has been idle since
	// Start, which may only arm on an explicit tick. Without it an ordinary
	// restart with an action configured in an earlier session would pause a
	// queue that was never given work this run.
	settled  bool
	armed    bool
	action   Action
	fireAt   time.Time
	everBusy bool
	// forceArm is set by Refresh and read once, then cleared, by the next
	// tick.
	forceArm bool
}

// NewController builds a Controller. It does not start it.
func NewController(o Options) (*Controller, error) {
	if o.Config == nil {
		return nil, errors.New("idleaction: Config is required")
	}
	if o.Idle == nil {
		return nil, errors.New("idleaction: Idle is required")
	}
	if o.Fire == nil {
		return nil, errors.New("idleaction: Fire is required")
	}
	clock := o.Clock
	if clock == nil {
		clock = systemClock{}
	}
	poll := o.Poll
	if poll <= 0 {
		poll = defaultPoll
	}
	return &Controller{
		cfg: o.Config, idle: o.Idle, fire: o.Fire, onChange: o.OnChange,
		clock: clock, poll: poll,
		wake: make(chan struct{}, 1), stop: make(chan struct{}), done: make(chan struct{}),
	}, nil
}

// Start begins watching in the background. Calling it twice, or after Close,
// is a no-op: a boot that fails between NewController and Start still runs a
// deferred Close, and the loop must not start afterwards and call Fire into a
// half torn down app.
func (c *Controller) Start() {
	c.startOnce.Do(func() {
		c.mu.Lock()
		defer c.mu.Unlock()
		select {
		case <-c.stop:
			return
		default:
		}
		c.started = true
		go c.loop()
	})
}

// Close stops the loop and waits for an in-flight tick, Fire included, to
// return, so the caller can tear down whatever Fire talks to without racing
// it.
func (c *Controller) Close() error {
	c.closeOnce.Do(func() { close(c.stop) })
	c.mu.Lock()
	started := c.started
	c.mu.Unlock()
	if started {
		<-c.done
	}
	return nil
}

// Cancel calls off a countdown in progress, and is a no-op when nothing is
// armed. It does not turn the feature off: the next time the queue goes from
// busy to idle, a fresh countdown starts under whatever is configured then.
func (c *Controller) Cancel() {
	c.mu.Lock()
	changed := c.armed
	c.armed = false
	c.settled = true
	c.mu.Unlock()
	if changed {
		c.nudge()
		if c.onChange != nil {
			c.onChange()
		}
	}
}

// Refresh asks the controller to re-read Config now rather than at the next
// poll, so a settings save made while the queue is already idle arms, or
// disarms, immediately. forceArm makes the arming half hold for a queue that
// has been idle since boot, which an ordinary poll must not arm on its own
// (see everBusy).
func (c *Controller) Refresh() {
	c.mu.Lock()
	c.forceArm = true
	c.mu.Unlock()
	c.nudge()
}

func (c *Controller) nudge() {
	select {
	case c.wake <- struct{}{}:
	default:
	}
}

// State reports the countdown as it stands right now.
func (c *Controller) State() State {
	cfg := c.cfg()
	idleNow := c.idle()
	c.mu.Lock()
	defer c.mu.Unlock()
	st := State{Config: cfg, Idle: idleNow, Armed: c.armed}
	if c.armed {
		st.Action = c.action
		fireAt := c.fireAt
		st.FireAt = &fireAt
	}
	return st
}

func (c *Controller) loop() {
	defer close(c.done)
	ticker := time.NewTicker(c.poll)
	defer ticker.Stop()
	// One immediate pass rather than waiting out the first Poll, with
	// forceArm left false: a fresh boot is not a person saving a setting, and
	// a queue that is idle because nothing has been added yet must not arm on
	// an action configured in an earlier session.
	c.tick()
	for {
		select {
		case <-c.stop:
			return
		case <-c.wake:
			c.tick()
		case <-ticker.C:
			c.tick()
		}
	}
}

// tick is one evaluation pass: read the current idle state and configuration,
// decide whether to arm, disarm or fire, and act on it.
//
// Arming needs idle now, nothing armed, the stretch not settled, and either
// everBusy or forceArm. The condition is level-triggered rather than a
// transition, because the first tick at Start consumes the only edge there
// ever was and a settings page saved while the queue is already idle could
// then never arm. everBusy and forceArm keep that level trigger from also
// matching a queue that has merely been idle since boot.
//
// forceArm is read once and cleared here whichever branch runs, so a Refresh
// arriving while the queue is busy leaves nothing behind to free a later tick
// from the everBusy gate.
//
// settled stops the same condition from re-arming on every remaining tick of
// one idle stretch once it has been dealt with. It is cleared only when idle
// goes false, the one event that makes the next stretch a fresh chance.
func (c *Controller) tick() {
	// forceArm is taken before the configuration is read, so a Refresh from a
	// save landing in between either brings another tick or is read here with
	// the configuration it stored. Taken after, it could be spent on the
	// configuration from before the save.
	c.mu.Lock()
	forceArm := c.forceArm
	c.forceArm = false
	c.mu.Unlock()
	cfg := c.cfg()
	idleNow := c.idle()
	now := c.clock.Now()

	var toFire Action
	c.mu.Lock()
	wasArmed := c.armed
	switch {
	case !idleNow:
		// Something to do again. A countdown in flight is called off, since
		// firing on an idle stretch that has ended would act on stale news,
		// and settled resets so the next stretch is not skipped by a Cancel
		// or a Fire that belonged to a different one. This is also the one
		// place everBusy is set: a queue that has been busy once is no
		// longer idle purely because nothing has happened yet.
		c.armed = false
		c.settled = false
		c.everBusy = true
	case c.armed && cfg.Action == ActionNone:
		// The action was switched off while a countdown armed under the
		// earlier configuration was still running. Without this case no
		// branch matches, c.action keeps its stale value and the fire check
		// below acts on it, pausing a queue whose settings page reads the
		// feature as off. settled stays as it is: this is "nothing is
		// configured", not "this stretch has been dealt with", so switching
		// the action back on in the same stretch arms on the next tick.
		c.armed = false
	case !c.armed && !c.settled && cfg.Action != ActionNone && (c.everBusy || forceArm):
		c.armed = true
		c.action = cfg.Action
		c.fireAt = now.Add(time.Duration(cfg.DelaySeconds) * time.Second)
	}
	if c.armed && !now.Before(c.fireAt) {
		toFire = c.action
		c.armed = false
		c.settled = true
	}
	changed := wasArmed != c.armed
	c.mu.Unlock()

	if toFire != "" {
		c.fire(toFire)
	}
	if changed && c.onChange != nil {
		c.onChange()
	}
}
