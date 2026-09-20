package app

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/core"
)

// A one-step move slides the selection as a block. Swapping each selected task
// with its neighbour instead would let two adjacent selected rows swap with each
// other and cancel out, so a selection of three would sit still.
func TestOneStepMovesSlideTheWholeSelection(t *testing.T) {
	a := newOrderApp(t)
	stage(a, "a", "b", "c", "d", "e")

	a.MoveIn(Selection{Ids: []string{"c", "d"}}, MoveUp)
	wantOrder(t, a, "a c d b e")

	a.MoveIn(Selection{Ids: []string{"a"}}, MoveBottom)
	wantOrder(t, a, "c d b e a")

	a.MoveIn(Selection{Ids: []string{"e"}}, MoveTop)
	wantOrder(t, a, "e c d b a")

	a.MoveIn(Selection{Ids: []string{"e"}}, MoveDown)
	wantOrder(t, a, "c e d b a")
}

// Priority outranks the manual position, so a task lifted above a
// higher-priority one sorts straight back. Renumbering across the boundary
// would leave a button that does nothing every few presses.
func TestAMoveNeverCrossesAPriorityBand(t *testing.T) {
	a := newOrderApp(t)
	stage(a, "high", "n1", "n2")
	a.mu.Lock()
	a.tasks["high"].Priority = PriorityHighest
	a.mu.Unlock()
	wantOrder(t, a, "high n1 n2")

	// Already at the front of its own band, so this is a no-op rather than an
	// overtake.
	a.MoveIn(Selection{Ids: []string{"n1"}}, MoveUp)
	wantOrder(t, a, "high n1 n2")

	// And the top of the band is the top of the band, not the top of the list.
	a.MoveIn(Selection{Ids: []string{"n2"}}, MoveTop)
	wantOrder(t, a, "high n2 n1")

	// Raising the priority is the control that does cross, and it has to.
	a.SetPriorityIn(Selection{Ids: []string{"n1"}}, PriorityHighest)
	wantOrder(t, a, "high n1 n2")
}

// Last inside a higher band is still sooner than first inside a lower one, so a
// promoted link joins at the end. Keeping its old position would seat it by a
// number that described a different band.
func TestRaisingPriorityJoinsTheBandAtItsEnd(t *testing.T) {
	a := newOrderApp(t)
	stage(a, "h1", "h2", "n1", "n2")
	a.SetPriorityIn(Selection{Ids: []string{"h1", "h2"}}, PriorityHigh)
	wantOrder(t, a, "h1 h2 n1 n2")

	a.MoveIn(Selection{Ids: []string{"n2"}}, MoveTop)
	wantOrder(t, a, "h1 h2 n2 n1")

	// Promoted whole, and the hand order inside the pair survives the move.
	a.SetPriorityIn(Selection{Ids: []string{"n1", "n2"}}, PriorityHigh)
	wantOrder(t, a, "h1 h2 n2 n1")
}

// A task nobody has moved carries position zero, which is why a renumbered run
// counts down. Numbering from zero upwards would put every link pasted after a
// manual reorder ahead of the ones already ordered.
func TestAPasteAfterAMoveLandsAtTheBack(t *testing.T) {
	a := newOrderApp(t)
	stage(a, "a", "b", "c")
	a.MoveIn(Selection{Ids: []string{"c"}}, MoveTop)
	wantOrder(t, a, "c a b")

	stage(a, "fresh")
	wantOrder(t, a, "c a b fresh")
}

// A list narrowed by a search can only send the ids it can see, so the package
// form moves the rows the filter hid along with them.
func TestMoveByPackageCarriesTheRowsAFilterHid(t *testing.T) {
	a := newOrderApp(t)
	stage(a, "loose1", "part1", "loose2", "part2")
	a.mu.Lock()
	a.tasks["part1"].Package = "Release"
	a.tasks["part2"].Package = "Release"
	a.mu.Unlock()

	pkg := "Release"
	a.MoveIn(Selection{Package: &pkg}, MoveTop)
	wantOrder(t, a, "part1 part2 loose1 loose2")
}

// A direction the server does not know is refused rather than read as one of
// the four, so a client's typo does not move a selection somewhere else.
func TestMoveRefusesADirectionItDoesNotKnow(t *testing.T) {
	a := newOrderApp(t)
	stage(a, "a", "b", "c")
	if moved := a.MoveIn(Selection{Ids: []string{"c"}}, "upwards"); moved != nil {
		t.Errorf("MoveIn accepted %q and reported %v", "upwards", moved)
	}
	wantOrder(t, a, "a b c")
}

// ReorderBand takes an arbitrary new order for one band in a single request,
// which a run of relative steps cannot do without letting two drags interleave.
func TestReorderBandAppliesTheDraggedOrder(t *testing.T) {
	a := newOrderApp(t)
	stage(a, "a", "b", "c", "d")

	ids, err := a.ReorderBand([]string{"d", "b", "a", "c"})
	if err != nil {
		t.Fatalf("ReorderBand: %v", err)
	}
	if got := strings.Join(ids, " "); got != "d b a c" {
		t.Errorf("ReorderBand reported %q, want the ids handed back in the order given", got)
	}
	wantOrder(t, a, "d b a c")
}

// The boundary MoveIn keeps: a drag surface shows one band at a time, so ids
// spanning two are a bug upstream, and reconciling them quietly would seat one
// band's row inside another's.
func TestReorderRefusesIdsFromTwoBands(t *testing.T) {
	a := newOrderApp(t)
	stage(a, "high", "n1", "n2")
	a.mu.Lock()
	a.tasks["high"].Priority = PriorityHighest
	a.mu.Unlock()

	if _, err := a.ReorderBand([]string{"high", "n1", "n2"}); err == nil {
		t.Error("ReorderBand accepted ids spanning two bands")
	}
	wantOrder(t, a, "high n1 n2")
}

// A band is (forced, priority) over every task the app holds, and no screen
// shows a whole one: the download tab and the collector tab each show part of
// it. A partial list therefore means what a drag inside one visible list means,
// these tasks in this order in the slots they already occupy, so an unnamed row
// stays where it is and the named ones swap around it.
func TestReorderAppliesAPartialBandInItsOwnSlots(t *testing.T) {
	a := newOrderApp(t)
	stage(a, "a", "b", "c", "d")

	if _, err := a.ReorderBand([]string{"d", "a"}); err != nil {
		t.Fatalf("ReorderBand refused a partial band: %v", err)
	}
	// a and d held slots 1 and 4; they come back in the order given, and b and c
	// have not been touched.
	wantOrder(t, a, "d b c a")
}

// Two surfaces dragging in different halves of one band do not scramble each
// other: reordering the collector's half leaves the queue's half in the order
// the queue is running it.
func TestReorderLeavesUnnamedTasksAlone(t *testing.T) {
	a := newOrderApp(t)
	stage(a, "q1", "q2", "c1", "c2")
	a.mu.Lock()
	a.tasks["c1"].Status = core.StatusCollected
	a.tasks["c2"].Status = core.StatusCollected
	a.mu.Unlock()

	if _, err := a.ReorderBand([]string{"c2", "c1"}); err != nil {
		t.Fatalf("ReorderBand refused the collector's own half: %v", err)
	}
	wantOrder(t, a, "q1 q2 c2 c1")
}

// An unknown id has nothing to renumber, and applying the rest of the list
// around it would drop the caller's mistake instead of reporting it.
func TestReorderRefusesAnUnknownId(t *testing.T) {
	a := newOrderApp(t)
	stage(a, "a", "b", "c")

	if _, err := a.ReorderBand([]string{"a", "b", "ghost"}); err == nil {
		t.Error("ReorderBand accepted an id that names no task")
	}
	wantOrder(t, a, "a b c")
}

func newOrderApp(t *testing.T) *App {
	t.Helper()
	return newQueueApp(t)
}

// stage puts tasks in the wait queue in the order given, a second apart so the
// created-at tiebreak is unambiguous. They are parked, because the dispatcher
// passes a held link over and leaves it where it is, so these tests stay about
// the waiting order and off the network. Halting the queue is not enough: the
// timetable owns that flag and writes it from its own goroutine.
func stage(a *App, ids ...string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, id := range ids {
		a.tasks[id] = &core.Task{
			ID: id, URL: "https://host.example/" + id + ".bin",
			Status: core.StatusQueued, Enabled: true, Hold: true,
			CreatedAt: time.Now().Add(time.Duration(len(a.tasks)) * time.Second),
		}
		a.queue = append(a.queue, id)
	}
}

// wantOrder asserts the order the dispatcher would take the queue in.
func wantOrder(t *testing.T, a *App, want string) {
	t.Helper()
	a.mu.Lock()
	a.sortQueueLocked()
	got := strings.Join(a.queue, " ")
	a.mu.Unlock()
	if got != want {
		t.Errorf("wait order is %q, want %q", got, want)
	}
}

// Sorting a forced task to the front only shortens its wait, so the dispatcher
// reads the flag as well and starts it past the concurrency and per-host limits.
func TestForcedRunsPastTheLimits(t *testing.T) {
	a := newQueueApp(t)
	a.mu.Lock()
	// The limits are spent by tasks on the one host the forced link also uses,
	// so both gates are shut rather than only the global one.
	for i := 0; i < 4; i++ {
		id := fmt.Sprintf("busy%d", i)
		a.tasks[id] = &core.Task{ID: id, URL: "https://host.example/busy.bin",
			Status: core.StatusRunning, Enabled: true}
		a.active[id] = true
		a.started[id] = true
	}
	a.mu.Unlock()

	forced := putTask(t, a, core.Task{ID: "forced", URL: "https://host.example/now.bin",
		Status: core.StatusQueued, Enabled: true, Forced: true})
	plain := putTask(t, a, core.Task{ID: "plain", URL: "https://host.example/later.bin",
		Status: core.StatusQueued, Enabled: true})

	a.mu.Lock()
	a.queue = []string{forced.ID, plain.ID}
	a.dispatchLocked()
	forcedActive, plainActive := a.active[forced.ID], a.active[plain.ID]
	a.mu.Unlock()

	if !forcedActive {
		t.Error("the forced task waited for a slot; forcing it did nothing the user can see")
	}
	// The ordinary one must still wait, or "force" would just be a word for
	// switching the limits off for everybody.
	if plainActive {
		t.Error("an ordinary task started past a full slot table")
	}
}

// TestForcedPoolIsBounded: forcing is one keystroke on a selection, so an
// unbounded pool turns "start now" on two hundred links into two hundred
// transfers, every one slower than the four that would have finished by now.
func TestForcedPoolIsBounded(t *testing.T) {
	a := newQueueApp(t)

	var ids []string
	for i := 0; i < maxForcedDownloads+2; i++ {
		id := fmt.Sprintf("f%d", i)
		putTask(t, a, core.Task{ID: id, URL: "https://host.example/" + id + ".bin",
			Status: core.StatusQueued, Enabled: true, Forced: true})
		ids = append(ids, id)
	}

	a.mu.Lock()
	a.queue = append([]string(nil), ids...)
	a.dispatchLocked()
	running := len(a.active)
	a.mu.Unlock()

	if running != maxForcedDownloads {
		t.Errorf("%d forced tasks started, want the pool bound of %d", running, maxForcedDownloads)
	}
}

// A restart clears the resolver, because keeping the one that just failed is
// the single choice guaranteed to fail the same way. A link pinned to a backend
// with no tracked account never counts as unroutable, so nothing else moves it.
func TestRestartRoutesFromScratch(t *testing.T) {
	a := newOrderApp(t)
	stage(a, "a")
	a.mu.Lock()
	a.tasks["a"].Status = core.StatusError
	a.tasks["a"].Resolver = "jd"
	a.tasks["a"].Error = "jd: the link never reached JDownloader's download list"
	a.mu.Unlock()

	a.RestartTasks(nil)

	a.mu.Lock()
	got := a.tasks["a"]
	resolver, status, errText := got.Resolver, got.Status, got.Error
	a.mu.Unlock()

	if resolver != "" {
		t.Errorf("resolver = %q after a restart, want it cleared so the task is routed again", resolver)
	}
	if status != core.StatusQueued {
		t.Errorf("status = %q, want %q", status, core.StatusQueued)
	}
	if errText != "" {
		t.Errorf("error = %q, want the previous failure cleared", errText)
	}
}
