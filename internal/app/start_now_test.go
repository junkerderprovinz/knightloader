package app

// "Start now" on a stopped queue: the links it was pressed for start, three at
// a time at most, and nothing else does.

import (
	"fmt"
	"testing"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/resolver/jd"
	"github.com/junkerderprovinz/knightloader/internal/settings"
)

// newStoppedApp is an instance whose queue was stopped by hand, with one slot
// and the JDownloader backend replaced by a stub that reports every start.
func newStoppedApp(t *testing.T) (*App, *stubBackend) {
	t.Helper()
	a, err := newApp(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { a.Close() })
	stub := &stubBackend{got: make(chan string, 16)}
	a.bmu.Lock()
	a.jd = stub
	a.bmu.Unlock()
	a.Registry.Register(jd.Resolver{})
	if _, err := a.ApplySettings(settings.Settings{
		MaxConcurrent: 1, MaxPerHost: 1, MaxRetries: 3, DownloadDir: t.TempDir(),
	}); err != nil {
		t.Fatal(err)
	}
	a.SetHalted(true)
	return a, stub
}

// queueLinks puts links in the wait queue, routed to the stub.
func queueLinks(t *testing.T, a *App, forced bool, ids ...string) {
	t.Helper()
	for _, id := range ids {
		putTask(t, a, core.Task{ID: id, URL: "https://h.example/" + id, Name: id,
			Status: core.StatusQueued, Enabled: true, Resolver: "jd", Forced: forced})
	}
	a.mu.Lock()
	a.queue = append(a.queue, ids...)
	a.mu.Unlock()
}

func TestStartNowStartsALinkOnAQueueStoppedByHand(t *testing.T) {
	a, stub := newStoppedApp(t)
	queueLinks(t, a, false, "now", "later")

	a.ForceDownload(Selection{Ids: []string{"now"}})

	if got := collect(t, stub.got, 1); !got["now"] {
		t.Fatalf("started %v, want the link Start now was pressed for", got)
	}
	expectNone(t, stub.got)
	if !a.Queue().Halted {
		t.Error("Start now released the stop, which is about the whole queue")
	}
	if w := liveTask(a, "later").Waiting; w != core.WaitingHalted {
		t.Errorf("the other link waits for %q, want the stopped queue", w)
	}
}

func TestStartNowOnAStoppedQueueStartsThreeAtATime(t *testing.T) {
	a, stub := newStoppedApp(t)
	var ids []string
	for i := 0; i < maxForcedDownloads+2; i++ {
		ids = append(ids, fmt.Sprintf("f%d", i))
	}
	queueLinks(t, a, false, ids...)

	a.ForceDownload(Selection{Ids: ids})

	collect(t, stub.got, maxForcedDownloads)
	expectNone(t, stub.got)
	waiting := 0
	for _, id := range ids {
		if liveTask(a, id).Waiting == core.WaitingForced {
			waiting++
		}
	}
	if waiting != 2 {
		t.Errorf("%d links wait for a Start now slot, want the 2 beyond the pool of %d", waiting, maxForcedDownloads)
	}
}

// Forced is stored, so a queue held at boot would let every forced link out if
// the flag alone opened a stopped queue.
func TestAForcedLinkWaitsOnAStoppedQueueUntilStartNowIsPressed(t *testing.T) {
	a, stub := newStoppedApp(t)
	queueLinks(t, a, true, "forced")

	a.mu.Lock()
	a.dispatchLocked()
	a.mu.Unlock()

	expectNone(t, stub.got)
	if w := liveTask(a, "forced").Waiting; w != core.WaitingHalted {
		t.Errorf("the forced link waits for %q, want the stopped queue", w)
	}
}

// The hard stop is the newer word on the queue, so what Start now let out
// waits with everything else afterwards.
func TestTheHardStopHoldsWhatStartNowStarted(t *testing.T) {
	a, stub := newStoppedApp(t)
	queueLinks(t, a, false, "now")
	a.ForceDownload(Selection{Ids: []string{"now"}})
	collect(t, stub.got, 1)

	a.StopAll()

	expectNone(t, stub.got)
	if live := liveTask(a, "now"); live.Status != core.StatusQueued || live.Waiting != core.WaitingHalted {
		t.Errorf("after the hard stop the link is %q waiting for %q, want queued behind the stop", live.Status, live.Waiting)
	}
}

// A retry puts the link back in the queue by itself, and it is still the start
// somebody asked for.
func TestStartNowOutlastsARetry(t *testing.T) {
	a, stub := newStoppedApp(t)
	queueLinks(t, a, false, "now")
	a.ForceDownload(Selection{Ids: []string{"now"}})
	collect(t, stub.got, 1)

	a.onUpdate("now", core.Update{Status: core.StatusError, Err: "503 Service Unavailable", Retry: 10 * time.Millisecond})

	if got := collect(t, stub.got, 1); !got["now"] {
		t.Fatalf("the retry started %v, want the link again", got)
	}
}
