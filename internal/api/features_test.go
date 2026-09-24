package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/app"
	"github.com/junkerderprovinz/knightloader/internal/cnl"
	"github.com/junkerderprovinz/knightloader/internal/reconnect"
	"github.com/junkerderprovinz/knightloader/internal/schedule"
	"github.com/junkerderprovinz/knightloader/internal/settings"
)

// TestEveryModuleWithoutASwitchSaysWhy checks that no row renders as a dead
// control without a reason beside it.
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

// TestModulePagesExist keeps the module rows and the page list pointing at
// each other.
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

// TestSwitchesReachTheSubsystem checks that a switch changes the state the
// subsystem itself reads.
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

// TestParkedSwitchRestoresWhatItCleared checks that switching folder watch off
// clears the folder and switching it on brings the same folder back.
func TestParkedSwitchRestoresWhatItCleared(t *testing.T) {
	a := testApp(t)
	// Sanitize drops a relative watch folder, and "/tmp/..." is relative on
	// Windows.
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

// TestParkingAnAlreadyEmptyValueKeepsTheOldOne checks that switching an
// already-off module off again does not park an empty value over the old one.
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

// TestSwitchingOnWithNothingParkedSaysSo checks that the error names the page
// the value has to be set on.
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

// TestUnswitchableModulesAreRefused checks that a request to switch a row
// without a switch is an error rather than a silent success.
func TestUnswitchableModulesAreRefused(t *testing.T) {
	a := testApp(t)
	// KL_JD is unset here, so JD and the captcha relay it feeds have no switch.
	for _, id := range []string{"cnl", "jd", "captcha", "tray", "nonsense"} {
		if err := setFeature(a, id, false); err == nil {
			t.Errorf("%s has no switch but setFeature accepted it", id)
		}
	}
}

// TestModuleSwitchesListTheModuleOffAndBackOn checks the modules without a
// setting of their own: off puts the id on ModulesOff once, on takes it away.
func TestModuleSwitchesListTheModuleOffAndBackOn(t *testing.T) {
	a := testApp(t)
	for _, id := range []string{"connections", "federation", "torrents", "scripting"} {
		for i := 0; i < 2; i++ {
			if err := setFeature(a, id, false); err != nil {
				t.Fatal(err)
			}
		}
		if got := a.Settings.Get().ModulesOff; !slices.Equal(got, []string{id}) {
			t.Fatalf("%s switched off twice: ModulesOff = %q, want it listed once", id, got)
		}
		if row := featureRow(t, a, id); row.Enabled {
			t.Errorf("%s is switched off but its row says on", id)
		}
		if err := setFeature(a, id, true); err != nil {
			t.Fatal(err)
		}
		if got := a.Settings.Get().ModulesOff; len(got) != 0 {
			t.Fatalf("%s switched back on: ModulesOff = %q, want empty", id, got)
		}
	}
}

// TestTwoSwitchesAtOnceKeepBoth checks that switches flipped together do not
// write over each other's ModulesOff.
func TestTwoSwitchesAtOnceKeepBoth(t *testing.T) {
	a := testApp(t)
	ids := []string{"connections", "federation", "torrents", "scripting"}
	var wg sync.WaitGroup
	for _, id := range ids {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := setFeature(a, id, false); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	got := slices.Clone(a.Settings.Get().ModulesOff)
	slices.Sort(got)
	want := slices.Clone(ids)
	slices.Sort(want)
	if !slices.Equal(got, want) {
		t.Fatalf("ModulesOff = %q after switching four modules off at once, want %q", got, want)
	}
}

// TestRuleSwitchesAreTheListsOwnFlag checks that the Packagizer and link
// filter rows switch the same flag as the Rules page, and keep the rules.
func TestRuleSwitchesAreTheListsOwnFlag(t *testing.T) {
	a := testApp(t)
	if err := setFeature(a, "packagizer", false); err != nil {
		t.Fatal(err)
	}
	if err := setFeature(a, "linkfilter", false); err != nil {
		t.Fatal(err)
	}
	s := a.Settings.Get()
	if !s.Packagizer.Disabled || !s.LinkFilter.Disabled {
		t.Fatalf("packagizer disabled %v, link filter disabled %v; want both off", s.Packagizer.Disabled, s.LinkFilter.Disabled)
	}
	if err := setFeature(a, "packagizer", true); err != nil {
		t.Fatal(err)
	}
	if a.Settings.Get().Packagizer.Disabled {
		t.Error("the packagizer switched back on but the list is still disabled")
	}
}

// TestSwitchedOffFederationShowsAndReachesNoPeer checks the routes a peer is
// reached through while the module is off.
func TestSwitchedOffFederationShowsAndReachesNoPeer(t *testing.T) {
	a := testApp(t)
	reg := newRegistry()
	registerFederation(reg, a)
	h := http.NewServeMux()
	reg.attach(h, http.NotFoundHandler())
	if err := setFeature(a, "federation", false); err != nil {
		t.Fatal(err)
	}

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/instances", nil))
	if strings.TrimSpace(rec.Body.String()) != "[]" {
		t.Errorf("GET /api/instances = %s while federation is off, want []", rec.Body.String())
	}
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/instances/peer/tasks", nil))
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("proxying to a peer answered %d while federation is off, want 503", rec.Code)
	}
}

func featureRow(t *testing.T, a *app.App, id string) Feature {
	t.Helper()
	for _, f := range featureList(a) {
		if f.ID == id {
			return f
		}
	}
	t.Fatalf("no module %q", id)
	return Feature{}
}

// freeLoopbackPort returns a port nothing listens on, so a test never binds the
// Click'n'Load port a real JDownloader may hold.
func freeLoopbackPort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	ln.Close()
	return port
}

// TestClickNLoadRowFollowsTheListener checks that the row reports the listener
// the process runs, as a code and the address next to the English sentence.
func TestClickNLoadRowFollowsTheListener(t *testing.T) {
	a := testApp(t)
	port := freeLoopbackPort(t)
	addr := fmt.Sprintf("127.0.0.1:%d", port)
	a.CnL = cnl.NewListener(a, port)
	t.Cleanup(a.CnL.Stop)

	if err := setFeature(a, "cnl", true); err != nil {
		t.Fatal(err)
	}
	f := featureRow(t, a, "cnl")
	if !f.Enabled || f.Switch != SwitchSetting || f.DetailCode != "cnlListening" || f.DetailArgs["address"] != addr {
		t.Errorf("bound listener: enabled %v, switch %q, code %q, args %v; want on, a switch and cnlListening with %s",
			f.Enabled, f.Switch, f.DetailCode, f.DetailArgs, addr)
	}
	if !strings.Contains(f.Detail, addr) {
		t.Errorf("bound listener: sentence %q does not name the address", f.Detail)
	}

	if err := setFeature(a, "cnl", false); err != nil {
		t.Fatal(err)
	}
	if f := featureRow(t, a, "cnl"); f.Enabled || f.DetailCode != "cnlOff" || f.DetailArgs != nil {
		t.Errorf("closed listener: enabled %v, code %q, args %v; want off, cnlOff and no values", f.Enabled, f.DetailCode, f.DetailArgs)
	}
}

// TestClickNLoadRowReportsATakenPort checks that a listener that could not bind
// reads as unavailable at its address, not as switched off.
func TestClickNLoadRowReportsATakenPort(t *testing.T) {
	held, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer held.Close()
	port := held.Addr().(*net.TCPAddr).Port

	a := testApp(t)
	a.CnL = cnl.NewListener(a, port)
	t.Cleanup(a.CnL.Stop)
	if err := setFeature(a, "cnl", true); err == nil {
		t.Fatal("switching on succeeded on a port another listener holds")
	}
	f := featureRow(t, a, "cnl")
	if f.Enabled || f.DetailCode != "cnlUnavailable" || f.DetailArgs["address"] != fmt.Sprintf("127.0.0.1:%d", port) {
		t.Errorf("taken port: enabled %v, code %q, args %v; want off and cnlUnavailable with the address", f.Enabled, f.DetailCode, f.DetailArgs)
	}
}

// TestClickNLoadRowWithoutAListenerClaimsNone checks a process that started no
// listener. KL_CNL describes a listener only when something opened one.
func TestClickNLoadRowWithoutAListenerClaimsNone(t *testing.T) {
	t.Setenv("KL_CNL", "9666")
	a := testApp(t)

	f := featureRow(t, a, "cnl")
	if f.Enabled || f.Switch != SwitchNone || f.ReasonCode != "cnlNoListener" || f.Detail != "" {
		t.Errorf("no listener: enabled %v, switch %q, reason code %q, detail %q; want off, no switch, cnlNoListener and no detail",
			f.Enabled, f.Switch, f.ReasonCode, f.Detail)
	}
	if err := setFeature(a, "cnl", true); !errors.Is(err, errNoSwitch) {
		t.Errorf("switching a missing listener on: %v, want errNoSwitch", err)
	}
}

// TestEveryModuleSentenceHasACode checks that no row sends an English line
// without the code an interface words it by, in the states a fresh install and
// a configured one put the rows in.
func TestEveryModuleSentenceHasACode(t *testing.T) {
	check := func(state string, a *app.App) {
		t.Helper()
		for _, m := range featureList(a) {
			if m.Reason != "" && m.ReasonCode == "" {
				t.Errorf("%s: module %q sends the reason %q without a code", state, m.ID, m.Reason)
			}
			if m.Detail != "" && m.DetailCode == "" {
				t.Errorf("%s: module %q sends the detail %q without a code", state, m.ID, m.Detail)
			}
		}
	}

	a := testApp(t)
	check("fresh install", a)

	a.CnL = cnl.NewListener(a, freeLoopbackPort(t))
	t.Cleanup(a.CnL.Stop)
	s := a.Settings.Get()
	s.Extract = true
	s.ArchiveDisposal = "trash"
	s.WatchDir = t.TempDir()
	s.DownloadClientAPI = true
	s.Metrics = true
	s.Packagizer.Disabled = true
	s.ModulesOff = []string{"connections", "federation", "torrents", "scripting", "ytdlp"}
	s.Reconnect.Method = "command"
	if _, err := a.ApplySettings(s); err != nil {
		t.Fatal(err)
	}
	check("configured", a)
}

// TestEnabledIsDerivedNotStored checks that a settings write from outside the
// switch moves the module row with it.
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
	// The command is not part of what the switch parked and must survive the
	// round trip.
	if got.Reconnect.Command != "/bin/true" {
		t.Errorf("the reconnect command was lost: %q", got.Reconnect.Command)
	}
}

// TestDefaultsAreRedacted checks that the defaults served to the advanced
// table carry no router password.
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
