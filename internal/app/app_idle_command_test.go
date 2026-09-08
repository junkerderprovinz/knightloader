package app

// The three host-level end-of-queue actions, against a real App.
//
// Every one of these drives fireIdleAction DIRECTLY rather than waiting for a
// countdown. The state machine's own timing is already pinned by
// internal/idleaction's fake-clock tests and the wiring by
// app_idle_test.go's real-timer ones; what is left to prove here is what each
// new branch DOES, and a two-second poll plus a five-second countdown in front
// of every case would buy nothing but seconds. The configured action stays
// "none" throughout, so the controller running in the background never fires
// anything of its own and these cases stay deterministic.
//
// NO TEST HERE SPAWNS A PROCESS: the runner is swapped for a fake, which is
// the reason idleRunLog carries one at all.

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

// TestQuitWithNothingWiredToQuitWithRecordsAFailedRun is the case that used to
// be invisible. Actions() is deliberately not filtered by capability, so a
// stored "quit" reaches a build with no RequestExit - hand-edited, or restored
// from a backup taken on the other deployment - and the operator watched a
// countdown that promised something.
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
		// false, not true: this is a quit. The difference is only the
		// caller's own log line (see App.RequestExit), but asking for a
		// restart here would say something untrue in it.
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
	// polkit's refusal on a headless or seatless session. It is the ONE
	// string that tells the operator what to fix, so it has to survive
	// unreworded all the way to the record.
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

// TestTheCommandDoesNotRunOnTheControllersGoroutine is the trap that would
// otherwise present itself as "the container will not stop".
//
// idleaction.Options.Fire runs on the controller's own goroutine and its own
// doc comment says it must not block for long; Controller.Close waits for an
// in-flight tick, Fire included, and App.Close closes the controller. So an
// exec run inline here means an `ssh nas poweroff` hanging on a dead
// connection stops the two-second poll loop AND holds up shutdown for the
// whole command timeout.
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

// TestCloseDoesNotWaitOutACommandThatIsStillRunning pins the other half of the
// same promise: the run's context comes from a.ctx, so Close cancelling it is
// what makes the wait bounded. Built with context.Background() instead, this
// hangs forever - which is precisely how it was nearly written.
func TestCloseDoesNotWaitOutACommandThatIsStillRunning(t *testing.T) {
	a, closeOnce := newClosableApp(t)
	a.idleRuns.setRunner(func(ctx context.Context, name string, args ...string) (string, error) {
		// A real command's exec.CommandContext is killed on cancellation and
		// returns; this stands in for that and for nothing else.
		<-ctx.Done()
		return "", ctx.Err()
	})
	if _, err := a.Settings.Set(settingsWithCommand(t, a, idleaction.CommandSpec{
		// An hour, so a Close that waits for the TIMEOUT rather than for the
		// cancellation fails this test instead of passing it slowly.
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

// TestTheProgramsOwnOutputIsRecordedWithTheCommandLineTakenOut is the second
// door on the redacted command line. The settings document hides it
// (settings.Settings.Redacted); this is what stops a program echoing its own
// arguments into the log line that the diagnostics bundle copies verbatim.
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
	// The program itself DOES travel, and only to the browser: the record
	// goes over the authenticated API and the hub, never into a log line.
	// See IdleRun.Program.
	if run.Program != spec.Program {
		t.Errorf("Program = %q, want %q so the settings card can say which command failed", run.Program, spec.Program)
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
	// A command that suspends the machine is killed on the way down about as
	// often as it returns, so "stopped at the limit" has to be its own answer
	// with its own sentence rather than a generic error.
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

// TestTheStateNeverCarriesTheCommandLine is the browser-facing half of the
// redaction: GET /api/idle-action serves this document.
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

// settingsWithCommand is the live settings with one command spec written into
// them and everything else left exactly as it stands. It goes through
// Settings.Set rather than assigning the struct, so the secret merge-back
// that protects a redacted command line is exercised on the way in.
//
// Action deliberately stays whatever it was - "none" on a fresh test App - so
// the controller running in the background never fires anything on its own and
// these cases stay deterministic.
func settingsWithCommand(t *testing.T, a *App, spec idleaction.CommandSpec) settings.Settings {
	t.Helper()
	s := a.Settings.Get()
	s.IdleAction.Command = spec
	return s
}

// newClosableApp is newQueueApp for the one test that closes the app itself.
// The cleanup only closes what the test did not, because Close is not written
// to be called twice and a second one would tear down a store that is already
// gone.
//
// It swaps a flag rather than using sync.Once, and that difference is the
// whole reason this helper is written out here. Once.Do makes the SECOND
// caller wait for the first, so a cleanup running after a Close that hung
// would hang with it - and the one test this helper exists for is the test
// whose failure mode is exactly a Close that never returns. That would turn a
// clear "Close did not return" into a ten-minute test-binary timeout with a
// stack dump instead of a message.
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

// waitForRun waits for a record to appear, because the two spawned actions
// record on their own goroutine.
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

// exitErr stands in for a real *exec.ExitError - see the identical helper in
// internal/idleaction/command_test.go for why one cannot be built by hand.
type exitErr int

func (e exitErr) Error() string { return "exit status " + string(rune('0'+int(e))) }
func (e exitErr) ExitCode() int { return int(e) }
