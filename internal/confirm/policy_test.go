package confirm

import "testing"

func TestParseFoldsUnknownAndUseGlobalToDefault(t *testing.T) {
	for _, s := range []string{"", "bogus", "USE-GLOBAL", "  ask  extra"} {
		if got := Parse(s); got != DefaultPolicy {
			t.Errorf("Parse(%q) = %q, want the default %q", s, got, DefaultPolicy)
		}
	}
}

// TestParseNeverDefaultsToExcludeAndRemove pins the one rule this whole
// package exists to enforce: a settings file this build cannot read must
// never be interpreted as permission to delete something.
func TestParseNeverDefaultsToExcludeAndRemove(t *testing.T) {
	for _, s := range []string{"", "nonsense", "exclude-and-remove-ish", "delete"} {
		if got := Parse(s); got == ExcludeAndRemove {
			t.Errorf("Parse(%q) = exclude-and-remove; a default may never delete", s)
		}
	}
}

func TestParseAcceptsEveryRealPolicyCaseInsensitively(t *testing.T) {
	for _, p := range []Policy{Include, Exclude, ExcludeAndRemove, Ask} {
		if got := Parse(string(p)); got != p {
			t.Errorf("Parse(%q) = %q, want it unchanged", p, got)
		}
		if got := Parse("  " + string(p) + "  "); got != p {
			t.Errorf("Parse of a padded %q = %q, want it trimmed and unchanged", p, got)
		}
	}
}

func TestResolveMatrix(t *testing.T) {
	cases := []struct {
		name        string
		batch       Policy
		global      Policy
		interactive bool
		want        Policy
	}{
		{"a concrete batch policy wins outright", Include, Exclude, true, Include},
		{"use-global defers to whatever global is", UseGlobal, Include, true, Include},
		{"an empty batch value reads the same as use-global", "", ExcludeAndRemove, false, ExcludeAndRemove},
		{"a garbled batch value also defers to global", "not-a-policy", Exclude, true, Exclude},
		{"ask stays ask when a person is watching", Ask, Exclude, true, Ask},
		{"ask falls back to global when nobody is watching", Ask, Include, false, Include},
		{"ask falls back to global's own exclude-and-remove, not softened", Ask, ExcludeAndRemove, false, ExcludeAndRemove},
		{"use-global deferring to an ask global still asks when interactive", UseGlobal, Ask, true, Ask},
		{"a global that is itself ask, asked non-interactively, settles on the package default", Ask, Ask, false, DefaultPolicy},
		{"a corrupt global (use-global) settles on the package default", UseGlobal, UseGlobal, true, DefaultPolicy},
		{"a corrupt global (empty) settles on the package default", "", "", false, DefaultPolicy},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := Resolve(c.batch, c.global, c.interactive); got != c.want {
				t.Errorf("Resolve(%q, %q, interactive=%v) = %q, want %q",
					c.batch, c.global, c.interactive, got, c.want)
			}
		})
	}
}

// TestResolveNeverInventsExcludeAndRemove is Resolve's own half of the
// package's one hard rule: nothing here may turn a batch or a global default
// that never named ExcludeAndRemove into one that does.
func TestResolveNeverInventsExcludeAndRemove(t *testing.T) {
	for _, batch := range Policies() {
		for _, global := range []Policy{Include, Exclude, Ask, "", "garbage"} {
			for _, interactive := range []bool{true, false} {
				got := Resolve(batch, global, interactive)
				if got == ExcludeAndRemove && batch != ExcludeAndRemove {
					t.Errorf("Resolve(%q, %q, %v) = exclude-and-remove out of nowhere", batch, global, interactive)
				}
			}
		}
	}
}

func TestTriggerInteractive(t *testing.T) {
	cases := map[Trigger]bool{
		TriggerManual:      true,
		TriggerAutoConfirm: false,
		TriggerWatch:       false,
		TriggerCnL:         false,
		Trigger("unknown"): false,
	}
	for trig, want := range cases {
		if got := trig.Interactive(); got != want {
			t.Errorf("%q.Interactive() = %v, want %v", trig, got, want)
		}
	}
}

// TestResolveConfigResolvesBothAxesIndependently is the shape ConfirmTasks
// actually calls: onDupes and onOffline may disagree about everything,
// including whether a person is there to ask, and neither axis's answer may
// leak into the other's.
func TestResolveConfigResolvesBothAxesIndependently(t *testing.T) {
	batch := Config{OnDupes: Include, OnOffline: UseGlobal}
	global := Config{OnDupes: ExcludeAndRemove, OnOffline: Ask}

	got := ResolveConfig(batch, global, TriggerManual)
	want := Config{OnDupes: Include, OnOffline: Ask}
	if got != want {
		t.Errorf("interactive ResolveConfig = %+v, want %+v", got, want)
	}

	got = ResolveConfig(batch, global, TriggerAutoConfirm)
	want = Config{OnDupes: Include, OnOffline: DefaultPolicy}
	if got != want {
		t.Errorf("non-interactive ResolveConfig = %+v, want %+v (onOffline's ask has nobody to ask, and the global itself is ask)", got, want)
	}
}
