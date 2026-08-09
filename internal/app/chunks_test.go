package app

// The connection count, which is one decision with four inputs and had four
// different readings of how they combine. The table below is that decision
// written as data, and it is deliberately in a file of its own: the formula is
// what people got wrong, so the place it is pinned should be findable by name.

import (
	"testing"

	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/rules"
	"github.com/junkerderprovinz/knightloader/internal/settings"
)

// TestConnectionCountPrecedence pins the whole of connsFor.
//
// Every row is a claim somebody got wrong at least once. The two that matter
// most are the last pair: a resolver's count is what the HOST tolerates, so it
// may cut a number down and may never lift one, and a zero anywhere in the chain
// means "no opinion" rather than "no connections".
func TestConnectionCountPrecedence(t *testing.T) {
	cases := []struct {
		name string
		// task is the count on the task, whether a rule wrote it as the link was
		// staged or the user typed it on the row afterwards.
		task int
		// global is the settings value; zero is a fresh install.
		global int
		// ceilings are what the host is said to tolerate, in the order the
		// dispatcher passes them.
		ceilings []int
		want     int
	}{
		{"nobody has an opinion", 0, 0, nil, defaultConns},
		{"the global setting, with nothing on the task", 0, 8, nil, 8},
		{"the task outranks the global setting", 2, 8, nil, 2},
		{"the task outranks the built-in default", 6, 0, nil, 6},
		// The one that makes a per-task spinner usable: leaving it at zero has to
		// hand the decision back, not ask for a download that opens nothing.
		{"zero on the task means the global, not none", 0, 8, nil, 8},
		{"zero on the task and no global means the default", 0, 0, nil, defaultConns},

		// A resolver's answer is a ceiling in both directions of the comparison,
		// and the second row is the whole point: a host that tolerates twelve does
		// not make somebody who asked for one open twelve.
		{"a resolver cap cuts a larger count down", 12, 0, []int{4}, 4},
		{"a resolver cap never lifts a smaller one", 1, 0, []int{12}, 1},
		{"a resolver cap cuts the global down too", 0, 16, []int{6}, 6},
		{"a resolver cap cuts the default down too", 0, 0, []int{2}, 2},
		// The user's 1 for a hoster that bans multi-connection, which is the case
		// the ceiling reading exists for.
		{"one chunk for a hoster that bans them survives", 1, 8, []int{8}, 1},

		// More ceilings than one, because the per-host and account-tier caps join
		// the same argument list rather than growing a branch of their own.
		{"the lowest ceiling wins", 16, 0, []int{12, 3, 8}, 3},
		{"a zero ceiling is a caller with nothing to say", 6, 0, []int{0, 0}, 6},

		{"a stored count past the engine's bound is cut", 99, 0, nil, rules.MaxChunks},
		{"so is a global past it", 0, 99, nil, rules.MaxChunks},
		{"and so is a resolver asking for too many", 0, 0, []int{99}, defaultConns},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := settings.Settings{Chunks: tc.global}
			got := connsFor(&core.Task{Chunks: tc.task}, cfg, tc.ceilings...)
			if got != tc.want {
				t.Errorf("connsFor = %d, want %d", got, tc.want)
			}
		})
	}
}

// TestAHandEditOutranksTheRuleThatSetTheChunkCount is the half of the precedence
// a table cannot show, because both terms land on the same field.
//
// The Packagizer writes its count as the link is staged; an edit on the row
// happens afterwards and writes over it. That ordering IS the precedence - there
// is no second field, and adding one would be a place for the two to disagree.
// So the claim worth testing is that neither write is dropped: the rule's number
// reaches a task nobody has touched, and the user's number reaches one they have.
func TestAHandEditOutranksTheRuleThatSetTheChunkCount(t *testing.T) {
	a, _ := newRuleApp(t, func(s *settings.Settings, _ string) {
		// A global that differs from both, so a row falling back to it is visible
		// as a wrong answer rather than passing by coincidence.
		s.Chunks = 16
		chunks := 6
		s.Packagizer = rules.Set{Rules: []rules.Rule{{
			Name:       "this hoster throttles",
			Conditions: []rules.Condition{{Field: rules.FieldHoster, Op: rules.OpEquals, Value: "films.example"}},
			Action:     rules.Action{Chunks: &chunks},
		}}}
	})

	created := a.AddLinks([]string{"https://films.example/one.mkv"}, "")
	if len(created) != 1 {
		t.Fatalf("staged %d tasks", len(created))
	}
	staged := created[0]
	cfg := a.Settings.Get()
	if got := connsFor(staged, cfg); got != 6 {
		t.Errorf("a staged link opens %d connections, want the rule's 6", got)
	}

	byHand := 2
	if err := a.SetTaskOptions([]string{staged.ID}, TaskOptions{Chunks: &byHand}); err != nil {
		t.Fatal(err)
	}
	edited := liveTask(a, staged.ID)
	if got := connsFor(&edited, cfg); got != 2 {
		t.Errorf("after the edit it opens %d, want the 2 that was typed on the row", got)
	}

	// And back to nothing: a cleared box is the global again, which is what makes
	// the override reversible. Set to zero it must not mean "no connections".
	none := 0
	if err := a.SetTaskOptions([]string{staged.ID}, TaskOptions{Chunks: &none}); err != nil {
		t.Fatal(err)
	}
	cleared := liveTask(a, staged.ID)
	if got := connsFor(&cleared, cfg); got != 16 {
		t.Errorf("a cleared override opens %d, want the global 16", got)
	}
}
