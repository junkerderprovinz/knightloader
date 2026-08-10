package idleaction

// Controller watches the queue and, once Config says to, carries out an
// action after a cancellable countdown.

import (
	"errors"
	"sync"
	"time"
)

// Clock is the time source Controller reads. Injected so a test can drive a
// countdown without waiting for one - the same reason
// internal/schedule.Clock exists, though this one only ever needs Now: a
// countdown here is checked against the wall clock on every poll rather than
// slept for its exact length, because the poll interval is already short next
// to any delay worth having a cancel button for (see defaultPoll).
type Clock interface {
	Now() time.Time
}

type systemClock struct{}

func (systemClock) Now() time.Time { return time.Now() }

// State is the countdown as it stands right now - everything a client needs
// to draw it without polling faster than the controller itself does.
type State struct {
	Config Config `json:"config"`
	// Idle is whether the queue has nothing left to do right now, read fresh
	// for every State call rather than from the last poll - see
	// Controller.State.
	Idle bool `json:"idle"`
	// Armed is whether a countdown is currently running.
	Armed bool `json:"armed"`
	// Action is which one is armed. Empty when Armed is false.
	Action Action `json:"action,omitempty"`
	// FireAt is the absolute instant the action fires, nil when nothing is
	// armed. Absolute rather than "seconds left", so a client that reloads
	// the page - or one that was simply asleep for a few seconds - draws the
	// same deadline the server is counting down to instead of restarting its
	// own clock from a number that was already stale by the time it arrived.
	// The same reason ScheduleState.Next and the captcha modal's ExpiresAt
	// are both absolute instants rather than a duration.
	FireAt *time.Time `json:"fireAt,omitempty"`
}

// defaultPoll is how often the controller re-checks Idle while nothing is
// armed, and how often it checks a running countdown against the clock.
// Short next to any delay worth a cancel button (DefaultDelaySeconds is a
// full minute; minDelaySeconds refuses anything under five seconds), so the
// gap between "the queue actually went idle" and "the countdown visibly
// started" is not something a person watching would notice.
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
// Close - the same shape internal/schedule.Runner already uses for the same
// reason: a background loop that reacts to more than a fixed timetable needs
// somewhere to hold state between wake-ups, and a bare goroutine has nowhere
// to put it that Close could find again.
type Controller struct {
	cfg      func() Config
	idle     func() bool
	fire     func(Action)
	onChange func()
	clock    Clock
	poll     time.Duration

	// wake lets Cancel and Refresh ask for an immediate re-evaluation instead
	// of waiting up to poll for the next tick. Buffered by one and sent to
	// without blocking, the identical shape schedule.Runner's own wake
	// channel already uses: two requests arriving before the loop gets to the
	// first collapse into one wake-up, which loses nothing because tick
	// always re-reads current state rather than acting on stale news.
	wake chan struct{}
	stop chan struct{}
	done chan struct{}

	startOnce sync.Once
	closeOnce sync.Once

	mu      sync.Mutex
	started bool
	// settled is true once the current idle stretch has already been acted
	// on - fired, or cancelled - so it is not re-armed on every remaining
	// tick of the same stretch. Cleared the instant the queue has something
	// to do again, which is what makes the NEXT idle stretch a fresh chance.
	//
	// This, together with armed, is most of the state machine - there is
	// deliberately no separate "was idle last tick" flag for the ordinary
	// case. The queue being idle when the controller has nothing armed and
	// this stretch is not yet settled is a reason to arm, whether that is
	// because idle just became true a moment ago or because a settings save
	// turned the feature on while the queue already had nothing to do (see
	// Refresh) - a save made while already idle must arm on its very next
	// tick, not wait for a busy period that may never come. An earlier
	// version of this file tracked a "rising edge of idle" instead and armed
	// only on the transition - which quietly meant that exact save-while-
	// idle case could never arm at all, because the one edge there ever was
	// had already been consumed, harmlessly, by the very first tick at boot.
	//
	// What level-triggering alone still got wrong: a boot with the queue
	// ALREADY idle (nothing pending) and Action already configured from a
	// PRIOR session armed on that very first tick too, with nobody having
	// touched anything - so a queue that had simply never been given work
	// this run got silently paused a minute after every ordinary restart.
	// everBusy is what tells those two boot-time cases apart from an
	// explicit tick (see tick's own explicit parameter): a queue this
	// Controller has genuinely seen busy at least once may arm on an
	// ordinary poll once it goes idle, exactly as before; a queue that has
	// been idle since Start may only arm on an EXPLICIT tick - Refresh,
	// i.e. a real settings save - never on a routine poll finding nothing
	// new to report.
	settled  bool
	armed    bool
	action   Action
	fireAt   time.Time
	everBusy bool
	// forceArm is set by Refresh and consumed (read once, cleared) by the
	// very next tick - see Refresh's own doc comment.
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

// Start begins watching in the background. Calling it twice, or calling it
// after Close, is a no-op - see schedule.Runner.Start for the same guard and
// the same reason: a boot that fails between NewController and Start still
// runs a deferred Close, and without this the loop would start afterwards and
// could call Fire into a half torn down app.
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

// Close stops the loop and waits for an in-flight tick (Fire included) to
// return, so the caller can tear down whatever Fire talks to without racing
// it - the same promise schedule.Runner.Close makes about Apply.
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

// Cancel calls off a countdown in progress. It is a no-op when nothing is
// armed. It does not turn the feature off: the next time the queue goes from
// busy to idle, a fresh countdown starts under whatever is configured then -
// "not now" rather than "not ever", which is what JDownloader's own countdown
// dialog means by Cancel too.
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
// poll, so a settings save made while the queue happens to already be idle
// arms - or disarms - immediately instead of up to Poll later. forceArm is
// what makes the "arms" half true even for a queue that has been idle since
// boot with nothing observed busy yet - see everBusy and tick's own doc
// comment for why an ordinary poll must not arm that same queue on its own.
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
	// One immediate pass rather than waiting out the first Poll - forceArm
	// is left false for it (the zero value): a fresh boot is not a person
	// saving a setting, and a queue that is idle purely because nothing has
	// been added yet this run must not arm just because Action happens to
	// already be configured from a previous session. See tick's own doc
	// comment and everBusy.
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
// Arming needs idle now, not already armed, not settled for this stretch,
// and (everBusy OR forceArm) - never a transition. A transition ("idle just
// became true") sounds like the safer trigger and is not: the controller's
// very first tick, at Start, already observes whatever the queue's state
// happens to be, and on an ordinary boot that is usually idle (nothing has
// been added yet). If arming required a FRESH edge, a settings page saved
// while the queue is already idle (Refresh's whole reason to exist) could
// never arm at all, because the one edge there ever was had already been
// consumed, harmlessly, by the very first tick. So the base condition is
// level-triggered, re-checked in full on every tick - but level-triggering
// alone armed on a boot-idle queue with a config left over from a previous
// session, too, silently pausing a queue that had never been given work
// this run. everBusy||forceArm is what keeps the level-trigger for the case
// it exists for (Refresh while idle) without also matching plain "still
// idle since boot, nobody touched anything." forceArm is read once and
// cleared here regardless of which branch of the switch below actually
// runs, so a Refresh that arrives while the queue happens to be busy (and
// therefore hits the first case, not the arm case) does not leave a stale
// forceArm sitting around to incorrectly free some LATER, unrelated tick
// from the everBusy gate it exists to enforce.
//
// settled is what stops that same level-triggered condition from re-arming
// every remaining tick of one continuous idle stretch once it has already
// been dealt with - fired, or cancelled. It is cleared only when idle goes
// false: the queue getting something new to do is the one event that makes
// the NEXT idle stretch a fresh chance.
func (c *Controller) tick() {
	cfg := c.cfg()
	idleNow := c.idle()
	now := c.clock.Now()

	var toFire Action
	c.mu.Lock()
	wasArmed := c.armed
	forceArm := c.forceArm
	c.forceArm = false
	switch {
	case !idleNow:
		// Something to do again. Whatever made the queue idle before no
		// longer holds, so a countdown in flight is called off - firing an
		// action on the strength of an idle stretch that has already ended
		// would be acting on stale news - and settled resets so the NEXT
		// idle stretch starts clean rather than being silently skipped by a
		// Cancel or a Fire that belonged to a different one. Also the one
		// place everBusy is set: a queue that is busy even once this run is
		// no longer "idle purely because nothing has happened yet", so a
		// later idle stretch is free to arm on an ordinary poll same as
		// before this fix.
		c.armed = false
		c.settled = false
		c.everBusy = true
	case c.armed && cfg.Action == ActionNone:
		// The feature was switched off (or Action set to "do nothing")
		// while a countdown armed under the PREVIOUS configuration was
		// still running. Without this case, neither branch above matches
		// (idleNow is still true, and c.armed is already true so the arm
		// branch's !c.armed guard refuses it too) - the switch would do
		// nothing at all, c.action would keep the stale value, and the
		// fire check below would act on it anyway, pausing a queue whose
		// own settings page reads the feature as off. settled is
		// deliberately left untouched (not forced true, the way Cancel
		// sets it): this is "nothing is configured", not "this stretch
		// has been dealt with", so re-enabling the action later in the
		// same idle stretch is free to arm fresh on its very next tick,
		// exactly what Refresh's "arms OR disarms immediately" promises.
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
