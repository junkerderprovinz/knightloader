package app

import (
	"errors"
	"testing"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/schedule"
	"github.com/junkerderprovinz/knightloader/internal/settings"
)

// bootPaused starts an app on dataDir whose timetable pauses the queue around
// the clock.
func bootPaused(t *testing.T, dataDir string) *App {
	t.Helper()
	a, err := New(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.ApplySettings(settings.Settings{
		DownloadDir:   t.TempDir(),
		MaxConcurrent: 2,
		MaxPerHost:    2,
		Schedule:      allDay(schedule.ActionPause),
	}); err != nil {
		a.Close()
		t.Fatal(err)
	}
	return a
}

func TestSuspendingTheScheduleLiftsItsPauseAndResumingBringsItBack(t *testing.T) {
	a := bootPaused(t, t.TempDir())
	defer a.Close()
	waitFor(t, "the pause window halting the queue", func() bool { return a.Queue().Halted })

	if err := a.SuspendSchedule(time.Time{}); err != nil {
		t.Fatalf("SuspendSchedule: %v", err)
	}
	waitFor(t, "the suspension releasing the queue", func() bool { return !a.Queue().Halted })
	st := a.ScheduleState()
	if !st.Suspended || st.SuspendedUntil != nil {
		t.Errorf("state reads suspended=%v until=%v, want suspended until lifted", st.Suspended, st.SuspendedUntil)
	}
	if st.State.Paused {
		t.Error("the reported state still says paused while the timetable is set aside")
	}
	if len(st.Entries) != 2 {
		t.Errorf("the timetable has %d rows after the suspension, want the 2 it had", len(st.Entries))
	}

	if err := a.ResumeSchedule(); err != nil {
		t.Fatalf("ResumeSchedule: %v", err)
	}
	waitFor(t, "the pause window halting the queue again", func() bool { return a.Queue().Halted })
	if a.ScheduleState().Suspended {
		t.Error("the state still reads suspended after ResumeSchedule")
	}
}

func TestScheduleSuspensionSurvivesARestart(t *testing.T) {
	dir := t.TempDir()
	until := time.Now().Add(time.Hour).Truncate(time.Second)

	a := bootPaused(t, dir)
	if err := a.SuspendSchedule(until); err != nil {
		t.Fatalf("SuspendSchedule: %v", err)
	}
	a.Close()

	b := bootPaused(t, dir)
	defer b.Close()
	st := b.ScheduleState()
	if !st.Suspended || st.SuspendedUntil == nil || !st.SuspendedUntil.Equal(until) {
		t.Fatalf("after a restart: suspended=%v until=%v, want suspended until %s", st.Suspended, st.SuspendedUntil, until)
	}
	waitFor(t, "the carried-over suspension keeping the queue running", func() bool {
		b.mu.Lock()
		defer b.mu.Unlock()
		// limitInForce is -1 until the runner's first pass.
		return b.limitInForce >= 0 && !b.halted
	})
}

func TestResumedScheduleStaysResumedAfterARestart(t *testing.T) {
	dir := t.TempDir()
	a := bootPaused(t, dir)
	if err := a.SuspendSchedule(time.Time{}); err != nil {
		t.Fatalf("SuspendSchedule: %v", err)
	}
	if err := a.ResumeSchedule(); err != nil {
		t.Fatalf("ResumeSchedule: %v", err)
	}
	a.Close()

	b := bootPaused(t, dir)
	defer b.Close()
	if b.ScheduleState().Suspended {
		t.Error("a lifted suspension came back after a restart")
	}
}

func TestSuspensionThatRanOutDuringARestartIsDropped(t *testing.T) {
	dir := t.TempDir()
	a := bootPaused(t, dir)
	if err := a.Store.SetUIState(suspendBucket, `{"until":"2020-01-01T00:00:00Z"}`); err != nil {
		t.Fatal(err)
	}
	a.Close()

	b := bootPaused(t, dir)
	defer b.Close()
	if b.ScheduleState().Suspended {
		t.Error("a suspension that ended while the box was down still reads as suspended")
	}
	waitFor(t, "the pause window halting the queue", func() bool { return b.Queue().Halted })
}

func TestSuspensionEndingInThePastIsRefused(t *testing.T) {
	a := bootPaused(t, t.TempDir())
	defer a.Close()
	if err := a.SuspendSchedule(time.Now().Add(-time.Minute)); !errors.Is(err, ErrSuspendEnded) {
		t.Errorf("SuspendSchedule with a past end = %v, want ErrSuspendEnded", err)
	}
	if a.ScheduleState().Suspended {
		t.Error("a refused suspension took effect")
	}
}
