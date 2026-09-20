package app

import (
	"testing"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/core"
)

// The wait order answers two separate questions: who may be moved, and who may
// be given a priority. Both live in one menu, and a single predicate gating both
// is narrower than either server answer.
//
// The interface reads its two sets off this measurement (MOVE_STATES and
// PRIORITY_STATES in web/src/components/ListToolbar.tsx, kept level with
// movable() and SetPriorityIn by check-queue-reach.mjs), so a task is built in
// every status the server has and both verbs are run against it.
func TestQueueReachPerStatus(t *testing.T) {
	cases := []struct {
		status core.Status
		// Whether the server takes a one-step move on a task in this state. It
		// is movable()'s answer, spelled out per status so a change to that one
		// line has to be re-justified here rather than quietly widening or
		// narrowing what the menu offers.
		movable bool
	}{
		{core.StatusCollected, true},
		{core.StatusQueued, true},
		{core.StatusRunning, true},
		{core.StatusPaused, true},
		{core.StatusExtracting, true},
		// A finished or failed download has no place left in the wait order, so
		// the four move verbs are the one thing the menu is right to leave off.
		{core.StatusDone, false},
		{core.StatusError, false},
	}

	for _, c := range cases {
		t.Run(string(c.status), func(t *testing.T) {
			// May it be moved?
			a := newQueueApp(t)
			stageIn(a, c.status, "first", "second")
			moved := a.MoveIn(Selection{Ids: []string{"second"}}, MoveTop)
			a.mu.Lock()
			posFirst, posSecond := a.tasks["first"].Position, a.tasks["second"].Position
			a.mu.Unlock()

			if got := len(moved) > 0; got != c.movable {
				t.Errorf("MoveIn on a %s task reported %v, want accepted=%v", c.status, moved, c.movable)
			}
			// Reporting ids is not the same as doing something: the positions
			// are the effect the user sees.
			if renumbered := posFirst != 0 || posSecond != 0; renumbered != c.movable {
				t.Errorf("after MoveIn on a %s task the positions are first=%d second=%d, want renumbered=%v",
					c.status, posFirst, posSecond, c.movable)
			}
			if c.movable && posSecond >= posFirst {
				t.Errorf("MoveIn(top) on a %s task left it at %d, behind %d", c.status, posSecond, posFirst)
			}

			// May it be given a priority? In every state: SetPriorityIn
			// resolves its selection with a nil keep, so nothing is filtered
			// out before the write.
			b := newQueueApp(t)
			stageIn(b, c.status, "one")
			named := b.SetPriorityIn(Selection{Ids: []string{"one"}}, PriorityHighest)
			b.mu.Lock()
			got := b.tasks["one"].Priority
			b.mu.Unlock()
			if len(named) != 1 {
				t.Errorf("SetPriorityIn on a %s task named %v, want the task back", c.status, named)
			}
			if got != PriorityHighest {
				t.Errorf("priority on a %s task is %d, want %d", c.status, got, PriorityHighest)
			}
		})
	}
}

// Why the seven priorities are offered on a selection the four move verbs are
// not. A priority on a done or failed task orders nothing while it sits there,
// but RestartTasksIn clears the status, the error, the byte count and the
// routing and leaves Priority standing, so setting it first and restarting
// afterwards says "try these again, ahead of the rest".
func TestPriorityOnAFinishedTaskSurvivesItsRestart(t *testing.T) {
	for _, status := range []core.Status{core.StatusError, core.StatusDone} {
		t.Run(string(status), func(t *testing.T) {
			a := newQueueApp(t)
			stageIn(a, status, "again")

			a.SetPriorityIn(Selection{Ids: []string{"again"}}, PriorityHighest)
			a.RestartTasks([]string{"again"})

			a.mu.Lock()
			got := *a.tasks["again"]
			a.mu.Unlock()

			if got.Status != core.StatusQueued {
				t.Fatalf("a %s task is %q after a restart, want it back in the queue", status, got.Status)
			}
			if got.Priority != PriorityHighest {
				t.Errorf("priority is %d after the restart, want the %d set before it on a %s task",
					got.Priority, PriorityHighest, status)
			}
		})
	}
}

// The package form of the same question, which the command palette asks on
// alt+up. A package with nothing movable left in it is accepted and carries out
// nothing, answering `{"ids":[],"count":0}`, so an interface gated on "is
// anything selected" sees no error. A package with one movable row still moves,
// so the interface has to ask about the package rather than about the rows the
// user clicked.
func TestPackageMoveNeedsOneMovableRowInThePackage(t *testing.T) {
	spent := newQueueApp(t)
	stagePackage(spent, "spent", stagedRow{"finished", core.StatusDone}, stagedRow{"failed", core.StatusError})
	stagePackage(spent, "waiting", stagedRow{"later", core.StatusQueued})

	name := "spent"
	if moved := spent.MoveIn(Selection{Package: &name}, MoveTop); len(moved) != 0 {
		t.Errorf("MoveIn on a package of done/error rows reported %v, want nothing moved", moved)
	}
	spent.mu.Lock()
	done, failed := spent.tasks["finished"].Position, spent.tasks["failed"].Position
	spent.mu.Unlock()
	if done != 0 || failed != 0 {
		t.Errorf("positions after the refused package move are %d/%d, want both still 0", done, failed)
	}

	live := newQueueApp(t)
	stagePackage(live, "front", stagedRow{"first", core.StatusQueued})
	stagePackage(live, "mixed", stagedRow{"already-done", core.StatusDone}, stagedRow{"still-waiting", core.StatusQueued})

	mixed := "mixed"
	moved := live.MoveIn(Selection{Package: &mixed}, MoveTop)
	if len(moved) != 1 || moved[0] != "still-waiting" {
		t.Fatalf("MoveIn on a mixed package moved %v, want just the one row the server may move", moved)
	}
	live.mu.Lock()
	front, wait, sat := live.tasks["first"].Position, live.tasks["still-waiting"].Position, live.tasks["already-done"].Position
	live.mu.Unlock()
	if wait >= front {
		t.Errorf("the movable row of the package sits at %d, behind %d; the move did not happen", wait, front)
	}
	if sat != 0 {
		t.Errorf("the finished row of the package was renumbered to %d; the move is for the rows the server may move", sat)
	}
}

// stageIn is stage with a chosen status instead of a fixed queued. The tasks are
// held, so the dispatcher passes them over and leaves them where they are.
func stageIn(a *App, status core.Status, ids ...string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, id := range ids {
		a.tasks[id] = &core.Task{
			ID: id, URL: "https://host.example/" + id + ".bin",
			Status: status, Enabled: true, Hold: true,
			CreatedAt: time.Now().Add(time.Duration(len(a.tasks)) * time.Second),
		}
		a.queue = append(a.queue, id)
	}
}

// stagedRow is one link with the status it is staged in, for a package holding
// several statuses at once.
type stagedRow struct {
	id     string
	status core.Status
}

// stagePackage is stageIn with a package name. The order given matters, because
// a queue with equal positions falls back to oldest first.
func stagePackage(a *App, pkg string, rows ...stagedRow) {
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, r := range rows {
		a.tasks[r.id] = &core.Task{
			ID: r.id, URL: "https://host.example/" + r.id + ".bin", Package: pkg,
			Status: r.status, Enabled: true, Hold: true,
			CreatedAt: time.Now().Add(time.Duration(len(a.tasks)) * time.Second),
		}
		a.queue = append(a.queue, r.id)
	}
}
