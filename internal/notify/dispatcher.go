// Package notify tells the services the operator set up when something
// happens here: a webhook of their own, Discord, Slack, Gotify, ntfy, Matrix,
// mail or Apprise. Each channel is a parleyport kit channel; this package is
// the subscriber on script's bus that turns a firing into the kit's event and
// keeps each channel's queue and health.
//
// It is not web/src/lib/notify.ts, which routes an event to a toast inside one
// browser tab and never leaves it, which is why the interface calls these
// event targets.
//
// Nothing is spooled to disk. A message that cannot be delivered is tried as
// often as its channel allows and then dropped: everything reported here is
// about a moment that has passed.
package notify

import (
	"context"
	"log"
	"net/http"
	"slices"
	"sync"
	"sync/atomic"
	"time"

	kit "github.com/junkerderprovinz/parleyport/kit/notify"

	"github.com/junkerderprovinz/knightloader/internal/script"
)

const (
	// queueDepth is per channel. link.added fires once per link, so one paste
	// of a two hundred link container is two hundred messages for a channel
	// that takes them; what does not fit is dropped and counted.
	queueDepth = 256

	// maxMessageAge is how stale a queued message may be when its turn comes.
	// A burst that took five minutes to work through is news nobody waits for.
	maxMessageAge = 5 * time.Minute
)

// Health is what one channel has been doing since this process started. A
// zero LastAttempt means nothing since then, not never.
type Health struct {
	ChannelID   string    `json:"channelId"`
	LastAttempt time.Time `json:"lastAttempt,omitzero"`
	LastOK      time.Time `json:"lastOk,omitzero"`
	// LastStatus is the HTTP status of the last failed attempt, 0 when nothing
	// answered.
	LastStatus int `json:"lastStatus"`
	// LastError is the failure in English with every secret taken out, and
	// LastCode the kit's code for it, which the page translates.
	LastError string `json:"lastError,omitempty"`
	LastCode  string `json:"lastCode,omitempty"`
	// Attempts counts requests, retries included, Sent the messages that
	// arrived and Dropped those the full queue turned away.
	Attempts int `json:"attempts"`
	Sent     int `json:"sent"`
	Dropped  int `json:"dropped"`
}

// Options is what a Dispatcher needs from the app.
type Options struct {
	// InstanceName is read at delivery, since the instance can be renamed
	// while it runs.
	InstanceName func() string
	// HTTP sends every request. Nil is the kit's own client.
	HTTP *http.Client
}

// Dispatcher fans the bus out to the channels that take each event.
type Dispatcher struct {
	opt Options

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

// New builds a dispatcher with no channels. It starts no goroutine until Set.
func New(o Options) *Dispatcher {
	ctx, cancel := context.WithCancel(context.Background())
	return &Dispatcher{opt: o, ctx: ctx, cancel: cancel, workers: map[string]*worker{}, health: map[string]*Health{}}
}

// worker is one channel's queue and the goroutine draining it. One per
// channel, so a slow server holds up only its own messages and each channel
// gets them in the order they happened.
type worker struct {
	d      *Dispatcher
	id     string
	queue  chan job
	ctx    context.Context
	cancel context.CancelFunc
	// cfg is swapped whole on a save, so a delivery reads one consistent
	// channel.
	cfg atomic.Pointer[sender]
}

// sender is a channel and the kit dispatcher that sends to it alone.
type sender struct {
	ch kit.Channel
	d  *kit.Dispatcher
}

type job struct {
	f  script.Firing
	at time.Time
}

// Set makes the running workers match the channels. It runs on every settings
// save and keeps the queue and health of a channel that stays. A channel that
// is switched off, or that the kit refuses, gets no worker.
func (d *Dispatcher) Set(channels []kit.Channel) {
	d.mu.Lock()
	defer d.mu.Unlock()

	keep := make(map[string]bool, len(channels))
	for _, c := range channels {
		if !c.Enabled || c.On == kit.OnNever {
			continue
		}
		kd, err := kit.NewDispatcher([]kit.Channel{c}, kit.Options{Source: "KnightLoader", HTTP: d.opt.HTTP})
		if err != nil {
			log.Printf("notify: channel %s cannot send: %v", c.ID, err)
			continue
		}
		keep[c.ID] = true
		s := &sender{ch: c, d: kd}
		if w, ok := d.workers[c.ID]; ok {
			w.cfg.Store(s)
			continue
		}
		if w := d.start(c.ID, s); w != nil {
			d.workers[c.ID] = w
		}
	}
	for id, w := range d.workers {
		if keep[id] {
			continue
		}
		w.cancel()
		delete(d.workers, id)
		d.forget(id)
	}
}

func (d *Dispatcher) start(id string, s *sender) *worker {
	d.closeMu.Lock()
	if d.closing {
		d.closeMu.Unlock()
		return nil
	}
	d.wg.Add(1)
	d.closeMu.Unlock()

	ctx, cancel := context.WithCancel(d.ctx)
	w := &worker{d: d, id: id, queue: make(chan job, queueDepth), ctx: ctx, cancel: cancel}
	w.cfg.Store(s)
	go func() {
		defer d.wg.Done()
		defer cancel()
		w.run()
	}()
	return w
}

// On is the bus subscription. It runs on the publisher's goroutine, so it
// only filters and queues.
func (d *Dispatcher) On(f script.Firing) {
	sev := severity(f)
	d.mu.Lock()
	var want []*worker
	for _, w := range d.workers {
		if takes(w.cfg.Load().ch, string(f.Trigger), sev) {
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
			d.dropped(w.id)
			log.Printf("notify: channel %s is not keeping up, dropping this %s", w.id, f.Trigger)
		}
	}
}

// takes is the kit's own rule for whether a channel carries an event, asked
// before queueing so a burst of events nobody wants never fills a queue.
func takes(c kit.Channel, kind string, sev kit.Severity) bool {
	if !c.Enabled || (len(c.Events) > 0 && !slices.Contains(c.Events, kind)) {
		return false
	}
	switch c.On {
	case kit.OnAlways:
		return true
	case kit.OnNever:
		return false
	}
	return sev == kit.SeverityWarning || sev == kit.SeverityFailure || sev == kit.SeverityCritical
}

// Health is one row per channel with a worker, in no particular order.
func (d *Dispatcher) Health() []Health {
	d.healthMu.Lock()
	defer d.healthMu.Unlock()
	out := make([]Health, 0, len(d.health))
	for _, h := range d.health {
		out = append(out, *h)
	}
	return out
}

// Close stops taking work and waits for every delivery to end or be
// cancelled. A second call does nothing.
func (d *Dispatcher) Close() error {
	d.closeMu.Lock()
	d.closing = true
	d.closeMu.Unlock()
	d.cancel()
	d.wg.Wait()
	return nil
}

func (d *Dispatcher) name() string {
	if d.opt.InstanceName == nil {
		return ""
	}
	return d.opt.InstanceName()
}

func (d *Dispatcher) entry(id string) *Health {
	h, ok := d.health[id]
	if !ok {
		h = &Health{ChannelID: id}
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

func (d *Dispatcher) record(id string, r kit.Result) {
	now := time.Now()
	d.healthMu.Lock()
	defer d.healthMu.Unlock()
	h := d.entry(id)
	h.LastAttempt = now
	h.Attempts += r.Attempts
	h.LastStatus, h.LastCode, h.LastError = r.Status, r.Code, ""
	if r.Err != nil {
		h.LastError = r.Err.Error()
		return
	}
	h.LastOK = now
	h.Sent++
}

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

// deliver sends one message. The kit retries inside the send, as often as the
// channel allows.
func (w *worker) deliver(j job) {
	if age := time.Since(j.at); age > maxMessageAge {
		log.Printf("notify: channel %s dropping a %s that has been queued for %s", w.id, j.f.Trigger, age.Round(time.Second))
		return
	}
	s := w.cfg.Load()
	for _, r := range s.d.Send(w.ctx, Event(j.f, w.d.name())) {
		if w.ctx.Err() != nil {
			return
		}
		w.d.record(w.id, r)
	}
}
