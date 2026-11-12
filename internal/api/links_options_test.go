package api

// The add-links form's batch fields on POST /api/links: destination, priority,
// unpacking switch, comment, the two passwords and the Overrule checkbox.
// linkServer (links_test.go) is reused, so these exercise the same route the
// plain-paste tests do.

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/junkerderprovinz/knightloader/internal/app"
	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/rules"
	"github.com/junkerderprovinz/knightloader/internal/settings"
)

// TestAddLinksBatchFieldsReachTheTask is the wire-level half of
// TestFormOptionsApplyWithNoRuleInvolved: every field the form can send lands
// on the created task, through the route rather than the App method.
func TestAddLinksBatchFieldsReachTheTask(t *testing.T) {
	t.Parallel()
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

// TestAddLinksRefusesABadDestination: a destination that cannot be used stops
// the whole submission with the server's own reason, rather than staging the
// batch to the default folder in silence.
func TestAddLinksRefusesABadDestination(t *testing.T) {
	t.Parallel()
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

// TestAddLinksWithNoBatchFieldsIsStillAPlainPaste: the route always calls
// AddLinksWithOptions, so a request naming none of the batch fields has to
// behave like a plain paste.
func TestAddLinksWithNoBatchFieldsIsStillAPlainPaste(t *testing.T) {
	t.Parallel()
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

// A format the server cannot stage refuses the submission rather than staging
// the link with the host's preset, which would download something the sender
// did not ask for.
func TestAddLinksRefusesAnUnknownFormat(t *testing.T) {
	t.Parallel()
	srv, a := linkServer(t)

	code, raw := postJSON(t, http.MethodPost, srv.URL+"/api/links", map[string]any{
		"links":  "https://host.example/clip",
		"format": "8k",
	})
	if code != http.StatusBadRequest {
		t.Fatalf("answered %d: %s, want 400", code, raw)
	}
	if n := len(a.Tasks()); n != 0 {
		t.Errorf("%d tasks were staged for a refused submission", n)
	}
}

// With report set, a link already in the list comes back under listed, with
// the row it was folded into, where the bare answer is only an empty list.
func TestAddLinksReportsALinkAlreadyInTheList(t *testing.T) {
	t.Parallel()
	srv, _ := linkServer(t)
	send := func(report bool) []byte {
		code, raw := postJSON(t, http.MethodPost, srv.URL+"/api/links", map[string]any{
			"links":  "https://host.example/listed.bin",
			"origin": "cnl",
			"report": report,
		})
		if code != http.StatusOK {
			t.Fatalf("answered %d: %s", code, raw)
		}
		return raw
	}
	var first struct{ Tasks, Listed []core.Task }
	if err := json.Unmarshal(send(true), &first); err != nil {
		t.Fatal(err)
	}
	if len(first.Tasks) != 1 || first.Listed == nil || len(first.Listed) != 0 {
		t.Fatalf("the first send answered %d tasks and %v listed, want one task and an empty list", len(first.Tasks), first.Listed)
	}
	var again struct{ Tasks, Listed []core.Task }
	if err := json.Unmarshal(send(true), &again); err != nil {
		t.Fatal(err)
	}
	if len(again.Tasks) != 0 || len(again.Listed) != 1 || again.Listed[0].ID != first.Tasks[0].ID {
		t.Errorf("sent again, the answer has %d tasks and %d listed, want none and the first send's row", len(again.Tasks), len(again.Listed))
	}
	if bare := strings.TrimSpace(string(send(false))); bare != "[]" {
		t.Errorf("without report the answer is %s, want the bare empty list", bare)
	}
}

// start reaches the batch: with auto-confirm off, the link leaves the
// collector, where it would otherwise wait.
func TestAddLinksStartsABatchSentToStart(t *testing.T) {
	t.Parallel()
	srv, a := linkServer(t)
	a.SetHalted(true)
	code, raw := postJSON(t, http.MethodPost, srv.URL+"/api/links", map[string]any{
		"links":  "https://host.example/now.bin",
		"origin": "cnl",
		"start":  true,
	})
	if code != http.StatusOK {
		t.Fatalf("answered %d: %s", code, raw)
	}
	var created []core.Task
	if err := json.Unmarshal(raw, &created); err != nil || len(created) != 1 {
		t.Fatalf("staged %d tasks: %v", len(created), err)
	}
	if got := a.Tasks()[0].Status; got != core.StatusQueued {
		t.Errorf("the link sent to start is %s, want queued", got)
	}
}

// A choice of rows that names something yt-dlp has not got is refused like
// an unknown format.
func TestAddLinksRefusesAChoiceOfNoRow(t *testing.T) {
	t.Parallel()
	srv, a := linkServer(t)
	code, raw := postJSON(t, http.MethodPost, srv.URL+"/api/links", map[string]any{
		"links":  "https://host.example/clip",
		"choice": map[string]any{"variants": []string{"video"}, "quality": "8k"},
	})
	if code != http.StatusBadRequest {
		t.Fatalf("answered %d: %s, want 400", code, raw)
	}
	if n := len(a.Tasks()); n != 0 {
		t.Errorf("%d tasks were staged for a refused submission", n)
	}
}

// resolver pins the links of a submission to one backend, which the browser
// extension's video button uses to have yt-dlp fetch the video it sits on.
func TestAddLinksPinsTheLinksToTheBackendNamed(t *testing.T) {
	t.Parallel()
	srv, a := linkServer(t)

	code, raw := postJSON(t, http.MethodPost, srv.URL+"/api/links", map[string]any{
		"links":    "https://host.example/clip",
		"resolver": "nosuch",
	})
	if code != http.StatusBadRequest {
		t.Fatalf("a backend the instance lacks answered %d: %s, want 400", code, raw)
	}
	if n := len(a.Tasks()); n != 0 {
		t.Fatalf("%d tasks were staged for a refused submission", n)
	}

	code, raw = postJSON(t, http.MethodPost, srv.URL+"/api/links", map[string]any{
		"links":    "https://host.example/pinned.bin",
		"resolver": "direct",
	})
	if code != http.StatusOK {
		t.Fatalf("answered %d: %s", code, raw)
	}
	var created []core.Task
	if err := json.Unmarshal(raw, &created); err != nil || len(created) != 1 {
		t.Fatalf("staged %d tasks: %v", len(created), err)
	}
	if created[0].ResolverPin != "direct" {
		t.Errorf("the staged row is pinned to %q, want direct", created[0].ResolverPin)
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
// reaches app.LinkBatchOptions.Overrule. The app-level tests cover what it
// does to the precedence; this only shows the field survives the JSON round
// trip against a real Packagizer rule.
func TestAddLinksOverruleField(t *testing.T) {
	t.Parallel()
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

// TestAddLinksWithoutOverruleLetsTheRuleWin is the same rule and form values
// with the field left out, which defaults to false on the wire as it does in
// Go.
func TestAddLinksWithoutOverruleLetsTheRuleWin(t *testing.T) {
	t.Parallel()
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
