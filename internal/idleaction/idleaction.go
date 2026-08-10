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
)

// Actions lists every action this build can offer, in menu order. A function
// rather than a package variable for the same reason internal/app.Priorities
// is: a caller must not be able to reorder the menu for everybody else by
// mutating a shared slice.
//
// OS-level actions - shut down, sleep, exit the app - belong on this list
// once something in the process can honestly promise to carry one out, and
// nothing can yet. internal/app's process is a container's PID 1 as often as
// not, and a shutdown call wired to that is a bug people would trip over
// hourly, not a feature (build-plan.md's Wave 10B brief). Checked again while
// this was written, now that Wave 10H's desktop build has landed alongside
// this one: it adds buildinfo.Deployment ("container"/"desktop") and
// App.RequestExit, and neither closes the gap by itself.
// buildinfo.Deployment says which binary is running, not that anything
// signed up to carry out a shutdown, sleep or hibernate of the HOST machine -
// 10H's own scope is tray and window chrome, not power state, and building
// that here, unreviewed and unasked for, is exactly the "wiring a shutdown
// call" this package exists to refuse. App.RequestExit is the opposite
// mismatch: it exists so a HEADLESS deployment can be told to quit over the
// API, and its own doc comment says the desktop build deliberately leaves it
// nil because its window chrome already has a graceful path of its own -
// using it here for "exit the app" would fire on exactly the deployment
// (container, behind a supervisor) where that same doc comment explains quit
// and restart are indistinguishable from outside, which is precisely the
// confusion an unattended idle action must not cause. Extending this list is
// a follow-up wired to a real power-state primitive, not a guess made now.
func Actions() []Action {
	return []Action{ActionNone, ActionPause}
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
}

// Defaults is what a fresh install has: armed at nothing, so this row is
// something to opt into rather than a surprise waiting in the defaults.
func Defaults() Config {
	return Config{Action: ActionNone, DelaySeconds: DefaultDelaySeconds}
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
	return c
}
