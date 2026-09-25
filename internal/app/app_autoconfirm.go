package app

// Auto-confirm: with AutoConfirm on, a newly staged batch leaves the collector
// by itself, at once or after AutoConfirmDelay seconds. Each batch counts down
// on its own timer, so one that arrives late in another's countdown still gets
// the whole delay to be looked at. The due time is kept on the rows, so a
// countdown a shutdown cuts short runs on after the restart.

import (
	"context"
	"sync"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/confirm"
	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/settings"
)

// autoConfirmSecond is one second of AutoConfirmDelay. Tests shorten it so a
// countdown runs out in milliseconds.
var autoConfirmSecond = time.Second

// autoConfirmWakes holds each App's wake channel. wakeAutoConfirm closes it
// and the next countdown to wait makes a fresh one.
var (
	autoConfirmMu    sync.Mutex
	autoConfirmWakes = map[*App]chan struct{}{}
)

// autoConfirm sets a newly staged batch on its way out of the collector when
// AutoConfirm is on: through ConfirmTasks at once when no delay is set, or
// when its countdown runs out.
func (a *App) autoConfirm(ids []string) {
	s := a.Settings.Get()
	if !s.AutoConfirm || len(ids) == 0 {
		return
	}
	if s.AutoConfirmDelay <= 0 {
		a.ConfirmTasks(ids, confirm.Config{}, confirm.TriggerAutoConfirm)
		return
	}
	a.startAutoConfirmCountdown(ids, time.Now(), s)
}

// startAutoConfirmCountdown holds a batch in the collector until the delay
// counted from begun has passed, then confirms whatever of it is still there.
// The countdown shows on the status strip, whose stop button calls it off and
// leaves the links where they are.
//
// Only the links a confirm would move right now are counted in, so a link the
// filter held stays out even when somebody restores it during the countdown.
func (a *App) startAutoConfirmCountdown(ids []string, begun time.Time, s settings.Settings) {
	a.mu.Lock()
	ids = idsOf(a.confirmableLocked(ids))
	a.mu.Unlock()
	if len(ids) == 0 {
		return
	}
	// Before track, so a batch staged while the app shuts down is still picked
	// up by the next start.
	a.markConfirmDue(ids, autoConfirmDue(begun, s))
	if !a.track() {
		return
	}
	ctx, cancel := context.WithCancel(a.ctx)
	run := a.startCountdown(ActivityAutoConfirm, cancel, autoConfirmDue(begun, s))
	go func() {
		defer a.wg.Done()
		defer cancel()
		fire := a.awaitAutoConfirm(ctx, ids, begun, run)
		// A shutdown leaves the due time for the next start to count down to.
		// The stop button, auto-confirm switched off or no link left end the
		// countdown for good, and the rows let go of it before the strip does.
		if !fire && a.ctx.Err() == nil {
			a.markConfirmDue(ids, time.Time{})
		}
		// Retired before the confirm, so the stop button goes as the links
		// start to move and the strip shows ConfirmTasks' own pass instead.
		run.done()
		if fire {
			a.ConfirmTasks(ids, confirm.Config{}, confirm.TriggerAutoConfirm)
			// startTasks cleared the links it started; one the confirm
			// excluded stays in the collector without a countdown.
			a.markConfirmDue(ids, time.Time{})
		}
	}()
}

// markConfirmDue records on the collected rows among ids when their countdown
// runs out, or with the zero time clears it from every row among ids.
func (a *App) markConfirmDue(ids []string, due time.Time) {
	a.mu.Lock()
	var touched []taskCopy
	for _, id := range ids {
		t := a.tasks[id]
		if t == nil || t.ConfirmDue.Equal(due) || (!due.IsZero() && t.Status != core.StatusCollected) {
			continue
		}
		t.ConfirmDue = due
		touched = append(touched, a.copyLocked(t))
	}
	a.mu.Unlock()
	a.publishTasks(touched)
}

// rearmAutoConfirm counts down again for the batches a shutdown cut short, one
// countdown per due time. One that fell due while the app was down confirms at
// once; each still reads AutoConfirm and the delay from the saved settings.
func (a *App) rearmAutoConfirm() {
	s := a.Settings.Get()
	a.mu.Lock()
	batches := map[int64][]string{}
	var stale []string
	for id, t := range a.tasks {
		switch {
		case t.ConfirmDue.IsZero():
		case t.Status == core.StatusCollected && !t.Skipped && !t.VariantOff:
			due := t.ConfirmDue.UnixMilli()
			batches[due] = append(batches[due], id)
		default:
			// A row the countdown would pass over, like one set aside since.
			// Left marked, it would count down alone once shown again.
			stale = append(stale, id)
		}
	}
	a.mu.Unlock()
	a.markConfirmDue(stale, time.Time{})
	delay := time.Duration(s.AutoConfirmDelay) * autoConfirmSecond
	for due, ids := range batches {
		a.startAutoConfirmCountdown(ids, time.UnixMilli(due).Add(-delay), s)
	}
}

// autoConfirmDue is when a countdown begun at begun runs out under s.
func autoConfirmDue(begun time.Time, s settings.Settings) time.Time {
	return begun.Add(time.Duration(s.AutoConfirmDelay) * autoConfirmSecond)
}

// awaitAutoConfirm waits out one countdown and reports whether its links are
// to be confirmed now. It answers false when the countdown was called off, the
// app is closing, AutoConfirm was switched off or none of the links is left in
// the collector.
//
// A settings save and a link leaving the collector wake it early, so a delay
// cut to zero confirms at once, and a countdown whose links somebody already
// dealt with goes away instead of running out over nothing.
func (a *App) awaitAutoConfirm(ctx context.Context, ids []string, begun time.Time, run *activityRun) bool {
	for {
		// Taken before anything is read, so a wake in between is not lost.
		wake := a.autoConfirmWake()
		s := a.Settings.Get()
		a.mu.Lock()
		left := len(a.confirmableLocked(ids))
		a.mu.Unlock()
		if !s.AutoConfirm || left == 0 {
			return false
		}
		due := autoConfirmDue(begun, s)
		wait := time.Until(due)
		if wait <= 0 {
			return ctx.Err() == nil
		}
		run.retime(due)
		// A new delay moves the due time a restart would count down to.
		a.markConfirmDue(ids, due)
		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			timer.Stop()
			return false
		case <-wake:
			timer.Stop()
		case <-timer.C:
		}
	}
}

// autoConfirmWake returns the channel the next wakeAutoConfirm closes.
func (a *App) autoConfirmWake() <-chan struct{} {
	autoConfirmMu.Lock()
	defer autoConfirmMu.Unlock()
	ch := autoConfirmWakes[a]
	if ch == nil {
		ch = make(chan struct{})
		autoConfirmWakes[a] = ch
	}
	return ch
}

// wakeAutoConfirm makes every waiting countdown read the settings and look at
// its links again. With nothing counting down it costs a map lookup.
func (a *App) wakeAutoConfirm() {
	autoConfirmMu.Lock()
	defer autoConfirmMu.Unlock()
	if ch := autoConfirmWakes[a]; ch != nil {
		close(ch)
		delete(autoConfirmWakes, a)
	}
}
