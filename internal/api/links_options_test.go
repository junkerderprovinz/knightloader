package api

// The add-links form's own batch fields on POST /api/links (build-plan.md
// §8A): destination, priority, unpacking switch, comment, the two passwords,
// and the Overrule checkbox. linkServer (links_test.go) is reused rather than
// rebuilt, so these exercise the exact same route the plain-paste tests do.

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/junkerderprovinz/knightloader/internal/app"
	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/rules"
	"github.com/junkerderprovinz/knightloader/internal/settings"
)

// TestAddLinksBatchFieldsReachTheTask is the wire-level half of
// TestFormOptionsApplyWithNoRuleInvolved: every field the form can send lands
// on the created task, through the route rather than the App method
// directly.
func TestAddLinksBatchFieldsReachTheTask(t *testing.T) {
	srv, _ := linkServer(t)

	code, raw := postJSON(t, http.MethodPost, srv.URL+"/api/links", map[string]any{
		"links":            "https://host.example/one.bin",
		"package":          "Batch",
		"dir":              filepath.Join(t.TempDir(), "batch-dest"),
		"password":         "archivepw",
		"downloadPassword": "linkpw",
		"comment":          "from the form",
		"priority":         -2,
		"autoExtract":      false,
	})
	if code != http.StatusOK {
		t.Fatalf("answered %d: %s", code, raw)
	}
	var created []core.Task
	if err := json.Unmarshal(raw, &created); err != nil {
		t.Fatal(err)
	}
	if len(created) != 1 {
		t.Fatalf("staged %d tasks", len(created))
	}
	got := created[0]
	if got.Password != "archivepw" {
		t.Errorf("archive password = %q, want the form's own", got.Password)
	}
	if got.DownloadPassword != "linkpw" {
		t.Errorf("link password = %q, want the form's own, and distinct from the archive one", got.DownloadPassword)
	}
	if got.Comment != "from the form" {
		t.Errorf("comment = %q", got.Comment)
	}
	if got.Priority != -2 {
		t.Errorf("priority = %d, want -2", got.Priority)
	}
	if got.AutoExtract == nil || *got.AutoExtract {
		t.Errorf("auto-extract = %v, want the form's own off", got.AutoExtract)
	}
	if got.Dir == "" {
		t.Error("dir is empty, want the form's own destination")
	}
}

// TestAddLinksRefusesABadDestination is the 400 path: a destination that
// cannot be used stops the whole submission, with the server's own reason,
// rather than staging the batch to the default folder in silence.
func TestAddLinksRefusesABadDestination(t *testing.T) {
	srv, a := linkServer(t)

	code, raw := postJSON(t, http.MethodPost, srv.URL+"/api/links", map[string]any{
		"links": "https://host.example/one.bin",
		"dir":   "not/absolute",
	})
	if code != http.StatusBadRequest {
		t.Fatalf("answered %d, want 400 for a relative destination: %s", code, raw)
	}
	if len(raw) == 0 {
		t.Error("the refusal carries no reason")
	}
	if n := len(a.Tasks()); n != 0 {
		t.Errorf("%d tasks staged despite the refused destination", n)
	}
}

// TestAddLinksWithNoBatchFieldsIsStillAPlainPaste guards the route's
// simplification of always calling AddLinksWithOptions once body.Passwords is
// empty: a request naming none of the new fields must behave exactly as it
// did before they existed.
func TestAddLinksWithNoBatchFieldsIsStillAPlainPaste(t *testing.T) {
	srv, _ := linkServer(t)

	code, raw := postJSON(t, http.MethodPost, srv.URL+"/api/links", map[string]any{
		"links": "https://host.example/plain.bin",
	})
	if code != http.StatusOK {
		t.Fatalf("answered %d: %s", code, raw)
	}
	var created []core.Task
	if err := json.Unmarshal(raw, &created); err != nil {
		t.Fatal(err)
	}
	if len(created) != 1 {
		t.Fatalf("staged %d tasks", len(created))
	}
	got := created[0]
	if got.Dir != "" || got.Password != "" || got.DownloadPassword != "" || got.Comment != "" || got.Priority != 0 || got.AutoExtract != nil {
		t.Errorf("an ordinary paste picked up a batch field it was never sent: %+v", got)
	}
}

// packagizerServer is linkServer's twin, with one Packagizer rule already in
// place: everything from rule.example gets the comment "ruled" and priority
// 3, so a test can tell the rule's answer apart from the form's.
func packagizerServer(t *testing.T) *httptest.Server {
	t.Helper()
	a, err := app.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { a.Close() })
	s := settings.Defaults()
	s.DownloadDir = t.TempDir()
	s.Crawl = false
	prio := 3
	s.Packagizer = rules.Set{Rules: []rules.Rule{{
		Name:       "rule wins here",
		Conditions: []rules.Condition{{Field: rules.FieldHoster, Op: rules.OpEquals, Value: "rule.example"}},
		Action:     rules.Action{Comment: "ruled", Priority: &prio},
	}}}
	if _, err := a.ApplySettings(s); err != nil {
		t.Fatal(err)
	}
	reg := newRegistry()
	registerLinks(reg, a)
	mux := http.NewServeMux()
	reg.attach(mux, http.NotFoundHandler())
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

// TestAddLinksOverruleField pins the wire spelling of the checkbox: "overrule"
// reaches app.LinkBatchOptions.Overrule, which the app-level tests already
// prove inverts the precedence in full. This only has to show the field
// survives the JSON round trip against a real Packagizer rule.
func TestAddLinksOverruleField(t *testing.T) {
	srv := packagizerServer(t)

	code, raw := postJSON(t, http.MethodPost, srv.URL+"/api/links", map[string]any{
		"links":    "https://rule.example/one.bin",
		"comment":  "from the form",
		"priority": -1,
		"overrule": true,
	})
	if code != http.StatusOK {
		t.Fatalf("answered %d: %s", code, raw)
	}
	var created []core.Task
	if err := json.Unmarshal(raw, &created); err != nil {
		t.Fatal(err)
	}
	if len(created) != 1 {
		t.Fatalf("staged %d tasks", len(created))
	}
	got := created[0]
	if got.Comment != "from the form" {
		t.Errorf("comment = %q, want the form's own: overrule=true was sent", got.Comment)
	}
	if got.Priority != -1 {
		t.Errorf("priority = %d, want the form's own -1: overrule=true was sent", got.Priority)
	}
}

// TestAddLinksWithoutOverruleLetsTheRuleWin is the same rule and the same form
// values with the field left out, which must default to false on the wire
// exactly as it does in Go.
func TestAddLinksWithoutOverruleLetsTheRuleWin(t *testing.T) {
	srv := packagizerServer(t)

	code, raw := postJSON(t, http.MethodPost, srv.URL+"/api/links", map[string]any{
		"links":    "https://rule.example/two.bin",
		"comment":  "from the form",
		"priority": -1,
	})
	if code != http.StatusOK {
		t.Fatalf("answered %d: %s", code, raw)
	}
	var created []core.Task
	if err := json.Unmarshal(raw, &created); err != nil {
		t.Fatal(err)
	}
	if len(created) != 1 {
		t.Fatalf("staged %d tasks", len(created))
	}
	got := created[0]
	if got.Comment != "ruled" {
		t.Errorf("comment = %q, want the rule's own: overrule was never sent", got.Comment)
	}
	if got.Priority != 3 {
		t.Errorf("priority = %d, want the rule's own 3: overrule was never sent", got.Priority)
	}
}
