// Package idleaction answers what happens once the wait queue has nothing
// left to do on its own, and how long a person gets to change their mind
// before it does.
//
// Whether the queue is idle and what follows are the two questions answered
// here. When they are worth asking and what the action does when it fires
// belong to the caller, see internal/app/app_idle.go.
package idleaction

// Action is what fires once the countdown reaches zero.
type Action string

const (
	// ActionNone does nothing, and is the default on a fresh install and after
	// every upgrade that adds a field here: an idle action is opted into.
	ActionNone Action = "none"

	// ActionPause halts the queue through internal/app.App.SetHalted, which
	// needs nothing but the running process. It stays meaningful on a queue
	// that is already idle, because halting also holds it there against
	// whatever would start it again: a schedule window opening, a watch folder
	// dropping a job, a link pasted from another browser tab.
	ActionPause Action = "pause"

	// ActionQuit drains whatever is still in flight and stops the process
	// through internal/app.App.RequestExit, the field POST /api/system/quit
	// calls (internal/api/routes_lifecycle.go). That field is set by
	// cmd/knightloader and left nil by the desktop build, whose window and
	// tray own a graceful path of their own, so quit is offered in the
	// container and not on the desktop.
	//
	// Controller.tick's everBusy gate is what keeps a container started with
	// `restart: unless-stopped` from quitting, coming back to an idle queue
	// and quitting again.
	ActionQuit Action = "quit"

	// ActionCommand runs one external program, see CommandSpec and
	// internal/app/app_idle_command.go. Always offered: every deployment can
	// exec, and only the operator knows what is worth exec'ing. Preflight
	// answers whether it would run, without running it.
	//
	// It is a program plus arguments rather than a shell command line, the
	// same split internal/reconnect/config.go's Command/Args makes. A pipe, a
	// redirect or two commands in a row belong in a script file this points
	// at.
	ActionCommand Action = "command"

	// ActionSuspend asks the operating system to put this machine to sleep
	// through internal/app.App.RequestSuspend, nil everywhere except the
	// desktop build (desktop/power.go). A container's process is PID 1 in its
	// own namespace and cannot reach the host's power state, so the container
	// answer is ActionCommand pointed at something that can.
	ActionSuspend Action = "suspend"
)

// Actions is every action this codebase knows, in menu order, unfiltered by
// what the running build can carry out. A function rather than a package
// variable, so no caller can reorder the vocabulary for everybody else by
// mutating a shared slice.
//
// The filtered list is Offered, and the split between the two matters. This
// one is the validation vocabulary: validAction reads it, Sanitize reads
// validAction, and settings.sanitize runs Sanitize on every settings save.
// Filtering it by deployment or capability would rewrite a container's stored
// "suspend" to "none" on the next unrelated save, and a backup taken on a
// desktop and restored into a container carries settings.json with it. A
// stored action this build cannot perform is reported as a failed run when it
// fires (internal/app.App.fireIdleAction) instead.
//
// What decides whether an action is offered is whether the function behind it
// is wired, not which binary is running. See Capabilities.
func Actions() []Action {
	return []Action{ActionNone, ActionPause, ActionQuit, ActionCommand, ActionSuspend}
}

// Capabilities is what the host process can carry out, asked of the host
// rather than derived from buildinfo.Deployment. internal/app.App answers it
// (App.IdleCapabilities) by reading whether the matching function field is
// nil.
type Capabilities struct {
	CanQuit    bool
	CanCommand bool
	CanSuspend bool
}

// Offered is the menu: the actions a build with these capabilities can show.
// It is not a validator for a stored value, see Actions.
//
// ActionNone and ActionPause are unconditional: one is the no-opinion value,
// the other needs nothing but the running process.
func Offered(c Capabilities) []Action {
	out := make([]Action, 0, len(Actions()))
	for _, a := range Actions() {
		switch a {
		case ActionQuit:
			if !c.CanQuit {
				continue
			}
		case ActionCommand:
			if !c.CanCommand {
				continue
			}
		case ActionSuspend:
			if !c.CanSuspend {
				continue
			}
		}
		out = append(out, a)
	}
	return out
}

func validAction(a Action) bool {
	for _, x := range Actions() {
		if x == a {
			return true
		}
	}
	return false
}

// DefaultDelaySeconds mirrors JDownloader's own default for its shutdown
// countdown: long enough that a person glancing at the screen has time to
// read it, short enough that "cancelled" and "did nothing" do not feel like
// the same wait.
const DefaultDelaySeconds = 60

// minDelaySeconds keeps the countdown long enough to act on.
const minDelaySeconds = 5

// maxDelaySeconds is a day. Past that, Action=ActionNone already says "never"
// with no number attached to misread.
const maxDelaySeconds = 24 * 60 * 60

// Config is what the user configured.
type Config struct {
	// Action is what fires once the countdown reaches zero. ActionNone is both
	// the zero value's meaning and the on/off switch, so there is no separate
	// Enabled flag for it to disagree with, matching CollisionPolicy,
	// ExtractCollision and ArchiveDisposal in settings.Settings.
	Action Action `json:"action"`
	// DelaySeconds is how long the cancellable countdown runs before Action
	// fires. Zero is not instant, see minDelaySeconds.
	DelaySeconds int `json:"delaySeconds"`
	// Command is what ActionCommand runs, kept here rather than in a separate
	// settings field so one save writes one object and the two cannot drift
	// apart. It is filled in whatever Action is, and Sanitize leaves it alone,
	// so switching the action away and back keeps what the operator typed.
	Command CommandSpec `json:"command"`
}

// Defaults is what a fresh install has, armed at nothing. The command's own
// numbers are filled in as well, so a settings form does not open on a zero
// timeout that sanitize rewrites on the first save.
func Defaults() Config {
	return Config{
		Action:       ActionNone,
		DelaySeconds: DefaultDelaySeconds,
		Command:      CommandSpec{TimeoutSeconds: DefaultCommandTimeout},
	}
}

// Sanitize repairs a settings file an older or hand-edited build wrote. It
// always succeeds: settings.sanitize, which calls it on every Store.Set, does
// not fail a save over one field.
func (c Config) Sanitize() Config {
	if !validAction(c.Action) {
		c.Action = ActionNone
	}
	if c.DelaySeconds < minDelaySeconds {
		c.DelaySeconds = DefaultDelaySeconds
	}
	if c.DelaySeconds > maxDelaySeconds {
		c.DelaySeconds = maxDelaySeconds
	}
	c.Command = c.Command.Sanitize()
	return c
}

// Redacted returns a copy safe to hand to a browser or to write into a file
// attached to a bug report. See CommandSpec.Redacted for what is hidden.
func (c Config) Redacted() Config {
	c.Command = c.Command.Redacted()
	return c
}

// WithSecretsFrom puts back what Redacted removed, so a settings form that was
// shown a redacted config and sent it straight back does not wipe the stored
// command. Called from settings.Store.setLocked, under the lock that read
// prev.
func (c Config) WithSecretsFrom(prev Config) Config {
	c.Command = c.Command.WithSecretsFrom(prev.Command)
	return c
}
