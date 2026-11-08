package onefichier

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
	// Handover starts the transfer of url. relink reads the file's page again
	// for a fresh url, for what is left when this one stops working part way.
	Handover(taskID, url string, conns int, relink func(context.Context) (string, error))
	Pause(taskID string)
	Resume(taskID string)
	Remove(taskID string, deleteFiles bool)
}

// maxBusy is how many rounds of busyWait a download sits out while 1fichier
// holds back free downloads, before it fails and the app's retries take over.
const maxBusy = 5

// Backend reads a file's page, waits out whatever 1fichier asks for, sends the
// download form and hands the link to the engine. A wait keeps the task
// running with the time left in its note, however long it is.
type Backend struct {
	client   *Client
	eng      Downloader
	onUpdate func(taskID string, u core.Update)

	// Password returns the download password a task carries, "" for none.
	Password func(taskID string) string

	sleep func(ctx context.Context, d time.Duration) error

	mu     sync.Mutex
	cancel map[string]context.CancelFunc
	link   map[string]string
	handed map[string]bool
	// waiting holds the run that is sitting out a wait, so a paused run that
	// winds down late cannot clear the wait of the run a Resume started.
	waiting map[string]context.Context
}

func NewBackend(c *Client, eng Downloader, onUpdate func(taskID string, u core.Update)) *Backend {
	return &Backend{
		client: c, eng: eng, onUpdate: onUpdate, sleep: sleep,
		cancel:  map[string]context.CancelFunc{},
		link:    map[string]string{},
		handed:  map[string]bool{},
		waiting: map[string]context.Context{},
	}
}

// Client is the page client the backend and its resolver share.
func (b *Backend) Client() *Client { return b.client }

// Waiting reports whether the task is sitting out a wait 1fichier asked for.
// It moves no bytes meanwhile, and that is no stall.
func (b *Backend) Waiting(taskID string) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	run := b.waiting[taskID]
	return run != nil && run.Err() == nil
}

func (b *Backend) Download(taskID, link string, _ map[string]string, _ int) {
	b.mu.Lock()
	b.link[taskID] = link
	b.handed[taskID] = false
	b.mu.Unlock()
	b.start(taskID, link)
}

// start begins a run. Its cancel stays in b.cancel until the next start or a
// Remove replaces it, so a run that ends cannot take out the entry of the run
// a Resume has just started.
func (b *Backend) start(taskID, link string) {
	ctx, cancel := context.WithCancel(context.Background())
	b.mu.Lock()
	b.cancel[taskID] = cancel
	b.mu.Unlock()
	go b.run(ctx, taskID, link)
}

func (b *Backend) run(ctx context.Context, taskID, link string) {
	busy := 0
	for {
		direct, err := b.unlock(ctx, taskID, link, func(w *WaitError) error { return b.wait(ctx, taskID, w) })
		var w *WaitError
		switch {
		case ctx.Err() != nil:
			// Pause and Remove set the task's state themselves.
			return
		case err == nil:
			b.handOn(ctx, taskID, link, direct)
			return
		case errors.As(err, &w) && (!w.Busy || busy < maxBusy):
			if w.Busy {
				busy++
			}
			if b.wait(ctx, taskID, w) != nil {
				return
			}
		default:
			b.fail(taskID, err)
			return
		}
	}
}

// unlock reads the file's page and sends its form for the download link.
// countdown waits out the page's countdown before the form goes in, since a
// form sent early is turned away.
func (b *Backend) unlock(ctx context.Context, taskID, link string, countdown func(*WaitError) error) (string, error) {
	p, err := b.client.Open(ctx, link)
	if p != nil && p.Name != "" {
		b.onUpdate(taskID, core.Update{Status: core.StatusRunning, Name: p.Name, Size: p.Size})
	}
	if err != nil {
		return "", err
	}
	if p.Countdown > 0 {
		if err := countdown(&WaitError{Wait: p.Countdown}); err != nil {
			return "", err
		}
	}
	return p.Fetch(ctx, b.password(taskID))
}

// handOn gives the link to the engine for one connection, all a free
// download gets.
func (b *Backend) handOn(ctx context.Context, taskID, link, direct string) {
	// Pause and Remove cancel under b.mu, so one that came after the form was
	// answered is seen here and the link is not handed on.
	b.mu.Lock()
	if ctx.Err() != nil {
		b.mu.Unlock()
		return
	}
	b.handed[taskID] = true
	b.mu.Unlock()
	b.eng.Handover(taskID, direct, 1, func(ctx context.Context) (string, error) {
		return b.unlock(ctx, taskID, link, func(w *WaitError) error { return b.sleep(ctx, w.Wait) })
	})
}

// wait sits out w, keeping the time left in the task's note.
func (b *Backend) wait(ctx context.Context, taskID string, w *WaitError) error {
	b.setWaiting(ctx, taskID, true)
	defer b.setWaiting(ctx, taskID, false)
	for left := w.Wait; left > 0; {
		b.onUpdate(taskID, core.Update{Status: core.StatusRunning, Note: waitNote(w, left)})
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

func waitNote(w *WaitError, left time.Duration) string {
	if w.Busy {
		return "1fichier is limiting free downloads, next try in " + span(left)
	}
	return "Waiting for 1fichier: " + span(left) + " left"
}

// span writes a wait as people read it, rounded up: "1 h 5 min", "37 min",
// "45 s".
func span(d time.Duration) string {
	switch {
	case d >= time.Hour:
		m := int((d + time.Minute - 1) / time.Minute)
		if m%60 == 0 {
			return fmt.Sprintf("%d h", m/60)
		}
		return fmt.Sprintf("%d h %d min", m/60, m%60)
	case d >= 2*time.Minute:
		return fmt.Sprintf("%d min", (d+time.Minute-1)/time.Minute)
	}
	return fmt.Sprintf("%d s", (d+time.Second-1)/time.Second)
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

// fail reports err with the cause the backend knows.
func (b *Backend) fail(taskID string, err error) {
	u := core.Update{Status: core.StatusError, Err: err.Error()}
	var w *WaitError
	switch {
	case errors.Is(err, ErrGone):
		u.Reason = core.ReasonGone
	case errors.Is(err, ErrPremium), errors.Is(err, ErrPassword), errors.Is(err, ErrPasswordRefused):
		u.Reason = core.ReasonAuth
	case errors.As(err, &w):
		u.Reason = core.ReasonUnavailable
	}
	b.onUpdate(taskID, u)
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

// Resume reads the page again, as the wait it was paused in may be over.
func (b *Backend) Resume(taskID string) {
	b.mu.Lock()
	handed, link := b.handed[taskID], b.link[taskID]
	b.mu.Unlock()
	if handed {
		b.eng.Resume(taskID)
		return
	}
	if link != "" {
		b.start(taskID, link)
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
