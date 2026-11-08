package ownlogin

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/core"
)

// Downloader is the byte-transfer backend the download link is handed to.
type Downloader interface {
	// Handover starts the transfer of url. relink asks the hoster again for a
	// fresh url, for what is left when this one stops working part way.
	Handover(taskID, url string, conns int, relink func(context.Context) (string, error))
	Pause(taskID string)
	Resume(taskID string)
	Remove(taskID string, deleteFiles bool)
}

// Unlock asks the hoster for the download link of the task's link and
// returns it with the file's size. delay sits out a wait the link comes with
// before it may be used.
type Unlock func(ctx context.Context, taskID, link string, delay func(time.Duration) error) (string, int64, error)

// Config is what a hoster brings to the downloads on its login.
type Config struct {
	Unlock Unlock
	// Reason is the cause a failed unlock is reported with.
	Reason func(error) core.Reason
	// WaitNote is the task's note during a wait the link comes with, %s
	// standing for the time left.
	WaitNote string
	// Busy is the hoster asking for a pause between sign-ins. The task waits
	// BusyWait under BusyNote and asks again, up to MaxBusy times, before it
	// fails and the app's retries take over. Nil fails at once.
	Busy     error
	BusyWait time.Duration
	MaxBusy  int
	BusyNote string
	// SmallFile is the size up to which a file gets one connection.
	SmallFile int64
}

// Tasks runs the downloads that go out on a hoster login: it asks the hoster
// for each download link and hands it to the engine. A wait the hoster asks
// for keeps the task running with the time left in its note.
type Tasks struct {
	// Sleep waits d, or less when ctx ends first.
	Sleep func(ctx context.Context, d time.Duration) error

	eng      Downloader
	onUpdate func(taskID string, u core.Update)
	cfg      Config

	mu      sync.Mutex
	cancel  map[string]context.CancelFunc
	link    map[string]string
	conns   map[string]int
	handed  map[string]bool
	waiting map[string]bool
}

func NewTasks(eng Downloader, onUpdate func(taskID string, u core.Update), cfg Config) *Tasks {
	return &Tasks{
		Sleep: Sleep, eng: eng, onUpdate: onUpdate, cfg: cfg,
		cancel:  map[string]context.CancelFunc{},
		link:    map[string]string{},
		conns:   map[string]int{},
		handed:  map[string]bool{},
		waiting: map[string]bool{},
	}
}

// Waiting reports whether the task is sitting out a wait the hoster asked
// for. It moves no bytes meanwhile, and that is no stall.
func (t *Tasks) Waiting(taskID string) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.waiting[taskID]
}

func (t *Tasks) Download(taskID, link string, _ map[string]string, conns int) {
	t.mu.Lock()
	t.link[taskID] = link
	t.conns[taskID] = conns
	t.handed[taskID] = false
	t.mu.Unlock()
	t.start(taskID, link)
}

// start begins a run. Its cancel stays in t.cancel until the next start or a
// Remove replaces it, so a run that ends cannot take out the entry of the run
// a Resume has just started.
func (t *Tasks) start(taskID, link string) {
	ctx, cancel := context.WithCancel(context.Background())
	t.mu.Lock()
	t.cancel[taskID] = cancel
	t.mu.Unlock()
	go t.run(ctx, taskID, link)
}

func (t *Tasks) run(ctx context.Context, taskID, link string) {
	busy := 0
	for {
		direct, size, err := t.cfg.Unlock(ctx, taskID, link, func(d time.Duration) error {
			return t.wait(ctx, taskID, d, t.cfg.WaitNote)
		})
		switch {
		case ctx.Err() != nil:
			// Pause and Remove set the task's state themselves.
			return
		case err == nil:
			t.handOn(ctx, taskID, link, direct, size)
			return
		case t.cfg.Busy != nil && errors.Is(err, t.cfg.Busy) && busy < t.cfg.MaxBusy:
			busy++
			if t.wait(ctx, taskID, t.cfg.BusyWait, t.cfg.BusyNote) != nil {
				return
			}
		default:
			t.onUpdate(taskID, core.Update{Status: core.StatusError, Err: err.Error(), Reason: t.cfg.Reason(err)})
			return
		}
	}
}

// handOn gives the link to the engine, on one connection for a small file.
func (t *Tasks) handOn(ctx context.Context, taskID, link, direct string, size int64) {
	// Pause and Remove cancel under t.mu, so one that came after the hoster
	// answered is seen here and the link is not handed on.
	t.mu.Lock()
	if ctx.Err() != nil {
		t.mu.Unlock()
		return
	}
	t.handed[taskID] = true
	conns := t.conns[taskID]
	t.mu.Unlock()
	if size <= t.cfg.SmallFile || conns < 1 {
		conns = 1
	}
	t.eng.Handover(taskID, direct, conns, func(ctx context.Context) (string, error) {
		direct, _, err := t.cfg.Unlock(ctx, taskID, link, func(d time.Duration) error { return t.Sleep(ctx, d) })
		return direct, err
	})
}

// wait sits out d, keeping the time left in the task's note.
func (t *Tasks) wait(ctx context.Context, taskID string, d time.Duration, note string) error {
	t.setWaiting(taskID, true)
	defer t.setWaiting(taskID, false)
	for left := d; left > 0; {
		t.onUpdate(taskID, core.Update{Status: core.StatusRunning, Note: fmt.Sprintf(note, span(left))})
		// Every note reaches every open browser, so a long wait counts down
		// in coarser steps.
		step := time.Second
		if left > 2*time.Minute {
			step = 30 * time.Second
		}
		step = min(step, left)
		if err := t.Sleep(ctx, step); err != nil {
			return err
		}
		left -= step
	}
	return nil
}

// span writes a wait as people read it, rounded up: "37 min", "45 s".
func span(d time.Duration) string {
	if d >= 2*time.Minute {
		return fmt.Sprintf("%d min", (d+time.Minute-1)/time.Minute)
	}
	return fmt.Sprintf("%d s", (d+time.Second-1)/time.Second)
}

func (t *Tasks) setWaiting(taskID string, on bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if on {
		t.waiting[taskID] = true
	} else {
		delete(t.waiting, taskID)
	}
}

func (t *Tasks) Pause(taskID string) {
	t.mu.Lock()
	handed := t.handed[taskID]
	if cancel := t.cancel[taskID]; cancel != nil && !handed {
		cancel()
	}
	t.mu.Unlock()
	if handed {
		t.eng.Pause(taskID)
		return
	}
	t.onUpdate(taskID, core.Update{Status: core.StatusPaused})
}

// Resume asks the hoster again, as the wait it was paused in may be over.
func (t *Tasks) Resume(taskID string) {
	t.mu.Lock()
	handed, link := t.handed[taskID], t.link[taskID]
	t.mu.Unlock()
	if handed {
		t.eng.Resume(taskID)
		return
	}
	if link != "" {
		t.start(taskID, link)
	}
}

func (t *Tasks) Remove(taskID string, deleteFiles bool) {
	t.mu.Lock()
	if cancel := t.cancel[taskID]; cancel != nil {
		cancel()
	}
	handed := t.handed[taskID]
	delete(t.cancel, taskID)
	delete(t.link, taskID)
	delete(t.conns, taskID)
	delete(t.handed, taskID)
	t.mu.Unlock()
	if handed {
		t.eng.Remove(taskID, deleteFiles)
	}
}

// Sleep waits d, or less when ctx ends first.
func Sleep(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
