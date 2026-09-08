package api

// The two health routes as they reach a caller, plus the one route this whole
// feature exists NOT to change.

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/junkerderprovinz/knightloader/internal/app"
	"github.com/junkerderprovinz/knightloader/internal/settings"
)

// healthServer is an instance with both health routes and the old /api/health
// attached, plus a switch for the metrics module.
//
// registerSystem is included on purpose rather than only registerHealth: the
// open-route test below is about whether /api/health being open opens the paths
// UNDER it, which cannot be asked of a registry that does not contain it.
func healthServer(t *testing.T, metrics bool) (*app.App, *httptest.Server) {
	t.Helper()
	// No sidecar, so the two rows that read KL_JD answer the same on a
	// developer machine that happens to have one running as on CI.
	t.Setenv("KL_JD", "")
	a := testApp(t)
	s := settings.Defaults()
	s.DownloadDir = t.TempDir()
	s.Metrics = metrics
	if _, err := a.ApplySettings(s); err != nil {
		t.Fatal(err)
	}
	reg := newRegistry()
	registerSystem(reg, a)
	registerHealth(reg, a)
	mux := http.NewServeMux()
	reg.attach(mux, http.NotFoundHandler())
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return a, srv
}

// TestTheOldHealthRouteIsUntouched is the most important test in this file and
// the one least about the feature being added.
//
// /api/health answers two fields, a literal "ok" and a 200, and three shipped
// things read it: the phone app's LAN discovery compares the string, the
// container's HEALTHCHECK reads the exit code, and the Click'n'Load bridge
// refuses to start on anything else. The phone app in particular updates on its
// own schedule, not with the container, so "degraded" here would make every
// phone in the wild stop finding the instance and there would be no way to take
// it back. This test is the tripwire on somebody helpfully wiring the new
// report into the old address.
func TestTheOldHealthRouteIsUntouched(t *testing.T) {
	_, srv := healthServer(t, false)
	code, raw := getRaw(t, srv.URL+"/api/health")
	if code != http.StatusOK {
		t.Fatalf("GET /api/health answered %d, want 200: %s", code, raw)
	}
	var got map[string]any
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	if got["status"] != "ok" {
		t.Errorf(`status = %v, want the literal "ok" - mobile/src/api/discover.ts compares this string and gives up on anything else`, got["status"])
	}
	if _, ok := got["version"].(string); !ok {
		t.Errorf("version is missing or not a string: %s", raw)
	}
	if len(got) != 2 {
		t.Errorf("the liveness answer has grown to %d fields (%s); adding one is safe, and this is the place to decide that deliberately", len(got), raw)
	}
}

// TestNeitherNewRouteAnswersWithoutASession pins the owner's decision.
//
// The item asked for a second OPEN endpoint and does not get one: routes_test.go
// keeps the open list with a written justification per entry, and an
// unauthenticated metrics route on a password-locked instance is a hole
// somebody would have to have chosen deliberately.
//
// The second half is the subtle one and is why registerSystem is in the fixture.
// /api/health IS open, and Registry.buildOpen only turns a route into a PREFIX
// match when its path contains a wildcard - so a child path under an open exact
// route stays guarded. If that ever changed, /api/health/detail would quietly
// become world-readable on every locked instance and nothing else would say so.
func TestNeitherNewRouteAnswersWithoutASession(t *testing.T) {
	reg := newRegistry()
	registerSystem(reg, testApp(t))
	registerHealth(reg, testApp(t))

	if !reg.open("/api/health") {
		t.Fatal("/api/health stopped being open; the container probe and the phone's discovery both need it on a locked instance")
	}
	for _, path := range []string{"/api/health/detail", metricsPath} {
		if reg.open(path) {
			t.Errorf("%s answers without a session; the owner settled that it must not", path)
		}
	}

	// And on its own, with nothing else in the table: registerHealth marks
	// nothing open at all. Asserted against a registry of its own rather than
	// against the one above, which is full of the login flow's own open routes -
	// the whole point of the fixture there being that /api/health IS open.
	alone := newRegistry()
	registerHealth(alone, testApp(t))
	for _, r := range alone.Routes() {
		if r.Open {
			t.Errorf("registerHealth marks %s %s open; neither of its routes may be", r.Method, r.Path)
		}
	}
}

// TestTheMetricsAddressDoesNotExistWhileTheSwitchIsOff. A door that is closed
// should not be able to tell somebody whether a key would have worked, and the
// wording is the /api/ catch-all's own so that a caller cannot tell a switched
// off module from a route that was never built - the same arrangement the
// SABnzbd door already has.
func TestTheMetricsAddressDoesNotExistWhileTheSwitchIsOff(t *testing.T) {
	_, srv := healthServer(t, false)
	code, raw := getRaw(t, srv.URL+metricsPath)
	if code != http.StatusNotFound {
		t.Fatalf("GET %s answered %d with the switch off, want 404: %s", metricsPath, code, raw)
	}
	if !strings.Contains(string(raw), "no such endpoint: GET "+metricsPath) {
		t.Errorf("the refusal reads %q; it has to be the /api/ catch-all's own wording, or a closed door is distinguishable from an absent one", strings.TrimSpace(string(raw)))
	}
	// And the detail route is unaffected by that switch: it is the readout the
	// settings page draws, not the door a collector fetches.
	if code, _ := getRaw(t, srv.URL+"/api/health/detail"); code != http.StatusOK {
		t.Errorf("GET /api/health/detail answered %d with the metrics switch off; the switch governs the metrics address only", code)
	}
}

// TestTheMetricsAddressAnswersExpositionTextWhenSwitchedOn checks the one part
// of the answer that is not the body.
//
// The content type is part of the contract - it is what tells a collector which
// exposition format this is - and it is the easiest thing in the file to lose:
// net/http commits the header block on the first Write, so a Content-Type set
// after any byte of the body is silently dropped and the whole thing is served
// as sniffed text.
func TestTheMetricsAddressAnswersExpositionTextWhenSwitchedOn(t *testing.T) {
	_, srv := healthServer(t, true)
	resp, err := http.Get(srv.URL + metricsPath)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET %s answered %d with the switch on", metricsPath, resp.StatusCode)
	}
	const want = "text/plain; version=0.0.4; charset=utf-8"
	if got := resp.Header.Get("Content-Type"); got != want {
		t.Errorf("Content-Type = %q, want %q", got, want)
	}
	body := make([]byte, 4096)
	n, _ := resp.Body.Read(body)
	if !strings.HasPrefix(string(body[:n]), "# HELP knightloader_build_info") {
		t.Errorf("the body does not open with a family declaration: %q", string(body[:n]))
	}
}

// TestTheDetailRouteCarriesEveryPartAndBothMaps is the wire shape, asserted
// against the JSON rather than against the Go value: the interface reads this,
// and the two failures worth catching here are both encoding ones. A nil map
// ships as JSON null and whatever walks it throws; a zero time.Time under
// omitempty ships as the year one and gets drawn as a real date.
func TestTheDetailRouteCarriesEveryPartAndBothMaps(t *testing.T) {
	_, srv := healthServer(t, false)
	code, raw := getRaw(t, srv.URL+"/api/health/detail")
	if code != http.StatusOK {
		t.Fatalf("GET /api/health/detail answered %d: %s", code, raw)
	}
	var got struct {
		Status     string `json:"status"`
		Version    string `json:"version"`
		Subsystems []struct {
			ID    string `json:"id"`
			State string `json:"state"`
			Since string `json:"since"`
		} `json:"subsystems"`
		Tasks struct {
			WaitingBy map[string]int `json:"waitingBy"`
			FailedBy  map[string]int `json:"failedBy"`
		} `json:"tasks"`
		Volumes []struct {
			Dir string `json:"dir"`
		} `json:"volumes"`
		UptimeSeconds int64 `json:"uptimeSeconds"`
	}
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	if got.Status == "" || got.Version == "" {
		t.Errorf("status %q / version %q: %s", got.Status, got.Version, raw)
	}
	if len(got.Subsystems) < 9 {
		t.Errorf("only %d parts reported; every part is reported every time: %s", len(got.Subsystems), raw)
	}
	for _, s := range got.Subsystems {
		if s.ID == "" || s.State == "" {
			t.Errorf("a part arrived without an id or a state: %+v", s)
		}
		if strings.HasPrefix(s.Since, "0001-") {
			t.Errorf("%s shipped a zero time as %q; that is omitzero's job and it is drawn as a real date", s.ID, s.Since)
		}
	}
	if got.Tasks.WaitingBy == nil || got.Tasks.FailedBy == nil {
		t.Errorf("a breakdown arrived as null rather than as an object: %s", raw)
	}
	if got.Volumes == nil {
		t.Errorf("volumes arrived as null rather than as a list: %s", raw)
	}
	if got.UptimeSeconds < 0 {
		t.Errorf("uptime = %d", got.UptimeSeconds)
	}
}

// TestTheMetricsModuleSwitchIsWiredUp is the check on the three-line edit in
// routes_features.go, which is a different file and a different lane.
//
// Without the `case "metrics"` there, setFeature falls through to its default
// and answers errNoSwitch - so the switch on the Health page would refuse every
// press with "this module has no switch here" while the row beside it kept
// offering one. That is exactly the shape of failure the module registry exists
// to prevent, so it gets a test rather than a promise.
func TestTheMetricsModuleSwitchIsWiredUp(t *testing.T) {
	a, _ := healthServer(t, false)

	if err := setFeature(a, "metrics", true); err != nil {
		t.Fatalf("switching the metrics module on failed: %v", err)
	}
	if !a.Settings.Get().Metrics {
		t.Error("the switch reported success and the setting did not move")
	}
	if err := setFeature(a, "metrics", false); err != nil {
		t.Fatalf("switching it off again failed: %v", err)
	}
	if a.Settings.Get().Metrics {
		t.Error("switching it off left the door open")
	}

	// And the row the page draws agrees with the setting, derived rather than
	// remembered - the rule the whole registry is built on.
	var row *Feature
	for i, f := range featureState(a).Modules {
		if f.ID == "metrics" {
			row = &featureState(a).Modules[i]
		}
	}
	if row == nil {
		t.Fatal("the metrics module has no row in the registry; the Health page would render the not-built placeholder")
	}
	if row.Enabled {
		t.Error("the row reads enabled while the setting is off")
	}
	if row.Page != "health" {
		t.Errorf("the row is filed under %q, want \"health\"", row.Page)
	}
	if !strings.Contains(row.Detail, metricsPath) {
		t.Errorf("the row's detail (%q) does not name the address somebody has to point a collector at", row.Detail)
	}

	// A module id nothing knows still refuses, so this case did not become a
	// catch-all that stores a flag nothing reads.
	if err := setFeature(a, "metricz", true); !errors.Is(err, errNoSwitch) {
		t.Errorf("a misspelt module id answered %v, want errNoSwitch", err)
	}
}

// TestTheHealthPageIsRegisteredInTheRail. featurePages() owns the SET and the
// ORDER of the settings sub-pages, and a module filed under a page that is not
// in that list is a row pointing at an address the rail never offers.
func TestTheHealthPageIsRegisteredInTheRail(t *testing.T) {
	var found bool
	for _, p := range featurePages() {
		if p.ID == "health" {
			found = true
			if len(p.Modules) != 1 || p.Modules[0] != "metrics" {
				t.Errorf("the health page carries %v, want just the metrics module", p.Modules)
			}
		}
	}
	if !found {
		t.Fatal("featurePages() has no health entry; the page would be unreachable and the metrics row would point at nothing")
	}
}
