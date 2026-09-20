package idleaction

import (
	"reflect"
	"testing"
)

func TestDefaultsAreOff(t *testing.T) {
	d := Defaults()
	if d.Action != ActionNone {
		t.Errorf("a fresh install must not be armed, got action %q", d.Action)
	}
	if d.DelaySeconds != DefaultDelaySeconds {
		t.Errorf("DelaySeconds = %d, want %d", d.DelaySeconds, DefaultDelaySeconds)
	}
	// reflect.DeepEqual and not ==: Config is not comparable, because
	// CommandSpec.Args is a slice.
	if got := d.Sanitize(); !reflect.DeepEqual(got, d) {
		t.Errorf("Defaults() must already be sane: Sanitize() changed it to %+v", got)
	}
}

// The command spec inside the defaults has to be sane on its own, or the
// settings form opens on a zero that sanitize rewrites on the first save.
func TestDefaultsGiveTheCommandAWorkingTimeout(t *testing.T) {
	d := Defaults()
	if d.Command.TimeoutSeconds != DefaultCommandTimeout {
		t.Errorf("Command.TimeoutSeconds = %d, want %d", d.Command.TimeoutSeconds, DefaultCommandTimeout)
	}
	if d.Command.Configured() {
		t.Error("a fresh install must not come with a program to run")
	}
}

func TestSanitizeFoldsUnknownActionToNone(t *testing.T) {
	got := Config{Action: "shutdown-the-datacenter", DelaySeconds: 30}.Sanitize()
	if got.Action != ActionNone {
		t.Errorf("Action = %q, want %q for an action this build does not know", got.Action, ActionNone)
	}
}

func TestSanitizeClampsDelay(t *testing.T) {
	cases := []struct {
		name string
		in   int
		want int
	}{
		{"too low falls back to the default, not the minimum", 1, DefaultDelaySeconds},
		{"exactly the minimum is left alone", minDelaySeconds, minDelaySeconds},
		{"a sane value is left alone", 120, 120},
		{"too high is capped at a day", maxDelaySeconds + 1000, maxDelaySeconds},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := Config{Action: ActionPause, DelaySeconds: c.in}.Sanitize()
			if got.DelaySeconds != c.want {
				t.Errorf("DelaySeconds = %d, want %d", got.DelaySeconds, c.want)
			}
		})
	}
}

func TestSanitizeOfTheZeroValueIsOffNotJustClamped(t *testing.T) {
	// The zero value, a settings file with no "idleAction" key at all, has to
	// sanitize to something inert: a field a build adds must not arm itself
	// on an existing install.
	got := Config{}.Sanitize()
	if got.Action != ActionNone {
		t.Errorf("the zero value must sanitize to ActionNone, got %q", got.Action)
	}
}

func TestActionsIsNoneFirst(t *testing.T) {
	a := Actions()
	if len(a) == 0 || a[0] != ActionNone {
		t.Fatalf("Actions() = %v, want ActionNone first", a)
	}
	for _, x := range a {
		if x == ActionNone {
			continue
		}
		// Every non-none action must be something Sanitize would keep, or the
		// menu and the storage layer would disagree about what is valid.
		if got := (Config{Action: x, DelaySeconds: DefaultDelaySeconds}).Sanitize(); got.Action != x {
			t.Errorf("Actions() offers %q but Sanitize folds it to %q", x, got.Action)
		}
	}
}

// Actions is the validation vocabulary and Offered is the menu. Folding them
// into one list filtered by capability costs an operator their configuration
// with no message anywhere: settings.sanitize runs Config.Sanitize on every
// save, so a container holding "suspend", hand-edited or restored from a
// desktop backup, would have it rewritten to "none" by an unrelated save.
func TestSanitizeKeepsAnActionThisBuildCannotOffer(t *testing.T) {
	nothingWired := Capabilities{}
	for _, a := range []Action{ActionQuit, ActionSuspend, ActionCommand} {
		if offered(nothingWired, a) && a != ActionCommand {
			t.Fatalf("Offered() lists %q on a build with nothing wired", a)
		}
		got := Config{Action: a, DelaySeconds: DefaultDelaySeconds}.Sanitize()
		if got.Action != a {
			t.Errorf("Sanitize rewrote a stored %q to %q; an unrelated save would take the "+
				"operator's end-of-queue action away", a, got.Action)
		}
	}
}

func TestOfferedFollowsTheWiringAndNothingElse(t *testing.T) {
	// Nothing wired: the two that need nothing but the running process, plus
	// the command, which every deployment can exec. What it can usefully exec
	// is the preflight's question.
	bare := Offered(Capabilities{CanCommand: true})
	want := []Action{ActionNone, ActionPause, ActionCommand}
	if len(bare) != len(want) {
		t.Fatalf("Offered() = %v, want %v", bare, want)
	}
	for i := range want {
		if bare[i] != want[i] {
			t.Fatalf("Offered() = %v, want %v", bare, want)
		}
	}

	// The container has RequestExit wired and RequestSuspend not.
	container := Offered(Capabilities{CanQuit: true, CanCommand: true})
	if !offered(Capabilities{CanQuit: true, CanCommand: true}, ActionQuit) {
		t.Errorf("Offered() = %v, want quit on a build whose RequestExit is wired", container)
	}
	if offered(Capabilities{CanQuit: true, CanCommand: true}, ActionSuspend) {
		t.Errorf("Offered() = %v, want no suspend where nothing can carry one out", container)
	}

	// Everything wired keeps menu order, which is Actions' order.
	full := Offered(Capabilities{CanQuit: true, CanCommand: true, CanSuspend: true})
	all := Actions()
	if len(full) != len(all) {
		t.Fatalf("Offered() = %v, want the whole list %v", full, all)
	}
	for i := range all {
		if full[i] != all[i] {
			t.Fatalf("Offered() = %v, want %v in the same order", full, all)
		}
	}
}

func offered(c Capabilities, a Action) bool {
	for _, x := range Offered(c) {
		if x == a {
			return true
		}
	}
	return false
}

// The other direction: a menu entry Sanitize would fold to "none" is a control
// that saves and then does nothing.
func TestEveryOfferedActionIsAlsoValid(t *testing.T) {
	for _, a := range Offered(Capabilities{CanQuit: true, CanCommand: true, CanSuspend: true}) {
		if !validAction(a) {
			t.Errorf("Offered() lists %q, which Sanitize does not accept", a)
		}
	}
}

func TestActionsReturnsAFreshSliceEachCall(t *testing.T) {
	// A caller must not be able to reorder or truncate the menu for everybody
	// else by mutating what it got back.
	a := Actions()
	a[0] = "tampered"
	b := Actions()
	if b[0] != ActionNone {
		t.Fatalf("Actions() shares backing storage across calls: got %q after mutating a previous result", b[0])
	}
}
