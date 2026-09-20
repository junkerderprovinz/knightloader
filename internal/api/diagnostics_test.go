package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/logring"
	"github.com/junkerderprovinz/knightloader/internal/proxycfg"
	"github.com/junkerderprovinz/knightloader/internal/reconnect"
	"github.com/junkerderprovinz/knightloader/internal/settings"
)

// getDiagnostics fetches the bundle and hands back the status, the decoded
// shape and the raw body. A caller looking for a secret that must not be in
// the document at all, however it is spelled, needs the raw bytes.
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

// TestDiagnosticsAnswersTheBasics: which build, which platform, and whether
// the process looks alive.
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
	// buildinfo.Deployment defaults to "container", and this test's path
	// (app.New plus Handler, not cmd/knightloader/main.go) never sets it to
	// "desktop".
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

// TestDiagnosticsRedactsSecrets mirrors TestSettingsNeverShipASecret in
// settings_test.go: the bundle carries the settings document, where the router
// password and every proxy password live, and people attach it to public bug
// reports.
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
	// Redacted rather than dropped: the field still says a secret is
	// configured, or the bundle reports an install with a reconnect method set
	// up as one with none.
	if !bytes.Contains(raw, []byte(reconnect.RedactedPassword)) {
		t.Error("the router password field is missing rather than redacted")
	}
}

// TestDiagnosticsRedactsArchivePasswords covers the gap Settings.Redacted()
// leaves open, since GET /api/settings uses the same method and the Archives
// page has to show a user their own passwords to edit them. The diagnostics
// route redacts this field further, because the bundle is attached to public
// bug reports.
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

// TestDiagnosticsCarriesTheDatabaseSizes: "the list takes a second to sort"
// and "the database is 6 GB, 4 of which is space deleted rows left behind" are
// the same report, and without these four fields nobody reading it could tell.
func TestDiagnosticsCarriesTheDatabaseSizes(t *testing.T) {
	srv, a := testServer(t)
	defer srv.Close()

	// Something in the database, so the size is a measurement rather than an
	// empty schema and a zero means a bug.
	for i := 0; i < 8; i++ {
		if err := a.Store.Save(&core.Task{
			ID:        fmt.Sprintf("diag-%d", i),
			URL:       "https://host.example/diag.bin",
			Name:      "diag.bin",
			Comment:   strings.Repeat("d", 16<<10),
			Status:    core.StatusPaused,
			CreatedAt: time.Now(),
		}); err != nil {
			t.Fatal(err)
		}
	}

	_, d, raw := getDiagnostics(t, srv.URL)
	if d.StoreBytes <= 0 {
		t.Errorf("storeBytes = %d in a bundle from an instance with rows in its database", d.StoreBytes)
	}
	if !bytes.Contains(raw, []byte(`"storeReclaimableBytes"`)) {
		t.Errorf("storeReclaimableBytes is missing from the bundle: %s", raw)
	}
	// settings.json is written the first time a settings page is saved, so the
	// flag has to say which of the two states this is: "0 bytes" for a file
	// that does not exist is a different claim.
	if d.SettingsPresent {
		t.Error("settingsPresent is true on an instance that has never saved a settings page")
	}
	if code, _, msg := putSettings(t, srv.URL, settingsWith(func(s *settings.Settings) {})); code != http.StatusOK {
		t.Fatalf("PUT /api/settings answered %d: %s", code, msg)
	}
	_, saved, _ := getDiagnostics(t, srv.URL)
	if !saved.SettingsPresent || saved.SettingsBytes <= 0 {
		t.Errorf("after a save the bundle says present=%v bytes=%d", saved.SettingsPresent, saved.SettingsBytes)
	}
}

// TestDiagnosticsShipsNoPaths makes the redaction tests' argument for a
// different kind of secret. People attach this bundle to public bug reports,
// and a desktop data directory sits under the user's own name. The sizes go in
// the bundle; the paths go on the session-guarded maintenance route.
func TestDiagnosticsShipsNoPaths(t *testing.T) {
	srv, a := testServer(t)
	defer srv.Close()

	_, _, raw := getDiagnostics(t, srv.URL)
	for _, secret := range []string{a.DataDir, "knightloader.db"} {
		for _, spelling := range spellingsInJSON(t, secret) {
			if bytes.Contains(raw, []byte(spelling)) {
				t.Errorf("GET /api/diagnostics shipped %q in the bundle (as %q)", secret, spelling)
			}
		}
	}
}

// spellingsInJSON is every way one path could appear in the response body.
// json.Marshal doubles the backslashes in a Windows path, so a search for the
// literal string finds nothing while the path sits in the document in plain
// sight; the forward-slash spelling covers the same miss elsewhere.
func spellingsInJSON(t *testing.T, path string) []string {
	t.Helper()
	encoded, err := json.Marshal(path)
	if err != nil {
		t.Fatal(err)
	}
	out := []string{path, strings.Trim(string(encoded), `"`)}
	if slashed := strings.ReplaceAll(path, `\`, "/"); slashed != path {
		out = append(out, slashed)
	}
	return out
}

// TestDiagnosticsIncludesRecentLogLines: an ordinary log.Print from anywhere
// in the process reaches the bundle with no plumbing at that call site.
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
