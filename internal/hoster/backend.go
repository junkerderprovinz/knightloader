package hoster

import (
	"context"
	"errors"
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
	*Runs

	// Name is the hoster as the task's note names it.
	Name string

	unlocker Unlocker
	eng      Downloader
	onUpdate func(taskID string, u core.Update)

	// Password returns the download password a task carries, "" for none.
	Password func(taskID string) string

	sleep func(ctx context.Context, d time.Duration) error
}

func NewBackend(name string, u Unlocker, eng Downloader, onUpdate func(taskID string, u core.Update)) *Backend {
	b := &Backend{Name: name, unlocker: u, eng: eng, onUpdate: onUpdate, sleep: Sleep}
	b.Runs = NewRuns(eng, onUpdate, b.run, func(ctx context.Context, d time.Duration) error { return b.sleep(ctx, d) })
	return b
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
	if !b.Handing(ctx, taskID) {
		return
	}
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
	return b.Wait(ctx, taskID, w.Wait, func(left time.Duration) string { return b.note(w, left) })
}

func (b *Backend) note(w *WaitError, left time.Duration) string {
	if w.Why != "" {
		return w.Why + ", next try in " + Span(left)
	}
	return "Waiting for " + b.Name + ": " + Span(left) + " left"
}

func (b *Backend) password(taskID string) string {
	if b.Password == nil {
		return ""
	}
	return b.Password(taskID)
}
