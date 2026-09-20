package app

// The connection count: one decision with four inputs, written as a table.

import (
	"testing"

	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/rules"
	"github.com/junkerderprovinz/knightloader/internal/settings"
)

// A resolver's count is what the host tolerates, so it can cut a number but
// never raise one, and a zero anywhere means no opinion, not no connections.
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
		// Zero on the task hands the decision back.
		{"zero on the task means the global, not none", 0, 8, nil, 8},
		{"zero on the task and no global means the default", 0, 0, nil, defaultConns},

		// A resolver's count is only a ceiling: a host that tolerates twelve does
		// not make a request for one open twelve.
		{"a resolver cap cuts a larger count down", 12, 0, []int{4}, 4},
		{"a resolver cap never lifts a smaller one", 1, 0, []int{12}, 1},
		{"a resolver cap cuts the global down too", 0, 16, []int{6}, 6},
		{"a resolver cap cuts the default down too", 0, 0, []int{2}, 2},
		// The user's 1 for a hoster that bans multiple connections.
		{"one chunk for a hoster that bans them survives", 1, 8, []int{8}, 1},

		// Per-host and account-tier caps join the same list of ceilings.
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

// The Packagizer writes its count at staging and a hand edit writes over it
// later, on the same field; neither write may be dropped.
func TestAHandEditOutranksTheRuleThatSetTheChunkCount(t *testing.T) {
	a, _ := newRuleApp(t, func(s *settings.Settings, _ string) {
		// A global that differs from both, so falling back to it shows.
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

	// Cleared to zero, it falls back to the global rather than meaning none.
	none := 0
	if err := a.SetTaskOptions([]string{staged.ID}, TaskOptions{Chunks: &none}); err != nil {
		t.Fatal(err)
	}
	cleared := liveTask(a, staged.ID)
	if got := connsFor(&cleared, cfg); got != 16 {
		t.Errorf("a cleared override opens %d, want the global 16", got)
	}
}
