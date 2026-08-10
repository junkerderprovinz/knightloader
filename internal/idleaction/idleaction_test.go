package idleaction

import "testing"

func TestDefaultsAreOff(t *testing.T) {
	d := Defaults()
	if d.Action != ActionNone {
		t.Errorf("a fresh install must not be armed, got action %q", d.Action)
	}
	if d.DelaySeconds != DefaultDelaySeconds {
		t.Errorf("DelaySeconds = %d, want %d", d.DelaySeconds, DefaultDelaySeconds)
	}
	if got := d.Sanitize(); got != d {
		t.Errorf("Defaults() must already be sane: Sanitize() changed it to %+v", got)
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
	// The Go zero value (a settings file with no "idleAction" key at all, or
	// one this build has never written) must sanitize to something inert -
	// the migration-safety rule build-plan.md section 4's conflict 7 states
	// for Task.Enabled applies here for the same reason: a field this build
	// adds must never retroactively arm itself on an existing install.
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

func TestActionsReturnsAFreshSliceEachCall(t *testing.T) {
	// A caller must not be able to reorder or truncate the menu for
	// everybody else by mutating what it got back - the same property
	// internal/app.Priorities() guarantees for its own menu.
	a := Actions()
	a[0] = "tampered"
	b := Actions()
	if b[0] != ActionNone {
		t.Fatalf("Actions() shares backing storage across calls: got %q after mutating a previous result", b[0])
	}
}
