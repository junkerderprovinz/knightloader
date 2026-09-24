package script

import (
	"context"
	"errors"
	"fmt"
	"log"
	"sync"
	"sync/atomic"
	"time"

	"github.com/dop251/goja"
	"golang.org/x/time/rate"
)

// workerCount is how many scripts may run at once. It is small so a burst of
// task completions does not compete with download I/O for CPU; further runs
// wait in the queue.
const workerCount = 4

// fireQueueDepth bounds the pending executions. A burst that outruns the
// workers drops and logs runs rather than growing memory without limit.
const fireQueueDepth = 128

// notifyPerSecond and notifyBurst limit notify() calls across the whole Host.
const (
	notifyPerSecond = 2
	notifyBurst     = 5
)

// Options configures a Host. Actions and Hub are required, so a missing one
// fails in NewHost rather than the first time a script uses it.
type Options struct {
	// DataDir is where scripts.json lives.
	DataDir string
	// Actions backs every task.* verb a script can call.
	Actions Actions
	// Hub receives notify() calls and a "script" event after every completed
	// run. *hub.Hub satisfies it.
	Hub Broadcaster
	// Bus is where the app publishes its events. Without one the Host builds
	// its own (see Host.Bus); the app should pass one in so other readers can
	// subscribe to it.
	Bus *Bus
}

// compiled pairs one stored Script with its *goja.Program, compiled once by
// rebuildIndex rather than on every firing.
type compiled struct {
	Script
	prog *goja.Program
}

// fireJob is one queued automatic execution. The Firing is copied, so a
// publisher reusing its struct cannot change what a queued script sees.
type fireJob struct {
	c *compiled
	f Firing
}

// Host is the VM host: the script store, the trigger index built from it, the
// worker pool that runs scripts, and its own tracked lifecycle.
type Host struct {
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	// closeMu guards closing only, separate from the index lock mu so the two
	// can never deadlock each other. See track.
	closeMu sync.Mutex
	closing bool

	st        *store
	actions   Actions
	hub       Broadcaster
	bus       *Bus
	notifyLim *rate.Limiter

	queue chan fireJob

	// off is the modules page's switch for scripting as a whole. While it is
	// set no event starts a script; RunNow ignores it, as it ignores Enabled.
	off atomic.Bool

	mu        sync.RWMutex
	byTrigger map[Trigger][]*compiled
}

// SetOff switches every trigger off or back on. Runs already queued are
// dropped and a script already running finishes.
func (h *Host) SetOff(off bool) { h.off.Store(off) }

// NewHost opens dataDir's script store, builds the trigger index and starts
// the worker pool. The workers run from the moment it returns, so Close must
// be called when the app shuts down.
func NewHost(o Options) (*Host, error) {
	if o.Actions == nil {
		return nil, errors.New("script: Options.Actions is required")
	}
	if o.Hub == nil {
		return nil, errors.New("script: Options.Hub is required")
	}
	st, err := openStore(o.DataDir)
	if err != nil {
		return nil, err
	}
	bus := o.Bus
	if bus == nil {
		bus = NewBus()
	}
	h := &Host{
		st:        st,
		actions:   o.Actions,
		hub:       o.Hub,
		bus:       bus,
		notifyLim: rate.NewLimiter(rate.Limit(notifyPerSecond), notifyBurst),
		queue:     make(chan fireJob, fireQueueDepth),
	}
	h.ctx, h.cancel = context.WithCancel(context.Background())
	h.rebuildIndex()
	for i := 0; i < workerCount; i++ {
		h.spawn(h.worker)
	}
	// Subscribed after the workers exist, so nothing queues before anything
	// drains.
	bus.Subscribe("scripts", h.fire)
	return h, nil
}

// Bus is the bus this Host listens on, the one from Options or its own, so a
// caller can publish to it and other consumers can subscribe.
func (h *Host) Bus() *Bus { return h.bus }

// spawn runs f on its own goroutine and makes Close wait for it. Once Close
// has begun, f does not start at all.
func (h *Host) spawn(f func()) {
	if !h.track() {
		return
	}
	go func() {
		defer h.wg.Done()
		f()
	}()
}

// track counts the caller in as work Close has to wait for, or reports false
// once Close has begun, in which case the caller must not touch h.wg. Every
// h.wg.Add(1) goes through here; the matching Done stays with the caller.
//
// The closing check and the Add happen under one lock. Checking the context
// first and adding afterwards leaves a gap in which Close can reach Wait with
// the counter at zero, which WaitGroup forbids: Close would return while a
// script still runs against a torn-down app, or the runtime would panic.
func (h *Host) track() bool {
	h.closeMu.Lock()
	defer h.closeMu.Unlock()
	if h.closing {
		return false
	}
	h.wg.Add(1)
	return true
}

// Close stops accepting new work and waits for every worker to finish its
// current script, which takes at most MaxTimeout. Calling it twice is
// harmless.
func (h *Host) Close() error {
	// The flag refuses new work, cancel stops work already running.
	h.closeMu.Lock()
	h.closing = true
	h.closeMu.Unlock()
	h.cancel()
	h.wg.Wait()
	return nil
}

// rebuildIndex replaces the trigger index wholesale from the store, so a
// reader mid-fire always sees one consistent index. Disabled scripts are left
// out, and so are scripts that no longer compile (after a hand edit or a
// newer build), which are logged instead of breaking their trigger.
func (h *Host) rebuildIndex() {
	scripts := h.st.list()
	idx := make(map[Trigger][]*compiled, len(scripts))
	for _, s := range scripts {
		if !s.Enabled {
			continue
		}
		prog, err := goja.Compile(s.ID, s.Code, true)
		if err != nil {
			log.Printf("script: %q (%s) no longer compiles, leaving it out of the trigger index: %v", s.Name, s.ID, err)
			continue
		}
		c := &compiled{Script: s, prog: prog}
		idx[s.Trigger] = append(idx[s.Trigger], c)
	}
	h.mu.Lock()
	h.byTrigger = idx
	h.mu.Unlock()
}

// SaveScript validates and persists s (see store.save), then rebuilds the
// trigger index so the change applies on the next firing.
func (h *Host) SaveScript(s Script) (Script, error) {
	saved, err := h.st.save(s)
	if err != nil {
		return Script{}, err
	}
	h.rebuildIndex()
	return saved, nil
}

// ListScripts returns every saved script, sorted by name.
func (h *Host) ListScripts() []Script { return h.st.list() }

// GetScript returns one saved script by id.
func (h *Host) GetScript(id string) (Script, bool) { return h.st.get(id) }

// DeleteScript removes a saved script and rebuilds the trigger index.
func (h *Host) DeleteScript(id string) error {
	if err := h.st.delete(id); err != nil {
		return err
	}
	h.rebuildIndex()
	return nil
}

// fire is this Host's Bus subscription and the only way an app event reaches
// the trigger registry. It is unexported so events always go through the Bus
// and stay visible to every subscriber.
//
// It must not block the publisher, so every script on the trigger is queued
// and a full queue drops that run with a log line. f.Queue was read at publish
// time, so scripts in one burst may see slightly different counters.
func (h *Host) fire(f Firing) {
	if h.off.Load() {
		return
	}
	h.mu.RLock()
	candidates := h.byTrigger[f.Trigger]
	h.mu.RUnlock()
	for _, c := range candidates {
		select {
		case h.queue <- fireJob{c: c, f: f}:
		default:
			log.Printf("script: trigger queue full, dropping this %s run of %q", f.Trigger, c.Name)
		}
	}
}

// RunNow executes one saved script immediately and synchronously, whatever its
// Enabled flag and Trigger, since testing a script before switching it on is
// the ordinary case. task may be nil. It stops when ctx is cancelled as well
// as on the script's own timeout, so an HTTP handler can pass the request
// context.
func (h *Host) RunNow(ctx context.Context, scriptID string, task *TaskView, queue QueueView) (Result, error) {
	s, ok := h.st.get(scriptID)
	if !ok {
		return Result{}, fmt.Errorf("script: %q not found", scriptID)
	}
	prog, err := goja.Compile(s.ID, s.Code, true)
	if err != nil {
		return Result{}, fmt.Errorf("script: does not compile: %w", err)
	}
	// Tracked like a worker so Close waits for it, but run here because the
	// Result goes back to the caller.
	if !h.track() {
		return Result{}, errors.New("script: host is shutting down")
	}
	defer h.wg.Done()
	// Either the caller going away or the host shutting down stops the run;
	// the caller's context alone would make Close wait out the full timeout.
	runCtx, cancel := mergeContexts(ctx, h.ctx)
	defer cancel()
	// Not published to the bus: a test run is not an app event, and a
	// subscriber must not announce a finished download because somebody
	// test-ran a task.done script.
	return h.runOne(runCtx, &s, prog, Firing{Trigger: TriggerOnDemand, At: time.Now(), Task: task, Queue: queue}), nil
}

// mergeContexts returns a context that is cancelled as soon as either a or b
// is.
func mergeContexts(a, b context.Context) (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithCancel(a)
	stop := context.AfterFunc(b, cancel)
	return ctx, func() {
		stop()
		cancel()
	}
}

// worker drains the fire queue one job at a time until Close cancels ctx.
func (h *Host) worker() {
	for {
		select {
		case <-h.ctx.Done():
			return
		case job := <-h.queue:
			if h.off.Load() {
				continue
			}
			h.runOne(h.ctx, &job.c.Script, job.c.prog, job.f)
		}
	}
}

// runOne runs one script for fire and RunNow alike, turns the outcome into a
// Result and broadcasts a "script" Event so a connected UI can show the run
// history live.
func (h *Host) runOne(ctx context.Context, s *Script, prog *goja.Program, f Firing) Result {
	timeout := clampTimeout(time.Duration(s.TimeoutMS) * time.Millisecond)

	var taskID string
	if f.Task != nil {
		taskID = f.Task.ID
	}
	// firedAt is when the event happened, not when a worker got to it.
	e := &execCtx{
		actions: h.actions,
		notify:  h.notify,
		trigger: f.Trigger,
		firedAt: f.At,
		taskID:  taskID,
		firing:  f,
	}

	started := time.Now()
	outcome := execute(ctx, prog, timeout, e)
	res := Result{
		ScriptID:   s.ID,
		Name:       s.Name,
		Trigger:    f.Trigger,
		TaskID:     taskID,
		StartedAt:  started,
		DurationMS: time.Since(started).Milliseconds(),
		Output:     outcome.output,
		OK:         outcome.err == nil,
		TimedOut:   outcome.timedOut,
	}
	if outcome.err != nil {
		res.Error = outcome.err.Error()
	}

	h.hub.Broadcast("script", Event{
		Kind:       "result",
		ScriptID:   res.ScriptID,
		Name:       res.Name,
		Trigger:    res.Trigger,
		TaskID:     res.TaskID,
		OK:         res.OK,
		Error:      res.Error,
		DurationMS: res.DurationMS,
	})
	return res
}

// notify is the Go side of the sandbox's notify(message). It reports whether
// the message went out under the Host-wide rate limit.
func (h *Host) notify(message string) bool {
	if !h.notifyLim.Allow() {
		return false
	}
	h.hub.Broadcast("script", Event{Kind: "notify", Message: message})
	return true
}
