package notify

// The bus subscriber, the queue behind it and the worker that empties it.
//
// THE ONE CONTRACT THAT SHAPES EVERYTHING HERE: script.Bus.Publish delivers
// synchronously, on the publisher's own goroutine, and the publishers are a
// download's update path (app_dispatch.go's onUpdate), the collector's put,
// verifyTask, settleExtraction and two 2 s poll loops. bus.go states it in
// capitals for exactly this subscriber. So On MUST return promptly: an
// http.Post written straight into it stalls the download that published for up
// to the target's timeout, and with three targets subscribed to task.done, for
// three times that.
//
// On therefore filters by trigger, drops the firing into a bounded channel and
// returns - the same shape script.Host.fire uses, including the drop-with-a-log
// on a full queue. dispatcher_test.go's TestOnDoesNotBlockThePublisher is what
// keeps it that way; it is written to go red the moment somebody "simplifies"
// this into a direct Send.
//
// ONE GOROUTINE PER TARGET, not a shared pool. Two independent reasons: a
// target whose server takes ten seconds must not delay a different target's
// message, and one target's own messages must stay in the order they happened,
// which a pool of workers pulling from one queue cannot promise. The cost is a
// goroutine per enabled target, which for a feature configured by hand is a
// handful.
//
// NOTHING IS SPOOLED TO DISK. A message that could not be delivered inside its
// attempts, or that has been queued longer than maxMessageAge, is abandoned.
// Everything here reports a moment that has already passed, and delivering "a
// package finished" an hour later - after a restart, from a file - is worse
// than not delivering it: the operator watched it finish and has moved on.

import (
	"context"
	"log"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/script"
)

const (
	// queueDepth is per target, not shared. link.added fires once per link
	// (script.go), so one paste of a two hundred link container is two hundred
	// messages, and this is the number that decides whether the two hundred and
	// first is dropped or is allowed to make the process buffer for a target
	// that is not keeping up. Dropping is the right answer and the drop is
	// counted, so the settings page can say the target is too slow for the
	// events that were ticked instead of the operator wondering.
	queueDepth = 256

	// maxMessageAge is how stale a queued message may be when its turn comes.
	// Past this it is abandoned rather than delivered: a burst that took five
	// minutes to work through is a target that is not keeping up, and the news
	// at the back of that queue is no longer news.
	maxMessageAge = 5 * time.Minute
)

// backoffSteps is the wait between attempts, indexed by the attempt that just
// failed. Short, then long: the failures worth repeating clear in seconds (a
// push server restarting, one dropped packet), and anything still failing after
// forty seconds is a thing to go and fix rather than to keep poking.
var backoffSteps = []time.Duration{2 * time.Second, 8 * time.Second, 30 * time.Second}

// Health is what one target has been doing since this process started.
//
// IN MEMORY AND NOT ON DISK, the same call feed.Health makes and with the same
// consequence: a zero LastAttempt means "nothing since this process started",
// never "never". A target that has been delivering for a year reads as silent
// for the seconds after a restart, and drawing that as a fault would be a false
// alarm on every boot - which is why the page has its own sentence for it.
type Health struct {
	TargetID string `json:"targetId"`
	// LastAttempt is when a request was last MADE, whether it worked or not.
	LastAttempt time.Time `json:"lastAttempt,omitzero"`
	// LastOK is when one last arrived. Separate from LastAttempt because the
	// gap between them is the whole story: both recent is a working target,
	// LastAttempt recent and LastOK old is a target that has been failing since
	// then.
	LastOK time.Time `json:"lastOk,omitzero"`
	// LastStatus is the status of the last attempt, 0 when nothing answered.
	LastStatus int `json:"lastStatus"`
	// LastError is the transport failure, redacted, empty when the far end
	// answered at all. A refusal is not an error here: it is in LastStatus and
	// LastCode.
	LastError string `json:"lastError,omitempty"`
	// LastCode is a Problem code, so the page can say what to try in the
	// reader's own language.
	LastCode string `json:"lastCode,omitempty"`
	// Attempts is how many requests this target has made, retries included. It
	// sits beside Sent so that "12 attempts, 4 delivered" reads as the retry
	// story it is.
	Attempts int `json:"attempts"`
	// Sent is how many messages arrived.
	Sent int `json:"sent"`
	// Dropped is how many never left the queue because it was full. It is the
	// number that says a target is too slow for the events it is subscribed to.
	Dropped int `json:"dropped"`
}

// Options is what a Dispatcher needs from the app it runs inside.
type Options struct {
	// InstanceName is read at DELIVERY time rather than captured once, because
	// it is a settings field somebody can rename while the app is running, and a
	// name captured at construction would go on being sent for as long as the
	// process lived. Nil is allowed and expands %%instance%% to empty.
	InstanceName func() string
}

// Dispatcher fans events out to the targets subscribed to them.
type Dispatcher struct {
	instanceName func() string

	// ctx/cancel/wg/closeMu/closing are lifted from script.Host's own
	// spawn/track/Close, whose doc comments explain at length why the closing
	// flag and the wg.Add have to happen under ONE lock: written as a context
	// check followed by a bare Add, Close can land in the gap, reach wg.Wait()
	// with the counter at zero, and either return while a delivery is still
	// running or panic outright with "WaitGroup misuse".
	ctx     context.Context
	cancel  context.CancelFunc
	closeMu sync.Mutex
	closing bool
	wg      sync.WaitGroup

	mu      sync.Mutex
	workers map[string]*worker

	healthMu sync.Mutex
	health   map[string]*Health
}

// New builds a dispatcher with no targets. It starts no goroutines until Set is
// called, so an install that has never configured a target costs one struct.
func New(o Options) *Dispatcher {
	ctx, cancel := context.WithCancel(context.Background())
	return &Dispatcher{
		instanceName: o.InstanceName,
		ctx:          ctx,
		cancel:       cancel,
		workers:      map[string]*worker{},
		health:       map[string]*Health{},
	}
}

// worker is one target's queue and the goroutine draining it.
type worker struct {
	d      *Dispatcher
	id     string
	queue  chan job
	ctx    context.Context
	cancel context.CancelFunc
	// cfg is replaced wholesale on every settings save rather than edited, so a
	// delivery mid-flight is reading one consistent Target and never a
	// half-applied one - the same "replaced wholesale, never edited in place"
	// rule script.Host.rebuildIndex states for its trigger index.
	cfg atomic.Pointer[Target]
}

// job is one queued message: what happened, and when it happened. The time is
// carried rather than read at delivery so maxMessageAge measures the age of the
// NEWS, not of the attempt.
type job struct {
	f  script.Firing
	at time.Time
}

// Set makes the running workers match the configuration. It runs on every
// settings save, so adding a target needs no restart.
//
// The live set is RECONCILED rather than rebuilt, for the reason applyFeeds
// gives about its pollers: a worker carries a queue and a health row, and
// rebuilding on every save would mean saving the speed limit throws away
// whatever was queued and blanks the table the operator is looking at.
//
// A target that is switched off, has no address or has no event ticked gets no
// worker at all. All three are the off state and none of them can ever produce
// a message, so a goroutine for one would be a goroutine that exists to select
// on a channel nothing writes to.
func (d *Dispatcher) Set(targets []Target) {
	d.mu.Lock()
	defer d.mu.Unlock()

	keep := make(map[string]bool, len(targets))
	for _, t := range targets {
		if !t.Enabled || len(t.Triggers) == 0 || strings.TrimSpace(t.URL) == "" || t.ID == "" {
			continue
		}
		keep[t.ID] = true
		cfg := t
		if w, ok := d.workers[t.ID]; ok {
			w.cfg.Store(&cfg)
			continue
		}
		if w := d.start(cfg); w != nil {
			d.workers[t.ID] = w
		}
	}
	for id, w := range d.workers {
		if keep[id] {
			continue
		}
		w.cancel()
		delete(d.workers, id)
		// The health row goes with the worker. A row for a target nobody is
		// sending to is a row that reports on a past this build cannot explain
		// - "4 delivered" beside a switch that is off reads as a live target -
		// and it is the same call feedState makes when it drops the record of a
		// subscription that is no longer configured.
		d.forget(id)
	}
}

// start launches one target's worker, or reports nil if Close has already
// committed to shutting down.
func (d *Dispatcher) start(t Target) *worker {
	if !d.track() {
		return nil
	}
	ctx, cancel := context.WithCancel(d.ctx)
	w := &worker{d: d, id: t.ID, queue: make(chan job, queueDepth), ctx: ctx, cancel: cancel}
	w.cfg.Store(&t)
	go func() {
		defer d.wg.Done()
		defer cancel()
		w.run()
	}()
	return w
}

// track counts the caller in as work Close has to wait for, or reports false
// once Close has committed. Copied from script.Host.track, whose own doc
// comment is the long version of why the check and the Add are one step under
// one lock.
func (d *Dispatcher) track() bool {
	d.closeMu.Lock()
	defer d.closeMu.Unlock()
	if d.closing {
		return false
	}
	d.wg.Add(1)
	return true
}

// On is the bus subscription. Wire it with
// bus.Subscribe("eventtargets", d.On).
//
// It filters, queues and returns. Nothing else may ever go in here - see this
// file's own opening note for whose goroutine it is running on.
func (d *Dispatcher) On(f script.Firing) {
	d.mu.Lock()
	var want []*worker
	for _, w := range d.workers {
		if w.cfg.Load().Wants(f.Trigger) {
			want = append(want, w)
		}
	}
	d.mu.Unlock()

	at := f.At
	if at.IsZero() {
		at = time.Now()
	}
	for _, w := range want {
		select {
		case w.queue <- job{f: f, at: at}:
		default:
			// Dropped rather than waited on, which is the whole point: waiting
			// here is waiting on the download that published. Counted so the
			// settings page can say so, and logged once per drop because a
			// target that is being outrun is a real configuration problem.
			d.dropped(w.id)
			log.Printf("notify: target %s is not keeping up, dropping this %s", w.id, f.Trigger)
		}
	}
}

// Health is one row per target that currently has a worker, in no particular
// order - the caller joins it onto the configured list, which is what decides
// the order the operator sees.
func (d *Dispatcher) Health() []Health {
	d.healthMu.Lock()
	defer d.healthMu.Unlock()
	out := make([]Health, 0, len(d.health))
	for _, h := range d.health {
		out = append(out, *h)
	}
	return out
}

// Close stops accepting new work and waits for every in-flight delivery to
// finish or be cancelled. Calling it twice is harmless: the flag and the cancel
// are both idempotent and a second Wait on a drained WaitGroup returns at once.
func (d *Dispatcher) Close() error {
	d.closeMu.Lock()
	d.closing = true
	d.closeMu.Unlock()
	d.cancel()
	d.wg.Wait()
	return nil
}

// name is the instance name at this moment, or "".
func (d *Dispatcher) name() string {
	if d.instanceName == nil {
		return ""
	}
	return d.instanceName()
}

// entry is this target's health row, created on first use. Callers hold nothing.
func (d *Dispatcher) entry(id string) *Health {
	h, ok := d.health[id]
	if !ok {
		h = &Health{TargetID: id}
		d.health[id] = h
	}
	return h
}

func (d *Dispatcher) dropped(id string) {
	d.healthMu.Lock()
	defer d.healthMu.Unlock()
	d.entry(id).Dropped++
}

func (d *Dispatcher) forget(id string) {
	d.healthMu.Lock()
	defer d.healthMu.Unlock()
	delete(d.health, id)
}

// record writes one attempt into the health row.
func (d *Dispatcher) record(id string, att Attempt) {
	now := time.Now()
	d.healthMu.Lock()
	defer d.healthMu.Unlock()
	h := d.entry(id)
	h.LastAttempt = now
	h.Attempts++
	h.LastStatus = att.Status
	h.LastError = att.Err
	h.LastCode = att.Code
	if att.OK() {
		h.LastOK = now
		h.Sent++
	}
}

// run drains this target's queue for as long as the worker lives.
func (w *worker) run() {
	for {
		select {
		case <-w.ctx.Done():
			return
		case j := <-w.queue:
			w.deliver(j)
		}
	}
}

// deliver sends one message, retrying as far as the row allows.
//
// The configuration is read ONCE, at the top, and the whole delivery uses that
// copy. A save that lands between two attempts therefore takes effect on the
// next MESSAGE rather than on the next attempt, which is the readable
// behaviour: a retry that went to a different address than the try before it
// would make the health row impossible to interpret.
func (w *worker) deliver(j job) {
	t := *w.cfg.Load()
	attempts := t.ResolvedAttempts()
	for n := 1; ; n++ {
		if w.ctx.Err() != nil {
			return
		}
		if age := time.Since(j.at); age > maxMessageAge {
			log.Printf("notify: target %s dropping a %s that has been queued for %s", w.id, j.f.Trigger, age.Round(time.Second))
			return
		}
		att := Send(w.ctx, t, j.f, w.d.name())
		w.d.record(w.id, att)
		if att.OK() || !att.Retryable || n >= attempts {
			return
		}
		if !sleep(w.ctx, backoffFor(n)) {
			return
		}
	}
}

// backoffFor is the wait after the nth failed attempt, capped at the last step
// so a row asking for five attempts does not need five numbers written down.
func backoffFor(n int) time.Duration {
	if n < 1 {
		n = 1
	}
	if n > len(backoffSteps) {
		n = len(backoffSteps)
	}
	return backoffSteps[n-1]
}

// sleep waits, and reports false if the wait was cut short by shutdown. A bare
// time.Sleep here would make Close wait out a thirty second backoff on a target
// that is already gone.
func sleep(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}
