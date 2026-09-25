package api

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

// healthServer is an instance with both health routes and the plain
// /api/health attached, plus a switch for the metrics module.
func healthServer(t *testing.T, metrics bool) (*app.App, *httptest.Server) {
	t.Helper()
	// No sidecar, so the rows that read KL_JD answer the same everywhere.
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

// TestTheOldHealthRouteIsUntouched pins /api/health: the phone app's LAN
// discovery compares the literal "ok", the container's HEALTHCHECK reads the
// status, and the Click'n'Load bridge refuses anything else. Phones update on
// their own schedule, so changing this answer could not be taken back.
//
// commit is only checked to be a string: test binaries carry no
// vcs.revision, so it is empty here (see buildinfo.Revision).
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
		t.Errorf(`status = %v, want the literal "ok"; mobile/src/api/discover.ts compares this string and gives up on anything else`, got["status"])
	}
	if _, ok := got["version"].(string); !ok {
		t.Errorf("version is missing or not a string: %s", raw)
	}
	if _, ok := got["commit"].(string); !ok {
		t.Errorf("commit is missing or not a string (%s); the key has to be there even when the build cannot fill it, or \"\" and \"too old to answer\" look the same to a caller", raw)
	}
	if len(got) != 3 {
		t.Errorf("the liveness answer has grown to %d fields (%s); adding one is safe, but decide it here", len(got), raw)
	}
}

// TestNeitherNewRouteAnswersWithoutASession also checks that the open
// /api/health does not open the paths below it: Registry.buildOpen only
// matches by prefix for wildcard routes.
func TestNeitherNewRouteAnswersWithoutASession(t *testing.T) {
	t.Parallel()
	reg := newRegistry()
	registerSystem(reg, testApp(t))
	registerHealth(reg, testApp(t))

	if !reg.open("/api/health") {
		t.Fatal("/api/health stopped being open; the container probe and the phone's discovery both need it on a locked instance")
	}
	for _, path := range []string{"/api/health/detail", metricsPath} {
		if reg.open(path) {
			t.Errorf("%s answers without a session", path)
		}
	}

	// On its own, registerHealth marks nothing open.
	alone := newRegistry()
	registerHealth(alone, testApp(t))
	for _, r := range alone.Routes() {
		if r.Open {
			t.Errorf("registerHealth marks %s %s open; neither of its routes may be", r.Method, r.Path)
		}
	}
}

// TestTheMetricsAddressDoesNotExistWhileTheSwitchIsOff checks for the /api/
// catch-all's wording, so a switched-off module looks like a missing route.
func TestTheMetricsAddressDoesNotExistWhileTheSwitchIsOff(t *testing.T) {
	_, srv := healthServer(t, false)
	code, raw := getRaw(t, srv.URL+metricsPath)
	if code != http.StatusNotFound {
		t.Fatalf("GET %s answered %d with the switch off, want 404: %s", metricsPath, code, raw)
	}
	if !strings.Contains(string(raw), "no such endpoint: GET "+metricsPath) {
		t.Errorf("the refusal reads %q; it has to be the /api/ catch-all's own wording, or a closed door is distinguishable from an absent one", strings.TrimSpace(string(raw)))
	}
	// The switch does not affect the detail route.
	if code, _ := getRaw(t, srv.URL+"/api/health/detail"); code != http.StatusOK {
		t.Errorf("GET /api/health/detail answered %d with the metrics switch off; the switch governs the metrics address only", code)
	}
}

// TestTheMetricsAddressAnswersExpositionTextWhenSwitchedOn checks the content
// type, which tells a collector the exposition format and is lost if set after
// the first Write.
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

// TestTheDetailRouteCarriesEveryPartAndBothMaps checks the JSON as the
// interface reads it: no nil map shipped as null and no zero time shipped as
// the year one.
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

// TestTheMetricsModuleSwitchIsWiredUp checks setFeature's "metrics" case,
// without which the Health page's switch would answer errNoSwitch.
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

	// The row is derived from the setting.
	var row *Feature
	for i, f := range featureState(a, "").Modules {
		if f.ID == "metrics" {
			row = &featureState(a, "").Modules[i]
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

	// An unknown module id still refuses.
	if err := setFeature(a, "metricz", true); !errors.Is(err, errNoSwitch) {
		t.Errorf("a misspelt module id answered %v, want errNoSwitch", err)
	}
}

func TestTheHealthPageIsRegisteredInTheRail(t *testing.T) {
	t.Parallel()
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
