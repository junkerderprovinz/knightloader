package eventprog

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/sys/windows"
)

func processGone(pid int) bool {
	proc, err := windows.OpenProcess(windows.SYNCHRONIZE, false, uint32(pid))
	if err != nil {
		return true
	}
	defer windows.CloseHandle(proc)
	event, _ := windows.WaitForSingleObject(proc, 0)
	return event == windows.WAIT_OBJECT_0
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
		"%EVENTPROG_MARK%.mkv",
		`a b" & echo INJECTED & "`,
		"100%",
		"plain",
	}
	env := append(os.Environ(), "EVENTPROG_MARK=expanded")

	out, err := ExecRunner(context.Background(), bat, args, env)
	if err != nil {
		t.Fatalf("the batch file did not run: %v\n%s", err, out)
	}
	lines := strings.Split(strings.ReplaceAll(out, "\r\n", "\n"), "\n")
	for _, l := range lines {
		if strings.TrimSpace(l) == "INJECTED" {
			t.Errorf("cmd.exe ran a command out of an argument:\n%s", out)
		}
	}
	if strings.Contains(out, "expanded") {
		t.Errorf("cmd.exe expanded a variable in an argument:\n%s", out)
	}
	for _, want := range []string{
		`one=["Show.S01E01&echo.INJECTED&.mkv"]`,
		`two=["%EVENTPROG_MARK%.mkv"]`,
		`three=["a b"" & echo INJECTED & """]`,
		`four=["100%"]`,
		`five=[plain]`,
	} {
		found := false
		for _, l := range lines {
			if strings.TrimSpace(l) == want {
				found = true
			}
		}
		if !found {
			t.Errorf("the batch file did not see %s; it printed:\n%s", want, out)
		}
	}
}

func TestABatchFileIsNotHandedALineBreak(t *testing.T) {
	bat := filepath.Join(t.TempDir(), "after.cmd")
	if err := os.WriteFile(bat, []byte("@echo off\r\necho ran\r\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := ExecRunner(context.Background(), bat, []string{"first line\r\necho INJECTED"}, os.Environ())
	if err == nil {
		t.Fatalf("an argument with a line break was passed to cmd.exe; it printed:\n%s", out)
	}
	if strings.Contains(out, "ran") || strings.Contains(out, "INJECTED") {
		t.Errorf("the batch file ran anyway:\n%s", out)
	}
}
