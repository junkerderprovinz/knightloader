package app

// What the three host-level end-of-queue actions actually DO, and the record
// of what happened when one of them last fired.
//
// Split out of app_idle.go rather than added to it, and the split is the
// point: app_idle.go stays a dispatcher - one switch, one line per action,
// nothing that can block - while everything with a timeout, a mutex or an
// error path lives here. See runIdleCommand for why that separation is not
// tidiness but the difference between a container that stops and one that
// hangs for the length of a command timeout on the way out.

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

// IdleRun is what the last end-of-queue action did. Before this existed
// fireIdleAction called log.Printf and nothing else, so a command that failed
// at four in the morning left no trace anywhere a person would look.
//
// IT DOES NOT SURVIVE A RESTART, on purpose and with a cost worth naming: for
// ActionQuit in particular, a run that SUCCEEDED takes this record with it
// when the process goes, so the only IdleRun a quit can ever leave behind is a
// failed one. That is the useful half, but it means the settings card has to
// say "since this start" rather than implying a history. The alternative - a
// row in the store - buys a history of an event that is nearly always either
// invisible (it worked, the process is gone) or immediately visible (it did
// not work, and you are reading it), at the price of a schema change.
type IdleRun struct {
	Action idleaction.Action `json:"action"`
	At     time.Time         `json:"at"`
	OK     bool              `json:"ok"`
	// Problem is empty exactly when OK is true.
	Problem  idleaction.Problem `json:"problem,omitempty"`
	ExitCode int                `json:"exitCode,omitempty"`
	// Output is the program's own combined output, capped, and with the
	// stored command line substituted out of it - see CommandSpec.RedactIn.
	Output string `json:"output,omitempty"`
	// Program is the command line's program as configured, unredacted.
	//
	// It travels to the BROWSER (GET /api/idle-action and the "idleAction"
	// hub broadcast, both behind the same authentication as everything else)
	// and never into a log line, which is the one place it would end up in
	// the diagnostics bundle. That asymmetry is deliberate: the operator
	// looking at their own settings page has every right to see which
	// program failed, and the file they attach to a public bug report has no
	// business carrying it. logIdleRun below is where that line is drawn.
	Program string `json:"program,omitempty"`
}

// IdleState is what GET /api/idle-action answers: the countdown as the
// controller sees it, plus the last run.
//
// The embedded State keeps the wire shape flat, so every existing client
// (components/IdleActionBanner.tsx, the settings card) goes on reading
// config/idle/armed/action/fireAt exactly where they have always been.
//
// The composition happens HERE rather than by adding a field to
// idleaction.State because IdleRun is app-shaped - it names an App capability
// and carries a program path - and internal/idleaction's package doc promises
// to answer only "is the queue idle" and "then what", leaving what an action
// DOES to the caller.
type IdleState struct {
	idleaction.State
	LastRun *IdleRun `json:"lastRun,omitempty"`
}

// idleRunLog holds the last run and the runner that produces one.
//
// ITS OWN MUTEX, never a.mu. a.mu is held by the dispatch paths that reach
// spawn on their way out (see closeMu's own comment on the App struct), and a
// lock taken from inside a spawned goroutine that a.mu-holding callers also
// take is the shape a deadlock grows out of. This lock is held only around
// two field assignments and nothing is called while it is held.
type idleRunLog struct {
	mu   sync.Mutex
	last *IdleRun
	// run is the injection seam for tests. Unexported, and nil means
	// idleaction.ExecRunner: a test in package app swaps it directly, the
	// same way app_idle_test.go already reaches a.mu and the task list. It is
	// deliberately NOT an exported field on App, because an exported
	// function field on App already has a fixed meaning in this codebase -
	// nil means "this build cannot do that", the convention RequestExit
	// established and routes_lifecycle.go reads - and a second exported
	// function field where nil meant "use the real one" would put two
	// opposite readings of the same nil side by side.
	run idleaction.Runner
}

// setRunner is the test seam. It exists as a method rather than a bare field
// assignment so that the swap goes through the same lock the read does: the
// suite runs under -race, and a field written from a test goroutine and read
// from a spawned one has to have an edge between them that the detector can
// see.
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

// IdleCapabilities reports which actions this build can actually carry out,
// read from the wiring rather than guessed from buildinfo.Deployment - the
// same read routes_lifecycle.go's deploymentInfo already makes for CanQuit.
//
// CanCommand is unconditionally true: every deployment can exec. WHAT it can
// usefully exec is a different question and not one this process may answer
// on the operator's behalf, which is what the preflight is for.
func (a *App) IdleCapabilities() idleaction.Capabilities {
	return idleaction.Capabilities{
		CanQuit:    a.RequestExit != nil,
		CanCommand: true,
		CanSuspend: a.RequestSuspend != nil,
	}
}

// IdleCommandCheck resolves the STORED command without running it, and fills
// in what only the caller can know. It is safe to call at any time, from any
// page, as often as anybody likes: it stats a file.
func (a *App) IdleCommandCheck(deployment string) idleaction.Check {
	c := a.Settings.Get().IdleAction.Command.Preflight(nil)
	c.Deployment = deployment
	return c
}

// runIdleCommand runs the configured program off the controller's goroutine
// and records what happened.
//
// SPAWNED, NEVER RUN INLINE, and this is the trap this whole file exists to
// avoid. idleaction.Options.Fire says in its own doc comment that it runs on
// the controller's goroutine and must not block for long, and
// Controller.Close waits for an in-flight tick - Fire included - so that a
// caller can tear down what Fire talks to without racing it. App.Close closes
// the controller. So an exec run inline inside fireIdleAction means an `ssh
// nas poweroff` hanging on a dead connection blocks the two-second poll loop
// AND blocks App.Close for the full command timeout, which the operator
// experiences as "the container will not stop".
//
// a.spawn is what makes the wait bounded and correct instead: it declines
// outright once Close has committed, and Close waits on a.wg for one that got
// in first - so a hanging command delays shutdown by at most its own timeout
// and never by an unbounded amount, because the context below is derived from
// a.ctx, which Close cancels.
func (a *App) runIdleCommand(spec idleaction.CommandSpec) {
	a.spawn(func() { a.RunIdleCommandNow(spec) })
}

// RunIdleCommandNow runs the command synchronously and returns the record it
// stored. POST /api/idle-action/run calls it directly so the operator gets
// the outcome in the response to their own click rather than having to wait
// for a broadcast; the countdown path goes through runIdleCommand above.
//
// The context is derived from a.ctx and not from the HTTP request: somebody
// who presses "run it now" and then navigates away has still asked for the
// command to run, and killing it on their navigation would be a test that
// behaves differently from the real thing it is testing.
func (a *App) RunIdleCommandNow(spec idleaction.CommandSpec) IdleRun {
	if !spec.Configured() {
		return a.recordIdleRun(IdleRun{Action: idleaction.ActionCommand, Problem: idleaction.ProblemEmpty})
	}
	ctx, cancel := context.WithTimeout(a.ctx, spec.Timeout())
	defer cancel()

	out, err := a.idleRuns.runner()(ctx, spec.Program, spec.Args...)
	// DeadlineExceeded specifically, not ctx.Err() != nil: a context
	// cancelled because the app is shutting down is not a timeout, and
	// reporting a shutdown as "your command took too long" would send the
	// operator to raise a limit that was never the problem.
	timedOut := errors.Is(ctx.Err(), context.DeadlineExceeded)
	problem, code := idleaction.Classify(err, timedOut)
	run := IdleRun{
		Action:   idleaction.ActionCommand,
		Problem:  problem,
		ExitCode: code,
		Output:   spec.RedactIn(idleaction.TrimOutput(out)),
		Program:  spec.Program,
	}
	return a.recordIdleRun(run)
}

// runIdleSuspend asks the host to sleep, off the controller's goroutine for
// the same reason runIdleCommand is: a power call can block for as long as
// the operating system feels like taking, and on Linux it can sit waiting on
// an authentication agent that will never answer.
func (a *App) runIdleSuspend() {
	a.spawn(func() {
		if a.RequestSuspend == nil {
			a.recordIdleRun(IdleRun{Action: idleaction.ActionSuspend, Problem: idleaction.ProblemNotSupported})
			return
		}
		err := a.RequestSuspend()
		run := IdleRun{Action: idleaction.ActionSuspend}
		if err != nil {
			// ProblemPermission for every failure here, rather than three
			// codes that would all print the same sentence. The realistic
			// refusals are polkit with no seat to ask, a Windows power
			// policy and a session that is simply not allowed to suspend:
			// all of them are "the system would not let this app do that",
			// and all of them are fixed in the same place. Nothing is lost
			// by not splitting them, because the system's own words travel
			// with the code below.
			run.Problem = idleaction.ProblemPermission
			// Verbatim and capped. "Interactive authentication required" is
			// polkit's refusal on a headless or seatless session and it is
			// the ONE string that tells the operator what to fix, so nothing
			// here rewords it or swallows it.
			run.Output = idleaction.TrimOutput(err.Error())
		}
		a.recordIdleRun(run)
	})
}

// recordIdleRun stores the outcome, writes exactly one log line, and tells
// every open tab.
//
// The broadcast is what makes the banner and the settings card repaint
// without either of them polling - the same "idleAction" kind the controller
// already broadcasts on arm and disarm, so no client needs a second
// subscription.
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

// logIdleRun writes the one line that outlives the process's own memory of
// the run.
//
// IT NEVER NAMES THE PROGRAM. log.Printf is tapped by internal/logring and
// the tail of that ring is copied verbatim into the diagnostics bundle, which
// is a file people attach to public bug reports - and the whole point of
// redacting the command line out of Settings.Redacted (see
// idleaction.CommandSpec.Redacted) is that it does not travel in that file.
// Putting the program path into a log line would walk the redaction straight
// back out through the other door, which is the "fixed one layer, missed the
// one that runs after it" shape. The action, the problem code, the exit
// status and the program's own already-redacted output are all the bundle
// needs to be worth reading.
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
