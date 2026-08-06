package settings

import (
	"testing"

	"github.com/junkerderprovinz/knightloader/internal/collide"
	"github.com/junkerderprovinz/knightloader/internal/dedupe"
	"github.com/junkerderprovinz/knightloader/internal/proxycfg"
	"github.com/junkerderprovinz/knightloader/internal/reconnect"
	"github.com/junkerderprovinz/knightloader/internal/rules"
	"github.com/junkerderprovinz/knightloader/internal/schedule"
)

// brokenFilter is a rule set nobody can compile: the pattern opens a group it
// never closes. It is the shape the whole "do not sanitise a rule list" decision
// exists for.
func brokenFilter() rules.Set {
	return rules.Set{
		StopAfterMatch: true,
		Rules: []rules.Rule{{
			Name: "half a pattern",
			Conditions: []rules.Condition{
				{Field: rules.FieldFilename, Op: rules.OpMatches, Value: "(unclosed"},
			},
			Action: rules.Action{Reject: true, Reason: "not wanted"},
		}},
	}
}

// TestBrokenRuleSurvivesTheRoundTrip is the reason neither rule list has a
// sanitiser. A rule the engine cannot compile is exactly the rule the user has
// to see: dropped on save it disappears from the form with nothing to explain
// it, and for a filter that means links they go on believing are being blocked.
func TestBrokenRuleSurvivesTheRoundTrip(t *testing.T) {
	dir := t.TempDir()
	st, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	n := Defaults()
	n.LinkFilter = brokenFilter()

	saved, err := st.Set(n)
	if err != nil {
		t.Fatal(err)
	}
	if len(saved.LinkFilter.Rules) != 1 {
		t.Fatalf("Set returned %d filter rules, want the broken one kept", len(saved.LinkFilter.Rules))
	}
	if got := saved.LinkFilter.Rules[0].Conditions[0].Value; got != "(unclosed" {
		t.Errorf("the pattern came back as %q, want it untouched", got)
	}

	// The file on disk is what matters: this is the copy the user reopens the
	// form against tomorrow.
	reloaded, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	back := reloaded.Get()
	if len(back.LinkFilter.Rules) != 1 {
		t.Fatalf("after reload %d filter rules survived, want 1", len(back.LinkFilter.Rules))
	}
	if back.LinkFilter.Rules[0].Name != "half a pattern" {
		t.Errorf("the reloaded rule is named %q", back.LinkFilter.Rules[0].Name)
	}

	// And it is still reportable, which is the other half of the bargain: the
	// rule is kept unusable, not kept and quietly applied.
	m, problems := rules.Compile(back.LinkFilter)
	if len(problems) != 1 {
		t.Fatalf("Compile reported %d problems, want the broken rule named", len(problems))
	}
	if !m.Empty() {
		t.Error("the broken rule was compiled into the matcher anyway")
	}
}

// TestSanitizeKeepsWhatOnlyTheUserCanFix pins which of the new fields are folded
// onto a safe value and which are left exactly as written. The split is the
// whole design: a policy string is a choice from a fixed menu and folding an
// unknown one costs nothing, while a rule list or a timetable is text the user
// wrote and folding it means deleting their work.
func TestSanitizeKeepsWhatOnlyTheUserCanFix(t *testing.T) {
	// A window a compiler would refuse: no weekday ticked.
	badWindow := schedule.Entry{Start: "22:00", End: "06:00", Action: schedule.ActionPause}

	in := Defaults()
	in.MirrorPolicy = "  FILENAME-ONLY "
	in.CollisionPolicy = "not a policy"
	in.CollisionMaxAttempts = collide.DefaultMaxAttempts * 10
	in.LinkFilter = brokenFilter()
	in.Schedule = []schedule.Entry{badWindow}

	got := sanitize(in)

	if got.MirrorPolicy != string(dedupe.PolicyFilenameOnly) {
		t.Errorf("mirror policy = %q, want the spelling folded onto the real one", got.MirrorPolicy)
	}
	if got.CollisionPolicy != string(collide.DefaultPolicy) {
		t.Errorf("collision policy = %q, want an unknown one folded onto the default", got.CollisionPolicy)
	}
	if got.CollisionMaxAttempts != collide.DefaultMaxAttempts {
		t.Errorf("attempt cap = %d, want it clamped to %d", got.CollisionMaxAttempts, collide.DefaultMaxAttempts)
	}
	if len(got.LinkFilter.Rules) != 1 {
		t.Error("sanitize deleted a filter rule; only Compile may refuse one, and only by reporting it")
	}
	if len(got.Schedule) != 1 || got.Schedule[0].Start != badWindow.Start || len(got.Schedule[0].Days) != 0 {
		t.Errorf("sanitize edited the timetable: %+v", got.Schedule)
	}
	if got.Schedule[0].Validate() == nil {
		t.Error("the unusable window was repaired; only the API may refuse it, and only with a reason")
	}
}

// TestSecretsSurviveARedactedRoundTrip is the settings form's actual journey: it
// is shown a redacted copy, the user flips something unrelated, and the whole
// object comes back. Without the merge in Set that visit clears the router
// password and every proxy password, and nobody notices until the next
// reconnect fails.
func TestSecretsSurviveARedactedRoundTrip(t *testing.T) {
	dir := t.TempDir()
	st, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	first := Defaults()
	first.Reconnect = reconnect.Config{
		Method:   reconnect.MethodHTTP,
		Username: "admin",
		Password: "router-secret",
		Requests: []reconnect.Request{{URL: "http://192.0.2.1/reboot"}},
		CheckURL: "http://192.0.2.9/ip",
	}
	first.Connections = []proxycfg.Entry{{
		Kind: proxycfg.KindHTTP, Host: "proxy.lan", Port: 8080,
		Username: "u", Password: "proxy-secret", Enabled: true,
	}}
	stored, err := st.Set(first)
	if err != nil {
		t.Fatal(err)
	}

	shown := stored.Redacted()
	if shown.Reconnect.Password != reconnect.RedactedPassword {
		t.Errorf("the router password left the process as %q", shown.Reconnect.Password)
	}
	if shown.Connections[0].Password != "" {
		t.Errorf("a proxy password left the process as %q", shown.Connections[0].Password)
	}

	// What the browser posts back: the redacted copy with one unrelated change.
	back := shown
	back.SpeedLimit = 1 << 20
	after, err := st.Set(back)
	if err != nil {
		t.Fatal(err)
	}
	if after.Reconnect.Password != "router-secret" {
		t.Errorf("router password after the round trip = %q, want it restored", after.Reconnect.Password)
	}
	if len(after.Connections) != 1 || after.Connections[0].Password != "proxy-secret" {
		t.Errorf("proxy password after the round trip = %+v, want it restored", after.Connections)
	}
	if after.SpeedLimit != 1<<20 {
		t.Errorf("the change the user actually made was lost: speed limit = %d", after.SpeedLimit)
	}

	// An empty password still clears it, which is the only way a stored password
	// can ever be removed through the form.
	cleared := after.Redacted()
	cleared.Reconnect.Password = ""
	final, err := st.Set(cleared)
	if err != nil {
		t.Fatal(err)
	}
	if final.Reconnect.Password != "" {
		t.Errorf("clearing the router password left %q behind", final.Reconnect.Password)
	}
}
