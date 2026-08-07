package api

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/junkerderprovinz/knightloader/internal/app"
	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/rules"
	"github.com/junkerderprovinz/knightloader/internal/settings"
)

// heldServer is an instance with one filter rule in it and one link already
// caught by that rule, which is the only state the holding-area routes have
// anything to say about.
func heldServer(t *testing.T) (*httptest.Server, *app.App, *core.Task) {
	t.Helper()
	a := testApp(t)
	s := settings.Defaults()
	s.DownloadDir = t.TempDir()
	// Off, so a pasted link is staged as itself and the test needs no network.
	s.Crawl = false
	s.LinkFilter = rules.Set{Rules: []rules.Rule{{
		Name:       "no samples",
		Conditions: []rules.Condition{{Field: rules.FieldURL, Op: rules.OpContains, Value: "sample"}},
		Action:     rules.Action{Reject: true, Reason: "sample files are not wanted here"},
	}}}
	if _, err := a.ApplySettings(s); err != nil {
		t.Fatal(err)
	}
	created := a.AddLinks([]string{"https://host.example/sample.mkv"}, "Batch")
	if len(created) != 1 {
		t.Fatalf("staged %d links", len(created))
	}

	reg := newRegistry()
	registerLinks(reg, a)
	mux := http.NewServeMux()
	reg.attach(mux, http.NotFoundHandler())
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv, a, created[0]
}

func getJSON(t *testing.T, url string, into any) int {
	t.Helper()
	resp, err := http.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if into != nil && len(raw) > 0 {
		if err := json.Unmarshal(raw, into); err != nil {
			t.Fatalf("%s answered unparseable JSON: %v (%s)", url, err, raw)
		}
	}
	return resp.StatusCode
}

// TestTheHoldingAreaIsServedWithItsReason is what a client that is not a browser
// has to be able to ask. The browser reads the same links off the task stream,
// but a route that does not exist cannot appear in the self-describing index,
// and then the holding area is a feature only the interface knows about.
func TestTheHoldingAreaIsServedWithItsReason(t *testing.T) {
	srv, _, held := heldServer(t)

	var got []core.Task
	if code := getJSON(t, srv.URL+"/api/collector/filtered", &got); code != http.StatusOK {
		t.Fatalf("the holding area answered %d", code)
	}
	if len(got) != 1 || got[0].ID != held.ID {
		t.Fatalf("holding %d links, want the one the filter caught", len(got))
	}
	if !got[0].Skipped {
		t.Error("the served link does not say it is held")
	}
	if got[0].SkipReason == "" {
		t.Error("the served link carries no reason, which is the one thing this list is for")
	}
	if len(got[0].MatchedRules) != 1 || got[0].MatchedRules[0] != "no samples" {
		t.Errorf("matched rules = %v, want the rule to name itself as data", got[0].MatchedRules)
	}
}

// TestRestoreEmptiesTheHoldingArea covers the button the list exists for.
func TestRestoreEmptiesTheHoldingArea(t *testing.T) {
	srv, a, held := heldServer(t)

	code, raw := postJSON(t, http.MethodPost, srv.URL+"/api/collector/filtered/restore",
		map[string]any{"ids": []string{held.ID}})
	if code != http.StatusOK {
		t.Fatalf("restore answered %d: %s", code, raw)
	}
	var restored []core.Task
	if err := json.Unmarshal(raw, &restored); err != nil {
		t.Fatal(err)
	}
	if len(restored) != 1 || restored[0].Skipped {
		t.Fatalf("restore returned %d links, want the one, no longer held: %s", len(restored), raw)
	}
	if n := len(a.FilteredLinks()); n != 0 {
		t.Errorf("%d links still held after restoring all of them", n)
	}
}

// TestClearingTakesIdsFromTheQuery pins the shape of the DELETE. A body on a
// DELETE is not something every client and proxy between a browser and this
// server passes on, and a clear that silently reached everything because the ids
// never arrived would delete links the user did not pick.
func TestClearingTakesIdsFromTheQuery(t *testing.T) {
	srv, a, held := heldServer(t)

	code, raw := postJSON(t, http.MethodDelete, srv.URL+"/api/collector/filtered?ids="+held.ID, nil)
	if code != http.StatusOK {
		t.Fatalf("clear answered %d: %s", code, raw)
	}
	var body struct {
		Removed int      `json:"removed"`
		IDs     []string `json:"ids"`
	}
	if err := json.Unmarshal(raw, &body); err != nil {
		t.Fatal(err)
	}
	if body.Removed != 1 || len(body.IDs) != 1 || body.IDs[0] != held.ID {
		t.Fatalf("clear reports %+v, want exactly the one link that was named", body)
	}
	if n := len(a.FilteredLinks()); n != 0 {
		t.Errorf("%d links still held", n)
	}
}

// TestIdsFromQuery is the difference between "these two" and "all of them". A
// trailing comma is the ordinary way a client builds that string, and reading it
// as one more id that matches nothing would turn a two-link clear into a no-op —
// while reading a genuinely empty parameter as an id would turn "clear all" into
// the same no-op from the other direction.
func TestIdsFromQuery(t *testing.T) {
	cases := map[string][]string{
		"":         nil,
		"   ":      nil,
		"a":        {"a"},
		"a,b":      {"a", "b"},
		"a, b ,":   {"a", "b"},
		",,":       nil,
		"a,,b,":    {"a", "b"},
		"?ids=set": {"?ids=set"}, // not re-parsed: whatever the value is, it is one id
	}
	for raw, want := range cases {
		req := httptest.NewRequest(http.MethodDelete, "/api/collector/filtered", nil)
		q := req.URL.Query()
		q.Set("ids", raw)
		req.URL.RawQuery = q.Encode()

		got := idsFromQuery(req)
		if len(got) != len(want) {
			t.Errorf("ids=%q gave %v, want %v", raw, got, want)
			continue
		}
		for i := range got {
			if got[i] != want[i] {
				t.Errorf("ids=%q gave %v, want %v", raw, got, want)
				break
			}
		}
	}
}

// linkServer is a plain instance with the link routes on it and no filter, for
// the questions that are about the entrance rather than about the holding area.
func linkServer(t *testing.T) (*httptest.Server, *app.App) {
	t.Helper()
	a := testApp(t)
	s := settings.Defaults()
	s.DownloadDir = t.TempDir()
	// Off, so a pasted link is staged as itself and the test needs no network.
	s.Crawl = false
	if _, err := a.ApplySettings(s); err != nil {
		t.Fatal(err)
	}
	reg := newRegistry()
	registerLinks(reg, a)
	mux := http.NewServeMux()
	reg.attach(mux, http.NotFoundHandler())
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv, a
}

// TestARelayedSubmissionKeepsItsEntranceAndItsPasswords is the bridge's half of
// Click'n'Load.
//
// CnL is hard-wired to the browser's own loopback, so the primary deployment —
// a container on a NAS — can only receive it through a bridge running on the
// user's desktop, which decodes the submission and forwards it over this route.
// Both of the things that submission knows used to be thrown away here: the
// entrance, so a browser button was filed as a paste in the one column somebody
// opens the holding area to read, and the archive passwords, which the bridge
// has always sent and nothing read — so the extraction then asked for a password
// the user had already handed over.
func TestARelayedSubmissionKeepsItsEntranceAndItsPasswords(t *testing.T) {
	srv, _ := linkServer(t)

	code, raw := postJSON(t, http.MethodPost, srv.URL+"/api/links", map[string]any{
		"links":     "https://host.example/secret.rar",
		"package":   "Batch",
		"origin":    "cnl",
		"passwords": []string{"hunter2", "spare"},
	})
	if code != http.StatusOK {
		t.Fatalf("answered %d: %s", code, raw)
	}
	var created []*core.Task
	if err := json.Unmarshal(raw, &created); err != nil {
		t.Fatal(err)
	}
	if len(created) != 1 {
		t.Fatalf("staged %d links", len(created))
	}
	if created[0].Origin != app.OriginCnL {
		t.Errorf("filed as %q, want %q: a relayed submission is still a browser button",
			created[0].Origin, app.OriginCnL)
	}
	if created[0].Password != "hunter2" {
		t.Errorf("the task carries password %q, want the one the submission supplied", created[0].Password)
	}
}

// TestAPasteIsStillAPaste guards the default. The interface sends neither field,
// and a route that started answering something else for it would relabel every
// link anybody has ever pasted.
func TestAPasteIsStillAPaste(t *testing.T) {
	srv, _ := linkServer(t)

	code, raw := postJSON(t, http.MethodPost, srv.URL+"/api/links", map[string]any{
		"links": "https://host.example/one.bin",
	})
	if code != http.StatusOK {
		t.Fatalf("answered %d: %s", code, raw)
	}
	var created []*core.Task
	if err := json.Unmarshal(raw, &created); err != nil {
		t.Fatal(err)
	}
	if len(created) != 1 || created[0].Origin != app.OriginPaste {
		t.Fatalf("staged %d links, first filed as %q, want one paste", len(created), created[0].Origin)
	}
}

// TestAnUnknownEntranceIsRefused keeps the column answerable. Filing a link
// under an entrance nobody recognises is worse than refusing it: "why is this
// here" then has an answer that looks real and is not, and a rule keyed on the
// entrance reads a value no part of this app ever writes.
func TestAnUnknownEntranceIsRefused(t *testing.T) {
	srv, a := linkServer(t)

	code, _ := postJSON(t, http.MethodPost, srv.URL+"/api/links", map[string]any{
		"links":  "https://host.example/one.bin",
		"origin": "smuggled",
	})
	if code != http.StatusBadRequest {
		t.Errorf("answered %d, want the submission refused with its reason", code)
	}
	if n := len(a.Tasks()); n != 0 {
		t.Errorf("%d links were staged under an entrance that does not exist", n)
	}
}
