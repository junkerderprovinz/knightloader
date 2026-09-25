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
// on anything but a 200.
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
		// A plain-text refusal still explains itself.
		out["error"] = strings.TrimSpace(string(raw))
	}
	return resp.StatusCode, out
}

// TestImportLeavesUnnamedSettingsExactlyAsStored imports one key into a box
// whose values differ from both the defaults and the document, and checks that
// nothing else changed. Routing the import through the PUT path would reset
// omitted keys to their zero values.
func TestImportLeavesUnnamedSettingsExactlyAsStored(t *testing.T) {
	t.Parallel()
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

	// The one key meant to travel, surrounded by zero values.
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
	// One check per field, so a failure names the field that was reset.
	if got.MaxConcurrent != 9 {
		t.Errorf("maxConcurrent = %d, want the stored 9; an unnamed key was overwritten", got.MaxConcurrent)
	}
	if got.MaxPerHost != 5 {
		t.Errorf("maxPerHost = %d, want the stored 5; an unnamed key was overwritten", got.MaxPerHost)
	}
	if !got.Extract {
		t.Error("extract was switched off by an import that never named it")
	}
	if !got.AutoStart {
		t.Error("autoStart was switched off by an import that never named it")
	}
	if got.InstanceName != "the receiving box" {
		t.Errorf("instanceName = %q, want the stored name; an unnamed key was overwritten", got.InstanceName)
	}
}

// TestImportNamesTheSecretsThatDidNotTravel covers a proxy row that arrives
// without its password and dials anyway, and a cleared router password that
// makes the nightly reconnect fail. Both must come back as codes, and the
// redaction placeholder must never be stored.
func TestImportNamesTheSecretsThatDidNotTravel(t *testing.T) {
	t.Parallel()
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
			t.Errorf("incomplete = %v, missing %q; nothing tells the user to type it back in", res.Incomplete, code)
		}
	}

	stored := a.Settings.Get()
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
	// The row still arrives; the missing password is reported, not dropped
	// with it.
	if stored.Connections[0].Username != "vpn" {
		t.Errorf("the connection row did not arrive: %#v", stored.Connections[0])
	}
}

// TestImportKeepsThisBoxIdentity checks at the route that a hand-edited
// document cannot give this box another instance id.
func TestImportKeepsThisBoxIdentity(t *testing.T) {
	t.Parallel()
	a, srv := transferServer(t)
	mine := a.Settings.Get().InstanceID
	if mine == "" {
		t.Fatal("this box has no instance id, so the test proves nothing")
	}

	doc, err := settings.Portable(settings.Defaults(), false, "v0.0.1", "container", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	// Put back by hand, as a text editor could.
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
		t.Errorf("instanceId = %q, want the box's own %q; two boxes now collide in one relay group", got.InstanceID, mine)
	} else if len(got.KnownDomains) != 0 {
		t.Errorf("knownDomains = %v, want none; those addresses never pointed at this box", got.KnownDomains)
	}
}

// TestImportReportsKeysThisBuildDoesNotHave checks that a key this build has no
// field for, such as the old deleteArchive, is reported rather than dropped
// silently by encoding/json.
func TestImportReportsKeysThisBuildDoesNotHave(t *testing.T) {
	t.Parallel()
	_, srv := transferServer(t)

	doc, err := settings.Portable(settings.Defaults(), false, "v0.0.1", "container", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	doc.Settings["deleteArchive"] = json.RawMessage(`true`)

	// Reported even when not asked for.
	res := postImport(t, srv, doc, []string{"speedLimit"})
	if !contains(res.Unknown, "deleteArchive") {
		t.Errorf("unknown = %v, want it to name deleteArchive", res.Unknown)
	}
	// Asked for, it is neither reported twice nor applied.
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

// TestImportRefusesADocumentFromANewerBuild checks the one-way version guard,
// as in backup.Stage: an older document is accepted, a newer one refused with
// a code the interface can translate. It writes buildinfo.Version, which is
// safe because no test in this package runs in parallel.
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
		t.Errorf("code = %v, want transfer.tooNew; the refusal cannot be translated", env["code"])
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

// TestExportOmitsSecretsUnlessAskedFor checks that only the exact spelling
// "include" puts passwords into the export.
func TestExportOmitsSecretsUnlessAskedFor(t *testing.T) {
	t.Parallel()
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

// TestImportReportsRulesThisBuildCannotCompile checks that an imported rule
// set that saves cleanly but cannot compile is counted in the answer.
func TestImportReportsRulesThisBuildCannotCompile(t *testing.T) {
	t.Parallel()
	_, srv := transferServer(t)

	src := settings.Defaults()
	// A pattern the engine cannot parse, in an enabled set.
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
