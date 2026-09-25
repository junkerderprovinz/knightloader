package eventprog

import (
	"bytes"
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/idleaction"
	"github.com/junkerderprovinz/knightloader/internal/script"
)

// helperEnv marks the test binary as started by a test here, to play the
// configured program. The name has no KL_ prefix, so Environ passes it on.
const helperEnv = "EVENTPROG_TEST_HELPER"

// TestHelperProcess is the program the tests below start. It does nothing in
// an ordinary test run. Its first argument after "--" says what to do:
//
//	echo ARGS...   print every argument and every KL_ variable, one per line
//	exit N TEXT    print TEXT and exit with status N
//	sleep          wait far longer than any test allows
//	spawn          start a sleeping child, print "child:PID" and sleep too,
//	               like a shell that put a command in the background
func TestHelperProcess(t *testing.T) {
	if os.Getenv(helperEnv) != "1" {
		return
	}
	args := os.Args
	for i, a := range args {
		if a == "--" {
			args = args[i+1:]
			break
		}
	}
	switch args[0] {
	case "echo":
		for _, a := range args[1:] {
			fmt.Println("arg:" + a)
		}
		for _, kv := range os.Environ() {
			if strings.HasPrefix(strings.ToUpper(kv), "KL_") {
				fmt.Println("env:" + kv)
			}
		}
		os.Exit(0)
	case "exit":
		code, _ := strconv.Atoi(args[1])
		fmt.Println(strings.Join(args[2:], " "))
		os.Exit(code)
	case "sleep":
		time.Sleep(time.Minute)
		os.Exit(0)
	case "spawn":
		child := exec.Command(os.Args[0], "-test.run=^TestHelperProcess$", "--", "sleep")
		if err := child.Start(); err != nil {
			fmt.Println(err)
			os.Exit(3)
		}
		fmt.Printf("child:%d\n", child.Process.Pid)
		time.Sleep(time.Minute)
		os.Exit(0)
	}
	os.Exit(2)
}

// helperProgram is a row that starts the test binary as TestHelperProcess.
func helperProgram(t *testing.T, timeoutSeconds int, args ...string) Program {
	t.Helper()
	t.Setenv(helperEnv, "1")
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	return Program{
		ID: "1", Name: "helper", Enabled: true,
		Command: idleaction.CommandSpec{
			Program:        self,
			Args:           append([]string{"-test.run=^TestHelperProcess$", "--"}, args...),
			TimeoutSeconds: timeoutSeconds,
		},
		Triggers: []script.Trigger{script.TriggerTaskDone},
	}
}

// captureLog sends the standard logger to a buffer for the rest of the test.
func captureLog(t *testing.T) *syncBuffer {
	t.Helper()
	var buf syncBuffer
	was := log.Writer()
	log.SetOutput(&buf)
	t.Cleanup(func() { log.SetOutput(was) })
	return &buf
}

type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.buf.Write(p)
}

func (s *syncBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.buf.String()
}

// runOnce sends one firing through a dispatcher with this row and waits for
// the run to be recorded.
func runOnce(t *testing.T, p Program, f script.Firing, where Where) Health {
	t.Helper()
	d := New(Options{
		InstanceName: func() string { return "box" },
		Locate:       func(script.Firing) Where { return where },
	})
	defer d.Close()
	d.Set([]Program{p})
	d.On(f)
	var h Health
	waitFor(t, "the run to be recorded", func() bool {
		h = healthOf(d, p.ID)
		return h.Runs > 0
	})
	return h
}

func TestTheProgramGetsItsArgumentsAndVariablesWithoutAShell(t *testing.T) {
	t.Setenv("KL_TORBOX", "service-key-that-must-stay-here")
	p := helperProgram(t, 30, "echo", "%%name%%", "%%file%%", "a b")
	file := "/downloads/Pack/" + shellBait
	h := runOnce(t, p, doneFiring(shellBait), Where{File: file, Folder: "/downloads/Pack", Category: "Films"})

	if h.LastProblem != idleaction.ProblemNone {
		t.Fatalf("the run failed (%s, exit %d): %s", h.LastProblem, h.LastExitCode, h.LastOutput)
	}
	lines := strings.Split(strings.ReplaceAll(h.LastOutput, "\r\n", "\n"), "\n")
	has := func(line string) bool {
		for _, l := range lines {
			if l == line {
				return true
			}
		}
		return false
	}
	for _, want := range []string{
		"arg:" + shellBait,
		"arg:" + file,
		"arg:a b",
		"env:KL_EVENT=task.done",
		"env:KL_TASK_ID=t1",
		"env:KL_NAME=" + shellBait,
		"env:KL_FILE=" + file,
		"env:KL_FOLDER=/downloads/Pack",
		"env:KL_PACKAGE=Pack",
		"env:KL_CATEGORY=Films",
	} {
		if !has(want) {
			t.Errorf("the program did not see %q; it printed:\n%s", want, h.LastOutput)
		}
	}
	if strings.Contains(h.LastOutput, "service-key-that-must-stay-here") {
		t.Errorf("the instance's own KL_TORBOX reached the program:\n%s", h.LastOutput)
	}
	if _, err := os.Stat("pwned"); err == nil {
		os.Remove("pwned")
		t.Error("the command substitution in the file name was run")
	}
}

func TestAProgramPastItsLimitIsKilledAndReportedAsATimeout(t *testing.T) {
	logs := captureLog(t)
	p := helperProgram(t, 1, "sleep")
	started := time.Now()
	h := runOnce(t, p, doneFiring("a.mkv"), Where{})
	if took := time.Since(started); took > 20*time.Second {
		t.Errorf("the run took %s; the program was not killed at its one second limit", took)
	}
	if h.LastProblem != idleaction.ProblemTimeout {
		t.Errorf("LastProblem = %q, want %q", h.LastProblem, idleaction.ProblemTimeout)
	}
	if h.Failed != 1 {
		t.Errorf("Failed = %d, want 1", h.Failed)
	}
	if !strings.Contains(logs.String(), `event program "helper" on task.done failed (timeout)`) {
		t.Errorf("the timeout was not logged:\n%s", logs.String())
	}
}

func TestTheTimeLimitEndsWhatTheProgramStarted(t *testing.T) {
	t.Setenv(helperEnv, "1")
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	out, _ := ExecRunner(ctx, self, []string{"-test.run=^TestHelperProcess$", "--", "spawn"}, os.Environ())

	m := regexp.MustCompile(`child:(\d+)`).FindStringSubmatch(out)
	if m == nil {
		t.Fatalf("the program did not say which child it started:\n%s", out)
	}
	pid, _ := strconv.Atoi(m[1])
	t.Cleanup(func() {
		if p, err := os.FindProcess(pid); err == nil {
			_ = p.Kill()
		}
	})
	waitFor(t, "the child to end with its parent", func() bool { return processGone(pid) })
}

func TestANonZeroExitIsLoggedWithItsCodeAndOutput(t *testing.T) {
	logs := captureLog(t)
	p := helperProgram(t, 30, "exit", "3", "disk", "is", "full")
	h := runOnce(t, p, doneFiring("a.mkv"), Where{})
	if h.LastProblem != idleaction.ProblemExit || h.LastExitCode != 3 {
		t.Errorf("LastProblem, LastExitCode = %q, %d, want exit, 3", h.LastProblem, h.LastExitCode)
	}
	if !strings.Contains(h.LastOutput, "disk is full") {
		t.Errorf("LastOutput = %q, want the program's own words", h.LastOutput)
	}
	line := logs.String()
	if !strings.Contains(line, `event program "helper" on task.done: exit 3 after`) || !strings.Contains(line, "disk is full") {
		t.Errorf("the log line lacks the exit code or the output:\n%s", line)
	}
	if strings.Contains(line, p.Command.Program) {
		t.Errorf("the log names the program's path, which the diagnostics bundle must not carry:\n%s", line)
	}
}

func TestACleanRunIsLoggedWithExitZero(t *testing.T) {
	logs := captureLog(t)
	h := runOnce(t, helperProgram(t, 30, "exit", "0", "done"), doneFiring("a.mkv"), Where{})
	if h.LastProblem != idleaction.ProblemNone || h.LastOK.IsZero() {
		t.Errorf("a clean run was recorded as %+v", h)
	}
	if !strings.Contains(logs.String(), `event program "helper" on task.done: exit 0 after`) {
		t.Errorf("the clean run was not logged:\n%s", logs.String())
	}
}

func TestAMissingProgramIsReportedAsNotFound(t *testing.T) {
	p := helperProgram(t, 30)
	p.Command.Program = "no-such-program-anywhere-e1f0"
	h := runOnce(t, p, doneFiring("a.mkv"), Where{})
	if h.LastProblem != idleaction.ProblemNotFound {
		t.Errorf("LastProblem = %q, want %q", h.LastProblem, idleaction.ProblemNotFound)
	}
}

func TestOutputBeyondTheCapIsReadAndDropped(t *testing.T) {
	var c cappedBuffer
	chunk := bytes.Repeat([]byte("x"), keptOutput-10)
	for range 3 {
		if n, err := c.Write(chunk); n != len(chunk) || err != nil {
			t.Fatalf("Write = %d, %v; a short write would stop the program's output", n, err)
		}
	}
	if c.buf.Len() != keptOutput {
		t.Errorf("kept %d bytes, want %d", c.buf.Len(), keptOutput)
	}
}

// The output is cut to what a log line carries. A token the program echoes
// right where the cut falls must go before the cut, or its first half is left
// over where the redaction cannot match it.
func TestATokenWhereTheOutputIsCutIsStillRedacted(t *testing.T) {
	args := []string{"exit", "0"}
	for range 100 {
		args = append(args, "wxyz")
	}
	args = append(args, "--token=abcdefghijkl")
	h := runOnce(t, helperProgram(t, 30, args...), doneFiring("a.mkv"), Where{})
	if strings.Contains(h.LastOutput, "--token=") {
		t.Errorf("part of the token reached the output:\n%s", h.LastOutput)
	}
}

func TestLongPartsOfTheCommandLineAreKeptOutOfTheOutput(t *testing.T) {
	cmd := idleaction.CommandSpec{Program: "sh", Args: []string{"-c", "--token=abcdefghijkl"}}
	got := redactOutput(cmd, "finished -c with --token=abcdefghijkl")
	if strings.Contains(got, "abcdefghijkl") {
		t.Errorf("the token is still in %q", got)
	}
	if !strings.Contains(got, "finished -c") {
		t.Errorf("a short program name or flag was starred out of %q", got)
	}
}
