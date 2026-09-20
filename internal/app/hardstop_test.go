package app

import (
	"fmt"
	"sync"
	"testing"

	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/settings"
)

// The halt is written before any slot is freed; otherwise each stop would
// dispatch the next waiting task and as many downloads would keep running.
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
	// Stopped transfers wait with the ones already queued; active == 0 above
	// shows no slot was refilled.
	if queued != len(waiting)+len(running) {
		t.Errorf("%d tasks left waiting, want %d: every stopped transfer waits with the ones already queued",
			queued, len(waiting)+len(running))
	}
	if !halted {
		t.Error("the queue is not halted, so the next finished download starts another one")
	}
	if !manual {
		// Otherwise the next window boundary would start the queue again.
		t.Error("the hard stop was not recorded as the manual halt")
	}
}

// Completions write a.active from backend goroutines while StopAll runs.
// Ranging the map without the lock is a fatal concurrent map write, detectable
// without the race detector; ranging it under a.mu deadlocks, since stopping
// takes the lock too.
func TestStopAllSurvivesCompletionsArrivingUnderneath(t *testing.T) {
	// Enough entries that the walk takes long enough for a writer to land
	// inside it.
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

	// A completion's effect on the map, hammered directly: onUpdate also writes
	// the store, which would make the collision too rare.
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

	// Then real completions through onUpdate while a second stop runs. Only a
	// few, since each one writes the store.
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
	// No slot may stay held; the queue would never get it back.
	if active != 0 {
		t.Errorf("%d downloads still hold a slot after the stop", active)
	}
}

// A resumable transfer costs nothing, an unresumable one its bytes, and one
// nobody has asked about is counted apart rather than as a loss.
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
	// Queued, so nothing in flight to lose.
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

// The schedule runner reads its base, releases the lock and applies later; a
// StopAll landing in between must not be undone by the stale answer. The
// interleaving is written out by hand rather than raced for.
func TestScheduleCannotUndoAHardStop(t *testing.T) {
	a := newStopApp(t, 2)

	// The App's own runner is stopped first: the correction keys off the base
	// the runner last read, and a live runner would be a second reader whose
	// first pass could land between the two calls below.
	if err := a.sched.Close(); err != nil {
		t.Fatal(err)
	}

	a.mu.Lock()
	a.tasks["r1"] = &core.Task{ID: "r1", URL: "https://host.example/r1", Status: core.StatusRunning, Enabled: true}
	a.active["r1"] = true
	a.started["r1"] = true
	a.tasks["w1"] = &core.Task{ID: "w1", URL: "https://host.example/w1", Status: core.StatusQueued, Enabled: true}
	a.queue = append(a.queue, "w1")
	a.mu.Unlock()

	// 1. The runner reads the base. Nothing is halted yet, so it reads "running".
	stale := a.scheduleBase()
	if stale.Paused {
		t.Fatal("fixture broken: the base should read as running before the stop")
	}

	// 2. The hard stop lands while the runner is between its read and its apply.
	a.StopAll()

	// 3. The runner applies the answer it computed from the stale base.
	a.applySchedule(stale)

	a.mu.Lock()
	halted := a.halted
	active := len(a.active)
	queued := len(a.queue)
	a.mu.Unlock()

	if !halted {
		t.Error("the schedule cleared a halt the user had just asked for by hand")
	}
	if active != 0 {
		t.Errorf("%d downloads are running again after the hard stop; the freed slot was refilled", active)
	}
	if queued != 2 {
		t.Errorf("%d tasks waiting, want 2: the stopped transfer and the one already queued", queued)
	}
}
