package idleaction

// Everything in this file runs against handmade errors and a fake lookPath.
// NOTHING HERE SPAWNS A PROCESS, which is the whole reason Runner and
// Preflight's lookPath are injected in the first place - a test suite that
// shells out is a test suite that fails differently on every machine and on
// CI, and internal/reconnect made exactly this call for exactly this reason.
// It is also why Classify matches an ExitCode() interface: the concrete
// *exec.ExitError cannot be built without running something.

import (
	"context"
	"errors"
	"io/fs"
	"os/exec"
	"strings"
	"testing"
)

func TestCommandSanitizeClampsTheTimeout(t *testing.T) {
	cases := []struct {
		name string
		in   int
		want int
	}{
		{"zero, meaning a settings file that never had the field, becomes the default", 0, DefaultCommandTimeout},
		{"below the floor becomes the default rather than the floor", 1, DefaultCommandTimeout},
		{"exactly the floor is left alone", minCommandTimeout, minCommandTimeout},
		{"a sane value is left alone", 120, 120},
		{"above the ceiling is capped", maxCommandTimeout + 1, maxCommandTimeout},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := CommandSpec{Program: "/bin/true", TimeoutSeconds: c.in}.Sanitize()
			if got.TimeoutSeconds != c.want {
				t.Errorf("TimeoutSeconds = %d, want %d", got.TimeoutSeconds, c.want)
			}
		})
	}
}

func TestCommandSanitizeTrimsTheProgramAndDropsBlankArgs(t *testing.T) {
	got := CommandSpec{
		Program: "  /usr/bin/systemctl \n",
		// The third one is not blank and must survive with its spaces: an
		// argument may legitimately have them, and quietly trimming produces
		// a command that fails with a message from the program that never
		// mentions the reason.
		Args: []string{"suspend", "   ", " --now "},
	}.Sanitize()
	if got.Program != "/usr/bin/systemctl" {
		t.Errorf("Program = %q, want it trimmed", got.Program)
	}
	if len(got.Args) != 2 {
		t.Fatalf("Args = %q, want the blank one dropped and the other two kept", got.Args)
	}
	if got.Args[1] != " --now " {
		t.Errorf("Args[1] = %q, want its spaces left alone", got.Args[1])
	}
}

func TestCommandSanitizeKeepsAnEmptyProgramRatherThanInventingOne(t *testing.T) {
	// The empty spec is the normal state of every install that has never
	// configured a command, and it must stay empty: a default program here
	// would be this package deciding what to run on somebody's machine.
	got := CommandSpec{}.Sanitize()
	if got.Program != "" {
		t.Errorf("Program = %q, want it left empty", got.Program)
	}
	if got.Configured() {
		t.Error("Configured() is true for a spec with no program")
	}
}

// TestPreflightNeverRuns is the promise the whole button rests on.
func TestPreflightNeverRuns(t *testing.T) {
	looked := 0
	spec := CommandSpec{Program: "systemctl", Args: []string{"suspend"}}
	spec.Preflight(func(name string) (string, error) {
		looked++
		return "/usr/bin/" + name, nil
	})
	if looked != 1 {
		t.Errorf("lookPath called %d times, want exactly one resolve and nothing else", looked)
	}
}

func TestPreflightReportsEachProblem(t *testing.T) {
	cases := []struct {
		name    string
		spec    CommandSpec
		look    func(string) (string, error)
		want    Problem
		wantArg []string
	}{
		{
			name: "no program at all",
			spec: CommandSpec{Program: "   "},
			want: ProblemEmpty,
		},
		{
			name: "nothing by that name, which is the container's usual answer",
			spec: CommandSpec{Program: "systemctl"},
			look: func(string) (string, error) { return "", exec.ErrNotFound },
			want: ProblemNotFound,
		},
		{
			name: "there but not executable",
			spec: CommandSpec{Program: "/config/suspend.sh"},
			look: func(n string) (string, error) { return "", &fs.PathError{Op: "stat", Path: n, Err: fs.ErrPermission} },
			want: ProblemNotExecutable,
		},
		{
			name: "found, with the argument vector spelled out",
			spec: CommandSpec{Program: "systemctl", Args: []string{"suspend", "--now"}},
			look: func(n string) (string, error) { return "/usr/bin/" + n, nil },
			want: ProblemNone,
			// The RESOLVED path leads the vector, not the word the operator
			// typed: "systemctl" resolving to /usr/bin/systemctl and
			// "systemctl" resolving to nothing are different problems with
			// different fixes, and only the resolved form says which.
			wantArg: []string{"/usr/bin/systemctl", "suspend", "--now"},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := c.spec.Preflight(c.look)
			if got.Problem != c.want {
				t.Errorf("Problem = %q, want %q", got.Problem, c.want)
			}
			if c.wantArg != nil {
				if strings.Join(got.Argv, "\x00") != strings.Join(c.wantArg, "\x00") {
					t.Errorf("Argv = %q, want %q", got.Argv, c.wantArg)
				}
				if got.ResolvedPath != c.wantArg[0] {
					t.Errorf("ResolvedPath = %q, want %q", got.ResolvedPath, c.wantArg[0])
				}
			}
		})
	}
}

func TestClassify(t *testing.T) {
	cases := []struct {
		name     string
		err      error
		timedOut bool
		want     Problem
		wantCode int
	}{
		{name: "a clean run", want: ProblemNone},
		{
			// Checked before err == nil on purpose - see Classify. A killed
			// program reported as a clean run would tell the operator the
			// machine went to sleep when what happened is that we stopped
			// waiting to find out.
			name: "killed at the deadline, even with no error", timedOut: true, want: ProblemTimeout,
		},
		{name: "killed at the deadline with an error too", err: context.DeadlineExceeded, timedOut: true, want: ProblemTimeout},
		{name: "no such program", err: exec.ErrNotFound, want: ProblemNotFound},
		{name: "a path that is not there", err: &fs.PathError{Err: fs.ErrNotExist}, want: ProblemNotFound},
		{name: "refused", err: &fs.PathError{Err: fs.ErrPermission}, want: ProblemPermission},
		{
			// The only branch that carries a number, and it has to be the
			// program's own: "exit 4" is what an operator greps their own
			// script for.
			name: "the program's own verdict", err: exitErr(4), want: ProblemExit, wantCode: 4,
		},
		{
			// Not the program's verdict and not a permission problem either.
			// -1 is what os/exec itself uses for "never got a status", and
			// guessing "you are not allowed" here would send somebody to
			// look at uids for a problem that is neither.
			name: "something else went wrong entirely", err: errors.New("fork/exec: resource temporarily unavailable"),
			want: ProblemExit, wantCode: -1,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, code := Classify(c.err, c.timedOut)
			if got != c.want {
				t.Errorf("Problem = %q, want %q", got, c.want)
			}
			if code != c.wantCode {
				t.Errorf("exit code = %d, want %d", code, c.wantCode)
			}
		})
	}
}

// exitErr stands in for an *exec.ExitError carrying a known status.
//
// os.ProcessState cannot be built by hand - its fields are unexported and
// there is no constructor - so the only way to get a real one is to spawn a
// process that fails, and this package's whole testing stance is that nothing
// here spawns anything. Classify matches an ExitCode() interface rather than
// the concrete type for exactly this reason (see its own comment); the
// production error satisfies it and so does this.
type exitErr int

func (e exitErr) Error() string { return "exit status " + string(rune('0'+int(e))) }
func (e exitErr) ExitCode() int { return int(e) }

func TestCommandRedactionRoundTrip(t *testing.T) {
	// The shape the settings form actually produces: the page is shown the
	// redacted spec and sends it straight back with only the timeout changed.
	stored := CommandSpec{Program: "/usr/bin/wget", Args: []string{"--header=Authorization: Bearer abc123", "http://nas/suspend"}, TimeoutSeconds: 60}
	shown := stored.Redacted()
	if shown.Program != RedactedCommand {
		t.Errorf("Program = %q, want it hidden", shown.Program)
	}
	if len(shown.Args) != 2 {
		t.Fatalf("Args = %q, want the COUNT kept so the page can say how many there are", shown.Args)
	}
	for i, a := range shown.Args {
		if a != RedactedCommand {
			t.Errorf("Args[%d] = %q, want it hidden - a token lives in an argument far more often than in a path", i, a)
		}
	}
	if shown.TimeoutSeconds != 60 {
		t.Errorf("TimeoutSeconds = %d, want the number left alone: it gives nothing away and the form needs it", shown.TimeoutSeconds)
	}

	back := shown
	back.TimeoutSeconds = 120
	got := back.WithSecretsFrom(stored)
	if got.Program != stored.Program {
		t.Errorf("Program = %q after a save that did not touch it, want %q back", got.Program, stored.Program)
	}
	if strings.Join(got.Args, "\x00") != strings.Join(stored.Args, "\x00") {
		t.Errorf("Args = %q, want %q back", got.Args, stored.Args)
	}
	if got.TimeoutSeconds != 120 {
		t.Errorf("TimeoutSeconds = %d, want the real edit kept", got.TimeoutSeconds)
	}
}

func TestAnEmptyProgramStillClearsTheStoredOne(t *testing.T) {
	// The one case a placeholder must never swallow. Without this, a command
	// could be configured once and never removed through the form again -
	// the same rule reconnect.RedactedPassword's own comment states.
	stored := CommandSpec{Program: "/usr/bin/wget", Args: []string{"http://nas/suspend"}}
	got := CommandSpec{Program: "", Args: nil}.WithSecretsFrom(stored)
	if got.Program != "" {
		t.Errorf("Program = %q, want an explicitly emptied field to clear the stored one", got.Program)
	}
	if len(got.Args) != 0 {
		t.Errorf("Args = %q, want them cleared with the program", got.Args)
	}
}

func TestRetypingOnlyTheProgramKeepsTheArguments(t *testing.T) {
	stored := CommandSpec{Program: "/usr/bin/wget", Args: []string{"http://nas/suspend"}}
	got := CommandSpec{Program: "/usr/bin/curl", Args: []string{RedactedCommand}}.WithSecretsFrom(stored)
	if got.Program != "/usr/bin/curl" {
		t.Errorf("Program = %q, want the retyped one", got.Program)
	}
	if len(got.Args) != 1 || got.Args[0] != "http://nas/suspend" {
		t.Errorf("Args = %q, want the stored ones back", got.Args)
	}
}

// TestRedactInKeepsTheCommandLineOutOfTheLog is the second door on the same
// secret: the settings document is redacted, and this is what stops a program
// echoing its own arguments back into a log line that the diagnostics bundle
// then copies verbatim.
func TestRedactInKeepsTheCommandLineOutOfTheLog(t *testing.T) {
	spec := CommandSpec{Program: "/usr/bin/wget", Args: []string{"--header=Authorization: Bearer abc123"}}
	out := spec.RedactIn("wget: unrecognised option '--header=Authorization: Bearer abc123'\nusage: /usr/bin/wget [option]...")
	if strings.Contains(out, "abc123") {
		t.Errorf("the token survived into %q", out)
	}
	if strings.Contains(out, "/usr/bin/wget") {
		t.Errorf("the program path survived into %q", out)
	}
	if !strings.Contains(out, "unrecognised option") {
		t.Errorf("the program's own complaint was thrown away too: %q", out)
	}
}

func TestTrimOutputCaps(t *testing.T) {
	long := strings.Repeat("x", maxCommandOutput*3)
	got := TrimOutput("  " + long + "  ")
	if len(got) != maxCommandOutput+3 {
		t.Errorf("len = %d, want the cap plus the ellipsis", len(got))
	}
	if !strings.HasSuffix(got, "...") {
		t.Errorf("a cut output must say it was cut, got %q", got[len(got)-10:])
	}
}
