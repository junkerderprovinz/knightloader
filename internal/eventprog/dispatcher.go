package eventprog

// The bus subscriber, the queue behind it and the worker per program.
//
// script.Bus.Publish delivers on the publisher's goroutine, which is a
// download's update path or a poll loop, so On only filters and queues, the
// shape notify.Dispatcher.On has. One worker per program keeps a slow program
// from holding up another one's events, and with Parallel at one it runs a
// program's events in the order they happened.
//
// Nothing is spooled to disk. A run still queued when the process stops is
// gone, and one under way is killed with it, along with whatever it started,
// rather than left behind as an orphan nobody is waiting for.

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/idleaction"
	"github.com/junkerderprovinz/knightloader/internal/script"
)

// queueDepth is per program. link.added fires once per link, so a pasted
// container is hundreds of events at once, and a program that takes a second
// per run falls behind. What does not fit is dropped and counted.
const queueDepth = 256

// minSecretLen is the shortest part of a command line that is looked for in a
// program's output and replaced before it is logged. A token or a home folder
// is longer than this; "-i" or "sh" is not, and replacing those would turn
// every "-i" and every "finished" in the output into stars.
const minSecretLen = 8

// deliveryGrace is how long a run waits for Options.Ready before it starts
// anyway, the ceiling mediahook.DeliveryGrace puts on the same wait: a move
// that failed for good leaves the file where it is, and the program should
// still run on it.
const deliveryGrace = 15 * time.Minute

// readyPoll is how often a waiting run asks Options.Ready again.
const readyPoll = time.Second

// Health is what one program has been doing since this process started. It is
// in memory, like notify.Health, so a zero LastStart means nothing has run since
// the start.
type Health struct {
	ProgramID string `json:"programId"`
	// LastStart is when the last run started, and LastEvent what started it.
	LastStart time.Time      `json:"lastStart,omitzero"`
	LastEvent script.Trigger `json:"lastEvent,omitempty"`
	// LastOK is when a run last ended cleanly.
	LastOK time.Time `json:"lastOk,omitzero"`
	// LastProblem is empty when the last run ended cleanly, and one of
	// idleaction's codes otherwise, so the page words it in the reader's
	// language.
	LastProblem  idleaction.Problem `json:"lastProblem,omitempty"`
	LastExitCode int                `json:"lastExitCode"`
	// LastOutput is the program's own output, capped, with the long parts of
	// the stored command line replaced.
	LastOutput     string `json:"lastOutput,omitempty"`
	LastDurationMS int64  `json:"lastDurationMs"`
	Runs           int    `json:"runs"`
	Failed         int    `json:"failed"`
	// Dropped is how many events never started a run because the queue was
	// full.
	Dropped int `json:"dropped"`
}

// Options is what a Dispatcher needs from the app it runs inside.
type Options struct {
	// InstanceName is read at run time for %%instance%%, since the instance
	// can be renamed while it runs.
	InstanceName func() string
	// Locate answers where an event's file is and which category it is in.
	// It is called on a worker's goroutine when the run starts, not on the
	// publisher's, so it may take the app's lock. Nil leaves those empty.
	Locate func(script.Firing) Where
	// Ready reports whether an event's files are where the program should
	// find them. A finished download is still in the working folder for a
	// while, being checked and then moved, and a program started then would
	// be handed a file about to disappear. A run waits until Ready says yes,
	// for at most deliveryGrace. Nil means always ready.
	Ready func(script.Firing) bool
	// Run starts the program. Nil means ExecRunner.
	Run Runner
}

// Dispatcher hands events to the programs subscribed to them.
type Dispatcher struct {
	instanceName func() string
	locate       func(script.Firing) Where
	ready        func(script.Firing) bool
	run          Runner
	// readyPoll and readyGrace are the constants, fields so a test can
	// shorten them.
	readyPoll  time.Duration
	readyGrace time.Duration

	// The closing flag and the wg.Add happen under one lock, for the reason
	// script.Host.track gives.
	ctx     context.Context
	cancel  context.CancelFunc
	closeMu sync.Mutex
	closing bool
	wg      sync.WaitGroup

	// off is the Modules page's switch. While it is set no event starts a
	// run, queued ones included; a run already under way finishes.
	off atomic.Bool

	// running counts the runs under way or waiting for their files.
	running atomic.Int32

	mu      sync.Mutex
	workers map[string]*worker

	healthMu sync.Mutex
	health   map[string]*Health
}

// New builds a dispatcher with no programs. It starts no goroutine until Set
// hands it one.
func New(o Options) *Dispatcher {
	ctx, cancel := context.WithCancel(context.Background())
	run := o.Run
	if run == nil {
		run = ExecRunner
	}
	return &Dispatcher{
		instanceName: o.InstanceName,
		locate:       o.Locate,
		ready:        o.Ready,
		run:          run,
		readyPoll:    readyPoll,
		readyGrace:   deliveryGrace,
		ctx:          ctx,
		cancel:       cancel,
		workers:      map[string]*worker{},
		health:       map[string]*Health{},
	}
}

// worker is one program's queue and the goroutine that starts its runs.
type worker struct {
	d      *Dispatcher
	id     string
	queue  chan script.Firing
	ctx    context.Context
	cancel context.CancelFunc
	// finished takes one signal per ended run. It holds MaxParallel, the most
	// runs one worker can have going, so a run ending after its worker has
	// stopped never blocks on it.
	finished chan struct{}
	// wake is nudged when the configuration changes, so a raised Parallel
	// takes effect before the next run ends.
	wake chan struct{}
	// cfg is replaced whole on a save, so a run reads one consistent row.
	cfg atomic.Pointer[Program]
}

// Set makes the running workers match the configuration. It runs on every
// settings save and reconciles rather than rebuilds, so a save elsewhere on
// the page keeps each program's queue and health row.
//
// A row that is switched off, has no program or has no event ticked gets no
// worker, since it can never start anything. Removing a worker drops what is
// queued for it and a run still waiting for its files; a run already under way
// finishes.
func (d *Dispatcher) Set(programs []Program) {
	d.mu.Lock()
	defer d.mu.Unlock()

	keep := make(map[string]bool, len(programs))
	for _, p := range programs {
		if !p.Runnable() {
			continue
		}
		keep[p.ID] = true
		cfg := p
		if w, ok := d.workers[p.ID]; ok {
			w.cfg.Store(&cfg)
			select {
			case w.wake <- struct{}{}:
			default:
			}
			continue
		}
		if w := d.start(cfg); w != nil {
			d.workers[p.ID] = w
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

// SetOff switches every program off or back on without touching the rows.
// What is queued while it is off is dropped as it comes up, so switching it
// back on does not start a backlog.
func (d *Dispatcher) SetOff(off bool) { d.off.Store(off) }

// Busy reports whether a run is under way or waiting for its files.
func (d *Dispatcher) Busy() bool { return d.running.Load() > 0 }

// start launches one program's worker, or reports nil once Close has begun.
func (d *Dispatcher) start(p Program) *worker {
	if !d.track() {
		return nil
	}
	ctx, cancel := context.WithCancel(d.ctx)
	w := &worker{
		d: d, id: p.ID, ctx: ctx, cancel: cancel,
		queue:    make(chan script.Firing, queueDepth),
		finished: make(chan struct{}, MaxParallel),
		wake:     make(chan struct{}, 1),
	}
	w.cfg.Store(&p)
	go func() {
		defer d.wg.Done()
		defer cancel()
		w.loop()
	}()
	return w
}

// track counts the caller in as work Close waits for, or reports false once
// Close has committed.
func (d *Dispatcher) track() bool {
	d.closeMu.Lock()
	defer d.closeMu.Unlock()
	if d.closing {
		return false
	}
	d.wg.Add(1)
	return true
}

// On is the bus subscription: bus.Subscribe("eventprograms", d.On). It
// filters, queues and returns, since it runs on the publisher's goroutine.
func (d *Dispatcher) On(f script.Firing) {
	if d.off.Load() {
		return
	}
	d.mu.Lock()
	var want []*worker
	for _, w := range d.workers {
		if w.cfg.Load().Wants(f.Trigger) {
			want = append(want, w)
		}
	}
	d.mu.Unlock()

	for _, w := range want {
		select {
		case w.queue <- f:
		default:
			// Waiting here would be waiting on the download that published.
			d.dropped(w)
			log.Printf("event program %s is not keeping up, dropping this %s", w.label(), f.Trigger)
		}
	}
}

// Health is one row per program that has a worker, in no particular order.
func (d *Dispatcher) Health() []Health {
	d.healthMu.Lock()
	defer d.healthMu.Unlock()
	out := make([]Health, 0, len(d.health))
	for _, h := range d.health {
		out = append(out, *h)
	}
	return out
}

// Close stops taking events, kills the runs under way and waits for them to
// end. Calling it twice is harmless.
func (d *Dispatcher) Close() error {
	d.closeMu.Lock()
	d.closing = true
	d.closeMu.Unlock()
	d.cancel()
	d.wg.Wait()
	return nil
}

// loop starts runs from the queue, never more at once than the row allows.
// A nil channel never delivers, which is how the queue is left alone while the
// limit is reached.
func (w *worker) loop() {
	active := 0
	for {
		var queue chan script.Firing
		if active < w.cfg.Load().ResolvedParallel() {
			queue = w.queue
		}
		select {
		case <-w.ctx.Done():
			return
		case <-w.wake:
		case <-w.finished:
			active--
		case f := <-queue:
			// Asked again here rather than only in On: the event may have
			// waited while the module was switched off or the row edited.
			if !w.wants(f) {
				continue
			}
			if !w.d.track() {
				return
			}
			active++
			w.d.running.Add(1)
			go func() {
				defer w.d.wg.Done()
				w.run(f)
				w.d.running.Add(-1)
				w.finished <- struct{}{}
			}()
		}
	}
}

// wants reports whether f should still start a run: the row is still there,
// the module is on and the row still has the event ticked.
func (w *worker) wants(f script.Firing) bool {
	return w.ctx.Err() == nil && !w.d.off.Load() && w.cfg.Load().Wants(f.Trigger)
}

// run waits until the event's files are in place and then starts the program
// as the row reads by then, unless the row went or stopped wanting the event
// in the meantime.
func (w *worker) run(f script.Firing) {
	if !w.d.awaitReady(w.ctx, f) || !w.wants(f) {
		return
	}
	w.d.execute(w, *w.cfg.Load(), f)
}

// awaitReady holds a run until Options.Ready says yes or the grace runs out.
// It reports false when ctx ended first, which is the row being removed or the
// process stopping.
func (d *Dispatcher) awaitReady(ctx context.Context, f script.Firing) bool {
	if d.ready == nil || d.ready(f) {
		return true
	}
	poll := time.NewTicker(d.readyPoll)
	defer poll.Stop()
	giveUp := time.NewTimer(d.readyGrace)
	defer giveUp.Stop()
	for {
		select {
		case <-ctx.Done():
			return false
		case <-giveUp.C:
			return true
		case <-poll.C:
			if d.ready(f) {
				return true
			}
		}
	}
}

// label is how a log line names a program: the name the operator gave it,
// never its path, which the diagnostics bundle must not carry.
func (w *worker) label() string {
	return label(*w.cfg.Load())
}

func label(p Program) string {
	if p.Name != "" {
		return fmt.Sprintf("%q", p.Name)
	}
	return "#" + p.ID
}

// execute runs one program for one event and records how it went on w's
// health row.
func (d *Dispatcher) execute(w *worker, p Program, f script.Firing) {
	var where Where
	if d.locate != nil {
		where = d.locate(f)
	}
	v := ValuesOf(f, where)
	args := Args(p.Command, f, v, d.name())
	env := Environ(os.Environ(), v)

	ctx, cancel := context.WithTimeout(d.ctx, p.Command.Timeout())
	defer cancel()
	started := time.Now()
	out, err := d.run(ctx, p.Command.Program, args, env)
	took := time.Since(started)
	// Only the row's own limit is a timeout; a shutdown cancels the context
	// too and is not.
	problem, code := idleaction.Classify(err, errors.Is(ctx.Err(), context.DeadlineExceeded))
	if out == "" && code == -1 {
		// The program never got as far as saying anything, so the reason it
		// did not start is the only evidence there is.
		out = err.Error()
	}
	// Redacted before it is cut, or a token straddling the cut would reach the
	// log as a prefix the redaction cannot match.
	out = idleaction.TrimOutput(redactOutput(p.Command, out))

	d.record(w, f.Trigger, started, took, problem, code, out)
	logRun(p, f.Trigger, took, problem, code, out)
}

// redactOutput replaces the long parts of the stored command line in what the
// program printed, for the reason idleaction.CommandSpec.RedactIn gives: the
// output goes into the log, and the log into the diagnostics bundle.
func redactOutput(cmd idleaction.CommandSpec, out string) string {
	long := idleaction.CommandSpec{}
	if len(strings.TrimSpace(cmd.Program)) >= minSecretLen {
		long.Program = cmd.Program
	}
	for _, a := range cmd.Args {
		if len(strings.TrimSpace(a)) >= minSecretLen {
			long.Args = append(long.Args, a)
		}
	}
	return long.RedactIn(out)
}

// logRun writes one line per run, with the exit code and the output, and
// without the program's path or arguments.
func logRun(p Program, tr script.Trigger, took time.Duration, problem idleaction.Problem, code int, out string) {
	took = took.Round(time.Millisecond)
	var msg string
	switch problem {
	case idleaction.ProblemNone:
		msg = fmt.Sprintf("event program %s on %s: exit 0 after %s", label(p), tr, took)
	case idleaction.ProblemExit:
		msg = fmt.Sprintf("event program %s on %s: exit %d after %s", label(p), tr, code, took)
	default:
		msg = fmt.Sprintf("event program %s on %s failed (%s) after %s", label(p), tr, problem, took)
	}
	if out != "" {
		// One run, one line: a program's second line of output would
		// otherwise read as a log entry of its own.
		msg += ": " + strings.Join(strings.Fields(strings.ReplaceAll(out, "\n", " | ")), " ")
	}
	log.Print(msg)
}

func (d *Dispatcher) name() string {
	if d.instanceName == nil {
		return ""
	}
	return d.instanceName()
}

// entry is this program's health row, created on first use. The caller holds
// healthMu.
func (d *Dispatcher) entry(id string) *Health {
	h, ok := d.health[id]
	if !ok {
		h = &Health{ProgramID: id}
		d.health[id] = h
	}
	return h
}

// current reports whether w is still the worker of its row. A row removed
// while one of its runs was going must not get its health row back when that
// run ends. The caller holds mu, which Set holds while it forgets a row.
func (d *Dispatcher) current(w *worker) bool {
	return d.workers[w.id] == w
}

func (d *Dispatcher) dropped(w *worker) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if !d.current(w) {
		return
	}
	d.healthMu.Lock()
	defer d.healthMu.Unlock()
	d.entry(w.id).Dropped++
}

func (d *Dispatcher) forget(id string) {
	d.healthMu.Lock()
	defer d.healthMu.Unlock()
	delete(d.health, id)
}

func (d *Dispatcher) record(w *worker, tr script.Trigger, at time.Time, took time.Duration, problem idleaction.Problem, code int, out string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if !d.current(w) {
		return
	}
	d.healthMu.Lock()
	defer d.healthMu.Unlock()
	h := d.entry(w.id)
	h.LastStart = at
	h.LastEvent = tr
	h.LastProblem = problem
	h.LastExitCode = code
	h.LastOutput = out
	h.LastDurationMS = took.Milliseconds()
	h.Runs++
	if problem == idleaction.ProblemNone {
		h.LastOK = at.Add(took)
	} else {
		h.Failed++
	}
}
