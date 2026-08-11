package script

import (
	"testing"
	"time"
)

// TestAllTriggers_MatchesValid pins AllTriggers and Valid to each other:
// whatever AllTriggers lists must be exactly the set Valid accepts, or a
// picker built from one and validated against the other drifts silently -
// see AllTriggers' own doc comment for why that is the failure this test
// exists to catch early.
func TestAllTriggers_MatchesValid(t *testing.T) {
	all := AllTriggers()
	seen := make(map[Trigger]bool, len(all))
	for _, tr := range all {
		if !tr.Valid() {
			t.Fatalf("AllTriggers() includes %q, which Valid() rejects", tr)
		}
		if seen[tr] {
			t.Fatalf("AllTriggers() lists %q more than once", tr)
		}
		seen[tr] = true
	}
	for _, tr := range []Trigger{TriggerTaskDone, TriggerTaskFailed, TriggerQueueIdle, TriggerOnDemand} {
		if !seen[tr] {
			t.Fatalf("AllTriggers() is missing %q", tr)
		}
	}
}

func TestClassifyTaskUpdate(t *testing.T) {
	future := time.Now().Add(time.Minute)
	cases := []struct {
		name   string
		view   TaskView
		want   Trigger
		wantOK bool
	}{
		{"done fires TaskDone", TaskView{Status: statusDone}, TriggerTaskDone, true},
		{"settled error fires TaskFailed", TaskView{Status: statusError}, TriggerTaskFailed, true},
		{"error with a pending retry does not fire", TaskView{Status: statusError, NextTry: future}, "", false},
		{"running does not fire", TaskView{Status: "running"}, "", false},
		{"queued does not fire", TaskView{Status: "queued"}, "", false},
		{"paused does not fire", TaskView{Status: "paused"}, "", false},
		{"collected does not fire", TaskView{Status: "collected"}, "", false},
		{"extracting does not fire", TaskView{Status: "extracting"}, "", false},
		{"empty status does not fire", TaskView{}, "", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, ok := ClassifyTaskUpdate(c.view)
			if ok != c.wantOK || got != c.want {
				t.Fatalf("ClassifyTaskUpdate(%+v) = %q, %v; want %q, %v", c.view, got, ok, c.want, c.wantOK)
			}
		})
	}
}

// TestClassifyTaskUpdate_RetryExhaustionEdge pins the exact edge
// internal/app/app_dispatch.go's onUpdate relies on: NextTry is zeroed the
// instant a task is left settled (retries exhausted, or a disk-full
// failure that never retries at all), and that zero value is the only
// signal this package has to tell "final" apart from "will try again
// shortly". A task on its very last retry, one tick before NextTry would
// have been set, still counts as pending - this test exists so a future
// change to the zero-value convention on either side breaks loudly here
// rather than showing up as a script firing once per backoff attempt.
func TestClassifyTaskUpdate_RetryExhaustionEdge(t *testing.T) {
	exhausted := TaskView{Status: statusError, Retries: 5, NextTry: time.Time{}}
	if got, ok := ClassifyTaskUpdate(exhausted); !ok || got != TriggerTaskFailed {
		t.Fatalf("exhausted retries: got %q, %v; want TriggerTaskFailed, true", got, ok)
	}
	pending := TaskView{Status: statusError, Retries: 1, NextTry: time.Now().Add(30 * time.Second)}
	if _, ok := ClassifyTaskUpdate(pending); ok {
		t.Fatalf("a task with a pending retry must not fire TaskFailed")
	}
}

func TestIsQueueIdle(t *testing.T) {
	cases := []struct {
		name string
		view QueueView
		want bool
	}{
		{"nothing at all", QueueView{}, true},
		{"everything disabled", QueueView{Files: 3, Disabled: 3}, true},
		{"one enabled file left", QueueView{Files: 3, Disabled: 2}, false},
		{"running counts as not idle via Files", QueueView{Files: 1, Disabled: 0, Running: 1}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := IsQueueIdle(c.view); got != c.want {
				t.Fatalf("IsQueueIdle(%+v) = %v, want %v", c.view, got, c.want)
			}
		})
	}
}

func TestTaskView_ProgressPct(t *testing.T) {
	cases := []struct {
		name string
		view TaskView
		want float64
	}{
		{"unknown size is zero, not a divide by zero", TaskView{Size: 0, Loaded: 500}, 0},
		{"halfway", TaskView{Size: 200, Loaded: 100}, 50},
		{"clamped at 100 even if loaded overshoots", TaskView{Size: 100, Loaded: 150}, 100},
		{"clamped at 0 for a negative loaded", TaskView{Size: 100, Loaded: -10}, 0},
		{"complete", TaskView{Size: 100, Loaded: 100}, 100},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := c.view.ProgressPct(); got != c.want {
				t.Fatalf("ProgressPct() = %v, want %v", got, c.want)
			}
		})
	}
}
