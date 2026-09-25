// Package keepawake keeps the computer from going to sleep while downloads and
// the work after them are under way, and lets it sleep again once they are
// done, the way JDownloader's and pyLoad's AntiStandby do.
//
// It decides when and nothing else. How is the desktop build's, which passes
// its power call in as Options.Hold (desktop/awake_*.go). Only that build
// imports this package, so the server binary carries neither the decision nor
// a power call: the machine under a container decides its own sleep.
package keepawake

import (
	"log"
	"sync"
	"time"
)

// defaultPoll is how often the guard looks at the queue. A sleep timer counts
// in minutes, so a few seconds between a download starting and the hold being
// taken is never the difference.
const defaultPoll = 5 * time.Second

// Options configures a Guard. Busy, Enabled and Hold are all required.
type Options struct {
	// Busy reports whether anything is under way that sleep would cut off.
	Busy func() bool
	// Enabled reports whether the setting is on. It is read on every pass, so
	// switching it off releases a hold without a restart.
	Enabled func() bool
	// Hold asks the operating system to stay awake until release is called.
	Hold func() (release func() error, err error)
	// Poll defaults to defaultPoll.
	Poll time.Duration
}

// Guard owns one goroutine, started by Start and stopped by Close, and at most
// one hold at a time.
type Guard struct {
	busy    func() bool
	enabled func() bool
	hold    func() (func() error, error)
	poll    time.Duration

	stop      chan struct{}
	done      chan struct{}
	startOnce sync.Once
	closeOnce sync.Once

	mu      sync.Mutex
	started bool
	release func() error
	// failed is set when a hold was refused, so a refusal is logged once per
	// busy stretch rather than on every pass. It clears when the queue goes
	// quiet, and the next stretch asks again.
	failed bool
}

// New builds a Guard. It does not start it.
func New(o Options) *Guard {
	poll := o.Poll
	if poll <= 0 {
		poll = defaultPoll
	}
	return &Guard{
		busy: o.Busy, enabled: o.Enabled, hold: o.Hold, poll: poll,
		stop: make(chan struct{}), done: make(chan struct{}),
	}
}

// Start begins watching in the background. A second call, or one after Close,
// does nothing.
func (g *Guard) Start() {
	g.startOnce.Do(func() {
		g.mu.Lock()
		defer g.mu.Unlock()
		select {
		case <-g.stop:
			return
		default:
		}
		g.started = true
		go g.loop()
	})
}

// Close stops the loop and gives back a hold it still has, so quitting in the
// middle of a download leaves the machine free to sleep.
func (g *Guard) Close() error {
	g.closeOnce.Do(func() { close(g.stop) })
	g.mu.Lock()
	started := g.started
	g.mu.Unlock()
	if started {
		<-g.done
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.letGoLocked()
}

// Held reports whether a hold is in place.
func (g *Guard) Held() bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.release != nil
}

func (g *Guard) loop() {
	defer close(g.done)
	ticker := time.NewTicker(g.poll)
	defer ticker.Stop()
	g.tick()
	for {
		select {
		case <-g.stop:
			return
		case <-ticker.C:
			g.tick()
		}
	}
}

// tick is one pass: take a hold while the queue is busy and the setting is
// on, give it back when either stops being true.
func (g *Guard) tick() {
	want := g.enabled() && g.busy()
	g.mu.Lock()
	defer g.mu.Unlock()
	switch {
	case want && g.release == nil && !g.failed:
		release, err := g.hold()
		if err != nil {
			g.failed = true
			log.Printf("keep awake: the computer may go to sleep during this download: %v", err)
			return
		}
		g.release = release
	case !want:
		g.failed = false
		if err := g.letGoLocked(); err != nil {
			log.Printf("keep awake: giving back the hold on sleep failed: %v", err)
		}
	}
}

// letGoLocked gives back the hold, if there is one. The caller holds mu.
func (g *Guard) letGoLocked() error {
	if g.release == nil {
		return nil
	}
	release := g.release
	g.release = nil
	return release()
}
