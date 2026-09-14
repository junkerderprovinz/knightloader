package app

import (
	"testing"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/core"
)

// The wait order asks TWO questions, and the server answers them differently.
//
// Who may be MOVED, and who may be given a PRIORITY. It is easy to read them as
// one question - both are "queue order", both live in one menu - and reading
// them as one is how the browser ended up offering neither to a running
// download: one predicate, narrower than either server answer, gating both.
//
// This is the measurement the interface's own two sets are read off
// (MOVE_STATES and PRIORITY_STATES in web/src/components/ListToolbar.tsx, kept
// level with movable() and SetPriorityIn by check-queue-reach.mjs). It builds a
// task in every status the server has and runs both verbs against it, because
// the only honest reason to leave a verb off a selection is that the server
// refuses it - and the only way to know that is to ask.
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
			// --- may it be moved? ------------------------------------------
			a := newQueueApp(t)
			stageIn(a, c.status, "first", "second")
			moved := a.MoveIn(Selection{Ids: []string{"second"}}, MoveTop)
			a.mu.Lock()
			posFirst, posSecond := a.tasks["first"].Position, a.tasks["second"].Position
			a.mu.Unlock()

			if got := len(moved) > 0; got != c.movable {
				t.Errorf("MoveIn on a %s task reported %v, want accepted=%v", c.status, moved, c.movable)
			}
			// Reporting ids is not the same as doing something. The positions
			// are the effect the user sees, and they are what the browser was
			// told there was none of.
			if renumbered := posFirst != 0 || posSecond != 0; renumbered != c.movable {
				t.Errorf("after MoveIn on a %s task the positions are first=%d second=%d, want renumbered=%v",
					c.status, posFirst, posSecond, c.movable)
			}
			if c.movable && posSecond >= posFirst {
				t.Errorf("MoveIn(top) on a %s task left it at %d, behind %d", c.status, posSecond, posFirst)
			}

			// --- may it be given a priority? -------------------------------
			//
			// EVERY state, and that is measured rather than assumed:
			// SetPriorityIn resolves its selection with a nil keep, so nothing
			// is filtered out before the write.
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

// TestPriorityOnAFinishedTaskSurvivesItsRestart is the whole reason the seven
// priorities are offered on a selection the four move verbs are not.
//
// A priority written on a done or failed task orders nothing while it sits
// there - it is not in the wait queue to be ordered. It is still not a dead
// control: RestartTasksIn clears the status, the error, the byte count and the
// routing, and deliberately leaves Priority alone, so the value the user set is
// in force the moment the row goes back into the queue. Setting the priority
// first and then restarting is the ordinary way to say "try these again, ahead
// of the rest", and the browser had stopped offering the first half of it.
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
				t.Errorf("priority is %d after the restart, want the %d that was set before it - "+
					"if a restart ever clears it, the seven rungs stop being worth offering on a %s selection",
					got.Priority, PriorityHighest, status)
			}
		})
	}
}

// TestPackageMoveNeedsOneMovableRowInThePackage is the measurement behind the
// PACKAGE form of the same question, which the command palette asks every time
// somebody presses alt+up.
//
// Two things are worth having in a test rather than in a comment. A package with
// nothing movable left in it is taken and carries out nothing - the answer is
// `{"ids":[],"count":0}`, an empty success, which is why an interface gated on
// "is anything selected" could offer the verb for years without a single error
// coming back. And a package with ONE movable row in it still moves, so the
// question the interface has to ask is about the package and not about the rows
// the user happened to click: a finished row picked inside a package that is
// still downloading is an ordinary, working move.
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
		t.Errorf("positions after the refused package move are %d/%d, want both still 0 - "+
			"an empty answer and an unchanged queue is what a dead control looks like from the outside", done, failed)
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
		t.Errorf("the movable row of the package sits at %d, behind %d - the move did not happen", wait, front)
	}
	if sat != 0 {
		t.Errorf("the finished row of the package was renumbered to %d; the move is for the rows the server may move", sat)
	}
}

// stageIn is stage() with the status as the point of the exercise rather than a
// fixed queued. Held, so the dispatcher passes them over and leaves them where
// they are: this is a test about what the wait order accepts, not one that hands
// links to a backend.
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

// stagedRow is one link with the status it is staged in, for the package test
// above: that one needs a package holding SEVERAL statuses at once, which
// stageIn's single-status signature cannot say.
type stagedRow struct {
	id     string
	status core.Status
}

// stagePackage is stageIn with a package name, in the order given - the order
// matters, because a queue with equal positions falls back to oldest first.
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
