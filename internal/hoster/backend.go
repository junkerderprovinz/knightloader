package hoster

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/core"
)

// Downloader is the byte-transfer backend the download link is handed to.
type Downloader interface {
	// HandoverHeaders starts the transfer of url with headers on every
	// request. relink asks the hoster again for a fresh url, for what is left
	// when this one stops working part way.
	HandoverHeaders(taskID, url string, headers map[string]string, conns int, relink func(context.Context) (string, error))
	Pause(taskID string)
	Resume(taskID string)
	Remove(taskID string, deleteFiles bool)
}

// maxWaits is how many pauses one run sits out before it fails and the
// app's retries take over, so a hoster that keeps asking does not hold a
// download slot for good.
const maxWaits = 6

// Backend asks a hoster's Unlocker for the download link, waits out whatever
// the hoster asks for and hands the link to the engine. A wait keeps the task
// running with the time left in its note.
type Backend struct {
	// Name is the hoster as the task's note names it.
	Name string

	unlocker Unlocker
	eng      Downloader
	onUpdate func(taskID string, u core.Update)

	// Password returns the download password a task carries, "" for none.
	Password func(taskID string) string

	sleep func(ctx context.Context, d time.Duration) error

	mu     sync.Mutex
	cancel map[string]context.CancelFunc
	link   map[string]string
	conns  map[string]int
	handed map[string]bool
	// waiting holds the run that is sitting out a wait, so a paused run that
	// winds down late cannot clear the wait of the run a Resume started.
	waiting map[string]context.Context
}

func NewBackend(name string, u Unlocker, eng Downloader, onUpdate func(taskID string, u core.Update)) *Backend {
	return &Backend{
		Name: name, unlocker: u, eng: eng, onUpdate: onUpdate, sleep: sleep,
		cancel:  map[string]context.CancelFunc{},
		link:    map[string]string{},
		conns:   map[string]int{},
		handed:  map[string]bool{},
		waiting: map[string]context.Context{},
	}
}

// Waiting reports whether the task is sitting out a wait the hoster asked
// for. It moves no bytes meanwhile, and that is no stall.
func (b *Backend) Waiting(taskID string) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	run := b.waiting[taskID]
	return run != nil && run.Err() == nil
}

func (b *Backend) Download(taskID, link string, _ map[string]string, conns int) {
	b.mu.Lock()
	b.link[taskID] = link
	b.conns[taskID] = conns
	b.handed[taskID] = false
	b.mu.Unlock()
	b.start(taskID, link, conns)
}

// start begins a run. Its cancel stays in b.cancel until the next start or a
// Remove replaces it, so a run that ends cannot take out the entry of the run
// a Resume has just started.
func (b *Backend) start(taskID, link string, conns int) {
	ctx, cancel := context.WithCancel(context.Background())
	b.mu.Lock()
	b.cancel[taskID] = cancel
	b.mu.Unlock()
	go b.run(ctx, taskID, link, conns)
}

func (b *Backend) run(ctx context.Context, taskID, link string, conns int) {
	for waits := 0; ; waits++ {
		l, err := b.unlocker.Unlock(ctx, link, b.password(taskID))
		var w *WaitError
		switch {
		case ctx.Err() != nil:
			// Pause and Remove set the task's state themselves.
			return
		case err == nil:
			b.handOn(ctx, taskID, link, l, conns)
			return
		case errors.As(err, &w) && waits < maxWaits:
			if b.wait(ctx, taskID, w) != nil {
				return
			}
		default:
			b.onUpdate(taskID, core.Update{Status: core.StatusError, Err: err.Error(), Reason: reasonOf(err)})
			return
		}
	}
}

// handOn gives the link to the engine, with no more connections than the
// task asked for or the hoster allows.
func (b *Backend) handOn(ctx context.Context, taskID, link string, l Link, conns int) {
	// Pause and Remove cancel under b.mu, so one that came after the hoster
	// answered is seen here and the link is not handed on.
	b.mu.Lock()
	if ctx.Err() != nil {
		b.mu.Unlock()
		return
	}
	b.handed[taskID] = true
	b.mu.Unlock()
	if l.Name != "" || l.Size > 0 {
		b.onUpdate(taskID, core.Update{Status: core.StatusRunning, Name: l.Name, Size: l.Size})
	}
	if l.Conns > 0 && (conns <= 0 || l.Conns < conns) {
		conns = l.Conns
	}
	b.eng.HandoverHeaders(taskID, l.URL, l.Headers, conns, func(ctx context.Context) (string, error) {
		return b.relink(ctx, taskID, link)
	})
}

// relink asks for a fresh link for the rest of a transfer, sitting out a
// wait quietly, since the task shows its transfer meanwhile.
func (b *Backend) relink(ctx context.Context, taskID, link string) (string, error) {
	for waits := 0; ; waits++ {
		l, err := b.unlocker.Unlock(ctx, link, b.password(taskID))
		var w *WaitError
		if err == nil || !errors.As(err, &w) || waits >= maxWaits {
			return l.URL, err
		}
		if err := b.sleep(ctx, w.Wait); err != nil {
			return "", err
		}
	}
}

// wait sits out w, keeping the time left in the task's note.
func (b *Backend) wait(ctx context.Context, taskID string, w *WaitError) error {
	b.setWaiting(ctx, taskID, true)
	defer b.setWaiting(ctx, taskID, false)
	for left := w.Wait; left > 0; {
		b.onUpdate(taskID, core.Update{Status: core.StatusRunning, Note: b.note(w, left)})
		// Every note reaches every open browser, so a long wait counts down
		// in coarser steps.
		step := time.Second
		if left > 2*time.Minute {
			step = 30 * time.Second
		}
		step = min(step, left)
		if err := b.sleep(ctx, step); err != nil {
			return err
		}
		left -= step
	}
	return nil
}

func (b *Backend) note(w *WaitError, left time.Duration) string {
	if w.Why != "" {
		return w.Why + ", next try in " + Span(left)
	}
	return "Waiting for " + b.Name + ": " + Span(left) + " left"
}

func (b *Backend) setWaiting(run context.Context, taskID string, on bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if on {
		b.waiting[taskID] = run
	} else if b.waiting[taskID] == run {
		delete(b.waiting, taskID)
	}
}

func (b *Backend) password(taskID string) string {
	if b.Password == nil {
		return ""
	}
	return b.Password(taskID)
}

func (b *Backend) Pause(taskID string) {
	b.mu.Lock()
	handed := b.handed[taskID]
	if cancel := b.cancel[taskID]; cancel != nil && !handed {
		cancel()
	}
	b.mu.Unlock()
	if handed {
		b.eng.Pause(taskID)
		return
	}
	b.onUpdate(taskID, core.Update{Status: core.StatusPaused})
}

// Resume asks the hoster again, as the wait it was paused in may be over.
func (b *Backend) Resume(taskID string) {
	b.mu.Lock()
	handed, link, conns := b.handed[taskID], b.link[taskID], b.conns[taskID]
	b.mu.Unlock()
	if handed {
		b.eng.Resume(taskID)
		return
	}
	if link != "" {
		b.start(taskID, link, conns)
	}
}

func (b *Backend) Remove(taskID string, deleteFiles bool) {
	b.mu.Lock()
	if cancel := b.cancel[taskID]; cancel != nil {
		cancel()
	}
	handed := b.handed[taskID]
	delete(b.cancel, taskID)
	delete(b.link, taskID)
	delete(b.conns, taskID)
	delete(b.handed, taskID)
	b.mu.Unlock()
	if handed {
		b.eng.Remove(taskID, deleteFiles)
	}
}

func sleep(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}
