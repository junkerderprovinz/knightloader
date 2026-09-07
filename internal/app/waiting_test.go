package app

// Why a queued task is not running, at the one place that decides it.

import (
	"testing"

	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/settings"
)

// TestWaitingSaysWhichLimitIsHoldingATask is the distinction the whole feature
// exists for: "all slots busy" and "this host's limit" look identical on a row
// that only says "waiting", and they are fixed by two different settings.
//
// The fixture uses two hosts on purpose. With one, the two limits are
// indistinguishable, and a test that cannot tell them apart would pass whichever
// reason the code happened to write.
func TestWaitingSaysWhichLimitIsHoldingATask(t *testing.T) {
	a := newStopApp(t, 2)
	if _, err := a.ApplySettings(settings.Settings{
		MaxConcurrent: 4, MaxPerHost: 1, DownloadDir: t.TempDir(),
	}); err != nil {
		t.Fatal(err)
	}

	a.mu.Lock()
	// One transfer already running on host A, so A is at its per-host ceiling
	// while the global one still has room.
	a.tasks["a1"] = &core.Task{ID: "a1", URL: "https://a.example/1", Status: core.StatusRunning, Enabled: true}
	a.active["a1"] = true
	a.started["a1"] = true
	// A second link on the same host: held by the HOST limit, not the global one.
	a.tasks["a2"] = &core.Task{ID: "a2", URL: "https://a.example/2", Status: core.StatusQueued, Enabled: true}
	// One switched off, and one parked by hand: two reasons that have nothing to
	// do with any limit and must not be reported as one.
	a.tasks["off"] = &core.Task{ID: "off", URL: "https://b.example/1", Status: core.StatusQueued}
	a.tasks["held"] = &core.Task{ID: "held", URL: "https://b.example/2", Status: core.StatusQueued, Enabled: true, Hold: true}
	a.queue = append(a.queue, "a2", "off", "held")
	a.dispatchLocked()
	got := map[string]core.Waiting{
		"a2":   a.tasks["a2"].Waiting,
		"off":  a.tasks["off"].Waiting,
		"held": a.tasks["held"].Waiting,
	}
	a.mu.Unlock()

	want := map[string]core.Waiting{
		"a2":   core.WaitingHost,
		"off":  core.WaitingDisabled,
		"held": core.WaitingHold,
	}
	for id, w := range want {
		if got[id] != w {
			t.Errorf("task %s waits with %q, want %q", id, got[id], w)
		}
	}
}

// TestWaitingClearsItselfWhenTheLimitIsRaised pins the property that makes this
// value safe to trust: it is recomputed from scratch, so nobody has to remember
// to erase it. A reason that had to be cleared by whoever fixed the cause would
// be a stale label on a healthy row within a day.
func TestWaitingClearsItselfWhenTheLimitIsRaised(t *testing.T) {
	a := newStopApp(t, 1)
	if _, err := a.ApplySettings(settings.Settings{
		MaxConcurrent: 1, MaxPerHost: 1, DownloadDir: t.TempDir(),
	}); err != nil {
		t.Fatal(err)
	}

	a.mu.Lock()
	a.tasks["r1"] = &core.Task{ID: "r1", URL: "https://a.example/1", Status: core.StatusRunning, Enabled: true}
	a.active["r1"] = true
	a.started["r1"] = true
	a.tasks["w1"] = &core.Task{ID: "w1", URL: "https://b.example/1", Status: core.StatusQueued, Enabled: true}
	a.queue = append(a.queue, "w1")
	a.dispatchLocked()
	held := a.tasks["w1"].Waiting
	a.mu.Unlock()

	if held != core.WaitingSlot {
		t.Fatalf("w1 waits with %q, want %q - the global limit is the one that is full", held, core.WaitingSlot)
	}

	// The transfer in flight finishes, so the slot it held comes free.
	a.mu.Lock()
	delete(a.active, "r1")
	a.dispatchLocked()
	after := a.tasks["w1"].Waiting
	a.mu.Unlock()

	if after != core.WaitingNone {
		t.Errorf("w1 still waits with %q after a slot came free - the reason is not recomputed, it is remembered", after)
	}
}

// TestWaitingSaysTheQueueIsStopped covers the blanket case. Every queued row
// carries it at once, and that is the point: a list where nothing moves should
// say so on the rows, not only in the head card somebody may have scrolled past.
func TestWaitingSaysTheQueueIsStopped(t *testing.T) {
	a := newStopApp(t, 4)

	a.mu.Lock()
	for _, id := range []string{"w1", "w2"} {
		a.tasks[id] = &core.Task{ID: id, URL: "https://a.example/" + id, Status: core.StatusQueued, Enabled: true}
		a.queue = append(a.queue, id)
	}
	a.halted = true
	a.dispatchLocked()
	got := []core.Waiting{a.tasks["w1"].Waiting, a.tasks["w2"].Waiting}
	a.mu.Unlock()

	for i, w := range got {
		if w != core.WaitingHalted {
			t.Errorf("queued task %d waits with %q, want %q", i+1, w, core.WaitingHalted)
		}
	}
}
