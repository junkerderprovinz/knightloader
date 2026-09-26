package eventprog

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/junkerderprovinz/knightloader/internal/idleaction"
	"github.com/junkerderprovinz/knightloader/internal/script"
)

func stored() Program {
	return Program{
		ID: "1", Name: "post-process", Enabled: true,
		Command: idleaction.CommandSpec{
			Program:        "/home/someone/bin/after-download.sh",
			Args:           []string{"--token=abcdefghijkl", "%%file%%"},
			TimeoutSeconds: 60,
		},
		Triggers: []script.Trigger{script.TriggerTaskDone},
	}
}

func TestAnUntouchedNewRowIsDroppedAndANamedOneKept(t *testing.T) {
	got := Sanitize([]Program{
		{},
		{Name: "  halfway  "},
	})
	if len(got) != 1 {
		t.Fatalf("Sanitize kept %d rows, want the named one only: %+v", len(got), got)
	}
	if got[0].Name != "halfway" || got[0].ID == "" {
		t.Errorf("the named row came back as %+v, want its name trimmed and an ID given", got[0])
	}
}

func TestAFreshRowGetsTheCommandDefaultsAndNoUnknownEvents(t *testing.T) {
	got := Sanitize([]Program{{
		Name:     "x",
		Command:  idleaction.CommandSpec{Program: " /bin/true "},
		Triggers: []script.Trigger{script.TriggerTaskDone, "no.such.event", script.TriggerTaskDone},
		Parallel: 99,
	}})[0]
	if got.Command.Program != "/bin/true" {
		t.Errorf("Program = %q, want it trimmed", got.Command.Program)
	}
	if got.Command.TimeoutSeconds != idleaction.DefaultCommandTimeout {
		t.Errorf("TimeoutSeconds = %d, want the end-of-queue command's default %d", got.Command.TimeoutSeconds, idleaction.DefaultCommandTimeout)
	}
	if len(got.Triggers) != 1 || got.Triggers[0] != script.TriggerTaskDone {
		t.Errorf("Triggers = %v, want task.done once", got.Triggers)
	}
	if got.Parallel != MaxParallel {
		t.Errorf("Parallel = %d, want it held to %d", got.Parallel, MaxParallel)
	}
}

// manual runs one script by hand and never reaches the bus, so a program
// ticked for it alone would look set up and never start.
func TestAProgramKeepsNoEventThatNeverArrives(t *testing.T) {
	got := Sanitize([]Program{{
		Name:     "x",
		Triggers: []script.Trigger{script.TriggerOnDemand, script.TriggerQueueIdle},
	}})[0]
	if len(got.Triggers) != 1 || got.Triggers[0] != script.TriggerQueueIdle {
		t.Errorf("Triggers = %v, want queue.idle alone", got.Triggers)
	}
}

func TestZeroParallelMeansOneRunAtATime(t *testing.T) {
	if n := (Program{}).ResolvedParallel(); n != 1 {
		t.Errorf("ResolvedParallel() = %d for an unset row, want 1", n)
	}
}

func TestAnIDSurvivesTheRowAboveItBeingDeleted(t *testing.T) {
	list := Sanitize([]Program{{Name: "a"}, {Name: "b"}, {Name: "c"}})
	kept := Sanitize(list[1:])
	if kept[0].ID != list[1].ID || kept[1].ID != list[2].ID {
		t.Errorf("IDs moved with the positions: before %s,%s after %s,%s", list[1].ID, list[2].ID, kept[0].ID, kept[1].ID)
	}
	added := Sanitize(append(kept, Program{Name: "d"}))
	seen := map[string]bool{}
	for _, p := range added {
		if seen[p.ID] {
			t.Fatalf("two rows share the ID %s: %+v", p.ID, added)
		}
		seen[p.ID] = true
	}
}

func TestTheBrowserIsNeverShownTheCommandLine(t *testing.T) {
	b, err := json.Marshal(Redacted(stored()))
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{"after-download.sh", "abcdefghijkl", "someone"} {
		if strings.Contains(string(b), secret) {
			t.Errorf("the redacted row still carries %q: %s", secret, b)
		}
	}
}

func TestSendingTheStarsBackKeepsTheStoredCommandLine(t *testing.T) {
	prev := []Program{stored()}
	shown := Redacted(stored())
	shown.Name = "renamed"

	got := Merge([]Program{shown}, prev)[0]
	if got.Command.Program != prev[0].Command.Program {
		t.Errorf("Program = %q, want the stored one back", got.Command.Program)
	}
	if strings.Join(got.Command.Args, " ") != strings.Join(prev[0].Command.Args, " ") {
		t.Errorf("Args = %v, want the stored ones back", got.Command.Args)
	}
	if got.Name != "renamed" {
		t.Errorf("the edit to the name was lost: %q", got.Name)
	}
}

func TestStarsWithNothingBehindThemAreClearedNotRun(t *testing.T) {
	orphan := Redacted(stored())
	orphan.ID = "7"
	got := Merge([]Program{orphan}, []Program{stored()})[0]
	if got.Command.Program != "" {
		t.Errorf("Program = %q; a row with no stored counterpart would run a program called %q", got.Command.Program, idleaction.RedactedCommand)
	}
	if len(got.Command.Args) != 0 {
		t.Errorf("Args = %v, want the placeholders gone", got.Command.Args)
	}
}

func TestRetypingTheProgramKeepsTheStoredArguments(t *testing.T) {
	shown := Redacted(stored())
	shown.Command.Program = "/usr/local/bin/other"
	got := Merge([]Program{shown}, []Program{stored()})[0]
	if got.Command.Program != "/usr/local/bin/other" {
		t.Errorf("Program = %q, want the retyped one", got.Command.Program)
	}
	if len(got.Command.Args) != 2 || got.Command.Args[0] != "--token=abcdefghijkl" {
		t.Errorf("Args = %v, want the stored ones", got.Command.Args)
	}
}

func TestADisabledRowWantsNothing(t *testing.T) {
	p := stored()
	if !p.Wants(script.TriggerTaskDone) {
		t.Fatal("an enabled row does not want the event it ticked")
	}
	if p.Wants(script.TriggerTaskFailed) {
		t.Error("the row wants an event it did not tick")
	}
	p.Enabled = false
	if p.Wants(script.TriggerTaskDone) || p.Runnable() {
		t.Error("a switched-off row still wants its event")
	}
}

func TestARemovedRowsIDIsNeverHandedToANewOne(t *testing.T) {
	list := Sanitize([]Program{{Name: "a"}, {Name: "b"}})
	later := Sanitize([]Program{list[1], {Name: "c"}})
	if later[1].ID == list[0].ID {
		t.Errorf("the new row took the removed row's ID %s, and with it the removed row's command line", list[0].ID)
	}
}

func TestOnlyAnIDInTheShapeTheServerGivesOutIsKept(t *testing.T) {
	given := Sanitize([]Program{{Name: "given"}})[0].ID
	got := Sanitize([]Program{
		{ID: "1", Name: "numbered"},
		{ID: "a\nevent program forged a log line", Name: "two lines"},
		{ID: strings.ToUpper(given), Name: "upper case"},
		{ID: given, Name: "given"},
	})
	for _, p := range got[:3] {
		if !wellFormed(p.ID) || p.ID == given {
			t.Errorf("row %q kept or got the ID %q, want a new one", p.Name, p.ID)
		}
	}
	if got[3].ID != given {
		t.Errorf("the row the server had named came back as %q, want %q", got[3].ID, given)
	}
}

// An API client that numbers its rows, here and on another instance, must not
// have one row's stars filled in from another row that got the same number.
func TestANumberedRowDoesNotRunAnotherNumberedRowsProgram(t *testing.T) {
	here := Sanitize([]Program{{
		ID: "1", Name: "delete sources", Enabled: true,
		Command:  idleaction.CommandSpec{Program: "/home/me/bin/delete-rars.sh", Args: []string{"%%folder%%"}},
		Triggers: []script.Trigger{script.TriggerExtractDone},
	}})
	imported := Redacted(Program{
		ID: "1", Name: "notify NAS on failure", Enabled: true,
		Command:  idleaction.CommandSpec{Program: "/usr/local/bin/notify-nas"},
		Triggers: []script.Trigger{script.TriggerTaskFailed},
	})

	got := Sanitize(Merge([]Program{imported}, here))[0]
	if got.Command.Program != "" {
		t.Errorf("the imported row runs %q", got.Command.Program)
	}
}

// Importing another instance's export without secrets brings rows with stars
// for a command line. Their IDs were handed out over there, so they must not
// fetch the command line of whichever row holds the same ID here.
func TestARowFromAnotherInstanceDoesNotRunThisOnesProgram(t *testing.T) {
	here := Sanitize([]Program{{
		Name: "delete sources", Enabled: true,
		Command:  idleaction.CommandSpec{Program: "/home/me/bin/delete-rars.sh", Args: []string{"%%folder%%"}},
		Triggers: []script.Trigger{script.TriggerExtractDone},
	}})
	there := Sanitize([]Program{{
		Name: "notify NAS on failure", Enabled: true,
		Command:  idleaction.CommandSpec{Program: "/usr/local/bin/notify-nas"},
		Triggers: []script.Trigger{script.TriggerTaskFailed},
	}})
	imported := Redacted(there[0])

	got := Merge([]Program{imported}, here)[0]
	if got.Command.Program != "" {
		t.Errorf("the imported row runs %q", got.Command.Program)
	}
}
