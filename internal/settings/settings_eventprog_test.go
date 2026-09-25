package settings

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/junkerderprovinz/knightloader/internal/eventprog"
	"github.com/junkerderprovinz/knightloader/internal/idleaction"
	"github.com/junkerderprovinz/knightloader/internal/script"
)

func programWithToken() eventprog.Program {
	return eventprog.Program{
		ID: "1", Name: "post-process", Enabled: true,
		Command: idleaction.CommandSpec{
			Program:        "/home/someone/bin/after.sh",
			Args:           []string{"--token=real-token", "%%file%%"},
			TimeoutSeconds: 60,
		},
		Triggers: []script.Trigger{script.TriggerTaskDone},
	}
}

func TestAFreshInstallStartsNoProgramAndNamesNone(t *testing.T) {
	if Defaults().EventPrograms != nil {
		t.Fatalf("a fresh install starts with %d event program(s)", len(Defaults().EventPrograms))
	}
	dir := t.TempDir()
	st, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.Set(Defaults()); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(dir, "settings.json"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), "eventPrograms") {
		t.Errorf("a fresh settings.json names eventPrograms:\n%s", b)
	}
}

func TestAnEventProgramsCommandLineSurvivesTheRedactedRoundTrip(t *testing.T) {
	dir := t.TempDir()
	st, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	n := Defaults()
	n.EventPrograms = []eventprog.Program{programWithToken()}
	saved, err := st.Set(n)
	if err != nil {
		t.Fatal(err)
	}

	shown := saved.Redacted()
	cmd := shown.EventPrograms[0].Command
	if cmd.Program != idleaction.RedactedCommand || strings.Contains(strings.Join(cmd.Args, " "), "real-token") {
		t.Fatalf("the browser is served %+v", cmd)
	}
	if saved.EventPrograms[0].Command.Program != programWithToken().Command.Program {
		t.Fatal("Redacted() reached the stored copy")
	}

	shown.MaxConcurrent = 7
	back, err := st.Set(shown)
	if err != nil {
		t.Fatal(err)
	}
	got := back.EventPrograms[0].Command
	want := programWithToken().Command
	if got.Program != want.Program || strings.Join(got.Args, " ") != strings.Join(want.Args, " ") {
		t.Errorf("after a save from a page that was shown stars the command is %+v, want %+v", got, want)
	}
}

func TestKeepAwakeIsOnForAnInstallThatNeverSawIt(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "settings.json"), []byte(`{"maxConcurrent": 2}`), 0o600); err != nil {
		t.Fatal(err)
	}
	st, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !st.Get().KeepAwake {
		t.Error("KeepAwake is off after an upgrade; the default is on")
	}
}
