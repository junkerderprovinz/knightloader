package feed

// The set of subscriptions. A box that follows three feeds wants all three
// polled, and each of them on its own period, so the set is a map of independent
// pollers rather than one loop walking a list: a publisher whose server hangs
// for thirty seconds must not delay the other two.
//
// This is internal/watch's Watcher with the subject changed, deliberately so,
// and Apply below is where the two part company: the cost of getting it wrong is
// worse here, because a folder polled by two pollers takes one file twice and
// leaves the file behind as evidence, while a feed polled by two pollers stages
// one entry twice and leaves nothing at all to explain it.

import (
	"context"
	"errors"
	"log"
	"net/http"
	"sort"
	"strings"
	"sync"

	"github.com/junkerderprovinz/knightloader/internal/httpx"
)

// Job is one feed entry ready to be staged, together with what the subscription
// asked for on top of it. Same shape and same reason as watch.Job: the entrance
// hands over a value, and the app decides what to do with it, so this package
// never has to know what a task is.
type Job struct {
	// URL is the link to stage. Exactly one, because a feed entry names one
	// thing; a drop file's job carries a list because a file can.
	URL string
	// Package is what the link is filed under in the collector, taken from the
	// entry's own title. Never empty, see packageName.
	Package string
	// Dir is the subscription's destination override, empty for none.
	Dir string
	// Priority is the subscription's priority, nil when it named none. A pointer
	// for the reason Subscription.Priority is one: zero is a real priority.
	Priority *int
	// Source is the feed this came from. Nothing acts on it; it is what a log
	// line names so that a link in the collector can be traced back to the
	// subscription that put it there.
	Source string
}

// State is where a runner keeps what each subscription has already handed over,
// so that a restart does not stage the same entries again.
//
// It is an interface rather than a file in this package because the app already
// has a place for small persistent values and a second store beside it would be
// a second thing to back up, to migrate and to forget. It is optional: a nil
// State gives a runner a memory that lasts as long as the process, which is what
// a test wants and which still never dumps a back catalogue, because the first
// poll seeds either way.
type State interface {
	// Seen returns the keys stored for one subscription.
	//
	// known is the part that matters and the reason this is not a plain
	// ([]string, error): it says whether anything has EVER been stored for this
	// address. A subscription that has run before and legitimately remembers
	// nothing, because the feed was empty, must not be treated as a subscription
	// that has never run, or the next document it does carry arrives all at once.
	Seen(url string) (keys []string, known bool, err error)
	// SetSeen replaces what is stored for one subscription.
	SetSeen(url string, keys []string) error
}

// Options configures a Runner.
type Options struct {
	// Subscriptions is the list to start with. Apply replaces it later.
	Subscriptions []Subscription

	// OnJob receives each new entry. It runs on that subscription's polling
	// goroutine, so it must not block for long or that subscription's next poll
	// is delayed behind it. Subscriptions poll independently, so a slow sink
	// holds up only its own.
	OnJob func(Job)

	// State persists what has already been handed over. Nil keeps it in memory
	// for the life of the process, see the type's own comment.
	State State

	// HTTP is the client feeds are fetched with. Nil means the app's shared
	// outbound policy (internal/httpx), which is also what gives a test the
	// option of handing in a client pointed at an httptest server instead.
	HTTP *http.Client

	// Context bounds every fetch. Cancelling it stops the runner the same way
	// Close does, which is what makes a shutdown that cancels one context enough
	// to stop a request already waiting on a stranger's server. Nil means
	// context.Background, so an embedder that only ever calls Close still works.
	//
	// It is held on the Runner rather than passed to each call because the thing
	// it bounds is a loop that outlives every call, the same arrangement
	// schedule.Runner has with its stop channel.
	Context context.Context
}

// Runner polls a set of feeds and hands each new entry to a sink.
type Runner struct {
	http  *http.Client
	state State
	onJob func(Job)
	ctx   context.Context

	// mu guards live, started and closed. It is held across a poller's close on
	// purpose, see Apply.
	mu      sync.Mutex
	live    map[string]*poller
	started bool
	closed  bool
}

// New builds a Runner over the configured subscriptions. Nothing polls until
// Start.
//
// It fails only when not one subscription can be polled. One row with a typo in
// it must not turn the whole intake off, so a row that fails alongside a row
// that works is logged here rather than returned: the caller has a usable
// runner, and a caller that read a partial failure as fatal would drop it on the
// floor with its goroutines still inside.
func New(o Options) (*Runner, error) {
	if o.OnJob == nil {
		// A runner without a sink would fetch feeds, write down every entry as
		// handed over, and hand nothing over. From the outside that is
		// indistinguishable from data loss, and it is not recoverable either: the
		// entries are remembered, so switching a sink on afterwards would not bring
		// them back.
		return nil, errors.New("feed: OnJob is required")
	}
	client := o.HTTP
	if client == nil {
		// The shared outbound policy, so a feed fetch carries the app's user agent
		// and its ceilings. A publisher that decides to refuse us can then say who
		// it is refusing.
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

// Apply reconciles the live set with subs and returns one error per subscription
// that cannot be polled; every other row in the list is running.
//
// A row that has changed only in its interval, its filter, its folder or its
// priority keeps its poller and everything that poller knows. Rebuilding it
// would throw away the memory of what has already been handed over, and on a
// subscription still on its very first poll it would re-run the seeding pass, so
// saving an unrelated setting would decide a second time that the whole current
// window is old news. Only the address builds a new poller, because the address
// IS the subscription's identity.
//
// TWO FEEDS MUST NEVER END UP WITH TWO POLLERS, because the second one would
// fetch with a memory of its own and stage every entry a second time. Three
// things keep that impossible, and they are worth naming because internal/watch
// has to work harder for the same guarantee.
//
// The live set is keyed on the address ALONE and not on the whole row. That is
// the difference from watch.Apply, whose key is the whole Folder, so that
// changing a folder's delete flag drops one key and adds another and it has to
// order the close against the open by hand. Here an edited row is the same key,
// so it cannot become a second poller no matter what was edited.
//
// Two rows naming one address are collapsed rather than reported, because naming
// one feed twice is something a person can reasonably do and the answer to it is
// one poller, not an error they would have no idea what to do with.
//
// And mu is held across the whole call, so two saves landing together cannot
// interleave one's close with the other's start. Closing waits for the
// goroutine, which is what makes "gone" mean gone rather than "on its way out".
func (r *Runner) Apply(subs []Subscription) []error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		// Silently accepting the list would leave a runner that reports
		// subscriptions it is not polling, which is the failure this whole package
		// exists to make impossible.
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
	// Still under the same lock, and before anything is started: newPoller above
	// only builds a value, it opens no connection and starts no goroutine, so
	// nothing is fetching until the loop below.
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

// URLs returns the feeds actually being polled, sorted, so that what is logged
// is what is polled rather than what was asked for.
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

// Close stops every polling loop and waits for the running polls to finish, so
// no OnJob call is still in flight once it returns.
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
