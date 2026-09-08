// Package idleaction answers what happens once the wait queue has nothing
// left to do on its own, and how long a person gets to change their mind
// before it does.
//
// It is deliberately small and deliberately conservative about what it will
// promise. "The queue is idle" and "then what" are the two questions this
// package answers; deciding WHEN they are worth asking (the queue actually
// changed, a settings save) and WHAT the action does when it fires (halt the
// queue, one day something a desktop build can honestly do) both belong to
// the caller - see internal/app/app_idle.go for how KnightLoader answers
// them today.
package idleaction

// Action is what fires once the countdown reaches zero.
type Action string

const (
	// ActionNone does nothing. It is the default, and it stays the default
	// after every fresh install and every upgrade that adds a field here: an
	// idle action is something to opt into, never a surprise sprung on
	// somebody who has not looked at this settings page - the same reasoning
	// build-plan.md section 4's conflict 7 already applies to Task.Enabled.
	ActionNone Action = "none"

	// ActionPause halts the queue - internal/app.App.SetHalted - which works
	// on every deployment this app ships on, container included: it needs
	// nothing but the process already running. It stays meaningful even
	// though the queue is already idle by the time it fires, because halting
	// also HOLDS the queue there against whatever would otherwise start it
	// again - a schedule window ending, a watch folder dropping a new job, a
	// link pasted from another browser tab.
	ActionPause Action = "pause"

	// ActionQuit drains whatever is still in flight and stops the process,
	// through the one path that already exists for it: internal/app.App's
	// RequestExit, the very field POST /api/system/quit calls
	// (internal/api/routes_lifecycle.go). It needs that field to be wired,
	// which is why Capabilities.CanQuit exists - and note which deployment
	// that leaves it on. RequestExit is set by cmd/knightloader and left nil
	// by the desktop build on purpose (see its own doc comment on App), so
	// quit is offered in the CONTAINER and not on the desktop, which reads
	// backwards until you know that desktop's window and tray already own a
	// graceful path of their own and quit/restart answer 501 there today.
	//
	// READ Controller.tick's everBusy gate before touching anything near
	// this one. That gate is the only reason a container started with
	// `restart: unless-stopped` - the Unraid template's own default - does
	// not quit, come back, find an idle queue, quit again, forever. While
	// the only action was ActionPause the worst that gate prevented was a
	// paused queue; it now stands between this build and a restart loop, and
	// TestDoesNotArmOnAnIdleBootWithAPersistedConfig is what pins it.
	ActionQuit Action = "quit"

	// ActionCommand runs one external program - see CommandSpec, and
	// internal/app/app_idle_command.go for the run itself. Always offered:
	// every deployment can exec, and what it can usefully exec is a question
	// only the operator can answer, so refusing it anywhere would be this
	// package guessing about somebody else's image (Preflight is what
	// answers that question honestly, without running anything).
	//
	// It is deliberately a program plus arguments and NOT a shell command
	// line, the same split internal/reconnect/config.go's Command/Args made
	// and for the same stated reason. A pipe, a redirect or two commands in
	// a row belong in a script file this points at.
	ActionCommand Action = "command"

	// ActionSuspend asks the operating system to put THIS machine to sleep,
	// through internal/app.App.RequestSuspend - nil everywhere except the
	// desktop build, which is the only place anything signed up to carry it
	// out (desktop/power.go). A container's process cannot do this and must
	// never claim it can: it is PID 1 in its own namespace and the host's
	// power state is not reachable from inside, which is exactly the
	// "wiring a shutdown call to a container's PID 1" this package spent its
	// first two waves refusing. The honest container answer is ActionCommand
	// pointed at something that reaches the host.
	ActionSuspend Action = "suspend"
)

// Actions is every action this codebase knows, in menu order, NEVER filtered
// by what the running build can carry out. A function rather than a package
// variable for the same reason internal/app.Priorities is: a caller must not
// be able to reorder the vocabulary for everybody else by mutating a shared
// slice.
//
// THE FILTERED LIST IS Offered, AND THE SPLIT BETWEEN THE TWO IS LOAD-BEARING.
// This one is the VALIDATION VOCABULARY: validAction reads it, Sanitize reads
// validAction, and settings.sanitize runs Sanitize on every single settings
// save (internal/settings/settings.go's sanitize chain). Filter this list by
// deployment or by capability - the obvious, tidy-looking change - and a
// container whose settings.json holds "suspend" has that value rewritten to
// "none" by the very next unrelated save: somebody changes the download
// folder and their end-of-queue action is gone, with no message anywhere. It
// breaks across builds too, since a backup taken on a desktop and restored
// into a container carries settings.json with it (internal/api/routes_backup.go).
// A stored action this build cannot perform is reported as a failed run when
// it fires (see internal/app.App.fireIdleAction) - loudly, once - which is a
// far better outcome than silently rewriting what the operator chose.
//
// The list used to hold only ActionNone and ActionPause, with a long comment
// refusing OS-level actions until "something in the process can honestly
// promise to carry one out". That condition is now met rather than waived:
// ActionQuit goes through App.RequestExit, the same field the quit route
// already calls, and ActionSuspend goes through App.RequestSuspend, which the
// desktop build wires to a real per-OS power call (desktop/power_*.go) and
// every other build leaves nil. Neither is guessed at from
// buildinfo.Deployment, which was the specific mistake that comment was
// written to prevent: what decides whether an action is offered is whether a
// FUNCTION is wired, not which binary is running. See Capabilities.
func Actions() []Action {
	return []Action{ActionNone, ActionPause, ActionQuit, ActionCommand, ActionSuspend}
}

// Capabilities is what the host process can actually carry out, asked of the
// host rather than derived from buildinfo.Deployment - see Actions' own
// comment for why that difference is the whole point. internal/app.App
// answers it (App.IdleCapabilities) by reading whether the matching function
// field is nil, the same "nil means not supported here" convention
// App.RequestExit established and routes_lifecycle.go already reads.
type Capabilities struct {
	CanQuit    bool
	CanCommand bool
	CanSuspend bool
}

// Offered is the MENU: the actions a build with these capabilities can
// honestly show. Nothing else may filter, and this must never be used to
// validate a stored value.
//
// ActionNone and ActionPause are unconditional - one is the no-opinion value
// and the other needs nothing but the process already running.
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

// minDelaySeconds keeps the countdown a countdown rather than a fire-now
// switch wearing a number nobody has time to act on.
const minDelaySeconds = 5

// maxDelaySeconds is a day. Past that, Action=ActionNone already says "never"
// with no number attached to misread.
const maxDelaySeconds = 24 * 60 * 60

// Config is what the user configured.
type Config struct {
	// Action is what fires once the countdown reaches zero. ActionNone is
	// both the zero value's meaning and the whole on/off switch - there is no
	// separate Enabled flag, matching every other plain-enum switch already
	// in settings.Settings (CollisionPolicy, ExtractCollision,
	// ArchiveDisposal): a redundant bool alongside the enum is one more place
	// for "on" and "the enum" to disagree.
	Action Action `json:"action"`
	// DelaySeconds is how long the cancellable countdown runs before Action
	// fires. Never read as instant at zero - see minDelaySeconds - because an
	// action nobody had time to notice, let alone cancel, defeats the one
	// thing a countdown is for.
	DelaySeconds int `json:"delaySeconds"`
	// Command is what ActionCommand runs. Kept here rather than in a
	// separate top-level settings field so that one save writes one object:
	// the action and the thing the action does cannot drift apart into two
	// documents where one of them is stale. It is filled in whether or not
	// Action is ActionCommand, and switching the action away and back must
	// not cost the operator what they typed - which is also why nothing in
	// Sanitize clears it when the action is something else.
	Command CommandSpec `json:"command"`
}

// Defaults is what a fresh install has: armed at nothing, so this row is
// something to opt into rather than a surprise waiting in the defaults.
// The command's own numbers are filled in too, even though there is no
// command: a settings form that opens on a zero timeout would show a number
// that sanitize rewrites the first time anything at all is saved, which is a
// control that lies about what saving it did.
func Defaults() Config {
	return Config{
		Action:       ActionNone,
		DelaySeconds: DefaultDelaySeconds,
		Command:      CommandSpec{TimeoutSeconds: DefaultCommandTimeout},
	}
}

// Sanitize repairs what a caller should never be refused over: reading a
// settings file an older or hand-edited build wrote. It always succeeds,
// which is the point - the one path that feeds it (settings.sanitize, called
// from every settings.Store.Set) never fails a save over one field, the same
// rule sanitizeQueue's MaxRetries clamp and sanitizeConfirm's
// AutoConfirmDelay clamp already follow for a number of exactly this shape.
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
// somebody attaches to a public bug report - see CommandSpec.Redacted for
// what is hidden and why the whole command line goes rather than a guess at
// which part of it is the secret.
func (c Config) Redacted() Config {
	c.Command = c.Command.Redacted()
	return c
}

// WithSecretsFrom puts back what Redacted removed, so a settings form that
// was shown a redacted config and sent it straight back does not wipe the
// stored command. The mirror image of reconnect.Config.WithSecretsFrom, and
// called from the same place: settings.Store.setLocked, under the lock that
// read prev.
func (c Config) WithSecretsFrom(prev Config) Config {
	c.Command = c.Command.WithSecretsFrom(prev.Command)
	return c
}
