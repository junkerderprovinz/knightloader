package ownlogin

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/core"
)

type idleEngine struct{}

func (idleEngine) Handover(string, string, int, func(context.Context) (string, error)) {}
func (idleEngine) Pause(string)                                                        {}
func (idleEngine) Resume(string)                                                       {}
func (idleEngine) Remove(string, bool)                                                 {}

func TestAWaitResumedAfterAPauseStillCountsAsWaitingWhenThePausedOneWindsDown(t *testing.T) {
	tasks := NewTasks(idleEngine{}, func(string, core.Update) {}, Config{
		Unlock: func(_ context.Context, _, _ string, delay func(time.Duration) error) (string, int64, error) {
			return "", 0, delay(90 * time.Second)
		},
		Reason:   func(error) core.Reason { return core.ReasonUnknown },
		WaitNote: "waiting %s",
	})
	waiting := make(chan struct{}, 2)
	release := make(chan struct{})
	var calls atomic.Int32
	tasks.Sleep = func(ctx context.Context, _ time.Duration) error {
		first := calls.Add(1) == 1
		waiting <- struct{}{}
		<-ctx.Done()
		if first {
			// The paused run lingers until the resumed one is waiting too.
			<-release
		}
		return ctx.Err()
	}
	t.Cleanup(func() { tasks.Remove("t1", false) })

	tasks.Download("t1", "https://example.com/file", nil, 1)
	<-waiting
	tasks.Pause("t1")
	if tasks.Waiting("t1") {
		t.Error("a paused task still counts as waiting")
	}
	tasks.Resume("t1")
	<-waiting
	close(release)

	deadline := time.Now().Add(200 * time.Millisecond)
	for time.Now().Before(deadline) {
		if !tasks.Waiting("t1") {
			t.Fatal("the paused run winding down took the resumed run's wait with it")
		}
		time.Sleep(5 * time.Millisecond)
	}
}
