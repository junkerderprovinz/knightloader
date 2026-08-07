package api

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/junkerderprovinz/knightloader/internal/rules"
)

func rulesServer(t *testing.T) *httptest.Server {
	t.Helper()
	reg := newRegistry()
	registerRules(reg, testApp(t))
	mux := http.NewServeMux()
	reg.attach(mux, http.NotFoundHandler())
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

// TestGrammarIsServedWhole is the contract the editor is built against: a form
// that offers an operator this build refuses produces a rule that saves cleanly
// and never fires, which is unfindable from the interface.
func TestGrammarIsServedWhole(t *testing.T) {
	srv := rulesServer(t)
	resp, err := http.Get(srv.URL + "/api/rules/grammar")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("grammar answered %d: %s", resp.StatusCode, raw)
	}
	var g rules.Grammar
	if err := json.Unmarshal(raw, &g); err != nil {
		t.Fatal(err)
	}
	if len(g.Fields) == 0 || len(g.Operators) == 0 || len(g.Actions) == 0 {
		t.Fatalf("grammar arrived incomplete: %d fields, %d operators, %d actions",
			len(g.Fields), len(g.Operators), len(g.Actions))
	}
	if len(g.Variables) == 0 || len(g.Categories) == 0 {
		t.Fatal("the variables menu and the category picker both render from the grammar; one of them arrived empty")
	}
	if g.Limits.PriorityMax != rules.PriorityMax || g.Limits.MaxChunks != rules.MaxChunks {
		t.Fatal("the bounds the form clamps to are not the bounds the engine refuses outside of")
	}
}

// TestPreviewReportsABadPatternAgainstItsRule is the whole reason the dry run is
// a route. An invalid regular expression must reach the user as a message on
// that rule; treated as a rule that simply matches nothing, it is the single
// most confusing failure a rule engine has.
func TestPreviewReportsABadPatternAgainstItsRule(t *testing.T) {
	srv := rulesServer(t)
	rep := postRules(t, srv, map[string]any{
		"set": rules.Set{Rules: []rules.Rule{
			{Name: "good", Conditions: []rules.Condition{
				{Field: rules.FieldURL, Op: rules.OpContains, Value: "films"},
			}, Action: rules.Action{PackageName: "Films"}},
			{Name: "broken", Conditions: []rules.Condition{
				{Field: rules.FieldFilename, Op: rules.OpMatches, Value: "("},
			}, Action: rules.Action{PackageName: "Nope"}},
		}},
		"links": []map[string]any{
			{"url": "https://films.example/one.mkv", "filename": "one.mkv"},
		},
	})

	if len(rep.Rules) != 2 {
		t.Fatalf("the report has %d rules, want one per rule in the set", len(rep.Rules))
	}
	if len(rep.Rules[0].Problems) != 0 {
		t.Fatalf("the good rule was reported as broken: %v", rep.Rules[0].Problems)
	}
	if len(rep.Rules[1].Problems) == 0 {
		t.Fatal("the unparsable pattern was accepted silently, which is the failure the test box exists to catch")
	}
	if rep.Rules[1].Index != 1 {
		t.Fatalf("the problem is keyed to rule %d, so the editor would draw it on the wrong rule", rep.Rules[1].Index)
	}
	if got := rep.Links[0].Result.Package; got != "Films" {
		t.Fatalf("the sample landed in %q; a broken rule must cost its own rule and nothing else", got)
	}
}

// TestPreviewSaysWhereALinkLands covers the answer the box is opened for: which
// rules fired, in order, and what came out.
func TestPreviewSaysWhereALinkLands(t *testing.T) {
	srv := rulesServer(t)
	rep := postRules(t, srv, map[string]any{
		"set": rules.Set{Rules: []rules.Rule{
			{Name: "by hoster", Conditions: []rules.Condition{
				{Field: rules.FieldHoster, Op: rules.OpEquals, Value: "films.example"},
			}, Action: rules.Action{PackageName: "Films", DownloadDir: "/data/<jd:packagename>"}},
			{Name: "samples out", Conditions: []rules.Condition{
				{Field: rules.FieldFilename, Op: rules.OpContains, Value: "sample"},
			}, Action: rules.Action{Reject: true, Reason: "a sample"}},
		}},
		"links": []map[string]any{
			{"url": "https://films.example/a.mkv", "filename": "a.mkv", "package": "Inbox"},
			{"url": "https://films.example/sample.mkv", "filename": "sample.mkv", "package": "Inbox"},
		},
	})

	if len(rep.Links) != 2 {
		t.Fatalf("got %d link reports, want 2", len(rep.Links))
	}
	if got := rep.Links[0].Result.Package; got != "Films" {
		t.Fatalf("first sample got package %q, want Films", got)
	}
	// "/data/Inbox", not "/data/Films". Variables resolve against the link as it
	// ARRIVED, so the folder template does not see the package name the very same
	// rule just set — rules do not chain onto each other's output. That is
	// deliberate in the engine and genuinely surprising, and showing it is half of
	// what the test box is for: found here it costs one glance, found afterwards
	// it costs a folder full of files in the wrong place.
	if got := rep.Links[0].Effect.Dir; got != "/data/Inbox" {
		t.Fatalf("the folder previewed as %q; the variable was not expanded the way staging would", got)
	}
	if rep.Links[0].Verdict.Rejected {
		t.Fatal("a link matching no reject rule was previewed as rejected")
	}
	if !rep.Links[1].Verdict.Rejected || rep.Links[1].Verdict.Reason != "a sample" {
		t.Fatalf("the rejected sample came back as %+v; a rejection must name its reason", rep.Links[1].Verdict)
	}
	if len(rep.Links[1].Matched) != 2 {
		t.Fatalf("the second sample matched %v, want both rules in order", rep.Links[1].Matched)
	}
}

// TestPreviewWorksOnASwitchedOffSet: a set cannot be repaired while it is off if
// being off also hides what is wrong with it.
func TestPreviewWorksOnASwitchedOffSet(t *testing.T) {
	srv := rulesServer(t)
	rep := postRules(t, srv, map[string]any{
		"set": rules.Set{Disabled: true, Rules: []rules.Rule{
			{Name: "off but broken", Conditions: []rules.Condition{
				{Field: rules.FieldFilename, Op: rules.OpMatches, Value: "["},
			}},
		}},
		"links": []map[string]any{{"url": "https://x.example/a.mkv", "filename": "a.mkv"}},
	})
	if !rep.Disabled {
		t.Fatal("the report does not say the set is switched off, so the editor cannot say so either")
	}
	if len(rep.Problems) == 0 {
		t.Fatal("a switched-off set reported no problems, so there is no way to fix it before switching it on")
	}
}

// TestPreviewRefusesTooManySamples: the cost is rules times links and both come
// from the request, so the ceiling has to be stated rather than discovered.
func TestPreviewRefusesTooManySamples(t *testing.T) {
	srv := rulesServer(t)
	links := make([]map[string]any, maxPreviewLinks+1)
	for i := range links {
		links[i] = map[string]any{"url": "https://x.example/a.mkv", "filename": "a.mkv"}
	}
	code, raw := postJSON(t, http.MethodPost, srv.URL+"/api/rules/preview",
		map[string]any{"set": rules.Set{}, "links": links})
	if code != http.StatusBadRequest {
		t.Fatalf("an oversized dry run answered %d: %s", code, raw)
	}
	if !strings.Contains(string(raw), "sample links") {
		t.Fatalf("the refusal does not say what was wrong: %s", raw)
	}
}

// TestPreviewDoesNotAdvanceTheLiveAppendCounter pins the reason Preview builds
// its own Matcher: a preview that counted would hand the next real download a
// suffix earned by somebody pressing a button.
func TestPreviewDoesNotAdvanceTheLiveAppendCounter(t *testing.T) {
	srv := rulesServer(t)
	set := rules.Set{Rules: []rules.Rule{
		{Name: "append", Action: rules.Action{PackageName: "Set<jd:append>"}},
	}}
	body := map[string]any{
		"set":   set,
		"links": []map[string]any{{"url": "https://x.example/a.mkv", "filename": "a.mkv"}},
	}
	first := postRules(t, srv, body)
	second := postRules(t, srv, body)
	if first.Links[0].Result.Package != "Set" || second.Links[0].Result.Package != "Set" {
		t.Fatalf("the counter carried across runs: %q then %q",
			first.Links[0].Result.Package, second.Links[0].Result.Package)
	}
}

func postRules(t *testing.T, srv *httptest.Server, body any) rules.Report {
	t.Helper()
	code, raw := postJSON(t, http.MethodPost, srv.URL+"/api/rules/preview", body)
	if code != http.StatusOK {
		t.Fatalf("preview answered %d: %s", code, raw)
	}
	var rep rules.Report
	if err := json.Unmarshal(raw, &rep); err != nil {
		t.Fatalf("decoding the report: %v", err)
	}
	return rep
}

// TestADryRunStagesNothing is the promise the route's summary makes. The test
// box runs on every keystroke, so a preview that leaked even one task into the
// collector would fill it while somebody was still typing the rule meant to keep
// those links out.
//
// This assertion arrived with POST /api/rules/test, which has since been removed
// for answering "no problems" about a rule set that does not compile whenever the
// set's master switch was off. The endpoint is gone; the thing it was right about
// belongs to whichever route answers the question.
func TestADryRunStagesNothing(t *testing.T) {
	a := testApp(t)
	reg := newRegistry()
	registerRules(reg, a)
	mux := http.NewServeMux()
	reg.attach(mux, http.NotFoundHandler())
	srv := httptest.NewServer(mux)
	defer srv.Close()

	code, raw := postJSON(t, http.MethodPost, srv.URL+"/api/rules/preview", map[string]any{
		"set": rules.Set{Rules: []rules.Rule{{
			Name:       "no samples",
			Conditions: []rules.Condition{{Field: rules.FieldURL, Op: rules.OpContains, Value: "sample"}},
			Action:     rules.Action{Reject: true, Reason: "sample files are not wanted"},
		}}},
		"links": []map[string]any{
			{"url": "https://host.example/sample.mkv", "filename": "sample.mkv"},
			{"url": "https://host.example/film.mkv", "filename": "film.mkv"},
		},
	})
	if code != http.StatusOK {
		t.Fatalf("preview answered %d: %s", code, raw)
	}
	var rep rules.Report
	if err := json.Unmarshal(raw, &rep); err != nil {
		t.Fatal(err)
	}
	if len(rep.Links) != 2 {
		t.Fatalf("reported %d links for 2 samples", len(rep.Links))
	}
	if !rep.Links[0].Verdict.Rejected {
		t.Error("the sample link came back accepted")
	}
	if rep.Links[1].Verdict.Rejected {
		t.Error("an unrelated link came back rejected")
	}
	if n := len(a.Tasks()); n != 0 {
		t.Errorf("the dry run staged %d tasks", n)
	}
}
