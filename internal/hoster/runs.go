package hoster

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/httpx"
)

// Transfers is the engine as Runs sees it once a task's link is handed on.
type Transfers interface {
	Pause(taskID string)
	Resume(taskID string)
	Remove(taskID string, deleteFiles bool)
}

// Runs keeps the runs of a backend that gets a download link from a hoster
// and hands it to the engine. Until the link is handed on, Pause ends the run
// and Resume starts a new one; afterwards both go to the engine. A wait the
// hoster asks for keeps the task running with the time left in its note.
type Runs struct {
	eng      Transfers
	onUpdate func(taskID string, u core.Update)
	run      func(ctx context.Context, taskID, link string, conns int)
	sleep    func(ctx context.Context, d time.Duration) error

	mu     sync.Mutex
	cancel map[string]context.CancelFunc
	link   map[string]string
	conns  map[string]int
	handed map[string]bool
	// waiting holds the run that is sitting out a wait, so a paused run that
	// winds down late cannot clear the wait of the run a Resume started.
	waiting map[string]context.Context
}

// NewRuns returns the runs of a backend whose run goes once at a task's link,
// until it hands the link on or fails, and returns early once ctx ends. sleep
// sits out a wait.
func NewRuns(eng Transfers, onUpdate func(taskID string, u core.Update),
	run func(ctx context.Context, taskID, link string, conns int),
	sleep func(ctx context.Context, d time.Duration) error,
) *Runs {
	return &Runs{
		eng: eng, onUpdate: onUpdate, run: run, sleep: sleep,
		cancel:  map[string]context.CancelFunc{},
		link:    map[string]string{},
		conns:   map[string]int{},
		handed:  map[string]bool{},
		waiting: map[string]context.Context{},
	}
}

func (r *Runs) Download(taskID, link string, _ map[string]string, conns int) {
	r.mu.Lock()
	r.link[taskID] = link
	r.conns[taskID] = conns
	r.handed[taskID] = false
	r.mu.Unlock()
	r.start(taskID, link, conns)
}

// start begins a run. Its cancel stays in r.cancel until the next start or a
// Remove replaces it, so a run that ends cannot take out the entry of the run
// a Resume has just started.
func (r *Runs) start(taskID, link string, conns int) {
	ctx, cancel := context.WithCancel(context.Background())
	r.mu.Lock()
	r.cancel[taskID] = cancel
	r.mu.Unlock()
	go r.run(ctx, taskID, link, conns)
}

// Handing marks the task's link as handed to the engine and reports true,
// unless the run of ctx was paused or removed after the hoster answered.
// Pause and Remove cancel under r.mu, so this sees them.
func (r *Runs) Handing(ctx context.Context, taskID string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if ctx.Err() != nil {
		return false
	}
	r.handed[taskID] = true
	return true
}

// Waiting reports whether the task is sitting out a wait the hoster asked
// for. It moves no bytes meanwhile, and that is no stall.
func (r *Runs) Waiting(taskID string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	run := r.waiting[taskID]
	return run != nil && run.Err() == nil
}

// Wait sits out d for the run of ctx, keeping note(left) in the task's note.
func (r *Runs) Wait(ctx context.Context, taskID string, d time.Duration, note func(left time.Duration) string) error {
	r.setWaiting(ctx, taskID, true)
	defer r.setWaiting(ctx, taskID, false)
	for left := d; left > 0; {
		r.onUpdate(taskID, core.Update{Status: core.StatusRunning, Note: note(left)})
		// Every note reaches every open browser, so a long wait counts down
		// in coarser steps.
		step := time.Second
		if left > 2*time.Minute {
			step = 30 * time.Second
		}
		step = min(step, left)
		if err := r.sleep(ctx, step); err != nil {
			return err
		}
		left -= step
	}
	return nil
}

func (r *Runs) setWaiting(run context.Context, taskID string, on bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if on {
		r.waiting[taskID] = run
	} else if r.waiting[taskID] == run {
		delete(r.waiting, taskID)
	}
}

func (r *Runs) Pause(taskID string) {
	r.mu.Lock()
	handed := r.handed[taskID]
	if cancel := r.cancel[taskID]; cancel != nil && !handed {
		cancel()
	}
	r.mu.Unlock()
	if handed {
		r.eng.Pause(taskID)
		return
	}
	r.onUpdate(taskID, core.Update{Status: core.StatusPaused})
}

// Resume asks the hoster again, as the wait the task was paused in may be
// over.
func (r *Runs) Resume(taskID string) {
	r.mu.Lock()
	handed, link, conns := r.handed[taskID], r.link[taskID], r.conns[taskID]
	r.mu.Unlock()
	if handed {
		r.eng.Resume(taskID)
		return
	}
	if link != "" {
		r.start(taskID, link, conns)
	}
}

func (r *Runs) Remove(taskID string, deleteFiles bool) {
	r.mu.Lock()
	if cancel := r.cancel[taskID]; cancel != nil {
		cancel()
	}
	handed := r.handed[taskID]
	delete(r.cancel, taskID)
	delete(r.link, taskID)
	delete(r.conns, taskID)
	delete(r.handed, taskID)
	r.mu.Unlock()
	if handed {
		r.eng.Remove(taskID, deleteFiles)
	}
}

// PassOnBrowserCheck hands the task to the next backend when err is the
// browser check of name's site, which no client of KnightLoader's can pass,
// and reports whether it did.
func (r *Runs) PassOnBrowserCheck(taskID, name string, err error) bool {
	if !errors.Is(err, httpx.ErrBrowserCheck) {
		return false
	}
	r.onUpdate(taskID, core.Update{Status: core.StatusError, Err: name + " asks for a browser check KnightLoader cannot pass",
		Unsupported: true, BrowserCheck: name})
	return true
}

// Sleep waits d, or less when ctx ends first.
func Sleep(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}
