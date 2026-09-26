package eventprog

import (
	"strings"
	"testing"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/idleaction"
	"github.com/junkerderprovinz/knightloader/internal/script"
)

// shellBait is a name every shell would do something with: a command
// substitution, a second command, a pipe, a redirect, quotes and a glob.
const shellBait = `Movie $(touch pwned) ; rm -rf x | tee y > z "q" 'r' *.mkv`

func doneFiring(name string) script.Firing {
	return script.Firing{
		Trigger: script.TriggerTaskDone,
		At:      time.Now(),
		Task:    &script.TaskView{ID: "t1", Name: name, Package: "Pack", Host: "example.invalid"},
	}
}

func envOf(env []string) map[string]string {
	out := map[string]string{}
	for _, kv := range env {
		k, v, _ := strings.Cut(kv, "=")
		out[k] = v
	}
	return out
}

func TestThePlaceholdersAndTheVariablesCarryTheSameEvent(t *testing.T) {
	f := doneFiring("film.mkv")
	w := Where{File: "/downloads/Pack/film.mkv", Folder: "/downloads/Pack", Category: "Films"}
	v := ValuesOf(f, w)

	cmd := idleaction.CommandSpec{Args: []string{
		"%%event%%", "%%task.id%%", "%%name%%", "%%file%%", "%%folder%%", "%%package%%", "%%category%%",
		"--host=%%task.host%%", "%%instance%%", "%%nosuch%%",
	}}
	got := Args(cmd, f, v, "box")
	want := []string{
		"task.done", "t1", "film.mkv", "/downloads/Pack/film.mkv", "/downloads/Pack", "Pack", "Films",
		"--host=example.invalid", "box", "%%nosuch%%",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("Args =\n%q\nwant\n%q", got, want)
	}

	env := envOf(Environ(v))
	for name, want := range map[string]string{
		EnvEvent: "task.done", EnvTaskID: "t1", EnvName: "film.mkv", EnvFile: w.File,
		EnvFolder: w.Folder, EnvPackage: "Pack", EnvCategory: "Films",
	} {
		if env[name] != want {
			t.Errorf("%s = %q, want %q", name, env[name], want)
		}
	}
}

func TestEveryVariableIsSetEvenWhenTheEventHasNothingForIt(t *testing.T) {
	env := Environ(ValuesOf(script.Firing{Trigger: script.TriggerQueueIdle}, Where{}))
	got := envOf(env)
	for _, name := range []string{EnvEvent, EnvTaskID, EnvName, EnvFile, EnvFolder, EnvPackage, EnvCategory, EnvExtractOK} {
		if _, ok := got[name]; !ok {
			t.Errorf("%s is missing; a script reading it would find it unset rather than empty", name)
		}
	}
	if got[EnvEvent] != "queue.idle" {
		t.Errorf("%s = %q", EnvEvent, got[EnvEvent])
	}
}

func TestAFileNameFullOfShellSyntaxStaysOneLiteralArgument(t *testing.T) {
	f := doneFiring(shellBait)
	got := Args(idleaction.CommandSpec{Args: []string{"%%name%%", "--in=%%file%%"}}, f, ValuesOf(f, Where{File: "/d/" + shellBait}), "")
	if len(got) != 2 {
		t.Fatalf("Args split the name into %d arguments: %q", len(got), got)
	}
	if got[0] != shellBait || got[1] != "--in=/d/"+shellBait {
		t.Errorf("Args = %q, want the name exactly as it was", got)
	}
}

func TestAValueThatLooksLikeAPlaceholderIsNotExpandedAgain(t *testing.T) {
	f := doneFiring("%%file%%")
	got := Args(idleaction.CommandSpec{Args: []string{"%%name%%"}}, f, ValuesOf(f, Where{File: "/secret/path"}), "")
	if got[0] != "%%file%%" {
		t.Errorf("Args = %q; a name that reads like a placeholder was expanded a second time", got)
	}
}

func TestAnUnpackingIsNamedAfterItsArchiveAndAPackageAfterItself(t *testing.T) {
	ex := script.Firing{
		Trigger: script.TriggerExtractDone,
		Extract: &script.ExtractView{Name: "set.part1.rar", Package: "Set", Dir: "/out"},
		Task:    &script.TaskView{ID: "t9", Name: "set.part1.rar", Package: "Set"},
	}
	if v := ValuesOf(ex, Where{}); v.Name != "set.part1.rar" || v.Package != "Set" || v.TaskID != "t9" {
		t.Errorf("extract.done gave %+v", v)
	}
	pkg := script.Firing{Trigger: script.TriggerPackageDone, Package: &script.PackageView{Name: "Series S01"}}
	if v := ValuesOf(pkg, Where{Folder: "/downloads/Series S01"}); v.Name != "Series S01" || v.Package != "Series S01" || v.TaskID != "" {
		t.Errorf("package.done gave %+v", v)
	}
}

func TestThePickerOffersEveryNameTheExpanderFillsIn(t *testing.T) {
	offered := map[string]bool{}
	for _, p := range Placeholders() {
		offered[p.Name] = true
	}
	for _, n := range ownNames {
		if !offered[n.name] {
			t.Errorf("%%%%%s%%%% is filled in but not offered", n.name)
		}
	}
	for _, shared := range []string{"event", "task.id", "task.name", "instance"} {
		if !offered[shared] {
			t.Errorf("%%%%%s%%%% from the event targets' table is not offered", shared)
		}
	}
}

func TestAnUnpackingSaysWhetherItWorked(t *testing.T) {
	failed := script.Firing{
		Trigger: script.TriggerExtractDone,
		Extract: &script.ExtractView{Name: "set.part1.rar", OK: false},
	}
	if got := envOf(Environ(ValuesOf(failed, Where{})))[EnvExtractOK]; got != "false" {
		t.Errorf("%s = %q for a failed unpacking, want false", EnvExtractOK, got)
	}
	failed.Extract.OK = true
	if got := envOf(Environ(ValuesOf(failed, Where{})))[EnvExtractOK]; got != "true" {
		t.Errorf("%s = %q for an unpacking that worked, want true", EnvExtractOK, got)
	}
	if got := envOf(Environ(ValuesOf(doneFiring("a.mkv"), Where{})))[EnvExtractOK]; got != "" {
		t.Errorf("%s = %q for a finished download, want it empty", EnvExtractOK, got)
	}
}
