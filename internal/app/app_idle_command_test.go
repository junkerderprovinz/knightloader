package app

// These tests call fireIdleAction directly instead of waiting for a countdown,
// and the configured action stays "none" so the background controller never
// fires on its own. The runner is always a fake; no test here starts a
// process.

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/idleaction"
	"github.com/junkerderprovinz/knightloader/internal/settings"
)

// TestQuitWithNothingWiredToQuitWithRecordsAFailedRun: a stored "quit" can reach
// a build with no RequestExit, and the countdown the operator watched must
// leave a record.
func TestQuitWithNothingWiredToQuitWithRecordsAFailedRun(t *testing.T) {
	a := newQueueApp(t)
	if a.RequestExit != nil {
		t.Fatal("a test App is supposed to have no way to quit; this test proves nothing now")
	}

	a.fireIdleAction(idleaction.ActionQuit)

	run, ok := a.LastIdleRun()
	if !ok {
		t.Fatal("nothing was recorded; the countdown promised something and left no trace")
	}
	if run.OK {
		t.Error("OK = true for an action this build cannot carry out")
	}
	if run.Problem != idleaction.ProblemNotSupported {
		t.Errorf("Problem = %q, want %q", run.Problem, idleaction.ProblemNotSupported)
	}
	if run.Action != idleaction.ActionQuit {
		t.Errorf("Action = %q, want %q", run.Action, idleaction.ActionQuit)
	}
}

func TestQuitGoesThroughTheSameFieldTheQuitRouteUses(t *testing.T) {
	a := newQueueApp(t)
	asked := make(chan bool, 1)
	a.RequestExit = func(restart bool) bool {
		asked <- restart
		return true
	}

	a.fireIdleAction(idleaction.ActionQuit)

	select {
	case restart := <-asked:
		if restart {
			t.Error("RequestExit was asked for a restart; the end-of-queue action is a quit")
		}
	default:
		t.Fatal("RequestExit was never called")
	}
}

func TestSuspendOnABuildThatCannotSleepRecordsAFailedRun(t *testing.T) {
	a := newQueueApp(t)
	a.fireIdleAction(idleaction.ActionSuspend)

	run := waitForRun(t, a)
	if run.Problem != idleaction.ProblemNotSupported {
		t.Errorf("Problem = %q, want %q on a build with no RequestSuspend", run.Problem, idleaction.ProblemNotSupported)
	}
}

func TestSuspendKeepsTheSystemsOwnWordsVerbatim(t *testing.T) {
	a := newQueueApp(t)
	// polkit's refusal on a headless or seatless session.
	a.RequestSuspend = func() error {
		return errors.New("Failed to suspend system via logind: Interactive authentication required.")
	}

	a.fireIdleAction(idleaction.ActionSuspend)

	run := waitForRun(t, a)
	if run.OK {
		t.Error("OK = true for a refused suspend")
	}
	if run.Problem != idleaction.ProblemPermission {
		t.Errorf("Problem = %q, want %q", run.Problem, idleaction.ProblemPermission)
	}
	if !strings.Contains(run.Output, "Interactive authentication required") {
		t.Errorf("Output = %q, want the system's own sentence kept", run.Output)
	}
}

// TestTheCommandDoesNotRunOnTheControllersGoroutine: an inline command would
// block the poll loop and App.Close for its whole timeout.
func TestTheCommandDoesNotRunOnTheControllersGoroutine(t *testing.T) {
	a := newQueueApp(t)
	started := make(chan struct{})
	release := make(chan struct{})
	a.idleRuns.setRunner(func(ctx context.Context, name string, args ...string) (string, error) {
		close(started)
		<-release
		return "", nil
	})
	if _, err := a.Settings.Set(settingsWithCommand(t, a, idleaction.CommandSpec{
		Program: "/usr/bin/true", TimeoutSeconds: 60,
	})); err != nil {
		t.Fatal(err)
	}

	returned := make(chan struct{})
	go func() {
		a.fireIdleAction(idleaction.ActionCommand)
		close(returned)
	}()

	select {
	case <-started:
	case <-time.After(10 * time.Second):
		close(release)
		t.Fatal("the command never started")
	}
	select {
	case <-returned:
	case <-time.After(10 * time.Second):
		close(release)
		t.Fatal("fireIdleAction had not returned while the command was still running: " +
			"it is running inline on the controller's goroutine, which blocks the poll loop and App.Close")
	}
	close(release)

	run := waitForRun(t, a)
	if !run.OK {
		t.Errorf("OK = false for a command that succeeded: %+v", run)
	}
}

// TestCloseDoesNotWaitOutACommandThatIsStillRunning: the run's context comes
// from a.ctx, so Close cancelling it bounds the wait.
func TestCloseDoesNotWaitOutACommandThatIsStillRunning(t *testing.T) {
	a, closeOnce := newClosableApp(t)
	a.idleRuns.setRunner(func(ctx context.Context, name string, args ...string) (string, error) {
		// Stands in for exec.CommandContext being killed on cancellation.
		<-ctx.Done()
		return "", ctx.Err()
	})
	if _, err := a.Settings.Set(settingsWithCommand(t, a, idleaction.CommandSpec{
		// An hour, so a Close that waits for the timeout fails instead of
		// passing slowly.
		Program: "/usr/bin/sleep", TimeoutSeconds: 3600,
	})); err != nil {
		t.Fatal(err)
	}

	a.fireIdleAction(idleaction.ActionCommand)

	done := make(chan struct{})
	go func() {
		closeOnce()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(30 * time.Second):
		t.Fatal("Close did not return while a command was in flight; the run's context is not derived from the app's")
	}
}

// TestTheProgramsOwnOutputIsRecordedWithTheCommandLineTakenOut: a program that
// echoes its arguments must not carry them into the log line that the
// diagnostics bundle copies.
func TestTheProgramsOwnOutputIsRecordedWithTheCommandLineTakenOut(t *testing.T) {
	a := newQueueApp(t)
	spec := idleaction.CommandSpec{
		Program:        "/usr/bin/wget",
		Args:           []string{"--header=Authorization: Bearer abc123"},
		TimeoutSeconds: 30,
	}
	a.idleRuns.setRunner(func(ctx context.Context, name string, args ...string) (string, error) {
		return "wget: unrecognised option '--header=Authorization: Bearer abc123'", exitErr(2)
	})

	run := a.RunIdleCommandNow(spec)

	if run.OK {
		t.Error("OK = true for a program that exited 2")
	}
	if run.Problem != idleaction.ProblemExit || run.ExitCode != 2 {
		t.Errorf("Problem/ExitCode = %q/%d, want %q/2", run.Problem, run.ExitCode, idleaction.ProblemExit)
	}
	if strings.Contains(run.Output, "abc123") {
		t.Errorf("the token survived into the recorded output: %q", run.Output)
	}
	if !strings.Contains(run.Output, "unrecognised option") {
		t.Errorf("the program's own complaint was thrown away with it: %q", run.Output)
	}
	if run.Program != spec.Program {
		t.Errorf("Program = %q, want %q so the settings card can say which command failed", run.Program, spec.Program)
	}
}

// The output is cut to what a log line carries. A token the program echoes
// right where the cut falls must go before the cut, or its first half is left
// over where the redaction cannot match it.
func TestATokenWhereTheOutputIsCutIsStillRedacted(t *testing.T) {
	a := newQueueApp(t)
	token := "--token=abcdefghijkl"
	a.idleRuns.setRunner(func(ctx context.Context, name string, args ...string) (string, error) {
		return strings.Repeat("x", 497) + " " + token, exitErr(1)
	})

	run := a.RunIdleCommandNow(idleaction.CommandSpec{Program: "/usr/bin/wget", Args: []string{token}, TimeoutSeconds: 30})
	if strings.Contains(run.Output, "--token=") {
		t.Errorf("part of the token reached the output:\n%s", run.Output)
	}
}

func TestWhyTheCommandDidNotStartIsRecorded(t *testing.T) {
	a := newQueueApp(t)
	spec := idleaction.CommandSpec{Program: "/home/someone/bin/after.sh", TimeoutSeconds: 30}
	a.idleRuns.setRunner(func(ctx context.Context, name string, args ...string) (string, error) {
		return "", errors.New("fork/exec " + name + ": resource temporarily unavailable")
	})

	run := a.RunIdleCommandNow(spec)
	if run.Problem != idleaction.ProblemExit || run.ExitCode != -1 {
		t.Errorf("Problem/ExitCode = %q/%d, want %q/-1", run.Problem, run.ExitCode, idleaction.ProblemExit)
	}
	if !strings.Contains(run.Output, "resource temporarily unavailable") {
		t.Errorf("Output = %q, want the reason the command did not start", run.Output)
	}
	if strings.Contains(run.Output, spec.Program) {
		t.Errorf("Output = %q names the program, which the log must not", run.Output)
	}
}

func TestRunningWithNoProgramConfiguredSaysSoRatherThanFailingObscurely(t *testing.T) {
	a := newQueueApp(t)
	run := a.RunIdleCommandNow(idleaction.CommandSpec{TimeoutSeconds: 30})
	if run.Problem != idleaction.ProblemEmpty {
		t.Errorf("Problem = %q, want %q", run.Problem, idleaction.ProblemEmpty)
	}
}

func TestATimedOutCommandIsNotReportedAsAFailure(t *testing.T) {
	t.Parallel()
	// A command that suspends the machine is often killed on the way down, so
	// hitting the limit gets its own answer.
	a := newQueueApp(t)
	a.idleRuns.setRunner(func(ctx context.Context, name string, args ...string) (string, error) {
		<-ctx.Done()
		return "", ctx.Err()
	})
	run := a.RunIdleCommandNow(idleaction.CommandSpec{Program: "/usr/bin/sleep", TimeoutSeconds: 1})
	if run.Problem != idleaction.ProblemTimeout {
		t.Errorf("Problem = %q, want %q", run.Problem, idleaction.ProblemTimeout)
	}
}

func TestIdleCapabilitiesReadTheWiringAndNotTheDeployment(t *testing.T) {
	a := newQueueApp(t)
	caps := a.IdleCapabilities()
	if caps.CanQuit || caps.CanSuspend {
		t.Errorf("%+v: nothing is wired on a test App, so nothing but the command may be offered", caps)
	}
	if !caps.CanCommand {
		t.Error("CanCommand = false; every deployment can exec, and what it can usefully exec is the preflight's question")
	}
	a.RequestSuspend = func() error { return nil }
	if !a.IdleCapabilities().CanSuspend {
		t.Error("CanSuspend = false with RequestSuspend wired")
	}
}

// TestTheStateNeverCarriesTheCommandLine covers the document GET
// /api/idle-action serves.
func TestTheStateNeverCarriesTheCommandLine(t *testing.T) {
	a := newQueueApp(t)
	if _, err := a.Settings.Set(settingsWithCommand(t, a, idleaction.CommandSpec{
		Program: "/usr/bin/wget", Args: []string{"http://nas/suspend?token=abc123"}, TimeoutSeconds: 30,
	})); err != nil {
		t.Fatal(err)
	}
	st := a.IdleActionState()
	if st.Config.Command.Program == "/usr/bin/wget" {
		t.Error("the stored program reached the state document in clear text")
	}
	for _, arg := range st.Config.Command.Args {
		if strings.Contains(arg, "abc123") {
			t.Errorf("a stored argument reached the state document in clear text: %q", arg)
		}
	}
	if len(st.Config.Command.Args) != 1 {
		t.Errorf("Args = %q, want the count kept so the card can say how many there are", st.Config.Command.Args)
	}
}

// settingsWithCommand returns the live settings with spec as the idle command
// and everything else, including the "none" action, unchanged.
func settingsWithCommand(t *testing.T, a *App, spec idleaction.CommandSpec) settings.Settings {
	t.Helper()
	s := a.Settings.Get()
	s.IdleAction.Command = spec
	return s
}

// newClosableApp is newQueueApp for a test that closes the app itself. The
// cleanup skips a second Close with a flag rather than sync.Once, because
// Once would make the cleanup wait on a hung Close and turn a clear failure
// into a test-binary timeout.
func newClosableApp(t *testing.T) (*App, func()) {
	t.Helper()
	a, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	var closing atomic.Bool
	closeOnce := func() {
		if closing.Swap(true) {
			return
		}
		_ = a.Close()
	}
	t.Cleanup(closeOnce)
	if _, err := a.ApplySettings(settings.Settings{
		MaxConcurrent: 4, MaxPerHost: 4, DownloadDir: t.TempDir(),
	}); err != nil {
		t.Fatal(err)
	}
	return a, closeOnce
}

// waitForRun waits for a record, since spawned actions record on their own
// goroutine.
func waitForRun(t *testing.T, a *App) IdleRun {
	t.Helper()
	var run IdleRun
	ok := pollUntil(t, 10*time.Second, func() bool {
		r, has := a.LastIdleRun()
		run = r
		return has
	})
	if !ok {
		t.Fatal("no run was ever recorded")
	}
	return run
}

// exitErr stands in for a real *exec.ExitError, which cannot be built by hand.
type exitErr int

func (e exitErr) Error() string { return "exit status " + string(rune('0'+int(e))) }
func (e exitErr) ExitCode() int { return int(e) }
