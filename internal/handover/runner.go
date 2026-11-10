package handover

import (
	"context"
	"log"
	"sort"
	"sync"
	"time"
)

// The waits of a hand-over. package.done fires while a finished file may still
// be on its way out of the working folder, and a job in move mode started then
// would miss it, so the call waits until the app says the files landed, but
// never longer than Grace: a move that failed for good would otherwise hold it
// forever. An instance that is away is asked again every Retry within the same
// Grace.
const (
	defaultTick  = time.Second
	defaultRetry = time.Minute
	DefaultGrace = 15 * time.Minute
)

// queueDepth bounds the hand-over from the event bus, which must not block.
const queueDepth = 256

// Options configures a Runner.
type Options struct {
	// Fortress is the fortress this instance is in, or nil.
	Fortress func() Fortress
	// Ready reports whether a package's files have reached their folders. Nil
	// is always ready.
	Ready func(pkg string) bool
	// Zero takes the defaults above.
	Tick, Retry, Grace time.Duration
}

// Result is the last hand-over to one target.
type Result struct {
	At      time.Time `json:"at"`
	Package string    `json:"package"`
	Job     string    `json:"job"`
	// Status is StatusStarted or StatusQueued when it went through.
	Status string `json:"status,omitempty"`
	// Error is a code of Code when it did not.
	Error string `json:"error,omitempty"`
}

// Runner starts the jobs finished packages ask for, one call per job for all
// the packages waiting on it.
type Runner struct {
	fortress func() Fortress
	ready    func(pkg string) bool
	tick     time.Duration
	retry    time.Duration
	grace    time.Duration

	queue  chan armed
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
	once   sync.Once

	mu   sync.Mutex
	last map[Target]Result
}

type armed struct {
	target Target
	pkg    string
}

// pending is one job owed a run.
type pending struct {
	packages map[string]bool
	giveUpAt time.Time
	// tryAt holds the call back after an instance that was away.
	tryAt time.Time
}

// New builds a Runner. Nothing goes out before Start.
func New(o Options) *Runner {
	r := &Runner{
		fortress: o.Fortress,
		ready:    o.Ready,
		tick:     orDefault(o.Tick, defaultTick),
		retry:    orDefault(o.Retry, defaultRetry),
		grace:    orDefault(o.Grace, DefaultGrace),
		queue:    make(chan armed, queueDepth),
		last:     map[Target]Result{},
	}
	if r.fortress == nil {
		r.fortress = func() Fortress { return nil }
	}
	if r.ready == nil {
		r.ready = func(string) bool { return true }
	}
	r.ctx, r.cancel = context.WithCancel(context.Background())
	return r
}

func orDefault(d, fallback time.Duration) time.Duration {
	if d > 0 {
		return d
	}
	return fallback
}

// Start begins the loop.
func (r *Runner) Start() {
	r.once.Do(func() {
		r.wg.Add(1)
		go r.loop()
	})
}

// Close stops the loop and waits for a call in flight. Jobs still waiting for
// their files are dropped, since starting them now would beat the move.
func (r *Runner) Close() error {
	r.cancel()
	r.wg.Wait()
	return nil
}

// Enqueue asks for t to run once pkg's files have landed. It never blocks, as
// the event bus requires.
func (r *Runner) Enqueue(t Target, pkg string) {
	select {
	case r.queue <- armed{t, pkg}:
	default:
		log.Printf("handover: the queue is full, %s does not start job %s", pkg, t.Job)
	}
}

// Last is the last hand-over to t in this process.
func (r *Runner) Last(t Target) (Result, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	res, ok := r.last[t]
	return res, ok
}

func (r *Runner) loop() {
	defer r.wg.Done()
	ticker := time.NewTicker(r.tick)
	defer ticker.Stop()
	owed := map[Target]*pending{}
	for {
		select {
		case <-r.ctx.Done():
			return
		case a := <-r.queue:
			p := owed[a.target]
			if p == nil {
				p = &pending{packages: map[string]bool{}, giveUpAt: time.Now().Add(r.grace)}
				owed[a.target] = p
			}
			p.packages[a.pkg] = true
		case now := <-ticker.C:
			for t, p := range owed {
				if now.Before(p.tryAt) || !r.landed(p) && now.Before(p.giveUpAt) {
					continue
				}
				if r.call(t, p) && now.Before(p.giveUpAt) {
					p.tryAt = now.Add(r.retry)
					continue
				}
				delete(owed, t)
			}
		}
	}
}

func (r *Runner) landed(p *pending) bool {
	for pkg := range p.packages {
		if !r.ready(pkg) {
			return false
		}
	}
	return true
}

// call starts t for the packages in p and reports whether to try again.
func (r *Runner) call(t Target, p *pending) (again bool) {
	names := make([]string, 0, len(p.packages))
	for pkg := range p.packages {
		names = append(names, pkg)
	}
	sort.Strings(names)
	status, err := Run(r.ctx, r.fortress(), t)
	res := Result{At: time.Now(), Package: names[0], Job: t.Job, Status: status, Error: Code(err)}
	r.mu.Lock()
	r.last[t] = res
	r.mu.Unlock()
	if err != nil {
		log.Printf("handover: %s could not start job %s: %v", names[0], t.Job, err)
		return res.Error == CodeUnreachable
	}
	log.Printf("handover: %s started job %s (%s)", names[0], t.Job, status)
	return false
}
