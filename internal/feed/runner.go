package feed

// The set of subscriptions: independent pollers, each on its own period, so a
// hanging publisher does not delay the others. It follows internal/watch's
// Watcher, but a feed polled twice stages entries twice without leaving any
// trace, so Apply guards against that more strictly.

import (
	"context"
	"errors"
	"log"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/httpx"
)

// Job is one feed entry ready to be staged, with what the subscription asked
// for. As with watch.Job, the app decides what to do with it.
type Job struct {
	// URL is the one link to stage.
	URL string
	// Package is what the link is filed under, never empty; see packageName.
	Package string
	// Dir is the subscription's destination override, empty for none.
	Dir string
	// Priority is the subscription's priority, nil when it named none.
	Priority *int
	// Source is the feed this came from, for log lines.
	Source string
}

// Health is how one subscription is doing. It lives in memory only: the
// persisted memory of handed-over entries is the one thing worth storing, so
// after a restart these rows start blank until LastPolled is set.
type Health struct {
	// URL is the subscription's address.
	URL string
	// LastPolled is when the last poll ran, successful or not, and zero if
	// none has run in this process. The fields below mean nothing until it is
	// set.
	LastPolled time.Time
	// LastError is why the last poll produced nothing, empty on success.
	LastError string
	// Seeded says whether the first poll, which hands nothing over (see
	// poller.seed), is done. It is the first thing to check when a new
	// subscription has added nothing.
	Seeded bool
	// Remembered is how many entries the subscription recognises, at most
	// maxSeen. Zero after a poll means the feed serves nothing.
	Remembered int
}

// State persists what each subscription has handed over, so a restart does
// not stage the same entries again. It uses the app's existing store for
// small values. It is optional: without one the memory lasts for the process,
// and the first poll still seeds.
type State interface {
	// Seen returns the keys stored for one subscription. known says whether
	// anything was ever stored for the address, so a subscription that ran
	// against an empty feed is not treated as never having run.
	Seen(url string) (keys []string, known bool, err error)
	// SetSeen replaces what is stored for one subscription.
	SetSeen(url string, keys []string) error
}

// Options configures a Runner.
type Options struct {
	// Subscriptions is the list to start with. Apply replaces it later.
	Subscriptions []Subscription

	// OnJob receives each new entry on that subscription's polling goroutine,
	// so a slow sink delays only its own subscription.
	OnJob func(Job)

	// State persists what has been handed over. Nil keeps it in memory.
	State State

	// HTTP is the client feeds are fetched with. Nil means the shared
	// internal/httpx policy.
	HTTP *http.Client

	// Context bounds every fetch; cancelling it stops the runner like Close.
	// Nil means context.Background.
	Context context.Context
}

// Runner polls a set of feeds and hands each new entry to a sink.
type Runner struct {
	http  *http.Client
	state State
	onJob func(Job)
	ctx   context.Context

	// mu guards live, started and closed, and is held across a poller's
	// close; see Apply.
	mu      sync.Mutex
	live    map[string]*poller
	started bool
	closed  bool
}

// New builds a Runner over the configured subscriptions. Nothing polls until
// Start. It fails only when no subscription at all can be polled; failing rows
// beside working ones are logged, so one typo does not turn off the intake.
func New(o Options) (*Runner, error) {
	if o.OnJob == nil {
		// Without a sink every entry would be remembered and none handed over,
		// and they could not be recovered later.
		return nil, errors.New("feed: OnJob is required")
	}
	client := o.HTTP
	if client == nil {
		client = httpx.New(httpx.Options{})
	}
	ctx := o.Context
	if ctx == nil {
		ctx = context.Background()
	}
	r := &Runner{
		http:  client,
		state: o.State,
		onJob: o.OnJob,
		ctx:   ctx,
		live:  make(map[string]*poller, len(o.Subscriptions)),
	}
	errs := r.Apply(o.Subscriptions)
	if len(r.URLs()) == 0 {
		if err := errors.Join(errs...); err != nil {
			return nil, err
		}
		return nil, errors.New("feed: no subscription configured")
	}
	for _, err := range errs {
		log.Printf("feed subscription is not being polled: %v", err)
	}
	return r, nil
}

// Apply reconciles the live set with subs and returns one error per
// subscription that cannot be polled; every other one is running.
//
// A subscription whose address is unchanged keeps its poller and its memory,
// whatever else was edited; rebuilding it would forget what was handed over
// or seed a second time. Only a new address builds a new poller.
//
// One feed must never get two pollers, or each would stage every entry. The
// live set is keyed by address alone (unlike watch.Apply, which keys by the
// whole folder row), duplicate rows collapse into one poller, and mu is held
// across the whole call, with close waiting for the removed poller's
// goroutine.
func (r *Runner) Apply(subs []Subscription) []error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		// Accepting the list would report subscriptions nobody polls.
		return []error{errors.New("feed: the runner is closed")}
	}

	next := make(map[string]*poller, len(subs))
	var errs []error
	for _, s := range subs {
		s.URL = strings.TrimSpace(s.URL)
		if s.URL == "" || next[s.URL] != nil {
			continue
		}
		if err := s.Validate(); err != nil {
			errs = append(errs, err)
			continue
		}
		if p := r.live[s.URL]; p != nil {
			p.configure(s)
			next[s.URL] = p
			continue
		}
		next[s.URL] = newPoller(r, s)
	}
	// newPoller starts nothing, so closing the removed pollers before the
	// loop below means no two pollers for one feed ever run.
	for u, p := range r.live {
		if next[u] == nil {
			p.close()
		}
	}
	if r.started {
		for _, p := range next {
			p.start()
		}
	}
	r.live = next
	return errs
}

// Start begins polling every subscription in the background, and every
// subscription added after it. Calling it twice is a no-op.
func (r *Runner) Start() {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.started || r.closed {
		return
	}
	r.started = true
	for _, p := range r.live {
		p.start()
	}
}

// URLs returns the feeds actually being polled, sorted.
func (r *Runner) URLs() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]string, 0, len(r.live))
	for u := range r.live {
		out = append(out, u)
	}
	sort.Strings(out)
	return out
}

// Health reports how each polled subscription is doing, sorted by address. A
// configured subscription missing from the list is one the runner refused.
// The runner's lock is taken before a poller's, never the other way round.
func (r *Runner) Health() []Health {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]Health, 0, len(r.live))
	for _, p := range r.live {
		out = append(out, p.health())
	}
	sort.Slice(out, func(i, j int) bool { return out[i].URL < out[j].URL })
	return out
}

// Close stops every polling loop and waits for running polls, so no OnJob
// call is in flight once it returns.
func (r *Runner) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for u, p := range r.live {
		p.close()
		delete(r.live, u)
	}
	r.closed = true
	return nil
}
