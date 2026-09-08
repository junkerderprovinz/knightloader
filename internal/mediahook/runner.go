package mediahook

// runner.go: the loop that owns the calls, and the two waits in front of them.
//
// THE SUBSCRIBER MUST NOT MAKE THE CALL, which is what this whole file is for.
// script.Bus delivers synchronously, on the publisher's own goroutine, and says
// so in capitals: "EVERY SUBSCRIBER MUST RETURN PROMPTLY". The publisher for
// package.done is the app's own package sweep, a ticker shared with everything
// else that watches the queue, and an HTTP call inside Subscribe would stall
// that sweep for up to twenty seconds per address. So Enqueue drops the fact
// into a bounded channel and returns, exactly as script.Host.fire does, and this
// loop is what actually goes out on the network.
//
// # The two waits
//
// THE COALESCE WINDOW is the user's own, per address (Hook.WaitSeconds). Twenty
// packages finishing in one 2-second sweep is twenty firings, and a library scan
// costs the media server real work: it walks a directory tree, hashes what is
// new and talks to a metadata provider. Twenty of those, started together, is a
// media server that is unusable for ten minutes because a download finished. One
// scan a minute later costs nobody anything. Zero seconds is a real answer and
// means "call after every package".
//
// THE DELIVERY WAIT is not the user's and cannot be switched off. package.done
// fires the moment nothing is left to WAIT for, and on an install with a working
// folder configured the finished file is at that moment still in the working
// folder: app_dispatch.go sets the status and then spawns the checksum and the
// move, which for a 40 GB film across a filesystem boundary is minutes. A scan
// started then finds nothing and never looks again, and it bites exactly the
// installs that set a working folder BECAUSE a scanner was picking up half
// files. So the call is held until the app says the package's files have
// actually landed - see Options.Ready - with a ceiling, because a move that
// failed for good would otherwise mean a call that never happens at all.

import (
	"context"
	"log"
	"net/http"
	"sort"
	"strconv"
	"sync"
	"time"
)

// defaultTick is how often the loop looks at what is waiting. A second, which is
// the resolution the two waits above are worth measuring in: the coalesce window
// is a user-typed number of seconds, and the delivery wait is a file move.
const defaultTick = time.Second

// DeliveryGrace is how long a call may be held waiting for the finished files to
// reach their destination folder before it goes out anyway.
//
// It exists because the delivery wait is a promise about a move this package
// cannot see the inside of. A move can fail permanently - no room on the target
// volume, a collision policy of "skip", a read-only mount - and the app records
// that on the row and leaves the file where it is. Without a ceiling the scan
// for that package would simply never be requested, which is the one failure
// mode worse than scanning too early: nothing on any page would say so.
//
// A quarter of an hour is chosen against the case it is FOR rather than against
// the failure: a 40 GB film copied across a filesystem boundary on a spinning
// disk is a few minutes, and a set of them is a few more.
const DeliveryGrace = 15 * time.Minute

// queueDepth bounds the hand-over channel. Sixteen addresses times a handful of
// packages settling in one sweep, with room to spare; a queue this deep can only
// fill if the loop itself is stuck, and a full queue is answered the way
// script.Host.fire answers one, with a log line and a dropped call rather than a
// stalled publisher.
const queueDepth = 256

// Options configures a Runner.
type Options struct {
	// Store is where the header values are read from. Nil sends every call
	// without its header, which is a Runner nobody wired a credential store into
	// and is worth surviving rather than crashing a download path over.
	Store *Store

	// Hooks reads the live list. A FUNCTION and not a slice, because the settings
	// document is edited while this runs: an address changed during a coalesce
	// window has to be called as it is NOW, not as it was when the package
	// finished. It is called from this package's own goroutine, so it must not
	// take a lock this package's callers already hold.
	Hooks func() []Hook

	// Ready answers whether one package's files have reached the folders they
	// belong in. Nil means "always ready", which is right for a test and for any
	// embedding with no working folder - see the delivery wait above.
	//
	// It is called from the loop's goroutine, once per tick per waiting call, so
	// it has to be cheap and must not block.
	Ready func(pkg string) bool

	// Client is what the calls go out on. Nil means NewClient, the shared
	// outbound policy with redirects switched off; a test hands in a client
	// pointed at its own server.
	Client *http.Client

	// Tick and Grace are the two intervals above, overridable so a test can reach
	// them. Zero means the constants.
	Tick  time.Duration
	Grace time.Duration
}

// Runner owns the queue, the waiting calls and the last result per address.
type Runner struct {
	store  *Store
	hooks  func() []Hook
	ready  func(pkg string) bool
	client *http.Client
	tick   time.Duration
	grace  time.Duration

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	queue chan armed

	// mu guards last and started. It is never held across a call: the call takes
	// up to CallTimeout and this lock is taken by the settings route that draws
	// the page.
	mu      sync.Mutex
	last    map[string]Result
	started bool
}

// armed is one package that wants an address called.
type armed struct {
	hookID  string
	pkg     string
	arrived time.Time
}

// waiting is one address with a call owed on it.
type waiting struct {
	// first is the package that armed this call and count is how many were
	// folded into it. The first one by NAME, not by arrival: two packages
	// finishing in one sweep arrive in a sorted order the app went out of its way
	// to make deterministic, and a result that named a different one of them on
	// every run would be a report nobody could reproduce.
	first string
	count int
	// fireAt is when the coalesce window closes.
	fireAt time.Time
	// giveUpAt is when the delivery wait stops holding the call back.
	giveUpAt time.Time
}

// New builds a Runner. Nothing is called until Start.
func New(o Options) *Runner {
	r := &Runner{
		store:  o.Store,
		hooks:  o.Hooks,
		ready:  o.Ready,
		client: o.Client,
		tick:   o.Tick,
		grace:  o.Grace,
		queue:  make(chan armed, queueDepth),
		last:   map[string]Result{},
	}
	if r.hooks == nil {
		r.hooks = func() []Hook { return nil }
	}
	if r.client == nil {
		r.client = NewClient()
	}
	if r.tick <= 0 {
		r.tick = defaultTick
	}
	if r.grace <= 0 {
		r.grace = DeliveryGrace
	}
	r.ctx, r.cancel = context.WithCancel(context.Background())
	return r
}

// Start begins the loop. Calling it twice is a no-op, which is what lets an
// embedding start it from two places without either having to know about the
// other.
func (r *Runner) Start() {
	r.mu.Lock()
	if r.started {
		r.mu.Unlock()
		return
	}
	r.started = true
	r.mu.Unlock()
	r.wg.Add(1)
	go r.loop()
}

// Close stops the loop and waits for a call in flight to finish, so that nothing
// is still writing to a store being torn down once this returns.
//
// It FLUSHES NOTHING. A call held back because its files have not landed yet is
// a call whose whole point was the files having landed, and firing it on the way
// out would tell the media server to scan a folder the app just stopped moving
// things into. What is dropped is logged, by address, because a missed scan is
// otherwise invisible: the library is simply missing an episode.
func (r *Runner) Close() error {
	r.cancel()
	r.wg.Wait()
	return nil
}

// Enqueue records that one package finished and wants hookID called.
//
// NON-BLOCKING, ALWAYS. Its caller is a bus subscriber running on the app's own
// package sweep - see the file comment - so a full queue drops this one call
// with a log line rather than holding that sweep up. The same answer
// script.Host.fire gives for the same contract.
func (r *Runner) Enqueue(hookID, packageName string) {
	id := HookID(hookID)
	if id == "" {
		return
	}
	select {
	case r.queue <- armed{hookID: id, pkg: packageName, arrived: time.Now()}:
	default:
		log.Printf("mediahook: the call queue is full, dropping the call to %q for the package %q", id, packageName)
	}
}

// CallNow makes one call immediately and records it as the last result, which is
// what the Test button on the settings page does.
//
// It goes out on the same client and through the same Call as a real firing, on
// purpose: a test that used a different client, followed redirects or skipped
// the header would prove nothing about the thing it is testing. ctx is the
// request's own, so a browser that navigated away does not leave this waiting on
// somebody's server for the full ceiling.
func (r *Runner) CallNow(ctx context.Context, h Hook) Result {
	res := Call(ctx, r.client, h, r.value(h.ID))
	res.Test = true
	r.record(h.ID, res)
	return res
}

// Last is the most recent call for one address, test calls included, and false
// when this process has not called it yet.
//
// IN MEMORY ONLY, and the interface says so where a person can read it. What
// would be gained by persisting it is a line on a page after a restart; what it
// would cost is a second document written on every download, inside the store's
// own budget, that can come back corrupt - the identical trade feed.Health
// already made and wrote down.
func (r *Runner) Last(hookID string) (Result, bool) {
	id := HookID(hookID)
	if id == "" {
		return Result{}, false
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	res, ok := r.last[id]
	return res, ok
}

func (r *Runner) record(hookID string, res Result) {
	id := HookID(hookID)
	if id == "" {
		return
	}
	r.mu.Lock()
	r.last[id] = res
	r.mu.Unlock()
}

// value is the sealed header value for one address, or "" when there is none and
// when the store cannot open it. See Store.Value for why a decrypt failure is
// silent here rather than an error on a download path.
func (r *Runner) value(hookID string) string {
	if r.store == nil {
		return ""
	}
	return r.store.Value(hookID)
}

// loop is the whole of this package's own concurrency: one goroutine, one map of
// waiting calls, one ticker.
//
// The calls go out ON THIS GOROUTINE and not on one of their own. A second
// address's scan being asked for twenty seconds late is not something anybody
// can perceive, and the alternative is concurrency in a loop with no need of it:
// a call per goroutine would need its own in-flight bookkeeping per address to
// stop one address being called twice at once, which is a lock and a leak for no
// gain. Close waits for the loop, so a call in flight is finished before the
// stores it reads are torn down.
func (r *Runner) loop() {
	defer r.wg.Done()
	ticker := time.NewTicker(r.tick)
	defer ticker.Stop()
	pending := map[string]*waiting{}
	for {
		select {
		case <-r.ctx.Done():
			r.reportDropped(pending)
			return
		case a := <-r.queue:
			r.arm(pending, a)
		case now := <-ticker.C:
			r.due(pending, now)
		}
	}
}

// arm folds one firing into the call owed on its address, creating it when there
// is none.
//
// A firing for an address that is no longer in the table is dropped here rather
// than at send time, so the log line names the moment the fact was lost: an
// address deleted between a package finishing and its window closing is an
// ordinary thing to do, and a call to nowhere is not worth keeping.
func (r *Runner) arm(pending map[string]*waiting, a armed) {
	h, ok := r.hook(a.hookID)
	if !ok {
		log.Printf("mediahook: no address is stored under %q any more, so the package %q calls nothing", a.hookID, a.pkg)
		return
	}
	w := pending[a.hookID]
	if w == nil {
		w = &waiting{
			first: a.pkg,
			// The window is measured from the FIRST package that armed the call
			// and is never extended by a later one. Extending it would mean a box
			// finishing a package a minute for an hour never calls at all, which
			// is the shape of bug that only shows up on the busiest install.
			fireAt:   a.arrived.Add(time.Duration(h.WaitSeconds) * time.Second),
			giveUpAt: a.arrived.Add(r.grace),
		}
		pending[a.hookID] = w
	}
	w.count++
	if w.first == "" || (a.pkg != "" && a.pkg < w.first) {
		w.first = a.pkg
	}
}

// due sends every call whose window has closed and whose files have landed.
func (r *Runner) due(pending map[string]*waiting, now time.Time) {
	// Sorted, because map iteration is random and two addresses coming due in the
	// same tick would otherwise be called in a different order every time - the
	// kind of nondeterminism that makes an intermittent report impossible to
	// reproduce. The app's own package sweep sorts for the same reason.
	ids := make([]string, 0, len(pending))
	for id := range pending {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		w := pending[id]
		if now.Before(w.fireAt) {
			continue
		}
		if !r.filesLanded(w.first) && now.Before(w.giveUpAt) {
			continue
		}
		delete(pending, id)
		h, ok := r.hook(id)
		if !ok {
			log.Printf("mediahook: no address is stored under %q any more, so the package %q calls nothing", id, w.first)
			continue
		}
		res := Call(r.ctx, r.client, h, r.value(id))
		res.Package, res.Packages = w.first, w.count
		r.record(id, res)
		if !res.OK {
			// The HOST and never the whole address: Plex's own documented refresh
			// call carries its token in the query string, and this line reaches
			// the log ring and from there the diagnostics bundle.
			log.Printf("mediahook: the call to %s (%s) did not work: %s", id, urlHost(h.URL), res.problem())
		}
	}
}

// problem is what a log line says went wrong: the code, plus the raw sentence
// when there is one. The code alone would be a word nobody can search for, and
// the sentence alone loses which of the eleven cases it was folded onto.
func (res Result) problem() string {
	switch {
	case res.Error != "" && res.Code != "":
		return res.Code + ": " + res.Error
	case res.Error != "":
		return res.Error
	case res.Status != 0:
		return res.Code + " (HTTP " + strconv.Itoa(res.Status) + ")"
	default:
		return res.Code
	}
}

// filesLanded asks the embedding whether this package's files are where they
// belong. A Runner with no Ready answers yes, which is the honest answer for an
// install with no working folder: the file was written straight to its
// destination and there was never anything to wait for.
func (r *Runner) filesLanded(pkg string) bool {
	if r.ready == nil || pkg == "" {
		return true
	}
	return r.ready(pkg)
}

// hook looks one address up in the live list.
func (r *Runner) hook(id string) (Hook, bool) {
	for _, h := range r.hooks() {
		if HookID(h.ID) == id {
			return h, true
		}
	}
	return Hook{}, false
}

// reportDropped says what was still owed when the process stopped. Sorted, so
// two shutdowns of the same state read the same.
func (r *Runner) reportDropped(pending map[string]*waiting) {
	if len(pending) == 0 {
		return
	}
	ids := make([]string, 0, len(pending))
	for id := range pending {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		w := pending[id]
		log.Printf("mediahook: shutting down with a call to %q still owed for %d package(s), starting with %q; it is not sent", id, w.count, w.first)
	}
}
