package app

// The fourth category override, and the only one whose correct home was a file
// nobody in that batch owned: the queue position a drawer starts its downloads
// at.
//
// It is written once, where the task is made, and never again. The dispatcher
// is the tempting place and the wrong one - dispatchLocked runs on nearly every
// event in the app, so the pass after somebody dragged a download up the list
// would put it back where the drawer says, silently. Nor can the exception be
// written: core.Task.Priority is a plain int on which 0 is the MIDDLE priority
// and a real answer, so once a task exists, "somebody set this by hand" and
// "nobody ever touched it" are the same value.
//
// Every case below states the instance's own default as something the drawer
// disagrees with, so a result it observes cannot have come from the fallback.

import (
	"testing"

	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/resolver"
	"github.com/junkerderprovinz/knightloader/internal/rules"
	"github.com/junkerderprovinz/knightloader/internal/settings"
)

// stageOne runs one already-named link through the same packagizer hand-off the
// pasted path uses. Already-resolved, so nothing here touches the network.
func stageOne(a *App, name string) *core.Task {
	created := a.AddResolvedLinksFrom([]resolver.Result{
		{Name: name, DirectURL: "https://host.example/" + name, Size: 1 << 20},
	}, "", OriginPaste)
	if len(created) != 1 {
		return nil
	}
	return created[0]
}

// TestADrawerSetsTheQueuePositionOfWhatItReceives is the guard on the one line
// in packagize. Three cases, each of which can actually fail:
//
// The first proves the drawer is read at all. The second proves a rule still
// beats a drawer, which is dirFor's order for the folder as well: a rule looked
// at THIS link, a drawer is a label on a whole batch, and more evidence wins.
// The third proves a drawer that says nothing about the position leaves it
// alone, so filing a download somewhere cannot move it by accident.
func TestADrawerSetsTheQueuePositionOfWhatItReceives(t *testing.T) {
	low, high := -2, 3

	filedBy := func(t *testing.T, cat settings.Category, effect rules.Action) *core.Task {
		t.Helper()
		a, _ := newRuleApp(t, func(s *settings.Settings, _ string) {
			// The instance's own answer, deliberately not what any drawer below
			// asks for. A test that agrees with the fallback passes against no
			// wiring at all.
			s.Categories = []settings.Category{cat}
			effect.Category = cat.ID
			s.Packagizer = rules.Set{Rules: []rules.Rule{{
				Name:       "in die Schublade",
				Conditions: []rules.Condition{{Field: rules.FieldFilename, Op: rules.OpContains, Value: "s01e"}},
				Action:     effect,
			}}}
		})
		task := stageOne(a, "Doctor.Who.S01E03.mkv")
		if task == nil {
			t.Fatal("the link was not staged")
		}
		if task.Category != cat.ID {
			t.Fatalf("the rule did not file the link: Category = %q, want %q", task.Category, cat.ID)
		}
		return task
	}

	t.Run("a drawer decides where its downloads start", func(t *testing.T) {
		task := filedBy(t, settings.Category{ID: "serien", Name: "Serien", Priority: &low}, rules.Action{})
		if task.Priority != low {
			t.Errorf("Priority = %d, want the drawer's %d; the drawer is being asked nothing", task.Priority, low)
		}
	})

	// NOT TESTED HERE, and the reason is worth more than the test would be: a
	// drawer asking for the MIDDLE position (0) cannot be observed at all from
	// the task. A new task's Priority is already 0, so "the drawer asked for 0
	// and was heard" and "the drawer was never read" produce the identical row.
	// A sub-test asserting Priority == 0 there would pass against no wiring
	// whatsoever, which is worse than no sub-test.
	//
	// That is exactly why the line uses PriorityFor's second result and not a
	// `p != 0` shortcut, and why settings.Category.Priority is a *int in the
	// first place. The distinction lives one level down and is guarded there,
	// in internal/settings' own tests for PriorityFor.

	t.Run("a rule that names a priority beats the drawer", func(t *testing.T) {
		task := filedBy(t, settings.Category{ID: "serien", Name: "Serien", Priority: &low}, rules.Action{Priority: &high})
		if task.Priority != high {
			t.Errorf("Priority = %d, want the rule's %d; a drawer must not overrule a rule that looked at this link", task.Priority, high)
		}
	})

	t.Run("a drawer with no opinion changes nothing", func(t *testing.T) {
		task := filedBy(t, settings.Category{ID: "serien", Name: "Serien"}, rules.Action{})
		if task.Priority != 0 {
			t.Errorf("Priority = %d on a link filed in a drawer that says nothing about it, want the untouched 0", task.Priority)
		}
	})
}
