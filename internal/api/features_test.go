package api

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/reconnect"
	"github.com/junkerderprovinz/knightloader/internal/schedule"
	"github.com/junkerderprovinz/knightloader/internal/settings"
)

// TestEveryModuleWithoutASwitchSaysWhy is the invariant the whole registry
// exists for. A row with no switch and no reason renders as a dead control, and
// a dead control with nothing beside it is indistinguishable from a bug — which
// is exactly the report this table is meant to prevent.
func TestEveryModuleWithoutASwitchSaysWhy(t *testing.T) {
	for _, m := range featureList(testApp(t)) {
		if m.Switch == SwitchNone && strings.TrimSpace(m.Reason) == "" {
			t.Errorf("module %q cannot be switched and does not say why", m.ID)
		}
		if m.Verdict != VerdictShipped && strings.TrimSpace(m.Reason) == "" {
			t.Errorf("module %q is %q and does not say why", m.ID, m.Verdict)
		}
		if m.Verdict != VerdictShipped && m.Switch != SwitchNone {
			t.Errorf("module %q is %q but offers a switch; there is nothing running to switch", m.ID, m.Verdict)
		}
	}
}

// TestModulePagesExist keeps the two halves of the table pointing at each other.
// A module filed under a page that is not registered is a reason nobody ever
// reads, because the page it would have been printed on does not exist.
func TestModulePagesExist(t *testing.T) {
	pages := map[string]bool{}
	for _, p := range featurePages() {
		if pages[p.ID] {
			t.Errorf("settings page %q is registered twice", p.ID)
		}
		pages[p.ID] = true
	}
	ids := map[string]bool{}
	for _, m := range featureList(testApp(t)) {
		if ids[m.ID] {
			t.Errorf("module %q is listed twice", m.ID)
		}
		ids[m.ID] = true
		if m.Page != "" && !pages[m.Page] {
			t.Errorf("module %q is filed under page %q, which is not registered", m.ID, m.Page)
		}
	}
	for _, p := range featurePages() {
		for _, id := range p.Modules {
			if !ids[id] {
				t.Errorf("page %q lists module %q, which is not in the registry", p.ID, id)
			}
		}
	}
}

// TestSwitchesReachTheSubsystem is the test that makes "real kill switch" mean
// something. It does not check that a boolean was stored — storing a boolean is
// the failure — it checks that the state the subsystem itself reads has changed.
func TestSwitchesReachTheSubsystem(t *testing.T) {
	a := testApp(t)

	if err := setFeature(a, "extraction", false); err != nil {
		t.Fatal(err)
	}
	if a.Settings.Get().Extract {
		t.Error("extraction switched off, but the flag extractWanted reads is still set")
	}

	if err := setFeature(a, "crawler", false); err != nil {
		t.Fatal(err)
	}
	if a.Settings.Get().Crawl {
		t.Error("crawler switched off, but the flag the staging path reads is still set")
	}
}

// TestParkedSwitchRestoresWhatItCleared covers the failure a naive kill switch
// has: switching folder watch off has to stop the watcher, which means clearing
// the folder, which must not lose the folder.
func TestParkedSwitchRestoresWhatItCleared(t *testing.T) {
	a := testApp(t)
	// A real absolute path, because sanitize drops a relative watch folder and
	// "/tmp/..." is relative on Windows — the test would then be asserting
	// against a value the settings store never accepted.
	dir := t.TempDir()

	s := a.Settings.Get()
	s.WatchDir = dir
	if _, err := a.ApplySettings(s); err != nil {
		t.Fatal(err)
	}

	if err := setFeature(a, "watch", false); err != nil {
		t.Fatal(err)
	}
	if got := a.Settings.Get().WatchDir; got != "" {
		t.Fatalf("folder watch switched off but the folder is still %q, so the watcher is still polling", got)
	}
	if err := setFeature(a, "watch", true); err != nil {
		t.Fatal(err)
	}
	if got := a.Settings.Get().WatchDir; got != dir {
		t.Errorf("folder watch switched back on with %q, want the parked %q", got, dir)
	}
}

// TestParkingAnAlreadyEmptyValueKeepsTheOldOne is the one-way-door bug: two
// clients both switching an already-off module off would otherwise park an
// empty string over the folder that is waiting to come back.
func TestParkingAnAlreadyEmptyValueKeepsTheOldOne(t *testing.T) {
	a := testApp(t)
	dir := t.TempDir()

	s := a.Settings.Get()
	s.WatchDir = dir
	if _, err := a.ApplySettings(s); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		if err := setFeature(a, "watch", false); err != nil {
			t.Fatal(err)
		}
	}
	if err := setFeature(a, "watch", true); err != nil {
		t.Fatal(err)
	}
	if got := a.Settings.Get().WatchDir; got != dir {
		t.Errorf("after three switch-offs the parked folder is %q, want %q", got, dir)
	}
}

// TestSwitchingOnWithNothingParkedSaysSo. Answering 204 and leaving the module
// off would look like the switch is broken; the message names the page the
// value has to be set on.
func TestSwitchingOnWithNothingParkedSaysSo(t *testing.T) {
	a := testApp(t)
	for _, id := range []string{"watch", "scheduler", "reconnect"} {
		err := setFeature(a, id, true)
		if err == nil {
			t.Errorf("%s switched on out of nothing and reported success", id)
			continue
		}
		if !strings.Contains(err.Error(), "page") {
			t.Errorf("%s: %q does not say where to set it", id, err)
		}
	}
}

// TestUnswitchableModulesAreRefused. The table already tells the client which
// rows can be switched, so a request for one that cannot is a client bug, and
// answering 200 to it makes that bug permanent and invisible.
func TestUnswitchableModulesAreRefused(t *testing.T) {
	a := testApp(t)
	for _, id := range []string{"cnl", "captcha", "tray", "federation", "nonsense"} {
		if err := setFeature(a, id, false); err == nil {
			t.Errorf("%s has no switch but setFeature accepted it", id)
		}
	}
}

// TestEnabledIsDerivedNotStored. The park is a convenience, never the answer to
// "is this on": a settings write from anywhere else — the Advanced table, a
// script, another browser — has to move the module row with it, or the page
// says off while the watcher adds links.
func TestEnabledIsDerivedNotStored(t *testing.T) {
	a := testApp(t)
	if err := setFeature(a, "watch", false); err != nil {
		t.Fatal(err)
	}
	s := a.Settings.Get()
	s.WatchDir = t.TempDir()
	if _, err := a.ApplySettings(s); err != nil {
		t.Fatal(err)
	}
	for _, m := range featureList(a) {
		if m.ID == "watch" && !m.Enabled {
			t.Error("a folder was set outside the switch and the module still reports off")
		}
	}
}

// TestScheduleAndReconnectParkTheirOwnShape checks the two parked values that
// are not strings survive the JSON round trip through the park document.
func TestScheduleAndReconnectParkTheirOwnShape(t *testing.T) {
	a := testApp(t)
	s := a.Settings.Get()
	s.Schedule = []schedule.Entry{{
		Name: "nightly", Days: []time.Weekday{time.Monday},
		Start: "22:00", End: "06:00", Action: schedule.ActionPause,
	}}
	s.Reconnect.Method = reconnect.MethodCommand
	s.Reconnect.Command = "/bin/true"
	if _, err := a.ApplySettings(s); err != nil {
		t.Fatal(err)
	}

	for _, id := range []string{"scheduler", "reconnect"} {
		if err := setFeature(a, id, false); err != nil {
			t.Fatalf("%s off: %v", id, err)
		}
	}
	if got := a.Settings.Get(); len(got.Schedule) != 0 || got.Reconnect.Method != reconnect.MethodNone {
		t.Fatalf("switching off left schedule=%d method=%q", len(got.Schedule), got.Reconnect.Method)
	}
	for _, id := range []string{"scheduler", "reconnect"} {
		if err := setFeature(a, id, true); err != nil {
			t.Fatalf("%s on: %v", id, err)
		}
	}
	got := a.Settings.Get()
	if len(got.Schedule) != 1 || got.Schedule[0].Name != "nightly" {
		t.Errorf("the timetable did not come back: %+v", got.Schedule)
	}
	if got.Reconnect.Method != reconnect.MethodCommand {
		t.Errorf("reconnect method came back as %q, want %q", got.Reconnect.Method, reconnect.MethodCommand)
	}
	// The password is not part of what the switch parked, so the round trip must
	// not have disturbed it — a kill switch that clears a credential on the way
	// past is a data-loss bug wearing a toggle.
	if got.Reconnect.Command != "/bin/true" {
		t.Errorf("the reconnect command was lost: %q", got.Reconnect.Command)
	}
}

// TestDefaultsAreRedacted. The route feeds the per-row reset in the advanced
// table, so it is served to a browser like any other settings response — and a
// default that is a secret today is a secret tomorrow.
func TestDefaultsAreRedacted(t *testing.T) {
	b, err := json.Marshal(settings.Defaults().Redacted())
	if err != nil {
		t.Fatal(err)
	}
	var back settings.Settings
	if err := json.Unmarshal(b, &back); err != nil {
		t.Fatal(err)
	}
	if back.Reconnect.Password != "" && back.Reconnect.Password != reconnect.RedactedPassword {
		t.Errorf("the defaults carry a router password: %q", back.Reconnect.Password)
	}
}
