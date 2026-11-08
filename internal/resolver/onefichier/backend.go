package onefichier

import (
	"context"
	"errors"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/hoster"
)

// Downloader is the byte-transfer backend the download link is handed to.
type Downloader interface {
	// HandoverOnce starts the transfer of url, which answers a single
	// request, over one connection. relink reads the file's page again for a
	// fresh url, for what is left when this one stops working part way.
	HandoverOnce(taskID, url string, relink func(context.Context) (string, error))
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
	*hoster.Runs

	client   *Client
	eng      Downloader
	onUpdate func(taskID string, u core.Update)

	// Password returns the download password a task carries, "" for none.
	Password func(taskID string) string

	sleep func(ctx context.Context, d time.Duration) error
}

func NewBackend(c *Client, eng Downloader, onUpdate func(taskID string, u core.Update)) *Backend {
	b := &Backend{client: c, eng: eng, onUpdate: onUpdate, sleep: hoster.Sleep}
	b.Runs = hoster.NewRuns(eng, onUpdate, b.run, func(ctx context.Context, d time.Duration) error { return b.sleep(ctx, d) })
	return b
}

// Client is the page client the backend and its resolver share.
func (b *Backend) Client() *Client { return b.client }

// run is one go at a free download. 1fichier gives a free download one
// connection, whatever the task asks for.
func (b *Backend) run(ctx context.Context, taskID, link string, _ int) {
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

// handOn gives the link to the engine. A free download gets one connection,
// and its link answers one request: a second, even for the rest of the file,
// is answered 410.
func (b *Backend) handOn(ctx context.Context, taskID, link, direct string) {
	if !b.Handing(ctx, taskID) {
		return
	}
	b.eng.HandoverOnce(taskID, direct, func(ctx context.Context) (string, error) {
		return b.unlock(ctx, taskID, link, func(w *WaitError) error { return b.sleep(ctx, w.Wait) })
	})
}

// wait sits out w, keeping the time left in the task's note.
func (b *Backend) wait(ctx context.Context, taskID string, w *WaitError) error {
	return b.Wait(ctx, taskID, w.Wait, func(left time.Duration) string { return waitNote(w, left) })
}

func waitNote(w *WaitError, left time.Duration) string {
	if w.Busy {
		return "1fichier is limiting free downloads, next try in " + hoster.Span(left)
	}
	return "Waiting for 1fichier: " + hoster.Span(left) + " left"
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
