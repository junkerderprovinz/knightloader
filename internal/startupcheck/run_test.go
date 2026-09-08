package startupcheck

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// ids is the report as a list of row ids, in order, which is what most of these
// assertions are really about: every target handed in came back, and the cheap
// bounded rows came back before the ones that can wait on a machine.
func ids(rep Report) []string {
	out := make([]string, 0, len(rep.Checks))
	for _, c := range rep.Checks {
		out = append(out, c.ID)
	}
	return out
}

func sameStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// TestRunAnswersForEveryTargetInTheSafeOrder.
//
// The order is not cosmetic. The data directory is local and answers instantly;
// the tools are each bounded by their own deadline; the clock costs nothing; the
// configured folders are the only rows that can wait on a mount that has gone
// away, so they go last and only they can be eaten by the total deadline.
func TestRunAnswersForEveryTargetInTheSafeOrder(t *testing.T) {
	base := t.TempDir()

	rep := Run(context.Background(), Input{
		Data: FolderTarget{Dir: base, Role: RoleWork},
		Tools: []ToolTarget{
			{ID: IDJava, SkipCode: "javaNotNeeded"},
			{ID: IDFfprobe, Bin: "kl-no-such-binary-8f2ca1", Args: []string{"-version"}, Optional: true},
		},
		Folders: []FolderTarget{
			{Dir: base, Role: RoleDownloads},
			{Dir: filepath.Join(base, "gone"), Role: RoleCategory},
		},
	})

	want := []string{IDData, IDJava, IDFfprobe, IDClock, IDFolder, IDFolder}
	if got := ids(rep); !sameStrings(got, want) {
		t.Errorf("rows = %v, want %v", got, want)
	}
	if rep.State != StateDone {
		t.Errorf("state = %q, want %q", rep.State, StateDone)
	}
	if rep.Probed {
		t.Error("probed = true on a pass that was never asked to write")
	}
	if rep.FinishedAt.Before(rep.StartedAt) {
		t.Errorf("finishedAt %s is before startedAt %s", rep.FinishedAt, rep.StartedAt)
	}
	if rep.Checks[4].Role != RoleDownloads || rep.Checks[5].Role != RoleCategory {
		t.Errorf("the folder rows lost their roles: %q, %q", rep.Checks[4].Role, rep.Checks[5].Role)
	}
}

// TestRunTimesOutIntoRowsRatherThanIntoSilence.
//
// A pass that ran out of time must still answer for everything it was given. An
// absent row is invisible - it reads exactly like a folder nobody configured -
// and a timeout row is a finding somebody can act on.
//
// Driven from a cancelled parent rather than from a tiny Total, because Total
// alone is a race: a context deadline of a nanosecond still fires on a timer,
// and Windows' timer resolution is coarse enough that two stats against a local
// temp folder finish first and the test passes for the wrong reason. A cancelled
// parent is also a state the program genuinely reaches - a boot pass still
// running when Close cancels a.ctx arrives here exactly like this.
func TestRunTimesOutIntoRowsRatherThanIntoSilence(t *testing.T) {
	base := t.TempDir()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	rep := Run(ctx, Input{
		Data:    FolderTarget{Dir: base},
		Folders: []FolderTarget{{Dir: base, Role: RoleDownloads}, {Dir: base, Role: RoleWork}},
		Tools:   []ToolTarget{{ID: IDJava, SkipCode: "javaNotNeeded"}},
	})

	if len(rep.Checks) != 5 {
		t.Fatalf("rows = %v, want one for every target even after the deadline blew", ids(rep))
	}
	for _, c := range rep.Checks {
		switch c.ID {
		case IDFolder, IDData:
			if c.Code != CodeTimeout {
				t.Errorf("folder row %q came back with code %q, want %q", c.Subject, c.Code, CodeTimeout)
			}
		}
	}
}

// TestRunAppliesItsTotalDeadline proves the budget is a real one rather than a
// field nothing reads: with a Total of a nanosecond and a real subprocess in
// front of them, every folder row is past the deadline by the time it is
// reached. The subprocess is what makes it deterministic - spawning a process
// costs far more than any timer resolution this could otherwise race.
func TestRunAppliesItsTotalDeadline(t *testing.T) {
	base := t.TempDir()

	rep := Run(context.Background(), Input{
		Tools:     []ToolTarget{helperTool(t, IDYtdlp, "version")},
		Folders:   []FolderTarget{{Dir: base, Role: RoleDownloads}},
		SkipClock: true,
		Total:     time.Nanosecond,
	})

	if len(rep.Checks) != 2 {
		t.Fatalf("rows = %v, want both targets", ids(rep))
	}
	if got := rep.Checks[1]; got.Code != CodeTimeout {
		t.Errorf("the folder row came back with code %q after the total ran out, want %q", got.Code, CodeTimeout)
	}
}

// TestRunNeverAnswersWithANilCheckList. A nil slice encodes as JSON null and the
// page that walks it throws instead of drawing nothing; app_diskreport.go
// initialises its own list for exactly this.
func TestRunNeverAnswersWithANilCheckList(t *testing.T) {
	rep := Run(context.Background(), Input{SkipClock: true})
	if rep.Checks == nil {
		t.Fatal("checks is nil")
	}
	b, err := json.Marshal(rep)
	if err != nil {
		t.Fatal(err)
	}
	var back struct {
		Checks []Check `json:"checks"`
	}
	if err := json.Unmarshal(b, &back); err != nil {
		t.Fatal(err)
	}
	if back.Checks == nil {
		t.Errorf("an empty report encodes its check list as null: %s", b)
	}
}

// TestRunProbesOnlyWhenAsked is the owner's decision at the level of a whole
// pass: the boot writes nothing anywhere, and the human press writes into every
// folder that is there.
func TestRunProbesOnlyWhenAsked(t *testing.T) {
	base := t.TempDir()
	one := filepath.Join(base, "one")
	two := filepath.Join(base, "two")
	for _, d := range []string{one, two} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}

	boot := Run(context.Background(), Input{
		Folders:   []FolderTarget{{Dir: one, Role: RoleDownloads}, {Dir: two, Role: RoleWork}},
		SkipClock: true,
	})
	for _, c := range boot.Checks {
		if c.Probed {
			t.Errorf("the boot pass wrote into %q", c.Subject)
		}
	}
	for _, d := range []string{one, two} {
		if got := entries(t, d); len(got) != 0 {
			t.Errorf("the boot pass left %v in %s", got, d)
		}
	}

	pressed := Run(context.Background(), Input{
		Folders:   []FolderTarget{{Dir: one, Role: RoleDownloads}, {Dir: two, Role: RoleWork}},
		Probe:     true,
		SkipClock: true,
	})
	if !pressed.Probed {
		t.Error("report.probed = false on a pass that was asked to write")
	}
	for _, c := range pressed.Checks {
		if !c.Probed {
			t.Errorf("%q was not written to on a pass that was asked to", c.Subject)
		}
	}
	for _, d := range []string{one, two} {
		if got := entries(t, d); len(got) != 0 {
			t.Errorf("the probe file was left behind in %s: %v", d, got)
		}
	}
}
