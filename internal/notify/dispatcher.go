package notify

// The bus subscriber, the queue behind it and the worker that empties it.
//
// script.Bus.Publish delivers synchronously, on the publisher's goroutine, and
// the publishers are a download's update path, the collector's put, verifyTask,
// settleExtraction and two poll loops. So On has to return promptly: an
// http.Post written straight into it stalls the download that published for up
// to the target's timeout, and for three times that with three targets
// subscribed to task.done. On filters by trigger, drops the firing into a
// bounded channel and returns, the shape script.Host.fire uses.
//
// One goroutine per target rather than a shared pool, for two reasons: a target
// whose server takes ten seconds must not delay another target's message, and
// one target's messages have to stay in the order they happened, which workers
// pulling from one queue cannot promise. The cost is a goroutine per enabled
// target, which for a hand-configured feature is a handful.
//
// Nothing is spooled to disk. A message that could not be delivered inside its
// attempts, or that has been queued longer than maxMessageAge, is abandoned:
// the operator watched the package finish and has moved on.

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
	// queueDepth is per target. link.added fires once per link, so one paste of
	// a two hundred link container is two hundred messages, and this decides
	// whether a target that is not keeping up starts making the process buffer.
	// Drops are counted, so the settings page can say the target is too slow
	// for the events that were ticked.
	queueDepth = 256

	// maxMessageAge is how stale a queued message may be when its turn comes.
	// Past this it is abandoned: a burst that took five minutes to work through
	// is a target that is not keeping up, and the news at the back of that
	// queue is no longer news.
	maxMessageAge = 5 * time.Minute
)

// backoffSteps is the wait between attempts, indexed by the attempt that just
// failed. Short, then long: the failures worth repeating clear in seconds (a
// push server restarting, one dropped packet), and anything still failing after
// forty seconds is something to go and fix.
var backoffSteps = []time.Duration{2 * time.Second, 8 * time.Second, 30 * time.Second}

// Health is what one target has been doing since this process started.
//
// It is in memory, the same call feed.Health makes, so a zero LastAttempt means
// "nothing since this process started" rather than "never". A target that has
// been delivering for a year reads as silent for the seconds after a restart,
// which the page has its own sentence for.
type Health struct {
	TargetID string `json:"targetId"`
	// LastAttempt is when a request was last made, whether it worked or not.
	LastAttempt time.Time `json:"lastAttempt,omitzero"`
	// LastOK is when one last arrived. The gap between the two is the story:
	// both recent is a working target, LastAttempt recent and LastOK old is a
	// target that has been failing since then.
	LastOK time.Time `json:"lastOk,omitzero"`
	// LastStatus is the status of the last attempt, 0 when nothing answered.
	LastStatus int `json:"lastStatus"`
	// LastError is the transport failure, redacted, empty when the far end
	// answered at all. A refusal is in LastStatus and LastCode instead.
	LastError string `json:"lastError,omitempty"`
	// LastCode is a Problem code, so the page can say what to try in the
	// reader's own language.
	LastCode string `json:"lastCode,omitempty"`
	// Attempts is how many requests this target has made, retries included, so
	// that "12 attempts, 4 delivered" reads as the retry story it is.
	Attempts int `json:"attempts"`
	// Sent is how many messages arrived.
	Sent int `json:"sent"`
	// Dropped is how many never left the queue because it was full, the number
	// that says a target is too slow for the events it is subscribed to.
	Dropped int `json:"dropped"`
}

// Options is what a Dispatcher needs from the app it runs inside.
type Options struct {
	// InstanceName is read at delivery time rather than captured once, since
	// somebody can rename the instance while the app runs and a captured name
	// would go on being sent for the life of the process. Nil expands
	// %%instance%% to empty.
	InstanceName func() string
}

// Dispatcher fans events out to the targets subscribed to them.
type Dispatcher struct {
	instanceName func() string

	// The closing flag and the wg.Add happen under one lock, as in
	// script.Host's spawn and Close. Written as a context check followed by a
	// bare Add, Close can land in the gap, reach wg.Wait with the counter at
	// zero, and either return while a delivery is running or panic with
	// "WaitGroup misuse".
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
	// cfg is replaced wholesale on a settings save rather than edited, so a
	// delivery mid-flight reads one consistent Target and never a half-applied
	// one, as script.Host.rebuildIndex does for its trigger index.
	cfg atomic.Pointer[Target]
}

// job is one queued message: what happened, and when. The time is carried
// rather than read at delivery, so maxMessageAge measures the age of the news
// rather than of the attempt.
type job struct {
	f  script.Firing
	at time.Time
}

// Set makes the running workers match the configuration. It runs on every
// settings save, so adding a target needs no restart.
//
// The live set is reconciled rather than rebuilt, as applyFeeds does with its
// pollers: a worker carries a queue and a health row, so rebuilding on every
// save would mean saving the speed limit throws away whatever was queued and
// blanks the table the operator is looking at.
//
// A target that is switched off, has no address or has no event ticked gets no
// worker. None of the three can produce a message, so the goroutine would only
// select on a channel nothing writes to.
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
		// The health row goes with the worker: "4 delivered" beside a switch
		// that is off reads as a live target. feedState drops the record of an
		// unconfigured subscription for the same reason.
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
// once Close has committed. The check and the Add are one step under one lock;
// script.Host.track explains why.
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
// It filters, queues and returns. Nothing slower belongs here, since it runs on
// the publisher's goroutine.
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
			// Dropped rather than waited on, because waiting here is waiting
			// on the download that published. Counted so the settings page can
			// say so, and logged, since a target being outrun is a real
			// configuration problem.
			d.dropped(w.id)
			log.Printf("notify: target %s is not keeping up, dropping this %s", w.id, f.Trigger)
		}
	}
}

// Health is one row per target that has a worker, in no particular order. The
// caller joins it onto the configured list, which decides the order on screen.
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
// The configuration is read once at the top and the whole delivery uses that
// copy, so a save landing between two attempts takes effect on the next message
// rather than the next attempt. A retry that went to a different address than
// the try before it would make the health row impossible to read.
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
