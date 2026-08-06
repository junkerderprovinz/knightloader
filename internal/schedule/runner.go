package schedule

import (
	"errors"
	"sync"
	"time"
)

// Clock is the time source a Runner sleeps on. It is injected so a test can
// drive a year of boundaries through the loop without waiting for any of them.
type Clock interface {
	Now() time.Time
	// After returns a channel that fires once d has passed, together with a
	// function that releases the timer when the wait is abandoned. Abandoning is
	// the normal case here — every saved settings page cuts one short — and a
	// wait can be a week long, so a timer nobody releases sits in the runtime for
	// that whole week.
	After(d time.Duration) (<-chan time.Time, func())
}

type systemClock struct{}

func (systemClock) Now() time.Time { return time.Now() }

func (systemClock) After(d time.Duration) (<-chan time.Time, func()) {
	t := time.NewTimer(d)
	return t.C, func() { t.Stop() }
}

// Options configures a Runner.
type Options struct {
	// Entries is the timetable to start with. Set replaces it later.
	Entries []Entry

	// Apply receives the state the queue should be in, and is called only when
	// that state actually changes. It runs on the Runner's own goroutine, so it
	// must not block for long or the next boundary is served late.
	Apply func(State)

	// Base reports the state to fall back to where no entry applies: the speed
	// limit and pause switch the user set by hand. It is read again on every pass,
	// so the timetable never has to be reloaded to pick up a new manual limit —
	// but a change made while the loop is asleep only reaches Apply at the next
	// boundary, so a caller that wants it reflected at once calls Set. Nil means
	// the zero state.
	Base func() State

	// Clock defaults to the wall clock.
	Clock Clock
}

// Runner applies a schedule as time passes. It owns exactly one goroutine,
// started by Start and stopped by Close.
type Runner struct {
	apply func(State)
	base  func() State
	clock Clock

	// last and have are touched only by the loop goroutine, so they need no lock.
	last State
	have bool

	// wake is how Set reaches the loop. Buffered by one and sent to without
	// blocking: a settings page saved twice in a row must not stall the saver,
	// and one pending wake-up is as good as two.
	wake chan struct{}
	stop chan struct{}
	done chan struct{}

	startOnce sync.Once
	closeOnce sync.Once

	mu      sync.Mutex
	sched   Schedule
	started bool
}

// NewRunner builds a Runner. It does not start it.
func NewRunner(o Options) (*Runner, error) {
	if o.Apply == nil {
		// A runner with nowhere to put its answer would evaluate the timetable and
		// throw the result away, which from the outside is indistinguishable from
		// a schedule that does not work.
		return nil, errors.New("schedule: Apply is required")
	}
	base := o.Base
	if base == nil {
		base = func() State { return State{} }
	}
	clock := o.Clock
	if clock == nil {
		clock = systemClock{}
	}
	return &Runner{
		apply: o.Apply,
		base:  base,
		clock: clock,
		sched: Compile(o.Entries),
		wake:  make(chan struct{}, 1),
		stop:  make(chan struct{}),
		done:  make(chan struct{}),
	}, nil
}

// Start begins applying the timetable in the background, beginning with one
// immediate Apply so a process that boots inside a nightly window is throttled
// from the first download rather than from the next boundary. Calling it twice
// is a no-op.
//
// Starting after Close is a no-op too. A boot that fails between NewRunner and
// Start still runs the deferred Close, and without this the loop would come up
// afterwards and call Apply into a half torn down engine, which Close is
// precisely the promise not to do.
func (r *Runner) Start() {
	r.startOnce.Do(func() {
		r.mu.Lock()
		defer r.mu.Unlock()
		select {
		case <-r.stop:
			return
		default:
		}
		r.started = true
		go r.loop()
	})
}

// Set installs a new timetable and applies it right away, so a saved settings
// page takes effect now instead of at whatever boundary the loop was sleeping
// towards.
func (r *Runner) Set(entries []Entry) {
	r.mu.Lock()
	r.sched = Compile(entries)
	r.mu.Unlock()
	select {
	case r.wake <- struct{}{}:
	default:
	}
}

// Close stops the loop and waits for an in-flight Apply to return, so the caller
// can tear down whatever Apply talks to without racing it.
func (r *Runner) Close() error {
	r.closeOnce.Do(func() { close(r.stop) })
	r.mu.Lock()
	started := r.started
	r.mu.Unlock()
	if started {
		<-r.done
	}
	return nil
}

func (r *Runner) loop() {
	defer close(r.done)
	for {
		// One clock reading serves the whole round. Asking twice could put the
		// evaluation on one side of a boundary and the sleep on the other, which
		// would compute a wait of nearly zero and spin.
		now := r.clock.Now()
		r.mu.Lock()
		sched := r.sched
		r.mu.Unlock()
		base := r.base()

		state := sched.At(now, base)
		// Applying only on a change matters because Apply reaches the download
		// engine and the UI: repeating an unchanged state at every wake-up would
		// put a stream of no-op events in front of the user.
		if !r.have || state != r.last {
			r.last, r.have = state, true
			r.apply(state)
		}

		var wait <-chan time.Time
		var release func()
		if next, ok := sched.Next(now, base); ok {
			wait, release = r.clock.After(next.Sub(now))
		}
		select {
		case <-r.stop:
			if release != nil {
				release()
			}
			return
		case <-r.wake:
			if release != nil {
				release()
			}
		case <-wait:
			// A nil channel blocks forever, which is the right answer when the
			// timetable has no next change: only Set or Close can matter then.
		}
	}
}
