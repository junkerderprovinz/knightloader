package app

// What the host-level end-of-queue actions do, and the record of the last run.
// fireIdleAction in app_idle.go only dispatches; everything that can block
// lives here.

import (
	"context"
	"errors"
	"log"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/idleaction"
)

// IdleRun is what the last end-of-queue action did. It is kept in memory only,
// so a successful quit leaves no record and the settings card speaks of "since
// this start".
type IdleRun struct {
	Action idleaction.Action `json:"action"`
	At     time.Time         `json:"at"`
	OK     bool              `json:"ok"`
	// Problem is empty exactly when OK is true.
	Problem  idleaction.Problem `json:"problem,omitempty"`
	ExitCode int                `json:"exitCode,omitempty"`
	// Output is the program's combined output, or why it did not start,
	// capped, with the stored command line removed (see CommandSpec.RedactIn).
	Output string `json:"output,omitempty"`
	// Program is the configured program, unredacted. It goes to the
	// authenticated browser API but never into a log line, because the log
	// ends up in the diagnostics bundle.
	Program string `json:"program,omitempty"`
}

// IdleState is what GET /api/idle-action returns: the controller's state plus
// the last run. Embedding State keeps the wire shape flat for existing clients.
type IdleState struct {
	idleaction.State
	LastRun *IdleRun `json:"lastRun,omitempty"`
}

// idleRunLog holds the last run and the runner that produces one. It has its
// own mutex rather than a.mu, which callers of spawn may hold; the lock is only
// ever held around field access.
type idleRunLog struct {
	mu   sync.Mutex
	last *IdleRun
	// run replaces idleaction.ExecRunner in tests. It is unexported because a
	// nil function field on App means "this build cannot do that", and here nil
	// means "use the real one".
	run idleaction.Runner
}

// setRunner swaps the runner under the lock, so the race detector sees the
// write happen before the spawned read.
func (l *idleRunLog) setRunner(r idleaction.Runner) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.run = r
}

func (l *idleRunLog) runner() idleaction.Runner {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.run != nil {
		return l.run
	}
	return idleaction.ExecRunner
}

func (l *idleRunLog) store(r IdleRun) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.last = &r
}

func (l *idleRunLog) get() (IdleRun, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.last == nil {
		return IdleRun{}, false
	}
	return *l.last, true
}

// LastIdleRun reports what the last end-of-queue action did, and whether one
// has run at all since this process started.
func (a *App) LastIdleRun() (IdleRun, bool) { return a.idleRuns.get() }

// IdleCapabilities reports which actions this build can carry out, read from
// the wiring rather than the deployment. Every deployment can run a command;
// whether it is a useful one is for the preflight to say.
func (a *App) IdleCapabilities() idleaction.Capabilities {
	return idleaction.Capabilities{
		CanQuit:    a.RequestExit != nil,
		CanCommand: true,
		CanSuspend: a.RequestSuspend != nil,
	}
}

// IdleCommandCheck resolves the stored command without running it. It only
// stats a file, so it is safe to call at any time.
func (a *App) IdleCommandCheck(deployment string) idleaction.Check {
	c := a.Settings.Get().IdleAction.Command.Preflight(nil)
	c.Deployment = deployment
	return c
}

// runIdleCommand runs the configured program off the controller's goroutine.
// Controller.Close waits for Fire, so an inline command hanging on a dead
// connection would stall the poll loop and App.Close for its whole timeout.
// Through a.spawn, with a context derived from a.ctx, Close cancels it
// instead.
func (a *App) runIdleCommand(spec idleaction.CommandSpec) {
	a.spawn(func() { a.RunIdleCommandNow(spec) })
}

// RunIdleCommandNow runs the command synchronously and returns the stored
// record; POST /api/idle-action/run uses it to answer the click directly. The
// context comes from a.ctx rather than the request, so navigating away does
// not kill the command.
func (a *App) RunIdleCommandNow(spec idleaction.CommandSpec) IdleRun {
	if !spec.Configured() {
		return a.recordIdleRun(IdleRun{Action: idleaction.ActionCommand, Problem: idleaction.ProblemEmpty})
	}
	ctx, cancel := context.WithTimeout(a.ctx, spec.Timeout())
	defer cancel()

	out, err := a.idleRuns.runner()(ctx, spec.Program, spec.Args...)
	// A shutdown cancels the context too, and that is not a timeout.
	timedOut := errors.Is(ctx.Err(), context.DeadlineExceeded)
	problem, code := idleaction.Classify(err, timedOut)
	if out == "" && code == -1 {
		// The program never got as far as saying anything, so the reason it
		// did not start is the only evidence there is.
		out = err.Error()
	}
	run := IdleRun{
		Action:   idleaction.ActionCommand,
		Problem:  problem,
		ExitCode: code,
		// Redacted before it is cut, or a token straddling the cut would reach
		// the log as a prefix the redaction cannot match.
		Output:  idleaction.TrimOutput(spec.RedactIn(out)),
		Program: spec.Program,
	}
	return a.recordIdleRun(run)
}

// runIdleSuspend asks the host to sleep off the controller's goroutine, since
// a power call can block, on Linux waiting for an authentication agent that
// never answers.
func (a *App) runIdleSuspend() {
	a.spawn(func() {
		if a.RequestSuspend == nil {
			a.recordIdleRun(IdleRun{Action: idleaction.ActionSuspend, Problem: idleaction.ProblemNotSupported})
			return
		}
		err := a.RequestSuspend()
		run := IdleRun{Action: idleaction.ActionSuspend}
		if err != nil {
			// Every realistic refusal (polkit, a Windows power policy, a
			// session not allowed to suspend) is fixed in the same place, and
			// the system's own message travels in Output.
			run.Problem = idleaction.ProblemPermission
			// Kept verbatim: polkit's "Interactive authentication required"
			// is what tells the operator what to fix.
			run.Output = idleaction.TrimOutput(err.Error())
		}
		a.recordIdleRun(run)
	})
}

// recordIdleRun stores the outcome, writes one log line and broadcasts the
// state on the "idleAction" kind the controller already uses, so open tabs
// repaint without polling.
func (a *App) recordIdleRun(run IdleRun) IdleRun {
	run.At = time.Now()
	run.OK = run.Problem == idleaction.ProblemNone
	a.idleRuns.store(run)
	logIdleRun(run)
	if a.Hub != nil {
		a.Hub.Broadcast("idleAction", a.IdleActionState())
	}
	return run
}

// logIdleRun logs the outcome without naming the program. The log tail goes
// into the diagnostics bundle, and the command line is redacted from the
// settings for exactly that reason.
func logIdleRun(run IdleRun) {
	if run.OK {
		log.Printf("idle action: %s finished cleanly", run.Action)
		return
	}
	msg := "idle action: " + string(run.Action) + " failed (" + string(run.Problem) + ")"
	if run.ExitCode != 0 {
		msg += " exit " + strconv.Itoa(run.ExitCode)
	}
	if out := strings.TrimSpace(run.Output); out != "" {
		msg += ": " + out
	}
	log.Print(msg)
}
