package ownlogin

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/hoster"
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
	*hoster.Runs

	// Sleep waits d, or less when ctx ends first.
	Sleep func(ctx context.Context, d time.Duration) error

	eng      Downloader
	onUpdate func(taskID string, u core.Update)
	cfg      Config
}

func NewTasks(eng Downloader, onUpdate func(taskID string, u core.Update), cfg Config) *Tasks {
	t := &Tasks{Sleep: hoster.Sleep, eng: eng, onUpdate: onUpdate, cfg: cfg}
	t.Runs = hoster.NewRuns(eng, onUpdate, t.run, func(ctx context.Context, d time.Duration) error { return t.Sleep(ctx, d) })
	return t
}

func (t *Tasks) run(ctx context.Context, taskID, link string, conns int) {
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
			t.handOn(ctx, taskID, link, direct, size, conns)
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
func (t *Tasks) handOn(ctx context.Context, taskID, link, direct string, size int64, conns int) {
	if !t.Handing(ctx, taskID) {
		return
	}
	if size <= t.cfg.SmallFile || conns < 1 {
		conns = 1
	}
	t.eng.Handover(taskID, direct, conns, func(ctx context.Context) (string, error) {
		direct, _, err := t.cfg.Unlock(ctx, taskID, link, func(d time.Duration) error { return t.Sleep(ctx, d) })
		return direct, err
	})
}

// wait sits out d, keeping the time left in the task's note, which is note
// with %s for the time left.
func (t *Tasks) wait(ctx context.Context, taskID string, d time.Duration, note string) error {
	return t.Wait(ctx, taskID, d, func(left time.Duration) string { return fmt.Sprintf(note, hoster.Span(left)) })
}
