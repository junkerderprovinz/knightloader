package app

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

// handoverSpy stands in for the engine a debrid account hands its links to.
type handoverSpy struct {
	handed, paused, removed chan string
}

func newHandoverSpy() *handoverSpy {
	return &handoverSpy{handed: make(chan string, 1), paused: make(chan string, 1), removed: make(chan string, 1)}
}

func (s *handoverSpy) Handover(taskID, _ string, _ int, _ func(context.Context) (string, error)) {
	s.handed <- taskID
}
func (s *handoverSpy) Pause(taskID string)          { s.paused <- taskID }
func (s *handoverSpy) Resume(string)                {}
func (s *handoverSpy) Remove(taskID string, _ bool) { s.removed <- taskID }

func expectCall(t *testing.T, ch <-chan string, want, what string) {
	t.Helper()
	select {
	case got := <-ch:
		if got != want {
			t.Fatalf("%s reached the engine for %q, want %q", what, got, want)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("%s of %s never reached the engine, which goes on downloading it", what, want)
	}
}

// A rewire builds every debrid account's backend anew. A link the old one had
// handed to the engine must still be paused and removed through the new one,
// or the transfer goes on drawing from the speed limit until a restart.
func TestALinkHandedOnBeforeARewireIsStillPausedAndRemoved(t *testing.T) {
	a := newQueueApp(t)
	svc := slotDebrid{url: "https://cdn.example/f.bin", unlocks: &atomic.Int32{}}
	eng := newHandoverSpy()

	a.linkBackend("alldebrid", svc, eng).Download("t1", "https://hoster.example/f.bin", nil, 2)
	expectCall(t, eng.handed, "t1", "the handover")

	rewired := a.linkBackend("alldebrid", svc, eng)
	rewired.Pause("t1")
	expectCall(t, eng.paused, "t1", "the pause")
	rewired.Remove("t1", false)
	expectCall(t, eng.removed, "t1", "the removal")
}
