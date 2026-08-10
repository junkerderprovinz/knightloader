package api

import (
	"bytes"
	"encoding/json"
	"log"
	"net/http"
	"runtime"
	"strings"
	"testing"

	"github.com/junkerderprovinz/knightloader/internal/logring"
	"github.com/junkerderprovinz/knightloader/internal/proxycfg"
	"github.com/junkerderprovinz/knightloader/internal/reconnect"
	"github.com/junkerderprovinz/knightloader/internal/settings"
)

// getDiagnostics fetches the bundle and hands back the status, the decoded
// shape and the raw body - callers that need to look for a substring that has
// no field of its own (a secret that must not be there at all, however it
// might be spelled in JSON) want the raw bytes, not the struct.
func getDiagnostics(t *testing.T, url string) (int, Diagnostics, []byte) {
	t.Helper()
	resp, err := http.Get(url + "/api/diagnostics")
	if err != nil {
		t.Fatal(err)
	}
	raw, err := readAll(resp) // closes resp.Body (federation_test.go)
	if err != nil {
		t.Fatal(err)
	}
	var d Diagnostics
	if resp.StatusCode == http.StatusOK {
		if err := json.Unmarshal(raw, &d); err != nil {
			t.Fatalf("decoding diagnostics: %v (%s)", err, raw)
		}
	}
	return resp.StatusCode, d, raw
}

// TestDiagnosticsAnswersTheBasics is what a bug report actually needs: which
// build, which platform, and whether the process looks alive.
func TestDiagnosticsAnswersTheBasics(t *testing.T) {
	srv, _ := testServer(t)
	defer srv.Close()

	code, d, raw := getDiagnostics(t, srv.URL)
	if code != http.StatusOK {
		t.Fatalf("GET /api/diagnostics answered %d: %s", code, raw)
	}
	if d.Version == "" {
		t.Error("version is empty")
	}
	// buildinfo.Deployment defaults to "container" and nothing in this test's
	// path (app.New + Handler, not cmd/knightloader/main.go) ever sets it to
	// "desktop" - see buildinfo.go's own doc comment on why that default exists.
	if d.Deployment != "container" {
		t.Errorf("deployment = %q, want the package default %q", d.Deployment, "container")
	}
	if !strings.HasPrefix(d.GoVersion, "go") {
		t.Errorf("goVersion = %q, does not look like a Go version", d.GoVersion)
	}
	if d.OS != runtime.GOOS {
		t.Errorf("os = %q, want %q", d.OS, runtime.GOOS)
	}
	if d.Arch != runtime.GOARCH {
		t.Errorf("arch = %q, want %q", d.Arch, runtime.GOARCH)
	}
	if d.Goroutines <= 0 {
		t.Errorf("goroutines = %d, want at least the one answering this request", d.Goroutines)
	}
	if d.GeneratedAt.IsZero() {
		t.Error("generatedAt was never set")
	}
	if d.LogCapacity != logring.Capacity {
		t.Errorf("logCapacity = %d, want the ring's own %d rather than a second, hand-copied number", d.LogCapacity, logring.Capacity)
	}
}

// TestDiagnosticsRedactsSecrets mirrors TestSettingsNeverShipASecret
// (settings_test.go): the bundle carries the settings document, which is
// exactly where the router password and every proxy password live, and this
// is a file a user may attach to a public bug report.
func TestDiagnosticsRedactsSecrets(t *testing.T) {
	srv, _ := testServer(t)
	defer srv.Close()

	withSecrets := settingsWith(func(s *settings.Settings) {
		s.Reconnect = reconnect.Config{
			Method:   reconnect.MethodHTTP,
			Username: "admin",
			Password: "router-secret",
			Requests: []reconnect.Request{{URL: "http://192.0.2.1/reboot"}},
			CheckURL: "http://192.0.2.9/ip",
		}
		s.Connections = []proxycfg.Entry{{
			Kind: proxycfg.KindHTTP, Host: "proxy.lan", Port: 8080,
			Username: "u", Password: "proxy-secret", Enabled: true,
		}}
	})
	if code, _, msg := putSettings(t, srv.URL, withSecrets); code != http.StatusOK {
		t.Fatalf("PUT /api/settings answered %d: %s", code, msg)
	}

	_, _, raw := getDiagnostics(t, srv.URL)
	for _, secret := range []string{"router-secret", "proxy-secret"} {
		if bytes.Contains(raw, []byte(secret)) {
			t.Errorf("GET /api/diagnostics shipped %q in the bundle", secret)
		}
	}
	// Redacted, not silently dropped - the field should still say a secret is
	// configured, or the bundle would misreport an install with a reconnect
	// method set up as one with none at all.
	if !bytes.Contains(raw, []byte(reconnect.RedactedPassword)) {
		t.Error("the router password field is missing rather than redacted")
	}
}

// TestDiagnosticsRedactsArchivePasswords covers the gap Settings.Redacted()
// itself deliberately leaves open (that method is also what GET /api/settings
// uses, where the Archives page needs to show a user their own passwords to
// edit them) - the diagnostics route has to redact this one field further on
// its own, because unlike a settings page this bundle is meant to be attached
// to a public bug report.
func TestDiagnosticsRedactsArchivePasswords(t *testing.T) {
	srv, _ := testServer(t)
	defer srv.Close()

	const secret = "archive-secret-pw"
	withSecret := settingsWith(func(s *settings.Settings) {
		s.ArchivePasswords = []string{secret, "second-one"}
	})
	if code, _, msg := putSettings(t, srv.URL, withSecret); code != http.StatusOK {
		t.Fatalf("PUT /api/settings answered %d: %s", code, msg)
	}

	_, _, raw := getDiagnostics(t, srv.URL)
	if bytes.Contains(raw, []byte(secret)) {
		t.Error("GET /api/diagnostics shipped an archive password in the bundle")
	}
	if !bytes.Contains(raw, []byte(`"archivePasswordCount":2`)) {
		t.Errorf("archivePasswordCount missing or wrong: %s", raw)
	}
}

// TestDiagnosticsIncludesRecentLogLines is the point of tapping the standard
// logger at all: an ordinary log.Print from anywhere in the process has to
// reach the bundle with no plumbing at that call site.
func TestDiagnosticsIncludesRecentLogLines(t *testing.T) {
	srv, _ := testServer(t)
	defer srv.Close()

	marker := "diagnostics-route-marker-7fh2q"
	log.Print(marker)

	_, d, _ := getDiagnostics(t, srv.URL)
	found := false
	for _, l := range d.LogLines {
		if strings.Contains(l, marker) {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("log.Print(%q) did not reach the diagnostics bundle: %v", marker, d.LogLines)
	}
}
