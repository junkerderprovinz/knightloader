package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/app"
	"github.com/junkerderprovinz/knightloader/internal/startupcheck"
)

// postStartup presses the button and hands back the status and the report.
func postStartup(t *testing.T, url string) (int, startupcheck.Report, []byte) {
	t.Helper()
	resp, err := http.Post(url+"/api/diagnostics/startup", "application/json", nil)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := readAll(resp) // closes resp.Body (federation_test.go)
	if err != nil {
		t.Fatal(err)
	}
	var rep startupcheck.Report
	if resp.StatusCode == http.StatusOK {
		if err := json.Unmarshal(raw, &rep); err != nil {
			t.Fatalf("decoding the start report: %v (%s)", err, raw)
		}
	}
	return resp.StatusCode, rep, raw
}

// waitForStartupState polls the bundle until the boot pass has finished. The
// pass runs on its own goroutine after the listener is up, which is the whole
// point of it, so there is nothing to wait on synchronously.
func waitForStartupState(t *testing.T, url string, want string) Diagnostics {
	t.Helper()
	deadline := time.Now().Add(60 * time.Second)
	for time.Now().Before(deadline) {
		_, d, _ := getDiagnostics(t, url)
		if d.Startup != nil && d.Startup.State == want {
			return d
		}
		time.Sleep(20 * time.Millisecond)
	}
	_, d, raw := getDiagnostics(t, url)
	t.Fatalf("the start report never reached %q: %s", want, raw)
	return d
}

// TestDiagnosticsSaysNullWhenNothingEverChecked.
//
// `startup: null` and `startup: {checks: []}` are different claims and the
// second one is a lie: an empty check list drawn as a clean bill of health is
// the worst version of "no opinion labelled as fine". app.New must never start a
// pass, so a bundle from a plain instance carries the null.
func TestDiagnosticsSaysNullWhenNothingEverChecked(t *testing.T) {
	srv, _ := testServer(t)
	defer srv.Close()

	_, d, raw := getDiagnostics(t, srv.URL)
	if d.Startup != nil {
		t.Errorf("startup = %+v on an instance where nothing ever started a check", d.Startup)
	}
	// The FIELD has to be there, carrying null - not omitted. A missing key and
	// a null one look the same to a JavaScript truth test and completely
	// different to anybody reading the saved file.
	var doc map[string]json.RawMessage
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	v, ok := doc["startup"]
	if !ok {
		t.Fatalf("the bundle has no startup field at all: %s", raw)
	}
	if string(v) != "null" {
		t.Errorf("startup = %s, want null", v)
	}
}

// probeGlob finds a probe file the check should not have left behind. It has to
// stay in step with startupcheck's own probePrefix, which is unexported there
// because nothing outside that package has any business building the name.
const probeGlob = ".knightloader-startup-*"

// TestStartupRecheckIsPostOnly.
//
// The re-run writes a probe file into every configured folder that exists.
// Registered as a GET it would be a route any browser prefetch, any link scanner
// and any speculative navigation fires by itself, so hovering a bookmark would
// drop files into somebody's download folder.
//
// The assertion is about the FILES and not only about the status code, because
// the status code is the weaker half of the promise: what must be true is that
// nothing was written, and only looking in the folder says so. (The code itself
// is 404 rather than the 405 the routing table's own comment predicts - the
// "/api/" catch-all in Registry.attach matches the path first, and answers "no
// such endpoint". Either is a refusal; neither is a write.)
func TestStartupRecheckIsPostOnly(t *testing.T) {
	srv, a := testServer(t)
	defer srv.Close()
	downloads := configureDownloadDir(t, a)

	resp, err := http.Get(srv.URL + "/api/diagnostics/startup")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 == 2 {
		t.Errorf("GET /api/diagnostics/startup answered %d; a route that writes must not be reachable by a GET", resp.StatusCode)
	}
	if got := probeFiles(t, downloads); len(got) > 0 {
		t.Errorf("a GET wrote %v into the download folder", got)
	}

	// And the POST really does write, or the test above would be passing
	// because the feature does nothing at all.
	code, rep, raw := postStartup(t, srv.URL)
	if code != http.StatusOK {
		t.Fatalf("POST /api/diagnostics/startup answered %d: %s", code, raw)
	}
	var probed bool
	for _, c := range rep.Checks {
		if c.ID == startupcheck.IDFolder && c.Subject == downloads {
			probed = c.Probed
		}
	}
	if !probed {
		t.Errorf("the POST never wrote into %s: %+v", downloads, rep.Checks)
	}
	if got := probeFiles(t, downloads); len(got) > 0 {
		t.Errorf("the POST left %v behind", got)
	}
}

// configureDownloadDir gives an instance a real, empty download folder of its
// own, so "was anything written in there" has a clean answer - on a default
// install that folder is inside the data directory, where the database and the
// settings would make every leftover ambiguous.
func configureDownloadDir(t *testing.T, a *app.App) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "downloads")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := a.Settings.Get()
	cfg.DownloadDir = dir
	if _, err := a.Settings.Set(cfg); err != nil {
		t.Fatal(err)
	}
	if got := a.Settings.Get().DownloadDir; got != dir {
		t.Fatalf("the settings store refused the download folder: %q", got)
	}
	return dir
}

func probeFiles(t *testing.T, dir string) []string {
	t.Helper()
	found, err := filepath.Glob(filepath.Join(dir, probeGlob))
	if err != nil {
		t.Fatal(err)
	}
	return found
}

// TestStartupRecheckIsNeitherRelayedNorForwarded.
//
// "Java is missing" about a PEER's box, shown while a peer's list is on screen,
// names the wrong machine with total confidence - which is the argument
// routes_diskspace.go already makes for disk figures. Both allowlists name their
// routes one by one, so this pins that nobody widened either of them.
func TestStartupRecheckIsNeitherRelayedNorForwarded(t *testing.T) {
	if relayForwardable(http.MethodPost, "/api/diagnostics/startup") {
		t.Error("the relay would carry the start check to another instance")
	}
	if relayForwardable(http.MethodGet, "/api/diagnostics") {
		t.Error("the relay would carry the diagnostics bundle, which now contains the start report")
	}
}

// TestPressingRecheckDoesNotBecomeTheBundlesReading is trap fourteen.
//
// If the press replaced the stored report, the evidence of what was true at boot
// would be destroyed the first time anybody pressed it - which is precisely the
// moment a support thread needs it.
func TestPressingRecheckDoesNotBecomeTheBundlesReading(t *testing.T) {
	srv, _ := testServer(t)
	defer srv.Close()

	code, rep, raw := postStartup(t, srv.URL)
	if code != http.StatusOK {
		t.Fatalf("POST /api/diagnostics/startup answered %d: %s", code, raw)
	}
	if rep.State != startupcheck.StateDone {
		t.Errorf("state = %q, want %q", rep.State, startupcheck.StateDone)
	}
	if !rep.Probed {
		t.Error("probed = false; the press is the pass that does write the test file")
	}
	if rep.Checks == nil {
		t.Error("checks is null, which throws whatever walks it")
	}

	_, d, _ := getDiagnostics(t, srv.URL)
	if d.Startup != nil {
		t.Errorf("the pressed pass became the bundle's reading: %+v", d.Startup)
	}
}

// TestABundleWithAStartReportStillShipsNoPaths.
//
// This is TestDiagnosticsShipsNoPaths' argument applied to the new field, and it
// is not theoretical: with no download folder configured the default one is
// INSIDE the data directory (app.go's dlDir), so an unmasked folder row would
// put a desktop user's own name - C:\Users\<their real name>\AppData\... - into
// a file people attach to public bug reports. It has to run with a report
// actually present, or it proves nothing at all.
func TestABundleWithAStartReportStillShipsNoPaths(t *testing.T) {
	srv, a := testServer(t)
	defer srv.Close()

	a.StartStartupCheck()
	d := waitForStartupState(t, srv.URL, startupcheck.StateDone)
	if len(d.Startup.Checks) == 0 {
		t.Fatal("the report finished with no rows, so this test is checking nothing")
	}

	_, _, raw := getDiagnostics(t, srv.URL)
	for _, secret := range []string{a.DataDir, "knightloader.db"} {
		for _, spelling := range spellingsInJSON(t, secret) {
			if bytes.Contains(raw, []byte(spelling)) {
				t.Errorf("the bundle with a start report in it shipped %q (as %q)", secret, spelling)
			}
		}
	}
	// And the row is still worth reading: masked, not blanked. A folder row with
	// no subject is the one row nobody can act on.
	var seen bool
	for _, c := range d.Startup.Checks {
		if c.ID == startupcheck.IDData || c.ID == startupcheck.IDFolder {
			if c.Subject == "" {
				t.Errorf("a %q row came back with no subject at all", c.ID)
			}
			seen = true
		}
	}
	if !seen {
		t.Error("no folder row in the report, so the masking was never exercised")
	}
}

// TestTheBootPassWritesNothing, from the outside. The owner's decision is that a
// start writes no probe file anywhere; this is the assertion an api-level reader
// can make about it without reaching into the app package.
func TestTheBootPassWritesNothing(t *testing.T) {
	srv, a := testServer(t)
	defer srv.Close()

	a.StartStartupCheck()
	d := waitForStartupState(t, srv.URL, startupcheck.StateDone)

	if d.Startup.Probed {
		t.Error("the boot report says it wrote its test file")
	}
	for _, c := range d.Startup.Checks {
		if c.Probed {
			t.Errorf("row %q says it was written to during a boot pass", c.Subject)
		}
	}
}
