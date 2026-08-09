package app

import (
	"fmt"
	"sync"
	"testing"

	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/settings"
)

// TestStopAllHaltsBeforeItFreesASlot is the ordering the hard stop lives or
// dies by.
//
// Pause frees a slot and dispatches on its way out. Against a queue that has
// not been halted yet, the stop therefore refills every slot it empties: the
// list settles with exactly as many downloads running as it started with, only
// different ones, and the button that says "stop everything" reads as broken
// rather than as buggy. Writing the halt first is a one-line difference and
// this is the only thing that pins it.
func TestStopAllHaltsBeforeItFreesASlot(t *testing.T) {
	a := newStopApp(t, 2)

	running := []string{"r1", "r2"}
	for _, id := range running {
		a.mu.Lock()
		a.tasks[id] = &core.Task{ID: id, URL: "https://host.example/" + id, Status: core.StatusRunning, Enabled: true}
		a.active[id] = true
		a.started[id] = true
		a.mu.Unlock()
	}
	waiting := []string{"w1", "w2", "w3"}
	for _, id := range waiting {
		a.mu.Lock()
		a.tasks[id] = &core.Task{ID: id, URL: "https://host.example/" + id, Status: core.StatusQueued, Enabled: true}
		a.queue = append(a.queue, id)
		a.mu.Unlock()
	}

	stopped := a.StopAll()
	if len(stopped) != len(running) {
		t.Errorf("StopAll reported %v, want the two transfers in flight", stopped)
	}

	a.mu.Lock()
	active := len(a.active)
	queued := len(a.queue)
	halted := a.halted
	manual := a.manualHalt
	a.mu.Unlock()

	if active != 0 {
		t.Errorf("%d downloads are still active after the hard stop", active)
	}
	if queued != len(waiting) {
		t.Errorf("%d tasks left waiting, want %d: the freed slots were refilled", queued, len(waiting))
	}
	if !halted {
		t.Error("the queue is not halted, so the next finished download starts another one")
	}
	if !manual {
		// Without this the timetable's own base still says "running", and the next
		// window boundary hands the runner a state that starts the queue again for
		// a reason nothing on screen could explain.
		t.Error("the hard stop was not recorded as the manual halt")
	}
}

// TestStopAllSurvivesCompletionsArrivingUnderneath is the race, driven hard
// enough to fail.
//
// Backends report into onUpdate from their own goroutines, and a finished
// download deletes its id from a.active. A stop that ranges that map while they
// do is a data race — and one Go turns into a fatal "concurrent map iteration
// and map write" that takes the whole test binary with it, which is what makes
// this catchable without the race detector. The other obvious spelling, ranging
// the map with a.mu held, deadlocks on the first entry because Pause takes the
// same lock; that shows up here as a test that never returns.
func TestStopAllSurvivesCompletionsArrivingUnderneath(t *testing.T) {
	// Large enough that walking the map takes long enough for a writer to land
	// inside it. With a handful of entries the broken spelling passes by luck,
	// which is the same as not testing it.
	const inFlight = 1500
	a := newStopApp(t, inFlight)

	ids := make([]string, 0, inFlight)
	for i := 0; i < inFlight; i++ {
		id := fmt.Sprintf("t%04d", i)
		ids = append(ids, id)
		a.mu.Lock()
		a.tasks[id] = &core.Task{
			ID: id, URL: "https://host.example/" + id + ".bin",
			Name: id + ".bin", Status: core.StatusRunning, Enabled: true, Size: 1000,
		}
		a.active[id] = true
		a.started[id] = true
		a.mu.Unlock()
	}

	// A completion's whole effect on the scheduling map, on the goroutine a
	// backend reports from: take a.mu, drop the id. It is hammered directly
	// rather than sent through onUpdate because onUpdate writes to the store,
	// and a collision that depends on a disk write landing in the right
	// microsecond is one this test would miss nine runs in ten.
	stop := make(chan struct{})
	var writer sync.WaitGroup
	writer.Add(1)
	go func() {
		defer writer.Done()
		for {
			select {
			case <-stop:
				return
			default:
			}
			a.mu.Lock()
			a.active["ghost"] = true
			delete(a.active, "ghost")
			a.mu.Unlock()
		}
	}()

	a.StopAll()
	close(stop)
	writer.Wait()

	// And then the real thing, to prove the two genuinely interleave: a handful
	// of downloads finishing through onUpdate while a second stop walks the list.
	// Deliberately a handful — every one of these writes the store, and a
	// thousand of them would turn this into a disk benchmark that happens to
	// contain a test.
	real := ids[:40]
	a.mu.Lock()
	for _, id := range real {
		a.active[id] = true
		a.tasks[id].Status = core.StatusRunning
	}
	a.halted = false
	a.mu.Unlock()

	var completions sync.WaitGroup
	completions.Add(1)
	go func() {
		defer completions.Done()
		for i, id := range real {
			if i%2 == 0 {
				a.onUpdate(id, core.Update{Status: core.StatusDone, Loaded: 1000})
			}
		}
	}()
	a.StopAll()
	completions.Wait()

	a.mu.Lock()
	active := len(a.active)
	halted := a.halted
	a.mu.Unlock()
	if !halted {
		t.Error("the queue came out of a hard stop unhalted")
	}
	// Nothing may be left holding a slot: the completions freed theirs and the
	// stop freed the rest, and a slot still held by a task nobody is downloading
	// is a slot the queue never gets back.
	if active != 0 {
		t.Errorf("%d downloads still hold a slot after the stop", active)
	}
}

// TestStopCostOnlyClaimsWhatIsActuallyLost is the warning being true.
//
// Three answers, not two: a transfer that resumes cleanly costs nothing, one
// that cannot costs its bytes, and one nobody has asked about is counted apart.
// Folding the third into the second is how "you will lose 4.2 GB" gets shown
// for a download that would have picked up exactly where it left off, and a
// dialog that has lied once is a dialog people click straight through.
func TestStopCostOnlyClaimsWhatIsActuallyLost(t *testing.T) {
	a := newStopApp(t, 8)
	yes, no := true, false

	add := func(id string, loaded int64, resumable *bool) {
		a.mu.Lock()
		a.tasks[id] = &core.Task{
			ID: id, URL: "https://host.example/" + id, Status: core.StatusRunning,
			Enabled: true, Loaded: loaded, Resumable: resumable,
		}
		a.active[id] = true
		a.mu.Unlock()
	}
	add("resumes", 5_000, &yes)
	add("lost1", 1_500, &no)
	add("lost2", 2_500, &no)
	add("nobodyAsked", 900, nil)
	// Queued, not running: it has nothing in flight to lose.
	a.mu.Lock()
	a.tasks["waiting"] = &core.Task{ID: "waiting", URL: "https://host.example/w", Status: core.StatusQueued, Enabled: true}
	a.mu.Unlock()

	cost := a.StopCost()
	if cost.Running != 4 {
		t.Errorf("Running = %d, want the four transfers in flight", cost.Running)
	}
	if len(cost.Losing) != 2 || cost.Losing[0] != "lost1" || cost.Losing[1] != "lost2" {
		t.Errorf("Losing = %v, want only the two that cannot resume", cost.Losing)
	}
	if cost.Bytes != 4_000 {
		t.Errorf("Bytes = %d, want 4000: only what the unresumable two have written", cost.Bytes)
	}
	if cost.Unknown != 1 || cost.UnknownBytes != 900 {
		t.Errorf("Unknown = %d / %d bytes, want the one nobody has asked about counted apart",
			cost.Unknown, cost.UnknownBytes)
	}
}

func newStopApp(t *testing.T, concurrent int) *App {
	t.Helper()
	a, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { a.Close() })
	if _, err := a.ApplySettings(settings.Settings{
		MaxConcurrent: concurrent, MaxPerHost: concurrent, DownloadDir: t.TempDir(),
	}); err != nil {
		t.Fatal(err)
	}
	return a
}
