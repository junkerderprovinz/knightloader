package script

import (
	"context"
	"errors"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/dop251/goja"
	"golang.org/x/time/rate"
)

// workerCount is how many scripts may run at once. Deliberately small and
// fixed: this is automation glue reacting to app events, not a compute
// cluster, and a burst of task completions should not compete hard with
// real download I/O for CPU. A fifth candidate simply waits its turn in the
// queue (see Fire) rather than spawning an unbounded goroutine per firing.
const workerCount = 4

// fireQueueDepth bounds how many pending executions Fire will hold before
// it starts dropping the newest arrivals - generous next to workerCount
// given MaxTimeout is 30s worst case, but still a hard number rather than
// an unbounded channel, for the same reason internal/hub.queueDepth is a
// hard number: a burst that outruns the workers should degrade (a dropped,
// logged firing) rather than grow memory without limit.
const fireQueueDepth = 128

// notifyPerSecond/notifyBurst bound the sandbox's notify() calls
// Host-wide, not per script - see the package doc comment's notify() entry
// for why the limit is shared rather than per-script.
const (
	notifyPerSecond = 2
	notifyBurst     = 5
)

// Options configures a Host. All three fields are required; NewHost refuses
// to build one without them rather than silently running with a nil
// Actions or Broadcaster, which would only surface the first time a script
// tried to use either.
type Options struct {
	// DataDir is where scripts.json lives - the same directory Settings,
	// Accounts, Federation and Auth already open their own files in.
	DataDir string
	// Actions backs every task.* verb a script can call. See Actions' own
	// doc comment for the contract an implementation must uphold.
	Actions Actions
	// Hub receives notify() calls and a "script" event after every
	// completed run. *hub.Hub satisfies this with no changes on its side.
	Hub Broadcaster
}

// compiled pairs one stored Script with its parsed *goja.Program, built
// once by rebuildIndex rather than re-parsed on every firing - the same
// compile-once-reuse-many shape internal/rules.Compile already uses for its
// own Matcher.
type compiled struct {
	Script
	prog *goja.Program
}

// fireJob is one queued automatic execution.
type fireJob struct {
	c       *compiled
	trigger Trigger
	task    *TaskView
	queue   QueueView
}

// Host is the VM host: the script store, the trigger index built from it,
// the worker pool that actually runs scripts, and this package's own
// tracked lifecycle. See the package doc comment's "every goroutine this
// package starts is tracked" section for why Host has its own
// ctx/cancel/WaitGroup rather than borrowing anything from internal/app.
type Host struct {
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	// closeMu guards closing and nothing else, and is deliberately not the
	// mu further down: that one is the trigger index's lock, and a single
	// mutex holding two unrelated jobs is how a later change to Fire or
	// rebuildIndex - either of which could one day want to reach a path
	// that registers work - turns into a lifecycle deadlock nobody
	// predicted. closing is what makes "is this host still accepting work?"
	// and "count me in" one atomic step; see track.
	closeMu sync.Mutex
	closing bool

	st        *store
	actions   Actions
	hub       Broadcaster
	notifyLim *rate.Limiter

	queue chan fireJob

	mu        sync.RWMutex
	byTrigger map[Trigger][]*compiled
}

// NewHost opens dataDir's script store, builds the initial trigger index
// and starts the worker pool. Call Close when the embedding app shuts down
// - see the package doc comment for why this is not optional in the way an
// idle, empty registry might suggest: the worker goroutines are running
// from the moment this returns.
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
	h := &Host{
		st:        st,
		actions:   o.Actions,
		hub:       o.Hub,
		notifyLim: rate.NewLimiter(rate.Limit(notifyPerSecond), notifyBurst),
		queue:     make(chan fireJob, fireQueueDepth),
	}
	h.ctx, h.cancel = context.WithCancel(context.Background())
	h.rebuildIndex()
	for i := 0; i < workerCount; i++ {
		h.spawn(h.worker)
	}
	return h, nil
}

// spawn runs f on its own goroutine and makes Close wait for it - this
// package's own copy of the a.spawn convention (internal/app/app.go), not
// that method itself: a.spawn is unexported on *app.App, and this package
// does not import internal/app (see the package doc comment). A goroutine
// that arrives after Close has committed to shutting down does not start at
// all, the same reasoning a.spawn's own doc comment gives: there is no
// useful work left for it once shutdown is under way. Which side of that
// line a given call falls on is track's decision, taken atomically - not a
// context check Close can overtake between the check and the register.
func (h *Host) spawn(f func()) {
	if !h.track() {
		return
	}
	go func() {
		defer h.wg.Done()
		f()
	}()
}

// track counts the caller in as work Close has to wait for, or reports
// false if Close has already committed to shutting down - in which case the
// caller must not touch h.wg at all. Every h.wg.Add(1) in this package goes
// through here; the matching Done stays with whoever called it.
//
// The check and the Add are one step under one lock on purpose. Written the
// obvious way instead - `if h.ctx.Err() != nil { return }` and then a bare
// h.wg.Add(1), which is what both spawn and RunNow used to do - the two
// halves are a check-then-act with a gap Close can land in: the caller
// finds the context still live, Close cancels and reaches h.wg.Wait() with
// the counter already at zero so Wait returns at once, and only then does
// the caller's Add(1) run. sync.WaitGroup names that case as misuse in so
// many words ("calls with a positive delta that occur when the counter is
// zero must happen before a Wait"), and it costs one of two things: a Close
// that returned while a script it was supposed to wait for is still running
// - free to go on calling h.actions and h.hub.Broadcast against a
// torn-down app, which is the exact regression the wg/ctx pairing exists to
// prevent - or an outright "sync: WaitGroup misuse: Add called concurrently
// with Wait" panic taking the process down. Holding closeMu across both
// halves closes the gap: a caller that gets in before Close's flip has its
// Add(1) done before Wait can be reached, and one that arrives after it is
// turned away instead of racing for the register.
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
// current script (bounded by that script's own timeout, at most
// MaxTimeout) before returning. Wire this into App.Close alongside sched
// and idleAction - see the package doc comment's own wiring note.
// Calling it twice is harmless: the flag and cancel are both idempotent and
// a second Wait on a drained WaitGroup returns immediately.
func (h *Host) Close() error {
	// Flipped first, under the same lock track() takes, so that from here on
	// no RunNow or spawn can still slip a wg.Add(1) past the Wait below - see
	// track's own comment for what that used to cost. cancel() stays after
	// it: the flag is what refuses new work, cancel is what stops work
	// already under way.
	h.closeMu.Lock()
	h.closing = true
	h.closeMu.Unlock()
	h.cancel()
	h.wg.Wait()
	return nil
}

// rebuildIndex replaces the trigger index wholesale from the current store
// contents - the same "replaced wholesale, never edited in place" shape
// internal/app's own pkgRules/filtRules use (app.go's applyRuleSets), kept
// for the identical reason: a reader mid-Fire must see one consistent
// index, never a partial edit of it. Disabled scripts and scripts that no
// longer compile (a hand-edited scripts.json, or a future build changing
// what compiles) are left out and logged once rather than allowed to break
// every other script sharing their trigger.
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

// SaveScript validates, compiles and persists s (see store.save), then
// rebuilds the trigger index so the change takes effect on the very next
// firing rather than waiting for a restart.
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

// Fire is the trigger registry's one entry point for a real app event. It
// never blocks its caller: every enabled script registered on trigger is
// queued for the worker pool, and a full queue drops that one script's run
// with a log line rather than making the caller - eventually a line right
// next to an existing a.Hub.Broadcast("task", &c) or
// a.Hub.Broadcast("queue", ...) call, per the package doc's wiring note -
// wait on however long the slowest currently-running script takes. This is
// the same promise Hub.Broadcast itself already makes, for the same
// reason.
//
// task is nil for TriggerQueueIdle and for any TriggerOnDemand firing not
// bound to one task; queue is read at call time, so scripts bound to the
// same trigger during one burst may legitimately see slightly different
// counters, exactly as two browsers polling Counters() a moment apart
// would.
func (h *Host) Fire(trigger Trigger, task *TaskView, queue QueueView) {
	h.mu.RLock()
	candidates := h.byTrigger[trigger]
	h.mu.RUnlock()
	for _, c := range candidates {
		job := fireJob{c: c, trigger: trigger, task: task, queue: queue}
		select {
		case h.queue <- job:
		default:
			log.Printf("script: trigger queue full, dropping this %s run of %q", trigger, c.Name)
		}
	}
}

// RunNow executes one saved script immediately and synchronously,
// regardless of its own Enabled flag or which Trigger it is bound to - the
// "run this now" path a future route (11B/11C's wiring) hangs off. task may
// be nil. It respects ctx in addition to the script's own timeout, so an
// HTTP handler can hand it the request's context and have it stop promptly
// if the caller disconnects, without needing a separate cancellation path.
//
// Running a script by hand does not require it to be Enabled on purpose:
// testing a script before switching it on is the ordinary case JD's own
// "Test Run" button (docs/jd-feature-census.md's Script editor row) exists
// for, not an edge one.
func (h *Host) RunNow(ctx context.Context, scriptID string, task *TaskView, queue QueueView) (Result, error) {
	s, ok := h.st.get(scriptID)
	if !ok {
		return Result{}, fmt.Errorf("script: %q not found", scriptID)
	}
	prog, err := goja.Compile(s.ID, s.Code, true)
	if err != nil {
		return Result{}, fmt.Errorf("script: does not compile: %w", err)
	}
	// Tracked the same way every worker-pool execution already is - see
	// spawn's own doc comment. Without this, Close() returned while a
	// RunNow script was still running, free to go on calling h.actions and
	// h.hub.Broadcast against a shut-down app. Not routed through spawn
	// itself: spawn is fire-and-forget, and RunNow has to hand its Result
	// back to the caller synchronously. Registering through track rather
	// than checking the context and then adding, because those two apart are
	// a race Close can land in the middle of - see track.
	if !h.track() {
		return Result{}, errors.New("script: host is shutting down")
	}
	defer h.wg.Done()
	// Merged so EITHER the caller disconnecting or the host shutting down
	// stops the script. runOne's own execute() only ever watches one ctx;
	// the caller's alone would leave Close() waiting out the script's full
	// timeout (up to MaxTimeout) instead of actually interrupting it.
	runCtx, cancel := mergeContexts(ctx, h.ctx)
	defer cancel()
	return h.runOne(runCtx, &s, prog, TriggerOnDemand, task, queue), nil
}

// mergeContexts returns a context cancelled the moment either a or b is -
// context.Context has no built-in way to watch two parents at once, and
// RunNow needs exactly that: the caller's own ctx (an HTTP request) and the
// Host's shutdown ctx both have to be able to stop the same run.
func mergeContexts(a, b context.Context) (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithCancel(a)
	stop := context.AfterFunc(b, cancel)
	return ctx, func() {
		stop()
		cancel()
	}
}

// worker drains the fire queue one job at a time until Close cancels ctx.
// Exactly workerCount of these run at once - see that constant's own
// comment - each processing jobs serially, so no single worker ever runs
// two scripts concurrently on the same goja.Runtime pool slot.
func (h *Host) worker() {
	for {
		select {
		case <-h.ctx.Done():
			return
		case job := <-h.queue:
			h.runOne(h.ctx, &job.c.Script, job.c.prog, job.trigger, job.task, job.queue)
		}
	}
}

// runOne is the one code path both Fire (via worker) and RunNow funnel
// through: resolve the timeout, build the execCtx, run it, turn the
// outcome into a Result, and broadcast a "script" Event so a connected UI
// can show a live run history without polling.
func (h *Host) runOne(ctx context.Context, s *Script, prog *goja.Program, trigger Trigger, task *TaskView, queue QueueView) Result {
	timeout := clampTimeout(time.Duration(s.TimeoutMS) * time.Millisecond)

	var taskID string
	if task != nil {
		taskID = task.ID
	}
	e := &execCtx{
		actions: h.actions,
		notify:  h.notify,
		trigger: trigger,
		firedAt: time.Now(),
		taskID:  taskID,
		task:    task,
		queue:   queue,
	}

	started := time.Now()
	outcome := execute(ctx, prog, timeout, e)
	res := Result{
		ScriptID:   s.ID,
		Name:       s.Name,
		Trigger:    trigger,
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

// notify is the Go side of the sandbox's notify(message) global - see the
// package doc comment's own entry for it. Shared, Host-wide rate limiting;
// a message dropped for being over the limit is simply not sent, without
// telling the script (there is nothing useful for it to do about that
// besides retry, which would only make the burst worse).
func (h *Host) notify(message string) bool {
	if !h.notifyLim.Allow() {
		return false
	}
	h.hub.Broadcast("script", Event{Kind: "notify", Message: message})
	return true
}
