package app

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/core"
)

// TestOneStepMovesSlideTheWholeSelection is the block move.
//
// Swapping each selected task with its neighbour one at a time looks like the
// same thing and is not: two adjacent selected rows swap with each other and
// cancel out, so a selection of three sits still while a selection of one
// moves. Selecting more and having less happen is the kind of bug people work
// around for months rather than report.
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

// TestAMoveNeverCrossesAPriorityBand pins the promise the one-step moves make.
//
// Priority outranks the manual position, so a task lifted above a
// higher-priority one sorts straight back to where it was. Renumbering across
// the boundary would leave the user pressing a button that does nothing every
// few presses, with no way to tell that press from a broken one.
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

// TestRaisingPriorityJoinsTheBandAtItsEnd pins where a promoted link lands, and
// that promoting a band somebody has ordered by hand does not scramble it.
//
// Last inside a higher band is still sooner than first inside a lower one, so
// joining at the end is what "run this sooner" actually asked for. Keeping the
// old position instead would seat the arrival by a number that described a
// different band entirely.
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

// TestAPasteAfterAMoveLandsAtTheBack is why the renumbered run is negative.
//
// A task nobody has moved carries position zero. Numbering a band from zero
// upwards would put every link pasted after a manual reorder ahead of the ones
// already ordered, so the queue would reshuffle itself whenever anything was
// added — with the newest link at the front, which is the opposite of what the
// list has promised since the first line of it.
func TestAPasteAfterAMoveLandsAtTheBack(t *testing.T) {
	a := newOrderApp(t)
	stage(a, "a", "b", "c")
	a.MoveIn(Selection{Ids: []string{"c"}}, MoveTop)
	wantOrder(t, a, "c a b")

	stage(a, "fresh")
	wantOrder(t, a, "c a b fresh")
}

// TestMoveByPackageCarriesTheRowsAFilterHid is what the package form is for. A
// list narrowed by a search can only send the ids it can see, and a package
// that arrives at the top in pieces is worse than one that did not move.
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

// TestMoveRefusesADirectionItDoesNotKnow keeps a client's typo from being
// carried out. The form this replaced read everything that was not "top" as
// "bottom", so a misspelled direction sent a selection to the end of the queue
// and was answered with a 204.
func TestMoveRefusesADirectionItDoesNotKnow(t *testing.T) {
	a := newOrderApp(t)
	stage(a, "a", "b", "c")
	if moved := a.MoveIn(Selection{Ids: []string{"c"}}, "upwards"); moved != nil {
		t.Errorf("MoveIn accepted %q and reported %v", "upwards", moved)
	}
	wantOrder(t, a, "a b c")
}

// --- helpers ---------------------------------------------------------------

func newOrderApp(t *testing.T) *App {
	t.Helper()
	return newQueueApp(t)
}

// stage puts tasks in the wait queue in the order given, a second apart so the
// created-at tiebreak is unambiguous.
//
// Every one of them is parked. The dispatcher passes a held link over and
// leaves it exactly where it is in the queue, which is what makes this a test
// about the order things wait in rather than one that hands five links to a
// backend and reaches the network. Halting the queue instead is not enough: the
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

// TestForcedRunsPastTheLimits is the other half of forcing, and the half that
// was missing: sorting a task to the front only shortens its wait, and until the
// dispatcher read the flag a forced task queued behind a full slot table exactly
// like every other one. The field's own comment promised "past the concurrency
// and per-host limits", and nothing kept that promise.
func TestForcedRunsPastTheLimits(t *testing.T) {
	a := newQueueApp(t)
	a.mu.Lock()
	// The limits are already spent, and by tasks on the one host the forced link
	// also uses - so both gates are shut, not just the global one.
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
