package execx

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// hasLine reports whether out has a line that reads want, give or take the
// spaces cmd.exe leaves around it.
func hasLine(out, want string) bool {
	for _, l := range strings.Split(strings.ReplaceAll(out, "\r\n", "\n"), "\n") {
		if strings.TrimSpace(l) == want {
			return true
		}
	}
	return false
}

// cmd.exe reads a batch file's command line the way a shell does, so every
// argument below would run a command or expand a variable if it reached
// cmd.exe as Go quotes an argument for an ordinary program.
func TestABatchFileGetsEachArgumentAsText(t *testing.T) {
	bat := filepath.Join(t.TempDir(), "after.bat")
	script := "@echo off\r\necho one=[%1]\r\necho two=[%2]\r\necho three=[%3]\r\necho four=[%4]\r\necho five=[%5]\r\n"
	if err := os.WriteFile(bat, []byte(script), 0o644); err != nil {
		t.Fatal(err)
	}
	args := []string{
		"Show.S01E01&echo.INJECTED&.mkv",
		"%EXECX_MARK%.mkv",
		`a b" & echo INJECTED & "`,
		"100%",
		"plain",
	}
	env := append(os.Environ(), "EXECX_MARK=expanded")

	out, err := Run(context.Background(), bat, args, env)
	if err != nil {
		t.Fatalf("the batch file did not run: %v\n%s", err, out)
	}
	if hasLine(out, "INJECTED") {
		t.Errorf("cmd.exe ran a command out of an argument:\n%s", out)
	}
	if strings.Contains(out, "expanded") {
		t.Errorf("cmd.exe expanded a variable in an argument:\n%s", out)
	}
	for _, want := range []string{
		`one=["Show.S01E01&echo.INJECTED&.mkv"]`,
		`two=["%EXECX_MARK%.mkv"]`,
		`three=["a b"" & echo INJECTED & """]`,
		`four=["100%"]`,
		`five=[plain]`,
	} {
		if !hasLine(out, want) {
			t.Errorf("the batch file did not see %s; it printed:\n%s", want, out)
		}
	}
}

func TestABatchFileIsNotHandedALineBreak(t *testing.T) {
	bat := filepath.Join(t.TempDir(), "after.cmd")
	if err := os.WriteFile(bat, []byte("@echo off\r\necho ran\r\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := Run(context.Background(), bat, []string{"first line\r\necho INJECTED"}, os.Environ())
	if err == nil {
		t.Fatalf("an argument with a line break was passed to cmd.exe; it printed:\n%s", out)
	}
	if strings.Contains(out, "ran") || strings.Contains(out, "INJECTED") {
		t.Errorf("the batch file ran anyway:\n%s", out)
	}
}
