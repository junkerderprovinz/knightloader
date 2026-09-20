package app

// A category's priority is written once, when the task is made. In the
// dispatcher it would undo every manual reorder, and afterwards a hand-set 0
// cannot be told from an untouched one.

import (
	"testing"

	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/resolver"
	"github.com/junkerderprovinz/knightloader/internal/rules"
	"github.com/junkerderprovinz/knightloader/internal/settings"
)

// stageOne runs one already resolved link through the Packagizer step the
// pasted path uses, without touching the network.
func stageOne(a *App, name string) *core.Task {
	created := a.AddResolvedLinksFrom([]resolver.Result{
		{Name: name, DirectURL: "https://host.example/" + name, Size: 1 << 20},
	}, "", OriginPaste)
	if len(created) != 1 {
		return nil
	}
	return created[0]
}

// A category sets the priority of what it receives; a rule naming a priority
// beats the category, as for the folder in dirFor; and a category without a
// priority leaves it alone.
func TestADrawerSetsTheQueuePositionOfWhatItReceives(t *testing.T) {
	low, high := -2, 3

	filedBy := func(t *testing.T, cat settings.Category, effect rules.Action) *core.Task {
		t.Helper()
		a, _ := newRuleApp(t, func(s *settings.Settings, _ string) {
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

	// A category asking for 0 is indistinguishable here from one never read,
	// since a new task's priority is already 0. That case is covered by
	// internal/settings' tests for PriorityFor, whose second result is why
	// Category.Priority is a *int.

	t.Run("a rule that names a priority beats the drawer", func(t *testing.T) {
		task := filedBy(t, settings.Category{ID: "serien", Name: "Serien", Priority: &low}, rules.Action{Priority: &high})
		if task.Priority != high {
			t.Errorf("Priority = %d, want the rule's %d; a category must not overrule a rule", task.Priority, high)
		}
	})

	t.Run("a drawer with no opinion changes nothing", func(t *testing.T) {
		task := filedBy(t, settings.Category{ID: "serien", Name: "Serien"}, rules.Action{})
		if task.Priority != 0 {
			t.Errorf("Priority = %d on a link filed in a drawer that says nothing about it, want the untouched 0", task.Priority)
		}
	})
}
