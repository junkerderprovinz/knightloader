package idleaction

// The one external program ActionCommand runs, everything that can go wrong
// with it as a value rather than as a sentence, and the check that answers
// "would this work?" without running anything.
//
// This file is modelled on internal/reconnect deliberately, not
// coincidentally. That package already solved the same three problems in this
// same codebase - a user-supplied program, a Runner injected so no test ever
// spawns a process (reconnect.Runner), and a closed set of Problem codes so
// the sentence can be translated in the browser rather than written in
// English on the server (reconnect.ConfigProblem). Inventing a second shape
// for the same job is how the two drift.

import (
	"context"
	"errors"
	"io/fs"
	"os/exec"
	"strings"
	"time"
)

// CommandSpec is the program ActionCommand runs, as the operator wrote it.
//
// A PROGRAM AND ARGUMENTS, NEVER A SHELL COMMAND LINE, and that is not an
// oversight to be fixed by the first bug report that asks for it.
// exec.CommandContext does not invoke a shell (the same fact
// internal/resolver/ytdlp/options.go documents twice), and
// internal/reconnect/config.go says outright why it has no shell-command-line
// field either. So `systemctl suspend && echo ok`, `curl ... | logger` and
// `cmd /c foo > log.txt` do not work here, cannot be made to work by quoting,
// and belong in a script file this points at instead. The hint text on the
// settings page says exactly that, in all 42 languages, because these will be
// the first three reports this feature gets.
type CommandSpec struct {
	// Program is the executable. Resolved through the same PATH lookup
	// exec.Command itself would use (see Preflight), so a bare "systemctl"
	// works where one is on the PATH and a full path works everywhere.
	Program string `json:"program"`
	// Args are handed over one by one, never split or joined on whitespace,
	// which is what keeps an argument with a space in it one argument.
	Args []string `json:"args,omitempty"`
	// TimeoutSeconds is how long the program may run before it is killed.
	// Being killed is NOT the same as having failed - see ProblemTimeout.
	TimeoutSeconds int `json:"timeoutSeconds"`
}

// The bounds Sanitize enforces. The floor exists because a one-second limit
// kills nearly everything worth running here before it has finished starting;
// the ceiling exists because an end-of-queue action that is still running an
// hour later has stopped being an end-of-queue action.
const (
	// DefaultCommandTimeout is exported for the same reason
	// DefaultDelaySeconds is: the settings form shows the number, and a
	// second copy of it written into the frontend is the copy that goes
	// stale.
	DefaultCommandTimeout = 60
	minCommandTimeout     = 5
	maxCommandTimeout     = 3600
)

// maxCommandOutput is how much of the program's own output is kept. Copied
// from internal/reconnect's constant of the same name, with the same
// reasoning: a script that dumps a megabyte on failure must not put a
// megabyte into the log line, the hub broadcast and the settings page that
// report it.
const maxCommandOutput = 512

// RedactedCommand is what Redacted puts in place of the stored command line,
// and the value WithSecretsFrom reads back as "the client did not retype it".
// It is deliberately the same visible placeholder reconnect.RedactedPassword
// uses, because it has to survive the same round trip and mean the same
// thing: an EMPTY program still has to mean "clear it", or a command could
// never be removed through the settings form once one had been saved.
const RedactedCommand = "********"

// Sanitize repairs what a caller must never be refused over - reading a
// settings file an older or hand-edited build wrote. It always succeeds, the
// same rule Config.Sanitize follows and for the same reason: the one path
// that feeds it never fails a whole settings save over one field.
func (s CommandSpec) Sanitize() CommandSpec {
	s.Program = strings.TrimSpace(s.Program)
	if len(s.Args) > 0 {
		// A blank argument is dropped rather than passed through. Passed
		// through it becomes an empty argv entry, which most programs read
		// as an empty positional argument and a few read as an error, and
		// neither is what somebody who left a blank line in the box meant.
		// Surrounding whitespace is NOT trimmed from the rest: an argument
		// may legitimately contain leading or trailing spaces, and quietly
		// removing them produces a command that fails with a message from
		// the program that never mentions the reason - the same call
		// reconnect.Sanitize makes about the router password.
		args := make([]string, 0, len(s.Args))
		for _, a := range s.Args {
			if strings.TrimSpace(a) == "" {
				continue
			}
			args = append(args, a)
		}
		if len(args) == 0 {
			args = nil
		}
		s.Args = args
	}
	if s.TimeoutSeconds < minCommandTimeout {
		s.TimeoutSeconds = DefaultCommandTimeout
	}
	if s.TimeoutSeconds > maxCommandTimeout {
		s.TimeoutSeconds = maxCommandTimeout
	}
	return s
}

// Timeout is how long the program may run.
func (s CommandSpec) Timeout() time.Duration {
	return time.Duration(s.TimeoutSeconds) * time.Second
}

// Configured reports whether there is anything to run at all.
func (s CommandSpec) Configured() bool { return strings.TrimSpace(s.Program) != "" }

// Redacted replaces the whole command line - program AND arguments - with a
// placeholder, leaving only the timeout, which is a number and gives nothing
// away.
//
// WHY THE WHOLE LINE AND NOT JUST THE ARGUMENTS (jdp's call, and it is not
// reopened here): Settings.Redacted feeds two readers, GET /api/settings and
// the diagnostics bundle (internal/api/routes_diagnostics.go), and the second
// one is a file people attach to PUBLIC bug reports. A command line is a
// secret store nobody declared: `wget --header=Authorization:\ Bearer\ abc123
// http://nas/suspend` puts a token in an argument, and the program half is no
// safer - routes_diagnostics.go already refuses to put PATHS in that bundle
// for its own store, in so many words, because a desktop path reads
// C:\Users\<a person's real name>\AppData\... Redacting the arguments and
// leaving the path is the "patched three sites, missed the fourth" shape that
// reconnect.redact's own comment warns about, so the line goes as one thing.
//
// What the operator loses is seeing their own command printed back on the
// settings page; what they get instead is POST /api/idle-action/check, which
// resolves the STORED spec and reports the real path and the real argv on
// demand, over an authenticated route, into a page rather than into a file.
// That is a live answer to "what will actually run", which is the question
// the printed-back field only appeared to answer.
func (s CommandSpec) Redacted() CommandSpec {
	if s.Program != "" {
		s.Program = RedactedCommand
	}
	if len(s.Args) > 0 {
		// Each argument is replaced individually rather than the list being
		// dropped, so the page can still say how many there are: "three
		// arguments, hidden" is diagnostically useful and gives nothing
		// away, while an empty list would read as "no arguments" and send
		// somebody looking for a bug that is not there.
		out := make([]string, len(s.Args))
		for i := range out {
			out[i] = RedactedCommand
		}
		s.Args = out
	}
	return s
}

// WithSecretsFrom puts back the command line Redacted removed. A settings
// form that was shown a redacted spec sends the placeholder back untouched,
// and without this every save from that page would wipe the stored command.
//
// The two halves are restored INDEPENDENTLY, and each only when it is still
// exactly the placeholder: retyping the program while leaving the arguments
// alone is a real edit somebody will make, and so is the reverse.
func (s CommandSpec) WithSecretsFrom(prev CommandSpec) CommandSpec {
	if s.Program == RedactedCommand {
		s.Program = prev.Program
	}
	if len(s.Args) > 0 && len(s.Args) == len(prev.Args) && allRedacted(s.Args) {
		s.Args = prev.Args
	}
	return s
}

func allRedacted(args []string) bool {
	for _, a := range args {
		if a != RedactedCommand {
			return false
		}
	}
	return true
}

// RedactIn substitutes the stored command line out of text the program itself
// produced, before that text reaches a log line.
//
// This exists because of where the log goes. log.Printf is tapped by
// internal/logring and the last lines of that ring are copied verbatim into
// the diagnostics bundle (internal/api/routes_diagnostics.go), which is a
// file people attach to public bug reports. Redacting the stored spec while
// letting a program echo its own argv into a log line that then travels in
// the same file would be a redaction that holds right up until the first
// program that prints its usage banner on a bad argument - which is most of
// them.
//
// One choke point on purpose, exactly as reconnect.redact argues for itself:
// the alternative is patching the log call, the broadcast and the settings
// card separately, and that is how the fourth site gets missed.
func (s CommandSpec) RedactIn(text string) string {
	if text == "" {
		return text
	}
	// Longest first, so replacing a short argument does not chop a longer one
	// that contains it into something that no longer matches.
	parts := make([]string, 0, len(s.Args)+1)
	if p := strings.TrimSpace(s.Program); p != "" {
		parts = append(parts, p)
	}
	for _, a := range s.Args {
		if strings.TrimSpace(a) != "" {
			parts = append(parts, a)
		}
	}
	for i := 0; i < len(parts); i++ {
		for j := i + 1; j < len(parts); j++ {
			if len(parts[j]) > len(parts[i]) {
				parts[i], parts[j] = parts[j], parts[i]
			}
		}
	}
	for _, p := range parts {
		text = strings.ReplaceAll(text, p, RedactedCommand)
	}
	return text
}

// Problem is why a run, or a check, did not come out clean. A closed set of
// codes rather than free text, for the reason internal/app's ActivityKind and
// reconnect.ConfigProblem both give: the sentence is not translatable and the
// interface is, so the code crosses the wire and the browser picks the words.
// Translating on the server would need the reader's language on a settings
// request and would write the log in whatever the last reader happened to
// prefer.
type Problem string

const (
	// ProblemNone is the empty string so the zero value means "nothing wrong"
	// and the field can be omitempty on the wire.
	ProblemNone Problem = ""
	// ProblemEmpty is no program configured at all.
	ProblemEmpty Problem = "empty"
	// ProblemNotFound is a program that does not resolve. In a container this
	// is the overwhelmingly common answer and the interface says so
	// separately: the image ships ca-certificates, yt-dlp, ffmpeg, tzdata and
	// a JRE on alpine (Dockerfile), so systemctl, curl, ssh, sudo and bash
	// are all simply absent.
	ProblemNotFound Problem = "notFound"
	// ProblemNotExecutable is a file that IS there and cannot be executed -
	// the execute bit, or a file the running user cannot read. In the
	// container that user is uid 1000 and not root (Dockerfile's USER
	// knight), which is the fix nobody guesses on their own.
	ProblemNotExecutable Problem = "notExecutable"
	// ProblemPermission is the program starting and the operating system
	// refusing what it then tried to do. Distinct from ProblemNotExecutable
	// because the fix is somewhere else entirely: not a file mode, but what
	// this process is allowed to do to the machine.
	ProblemPermission Problem = "permission"
	// ProblemTimeout is the program still running when its limit expired, so
	// it was killed. NOT folded into ProblemExit, because it is the one
	// outcome here that routinely means the job WAS done: a command that
	// suspends the machine is killed on the way down about as often as it
	// returns.
	ProblemTimeout Problem = "timeout"
	// ProblemExit is a program that ran and reported failure itself.
	ProblemExit Problem = "exit"
	// ProblemNotSupported is the action being configured on a build that
	// cannot carry it out - quit with no RequestExit, suspend with no
	// RequestSuspend. It exists because Actions() is deliberately NOT
	// filtered by capability (see its own comment), so a stored action can
	// legitimately outrun the build that reads it, and the operator has to be
	// told rather than left with a countdown that promised something and did
	// nothing.
	ProblemNotSupported Problem = "notSupported"
)

// Check is what a preflight found. It never runs anything.
type Check struct {
	Problem Problem `json:"problem,omitempty"`
	// ResolvedPath is what the program name actually resolves to, which is
	// the single most useful line at 3am: "systemctl" resolving to nothing
	// and "/usr/bin/systemctl" resolving to itself are different problems
	// with different fixes.
	ResolvedPath string `json:"resolvedPath,omitempty"`
	// Argv is the exact argument vector, resolved program first - the answer
	// to "what will actually run", which matters most for the operator who
	// believes a shell is involved.
	Argv []string `json:"argv,omitempty"`
	// Deployment is "container" or "desktop". Filled in by the HTTP layer
	// (internal/api/routes_idleaction.go), NOT here: this package would have
	// to import internal/buildinfo to answer it, and its package doc promises
	// to stay out of the caller's vocabulary. The field lives here because it
	// travels with the answer and the browser needs it to pick between two
	// different explanations of the same problem code.
	Deployment string `json:"deployment"`
}

// Runner executes an external program and reports its combined output. A
// function type rather than a direct call to os/exec, copied from
// reconnect.Runner for its exact reason: a test can then prove which program
// WOULD have run without any test run ever spawning a process.
type Runner func(ctx context.Context, name string, args ...string) (string, error)

// ExecRunner is the default Runner.
//
// CombinedOutput, so the program's own complaint is captured whichever stream
// it chose - reconnect's execRunner makes the same call for the same stated
// reason: a command that fails silently is untraceable, because an exit
// status alone never says which line gave up. The output is capped here
// rather than at the far end, so nothing downstream ever holds the megabyte.
func ExecRunner(ctx context.Context, name string, args ...string) (string, error) {
	out, err := exec.CommandContext(ctx, name, args...).CombinedOutput()
	return TrimOutput(string(out)), err
}

// TrimOutput cuts a program's output down to something a log line, a
// websocket broadcast and a settings card can all carry.
func TrimOutput(s string) string {
	s = strings.TrimSpace(s)
	if len(s) > maxCommandOutput {
		return s[:maxCommandOutput] + "..."
	}
	return s
}

// Preflight resolves the program and reports what it found, WITHOUT running
// anything. This is the button somebody presses at 3am, and the whole reason
// it can be trusted is that pressing it costs nothing: it stats a file, it
// does not suspend a machine.
//
// lookPath is injected so this is testable against a table of answers rather
// than against whatever happens to be installed on the machine running the
// tests - pass nil for exec.LookPath, which is the same resolution
// exec.Command itself performs, including Windows' extension handling. Using
// os.Stat instead would be a second, subtly different resolver: it would
// refuse a bare "systemctl" that would in fact have run perfectly.
func (s CommandSpec) Preflight(lookPath func(string) (string, error)) Check {
	if lookPath == nil {
		lookPath = exec.LookPath
	}
	prog := strings.TrimSpace(s.Program)
	if prog == "" {
		return Check{Problem: ProblemEmpty}
	}
	path, err := lookPath(prog)
	if err != nil {
		return Check{Problem: lookProblem(err)}
	}
	argv := make([]string, 0, len(s.Args)+1)
	argv = append(argv, path)
	argv = append(argv, s.Args...)
	return Check{ResolvedPath: path, Argv: argv}
}

// lookProblem turns a lookup failure into one of the closed codes.
//
// A permission error here means the file exists and cannot be executed, which
// is exec.LookPath's own contract (it checks the executable bit and reports
// fs.ErrPermission when it is missing) - so it maps to ProblemNotExecutable
// rather than to ProblemPermission, which is about what a program is allowed
// to DO once it has started. Anything else at all is reported as
// ProblemNotFound: every remaining lookup failure means "this program cannot
// be started as written", and that is the sentence which then names what the
// container image does and does not contain, which is the actionable half.
func lookProblem(err error) Problem {
	switch {
	case errors.Is(err, fs.ErrPermission):
		return ProblemNotExecutable
	default:
		return ProblemNotFound
	}
}

// Classify turns the outcome of one run into a code and, where there is one,
// the program's own exit status.
//
// timedOut is passed in rather than sniffed from the error because only the
// caller can tell the two cancellations apart: a context that expired because
// the SPEC's timeout ran out is a timeout, and a context cancelled because
// the app is shutting down is not - and both surface here as the same killed
// process. internal/app/app_idle_command.go checks
// errors.Is(ctx.Err(), context.DeadlineExceeded) for exactly that reason.
func Classify(err error, timedOut bool) (Problem, int) {
	if timedOut {
		// Checked BEFORE err == nil on purpose: a program killed at the
		// deadline can still exit 0 on some platforms, and reporting that as
		// a clean run would tell the operator the machine went to sleep when
		// what actually happened is that we stopped waiting to find out.
		return ProblemTimeout, 0
	}
	if err == nil {
		return ProblemNone, 0
	}
	// An INTERFACE and not *exec.ExitError, which is what this matched
	// first. os.ProcessState's fields are unexported and there is no
	// portable way to build one, so a table test could only reach this
	// branch by actually spawning a process - and a test suite that shells
	// out fails differently on every machine, which is the exact thing
	// Runner and Preflight's injected lookPath exist to avoid. *exec.ExitError
	// satisfies this interface, so production behaviour is unchanged, and a
	// test can now hand over an error that reports the status it wants.
	var coder interface{ ExitCode() int }
	switch {
	case errors.Is(err, exec.ErrNotFound), errors.Is(err, fs.ErrNotExist):
		return ProblemNotFound, 0
	case errors.Is(err, fs.ErrPermission):
		// ProblemPermission and NOT ProblemNotExecutable, which is the split
		// between this function and Preflight and is worth stating: a
		// preflight has stat'ed the file and can honestly say "the execute
		// bit is missing", so it gets the file-mode sentence. A refused RUN
		// knows less than that - it can be the mode, a nosuid/noexec mount,
		// a seccomp or no-new-privileges policy - so it gets the sentence
		// about what THIS PROCESS is allowed to do, which in the container
		// means uid 1000 with no root and no reach onto the host. Sending
		// somebody to chmod a file that is already 0755 is worse than saying
		// less.
		return ProblemPermission, 0
	case errors.As(err, &coder):
		return ProblemExit, coder.ExitCode()
	default:
		// Something went wrong that is not the program's own verdict - a
		// fork failure, a broken pipe on the output. ProblemPermission is
		// deliberately NOT the catch-all: guessing "you are not allowed"
		// sends the operator to look at uids and capabilities for a problem
		// that is neither. ProblemExit with an exit code of -1, which is
		// what os/exec itself uses for "never got a status", keeps the
		// program's own captured output as the thing the sentence shows,
		// and that output is the only real evidence in this branch.
		return ProblemExit, -1
	}
}
