package api

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/app"
	"github.com/junkerderprovinz/knightloader/internal/buildinfo"
	"github.com/junkerderprovinz/knightloader/internal/proxycfg"
	"github.com/junkerderprovinz/knightloader/internal/reconnect"
	"github.com/junkerderprovinz/knightloader/internal/settings"
)

func transferServer(t *testing.T) (*app.App, *httptest.Server) {
	t.Helper()
	a := testApp(t)
	reg := newRegistry()
	registerSettingsTransfer(reg, a)
	mux := http.NewServeMux()
	reg.attach(mux, http.NotFoundHandler())
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return a, srv
}

// postImport sends one import and returns the decoded answer, failing the test
// on anything but a 200. The refusal paths have their own helper below, so a
// test that expects success never has to check a status itself.
func postImport(t *testing.T, srv *httptest.Server, doc settings.PortableDoc, keys []string) importResult {
	t.Helper()
	body, err := json.Marshal(importRequest{Document: doc, Keys: keys})
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.Post(srv.URL+"/api/settings/import", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("POST import = %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	var out importResult
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("the answer is not an importResult: %v (%s)", err, raw)
	}
	return out
}

// refuseImport sends one import that is expected to fail and hands back the
// status and the decoded error envelope.
func refuseImport(t *testing.T, srv *httptest.Server, doc settings.PortableDoc, keys []string) (int, map[string]any) {
	t.Helper()
	body, err := json.Marshal(importRequest{Document: doc, Keys: keys})
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.Post(srv.URL+"/api/settings/import", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode == http.StatusOK {
		t.Fatalf("the import was accepted and should not have been: %s", raw)
	}
	out := map[string]any{}
	_ = json.Unmarshal(raw, &out)
	if _, ok := out["error"]; !ok {
		// Not fatal: a refusal that answered plain text still explained itself,
		// and the client understands both. The tests that care assert on the
		// typed half explicitly.
		out["error"] = strings.TrimSpace(string(raw))
	}
	return resp.StatusCode, out
}

// TestImportLeavesUnnamedSettingsExactlyAsStored is THE test of this feature,
// and it is aimed at one specific mistake somebody will eventually make: sending
// the import through ApplySettings (PUT's path) instead of PatchSettings.
//
// PUT decodes into a fresh `var s settings.Settings`, so every key the body
// omits becomes Go's ZERO value rather than its default - the exact opposite of
// what Load does, which unmarshals over Defaults() "so new fields keep their
// default value". An import routed that way switches extract, autoStart,
// crawlSameHost, verifyChecksums and preParserEnabled off, sets maxConcurrent
// and maxPerHost to 0 and blanks shape and navLabels, on a box whose owner asked
// to take over a speed limit and nothing else.
//
// So: the receiving box is given values that are neither the defaults nor the
// document's, one single key is imported, and everything else has to still be
// standing afterwards.
func TestImportLeavesUnnamedSettingsExactlyAsStored(t *testing.T) {
	a, srv := transferServer(t)

	stored := a.Settings.Get()
	stored.MaxConcurrent = 9
	stored.MaxPerHost = 5
	stored.Extract = true
	stored.AutoStart = true
	stored.SpeedLimit = 0
	stored.InstanceName = "the receiving box"
	if _, err := a.ApplySettings(stored); err != nil {
		t.Fatal(err)
	}

	// The sending box: the one key that is meant to travel, and around it the
	// zero values that a PUT-shaped import would smear over everything.
	src := settings.Defaults()
	src.SpeedLimit = 1 << 20
	src.MaxConcurrent = 0
	src.MaxPerHost = 0
	src.Extract = false
	src.AutoStart = false
	src.InstanceName = "the sending box"
	doc, err := settings.Portable(src, false, "v0.0.1", "container", time.Now())
	if err != nil {
		t.Fatal(err)
	}

	res := postImport(t, srv, doc, []string{"speedLimit"})
	if len(res.Applied) != 1 || res.Applied[0] != "speedLimit" {
		t.Fatalf("applied = %v, want exactly [speedLimit]", res.Applied)
	}

	got := a.Settings.Get()
	if got.SpeedLimit != 1<<20 {
		t.Errorf("speedLimit = %d, want the imported %d", got.SpeedLimit, 1<<20)
	}
	// Each of these is a separate line on purpose: when this test fails, the
	// name of the field that was reset is the whole of the diagnosis.
	if got.MaxConcurrent != 9 {
		t.Errorf("maxConcurrent = %d, want the stored 9 - an unnamed key was overwritten", got.MaxConcurrent)
	}
	if got.MaxPerHost != 5 {
		t.Errorf("maxPerHost = %d, want the stored 5 - an unnamed key was overwritten", got.MaxPerHost)
	}
	if !got.Extract {
		t.Error("extract was switched off by an import that never named it")
	}
	if !got.AutoStart {
		t.Error("autoStart was switched off by an import that never named it")
	}
	if got.InstanceName != "the receiving box" {
		t.Errorf("instanceName = %q, want the stored name - an unnamed key was overwritten", got.InstanceName)
	}
}

// TestImportNamesTheSecretsThatDidNotTravel covers the two failures that save
// cleanly and only show up hours later:
//
//   - a proxy row arrives with a username and no password, and dials anyway
//     (proxycfg.Merge returns next untouched when prev is empty, which is a
//     fresh box; Validate never looks at the password), sending the traffic the
//     user was hiding out over their own connection;
//   - the router password is silently cleared, and the nightly reconnect then
//     reports "the address did not change", which points the operator at their
//     router rather than at an empty field.
//
// Both have to come back as machine-readable codes, and the placeholder must
// never be what gets stored.
func TestImportNamesTheSecretsThatDidNotTravel(t *testing.T) {
	a, srv := transferServer(t)

	src := settings.Defaults()
	src.Reconnect = reconnect.Config{
		Method:   reconnect.MethodHTTP,
		Username: "admin",
		Password: "router-secret",
		CheckURL: "https://example.invalid/ip",
		Requests: []reconnect.Request{{URL: "https://192.168.0.1/reconnect"}},
	}
	src.Connections = []proxycfg.Entry{{
		ID:       "c1",
		Kind:     proxycfg.KindHTTP,
		Host:     "proxy.example.invalid",
		Port:     3128,
		Username: "vpn",
		Password: "proxy-secret",
		Enabled:  true,
	}}
	doc, err := settings.Portable(src, false, "v0.0.1", "container", time.Now())
	if err != nil {
		t.Fatal(err)
	}

	res := postImport(t, srv, doc, []string{"reconnect", "connections"})
	want := map[string]bool{settings.SecretlessReconnect: true, settings.SecretlessConnections: true}
	got := map[string]bool{}
	for _, code := range res.Incomplete {
		got[code] = true
	}
	for code := range want {
		if !got[code] {
			t.Errorf("incomplete = %v, missing %q - nothing tells the user to type it back in", res.Incomplete, code)
		}
	}

	stored := a.Settings.Get()
	// The placeholder is a display value, never a password. If it is what got
	// stored, the reconnect posts eight asterisks to the router forever.
	if stored.Reconnect.Password == reconnect.RedactedPassword {
		t.Error("the redaction placeholder was stored as the router password")
	}
	if stored.Reconnect.Password != "" {
		t.Errorf("the router password is %q on a box that never had one", stored.Reconnect.Password)
	}
	if len(stored.Connections) != 1 {
		t.Fatalf("connections = %#v, want the one imported row", stored.Connections)
	}
	if stored.Connections[0].Password != "" {
		t.Errorf("a proxy password appeared out of a secretless document: %q", stored.Connections[0].Password)
	}
	// And the row that carries no password is still the row the user chose to
	// take over - the point of `incomplete` is that it is reported, not that the
	// import silently drops half of it.
	if stored.Connections[0].Username != "vpn" {
		t.Errorf("the connection row did not arrive: %#v", stored.Connections[0])
	}
}

// TestImportKeepsThisBoxIdentity pins NeverPortable at the route as well as in
// the package: a hand-edited document that names instanceId must not be able to
// hand this box somebody else's id, whatever the caller asks for.
func TestImportKeepsThisBoxIdentity(t *testing.T) {
	a, srv := transferServer(t)
	mine := a.Settings.Get().InstanceID
	if mine == "" {
		t.Fatal("this box has no instance id, so the test proves nothing")
	}

	doc, err := settings.Portable(settings.Defaults(), false, "v0.0.1", "container", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	// Put them back in by hand, which is exactly what a text editor can do.
	doc.Settings["instanceId"] = json.RawMessage(`"ffffffffffffffffffffffffffffffffffffffff"`)
	doc.Settings["knownDomains"] = json.RawMessage(`["somebody-elses.example.invalid"]`)

	res := postImport(t, srv, doc, []string{"instanceId", "knownDomains", "speedLimit"})
	for _, k := range []string{"instanceId", "knownDomains"} {
		if !contains(res.Skipped, k) {
			t.Errorf("skipped = %v, want it to name %q", res.Skipped, k)
		}
		if contains(res.Applied, k) {
			t.Errorf("applied = %v, and it must never name %q", res.Applied, k)
		}
	}
	if got := a.Settings.Get(); got.InstanceID != mine {
		t.Errorf("instanceId = %q, want the box's own %q - two boxes now collide in one relay group", got.InstanceID, mine)
	} else if len(got.KnownDomains) != 0 {
		t.Errorf("knownDomains = %v, want none - those addresses never pointed at this box", got.KnownDomains)
	}
}

// TestImportReportsKeysThisBuildDoesNotHave covers the silence
// settings.ApplyPatch would otherwise leave: it merges as raw JSON and
// re-unmarshals into Settings, and encoding/json drops an unrecognised key
// without an error. A document old enough to carry `deleteArchive` (which became
// archiveDisposal, and is mapped only by migrate(), which runs solely inside
// Load against settings.json's own bytes) would import as absolutely nothing.
func TestImportReportsKeysThisBuildDoesNotHave(t *testing.T) {
	_, srv := transferServer(t)

	doc, err := settings.Portable(settings.Defaults(), false, "v0.0.1", "container", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	doc.Settings["deleteArchive"] = json.RawMessage(`true`)

	// Not even asked for, and still reported: the person who needs to know is
	// the one still standing in front of the old box.
	res := postImport(t, srv, doc, []string{"speedLimit"})
	if !contains(res.Unknown, "deleteArchive") {
		t.Errorf("unknown = %v, want it to name deleteArchive", res.Unknown)
	}
	// Asked for as well, which must not double-report it or apply it.
	res = postImport(t, srv, doc, []string{"deleteArchive"})
	if !contains(res.Unknown, "deleteArchive") {
		t.Errorf("unknown = %v, want it to name deleteArchive", res.Unknown)
	}
	if len(res.Applied) != 0 {
		t.Errorf("applied = %v, want nothing", res.Applied)
	}
	if n := countOf(res.Unknown, "deleteArchive"); n != 1 {
		t.Errorf("deleteArchive is reported %d times, want once", n)
	}
}

// TestImportRefusesADocumentFromANewerBuild copies backup.Stage's own one-way
// version guard: an OLDER document is the normal case and the whole point of an
// export kept for two years, a NEWER one is refused, and the refusal has to
// carry the typed half so the interface can say it in the reader's language.
//
// buildinfo.Version is a package variable and this test writes it. That is safe
// here and nowhere near safe in general: nothing in this package calls
// t.Parallel(), so the tests in it run one at a time, and the value is put back
// before the test returns.
func TestImportRefusesADocumentFromANewerBuild(t *testing.T) {
	was := buildinfo.Version
	buildinfo.Version = "v1.9.0"
	t.Cleanup(func() { buildinfo.Version = was })

	_, srv := transferServer(t)
	doc, err := settings.Portable(settings.Defaults(), false, "v2.0.0", "container", time.Now())
	if err != nil {
		t.Fatal(err)
	}

	status, env := refuseImport(t, srv, doc, []string{"speedLimit"})
	if status != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", status)
	}
	if env["code"] != "transfer.tooNew" {
		t.Errorf("code = %v, want transfer.tooNew - the refusal cannot be translated", env["code"])
	}
	params, _ := env["params"].(map[string]any)
	if params["version"] != "v2.0.0" || params["running"] != "v1.9.0" {
		t.Errorf("params = %v, want the two versions", params)
	}

	// The other direction is not a refusal at all.
	older, err := settings.Portable(settings.Defaults(), false, "v1.0.0", "container", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	postImport(t, srv, older, []string{"speedLimit"})
}

// TestExportOmitsSecretsUnlessAskedFor is the owner's decision, pinned (jdp,
// 2026-09-08: the tick exists and is off when the dialog opens). The parameter
// is a whitelist of one spelling, so a caller that has not been taught about it
// gets the safe answer rather than the leak.
func TestExportOmitsSecretsUnlessAskedFor(t *testing.T) {
	a, srv := transferServer(t)
	s := a.Settings.Get()
	s.ArchivePasswords = []string{"hunter2"}
	s.Reconnect = reconnect.Config{
		Method:   reconnect.MethodHTTP,
		Username: "admin",
		Password: "router-secret",
		CheckURL: "https://example.invalid/ip",
		Requests: []reconnect.Request{{URL: "https://192.168.0.1/reconnect"}},
	}
	if _, err := a.ApplySettings(s); err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		query   string
		secrets string
		leaks   bool
	}{
		{"", settings.SecretsOmitted, false},
		{"?secrets=omit", settings.SecretsOmitted, false},
		{"?secrets=nonsense", settings.SecretsOmitted, false},
		{"?secrets=include", settings.SecretsIncluded, true},
	}
	for _, c := range cases {
		resp, err := http.Get(srv.URL + "/api/settings/export" + c.query)
		if err != nil {
			t.Fatal(err)
		}
		raw, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("GET export%s = %d", c.query, resp.StatusCode)
		}
		if ct := resp.Header.Get("Content-Type"); ct != "application/json" {
			t.Errorf("export%s Content-Type = %q", c.query, ct)
		}
		if resp.Header.Get("X-Content-Type-Options") != "nosniff" {
			t.Errorf("export%s is missing X-Content-Type-Options: nosniff", c.query)
		}
		cd := resp.Header.Get("Content-Disposition")
		if !strings.Contains(cd, "attachment") || !strings.Contains(cd, "knightloader-settings-") {
			t.Errorf("export%s Content-Disposition = %q, want an attachment named settings, not backup", c.query, cd)
		}

		var doc settings.PortableDoc
		if err := json.Unmarshal(raw, &doc); err != nil {
			t.Fatalf("export%s did not answer a document: %v", c.query, err)
		}
		if doc.Secrets != c.secrets {
			t.Errorf("export%s secrets = %q, want %q", c.query, doc.Secrets, c.secrets)
		}
		for _, secret := range []string{"router-secret", "hunter2"} {
			if strings.Contains(string(raw), secret) != c.leaks {
				t.Errorf("export%s: %q present = %v, want %v", c.query, secret, !c.leaks, c.leaks)
			}
		}
	}
}

// TestImportReportsRulesThisBuildCannotCompile is the other silence: sanitizeRules
// changes nothing on purpose, because a filter rule that vanishes on save is a
// filter the user goes on believing in. So a broken set saves cleanly and never
// fires, and an import that answered only "ok" would be that failure delivered
// through a new door.
func TestImportReportsRulesThisBuildCannotCompile(t *testing.T) {
	_, srv := transferServer(t)

	src := settings.Defaults()
	// A pattern the engine cannot parse. The set is enabled, so the compile is
	// actually attempted rather than skipped.
	raw := json.RawMessage(
		`{"rules":[{"name":"broken","conditions":[{"field":"url","op":"matches","value":"([unclosed"}],` +
			`"action":{"reject":true}}]}`)
	doc, err := settings.Portable(src, false, "v0.0.1", "container", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	doc.Settings["linkFilter"] = raw

	res := postImport(t, srv, doc, []string{"linkFilter"})
	if res.RuleProblems == 0 {
		t.Error("ruleProblems = 0 after importing a rule set that does not compile; the link filter now passes everything and nothing says so")
	}
}

func contains(list []string, want string) bool {
	for _, s := range list {
		if s == want {
			return true
		}
	}
	return false
}

func countOf(list []string, want string) int {
	n := 0
	for _, s := range list {
		if s == want {
			n++
		}
	}
	return n
}
