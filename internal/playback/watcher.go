package playback

// watcher.go: the loop that asks every server in turn and folds the answers
// into one bit.

import (
	"context"
	"errors"
	"log"
	"net/http"
	"slices"
	"sync"
	"time"
)

// Interval is how often the servers are asked. Ten seconds is as quick as
// anybody notices a film start, and slow enough that a server never sees the
// questions in its own activity log as load.
const Interval = 10 * time.Second

// releaseAfter is how long quiet mode outlives the last stream. A series that
// plays the next episode by itself leaves a gap of a few seconds between two
// streams, and a queue that sped up and slowed down again in that gap would
// be the flicker this is meant to prevent.
const releaseAfter = 30 * time.Second

// errorGrace is how long a server that stops answering keeps its last answer.
// A server restarting under a stream would otherwise hand the uplink back for
// the minute it takes to come up.
const errorGrace = time.Minute

// ServerState is the last question to one server, for its row on the card.
type ServerState struct {
	ID        string    `json:"id"`
	CheckedAt time.Time `json:"checkedAt"`
	OK        bool      `json:"ok"`
	Failure   *Failure  `json:"failure,omitempty"`
}

// Status is what the watcher last concluded.
type Status struct {
	Enabled bool `json:"enabled"`
	// Active is the one bit the queue acts on.
	Active bool `json:"active"`
	// Since is when Active last turned true, nil while it is false.
	Since *time.Time `json:"since,omitempty"`
	// Streams is everything playing, counted or not, so the card can say why
	// a stream on the sofa did not slow anything down.
	Streams []Stream      `json:"streams"`
	Servers []ServerState `json:"servers"`
}

// Options wires a Watcher to the app.
type Options struct {
	// Settings and Servers are read on every pass, so a save takes effect at
	// the next one.
	Settings func() Settings
	Servers  func() []Server
	Token    func(id string) string
	// OnChange is called from the watcher's goroutine whenever Active flips.
	OnChange func(active bool)
	Client   *http.Client
	Now      func() time.Time
}

// Watcher asks the configured servers on a timer.
type Watcher struct {
	o    Options
	wake chan struct{}

	mu     sync.Mutex
	status Status
	t      tracker
}

// NewWatcher builds a watcher; Run starts it.
func NewWatcher(o Options) *Watcher {
	if o.Client == nil {
		o.Client = NewClient()
	}
	if o.Now == nil {
		o.Now = time.Now
	}
	return &Watcher{o: o, wake: make(chan struct{}, 1), t: newTracker()}
}

// Run asks until ctx ends. It asks once at the start, so a box that boots
// mid-film throttles before the first interval is up.
func (w *Watcher) Run(ctx context.Context) {
	tick := time.NewTicker(Interval)
	defer tick.Stop()
	for {
		w.pass(ctx)
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		case <-w.wake:
		}
	}
}

// Wake asks again now rather than at the next tick, after a save.
func (w *Watcher) Wake() {
	select {
	case w.wake <- struct{}{}:
	default:
	}
}

// Status is a copy of the last conclusion.
func (w *Watcher) Status() Status {
	w.mu.Lock()
	defer w.mu.Unlock()
	st := w.status
	st.Streams = slices.Clone(st.Streams)
	st.Servers = slices.Clone(st.Servers)
	return st
}

// Check asks one server once, for the card's test button. It changes nothing
// the queue acts on.
func (w *Watcher) Check(ctx context.Context, s Server) ([]Stream, error) {
	return Ask(ctx, w.o.Client, s, w.o.Token(s.ID))
}

func (w *Watcher) pass(ctx context.Context) {
	cfg, servers := w.o.Settings(), w.o.Servers()
	answers := make(map[string]answer, len(servers))
	if cfg.Enabled {
		var mu sync.Mutex
		var wg sync.WaitGroup
		for _, s := range servers {
			wg.Go(func() {
				streams, err := Ask(ctx, w.o.Client, s, w.o.Token(s.ID))
				mu.Lock()
				answers[s.ID] = answer{streams: streams, err: err}
				mu.Unlock()
			})
		}
		wg.Wait()
	}
	if ctx.Err() != nil {
		return
	}
	now := w.o.Now()

	w.mu.Lock()
	was := w.status.Active
	streams, active := w.t.observe(now, cfg, servers, answers)
	st := Status{Enabled: cfg.Enabled, Active: active, Streams: streams, Since: w.status.Since}
	if active && !was {
		st.Since = &now
	}
	if !active {
		st.Since = nil
	}
	for _, s := range servers {
		a, ok := answers[s.ID]
		if !ok {
			continue
		}
		row := ServerState{ID: s.ID, CheckedAt: now, OK: a.err == nil}
		var f *Failure
		if errors.As(a.err, &f) {
			row.Failure = f
		}
		st.Servers = append(st.Servers, row)
		if failedBefore := w.failed(s.ID); a.err != nil && !failedBefore {
			// Once per outage, not every ten seconds. The host only: the
			// address may carry a path somebody would rather not see in a log.
			log.Printf("playback: %s (%s) did not answer: %v", s.ID, s.Host(), a.err)
		}
	}
	w.status = st
	w.mu.Unlock()

	if active != was && w.o.OnChange != nil {
		w.o.OnChange(active)
	}
}

// failed reports whether the last pass had a failure for id. Caller holds w.mu.
func (w *Watcher) failed(id string) bool {
	for _, s := range w.status.Servers {
		if s.ID == id {
			return !s.OK
		}
	}
	return false
}

type answer struct {
	streams []Stream
	err     error
}

// tracker is the memory the decision needs between passes. It is apart from
// the loop so the rules can be tested against a clock that is only a number.
type tracker struct {
	pausedSince map[string]time.Time
	lastGood    map[string]goodAnswer
	holdUntil   time.Time
}

type goodAnswer struct {
	at      time.Time
	streams []Stream
}

func newTracker() tracker {
	return tracker{pausedSince: map[string]time.Time{}, lastGood: map[string]goodAnswer{}}
}

// observe folds one pass into the streams to show and whether quiet mode
// should be on.
func (t *tracker) observe(now time.Time, cfg Settings, servers []Server, answers map[string]answer) ([]Stream, bool) {
	var all []Stream
	for _, s := range servers {
		a, ok := answers[s.ID]
		if !ok {
			continue
		}
		if a.err == nil {
			t.lastGood[s.ID] = goodAnswer{at: now, streams: a.streams}
			all = append(all, a.streams...)
			continue
		}
		if g, ok := t.lastGood[s.ID]; ok && now.Sub(g.at) < errorGrace {
			all = append(all, g.streams...)
		}
	}

	seen := make(map[string]bool, len(all))
	counting := false
	for i := range all {
		s := &all[i]
		key := s.Server + "/" + s.Session
		seen[key] = true
		if !s.Paused {
			delete(t.pausedSince, key)
		} else if _, ok := t.pausedSince[key]; !ok {
			t.pausedSince[key] = now
		}
		switch {
		case cfg.IgnoreLocal && s.Local:
			s.Reason = "local"
		case s.Paused && now.Sub(t.pausedSince[key]) >= cfg.pausedFor():
			s.Reason = "paused"
		default:
			s.Counts = true
			counting = true
		}
	}
	for key := range t.pausedSince {
		if !seen[key] {
			delete(t.pausedSince, key)
		}
	}

	if !cfg.Enabled {
		t.holdUntil = time.Time{}
		return all, false
	}
	if counting {
		t.holdUntil = now.Add(releaseAfter)
		return all, true
	}
	return all, now.Before(t.holdUntil)
}
