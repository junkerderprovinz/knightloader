package api

// Route-level tests for the media library call, against a real app on a
// throwaway data directory - the shape routes_hostheaders_test.go uses next
// door, and for the same reason: what is asserted here is what actually crosses
// the wire and what actually lands in the encrypted store, and a stubbed app can
// answer for neither.

import (
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/junkerderprovinz/knightloader/internal/accounts"
	"github.com/junkerderprovinz/knightloader/internal/app"
	"github.com/junkerderprovinz/knightloader/internal/mediahook"
	"github.com/junkerderprovinz/knightloader/internal/settings"
)

// hookToken is the one string every leak assertion in this file hunts for: a
// single distinctive needle, so a hit anywhere is unambiguous and a miss is not a
// coincidence.
const hookToken = "SECRET-emby-4t9x-no-response-may-carry-this"

func mediaHookServer(t *testing.T) (*app.App, *httptest.Server) {
	t.Helper()
	a := testApp(t)
	reg := newRegistry()
	registerMediaHooks(reg, a)
	mux := http.NewServeMux()
	reg.attach(mux, http.NotFoundHandler())
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return a, srv
}

// storeHook saves one address THROUGH THE ROUTE, so every assertion below is
// about what this HTTP surface did rather than about a store a test filled in
// behind its back.
func storeHook(t *testing.T, srv *httptest.Server, body map[string]any) []byte {
	t.Helper()
	code, raw := postJSON(t, http.MethodPost, srv.URL+"/api/mediahooks", body)
	if code != http.StatusOK {
		t.Fatalf("POST /api/mediahooks answered %d: %s", code, raw)
	}
	return raw
}

func jellyfinBody() map[string]any {
	return map[string]any{
		"id":          "jellyfin",
		"name":        "Jellyfin",
		"url":         "http://jellyfin.lan:8096/Library/Refresh",
		"method":      "POST",
		"headerName":  "X-Emby-Token",
		"headerValue": hookToken,
		"waitSeconds": 60,
	}
}

func listHooks(t *testing.T, srv *httptest.Server) []byte {
	t.Helper()
	resp, err := srv.Client().Get(srv.URL + "/api/mediahooks")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /api/mediahooks answered %d", resp.StatusCode)
	}
	out, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

type hookRow struct {
	ID          string          `json:"id"`
	Name        string          `json:"name"`
	URL         string          `json:"url"`
	Method      string          `json:"method"`
	HeaderName  string          `json:"headerName"`
	WaitSeconds int             `json:"waitSeconds"`
	HasValue    bool            `json:"hasValue"`
	Host        string          `json:"host"`
	Private     bool            `json:"private"`
	UsedBy      []string        `json:"usedBy"`
	Last        json.RawMessage `json:"last"`
}

func decodeRows(t *testing.T, raw []byte) []hookRow {
	t.Helper()
	var rows []hookRow
	if err := json.Unmarshal(raw, &rows); err != nil {
		t.Fatalf("the listing is not a list of addresses: %v (%s)", err, raw)
	}
	return rows
}

// TestTheListingCarriesNamesAndNeverTheValue is the one promise this surface
// makes that cannot be taken back once broken.
func TestTheListingCarriesNamesAndNeverTheValue(t *testing.T) {
	_, srv := mediaHookServer(t)
	storeHook(t, srv, jellyfinBody())

	raw := listHooks(t, srv)
	if strings.Contains(string(raw), hookToken) {
		t.Fatalf("the header value is in the listing: %s", raw)
	}
	rows := decodeRows(t, raw)
	if len(rows) != 1 {
		t.Fatalf("the listing has %d rows, want one", len(rows))
	}
	r := rows[0]
	if !r.HasValue {
		t.Error("hasValue is false although a value was stored")
	}
	if r.Host != "jellyfin.lan:8096" {
		t.Errorf("host = %q", r.Host)
	}
	// A host NAME is not something this can resolve without a lookup, and it
	// answers false rather than guessing - which draws the extra "this leaves
	// your network" sentence. Erring that way is the point.
	if r.Private {
		t.Error("a host name was reported as private without a lookup")
	}
	if r.UsedBy == nil {
		t.Error("usedBy is null; a card mapping over it would have to guard first")
	}
	if len(r.UsedBy) != 0 {
		t.Errorf("usedBy = %v on an address no drawer points at", r.UsedBy)
	}
	if string(r.Last) != "null" {
		t.Errorf("last = %s before anything has been called", r.Last)
	}
}

// TestThePlaceholderKeepsTheStoredValue is the trap every secret in this app
// carries, and the one HeaderProfiles.tsx documents at length: the listing has no
// value to send back, so a form that re-sent what it was given would send an
// empty one - and empty means "clear this" here as it does everywhere else. A
// person editing the WAIT would silently lose their token.
func TestThePlaceholderKeepsTheStoredValue(t *testing.T) {
	a, srv := mediaHookServer(t)
	storeHook(t, srv, jellyfinBody())

	edited := jellyfinBody()
	edited["waitSeconds"] = 300
	edited["headerValue"] = accounts.Redacted
	storeHook(t, srv, edited)

	if got := a.MediaHookStore().Value("jellyfin"); got != hookToken {
		t.Fatalf("the stored value is %q after an edit that did not touch it", got)
	}
	rows := decodeRows(t, listHooks(t, srv))
	if len(rows) != 1 || rows[0].WaitSeconds != 300 {
		t.Errorf("the edit did not land: %+v", rows)
	}
	// And the placeholder itself was never sealed as the value, which is the
	// other half of the same mistake: eight stars sent as a token is a 401
	// nobody can diagnose.
	if got := a.MediaHookStore().Value("jellyfin"); got == accounts.Redacted {
		t.Error("the literal placeholder was stored as the header value")
	}
}

// TestAnEmptyValueClears, which has to keep working or a stored token could
// never be removed through the page at all.
func TestAnEmptyValueClears(t *testing.T) {
	a, srv := mediaHookServer(t)
	storeHook(t, srv, jellyfinBody())

	cleared := jellyfinBody()
	cleared["headerValue"] = ""
	storeHook(t, srv, cleared)

	if a.MediaHookStore().Has("jellyfin") {
		t.Error("the value survived a save that cleared it")
	}
	rows := decodeRows(t, listHooks(t, srv))
	if len(rows) != 1 || rows[0].HasValue {
		t.Errorf("hasValue is still true: %+v", rows)
	}
}

func TestASaveIsRefusedWithASentenceNamingTheField(t *testing.T) {
	for _, c := range []struct {
		name string
		edit func(map[string]any)
		want string
	}{
		{"a name nothing could point at", func(b map[string]any) { b["id"] = "jellyfin lan" }, "letters, digits"},
		{"a relative address", func(b map[string]any) { b["url"] = "/Library/Refresh" }, "http://"},
		{"a method this build does not send", func(b map[string]any) { b["method"] = "DELETE" }, "GET"},
		{"a whole header line in the name", func(b map[string]any) { b["headerName"] = "X-Emby-Token: abc" }, "header name"},
		{"a wait outside the range", func(b map[string]any) { b["waitSeconds"] = 99999 }, "outside"},
	} {
		t.Run(c.name, func(t *testing.T) {
			a, srv := mediaHookServer(t)
			body := jellyfinBody()
			c.edit(body)
			code, raw := postJSON(t, http.MethodPost, srv.URL+"/api/mediahooks", body)
			if code != http.StatusBadRequest {
				t.Fatalf("answered %d: %s", code, raw)
			}
			if !strings.Contains(string(raw), c.want) {
				t.Errorf("the refusal is %q, which does not say %q", raw, c.want)
			}
			// And nothing was stored on the way to the refusal - including the
			// value, which is the half that would otherwise be sealed under an
			// id no row names.
			if len(a.Settings.Get().MediaHooks) != 0 || a.MediaHookStore().Has("jellyfin") {
				t.Error("a refused save left something behind")
			}
		})
	}
}

// TestDeleteIsRefusedWhileADrawerStillCallsIt. ValidateMediaHooks refuses the
// whole settings document for a drawer pointing at an address that is not there,
// so deleting one out from under a drawer would leave the settings page
// unsaveable with nothing on it saying why.
func TestDeleteIsRefusedWhileADrawerStillCallsIt(t *testing.T) {
	a, srv := mediaHookServer(t)
	storeHook(t, srv, jellyfinBody())

	cfg := a.Settings.Get()
	cfg.Categories = []settings.Category{{ID: "serien", Name: "Serien", Notify: "jellyfin"}}
	if _, err := a.Settings.Set(cfg); err != nil {
		t.Fatal(err)
	}

	code, raw := postJSON(t, http.MethodDelete, srv.URL+"/api/mediahooks/jellyfin", nil)
	if code != http.StatusConflict {
		t.Fatalf("DELETE answered %d: %s", code, raw)
	}
	if !strings.Contains(string(raw), "serien") {
		t.Errorf("the refusal does not name the drawer that still calls it: %s", raw)
	}
	if _, ok := a.Settings.Get().MediaHookFor("jellyfin"); !ok {
		t.Error("the address was deleted anyway")
	}
	// The listing says the same thing the refusal does, so somebody can find the
	// drawers before pressing the button rather than after.
	rows := decodeRows(t, listHooks(t, srv))
	if len(rows) != 1 || len(rows[0].UsedBy) != 1 || rows[0].UsedBy[0] != "serien" {
		t.Errorf("usedBy = %v", rows[0].UsedBy)
	}
}

func TestDeleteTakesTheSealedValueWithIt(t *testing.T) {
	a, srv := mediaHookServer(t)
	storeHook(t, srv, jellyfinBody())

	code, raw := postJSON(t, http.MethodDelete, srv.URL+"/api/mediahooks/jellyfin", nil)
	if code != http.StatusNoContent {
		t.Fatalf("DELETE answered %d: %s", code, raw)
	}
	if _, ok := a.Settings.Get().MediaHookFor("jellyfin"); ok {
		t.Error("the address is still in the table")
	}
	// A value left sealed under an id nothing names is a credential nobody can
	// see and nobody can remove.
	if a.MediaHookStore().Has("jellyfin") {
		t.Error("the header value outlived the address it belonged to")
	}
}

func TestDeletingSomethingThatIsNotThereIs404(t *testing.T) {
	_, srv := mediaHookServer(t)
	code, raw := postJSON(t, http.MethodDelete, srv.URL+"/api/mediahooks/kodi", nil)
	if code != http.StatusNotFound {
		t.Fatalf("DELETE answered %d: %s", code, raw)
	}
}

// TestTheTestCallAnswers200WithTheReasonInTheBody is the same call POST
// /api/feeds/test makes: the request was perfectly good, it was the far end that
// was not, and a 4xx would have the browser log the one answer somebody is meant
// to read.
func TestTheTestCallAnswers200WithTheReasonInTheBody(t *testing.T) {
	_, srv := mediaHookServer(t)

	// A listener taken straight back down, so the address is one nothing can be
	// listening on rather than a number picked and hoped for.
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := l.Addr().String()
	if err := l.Close(); err != nil {
		t.Fatal(err)
	}
	body := jellyfinBody()
	body["url"] = "http://" + addr + "/Library/Refresh"
	body["method"] = "GET"
	storeHook(t, srv, body)

	code, raw := postJSON(t, http.MethodPost, srv.URL+"/api/mediahooks/jellyfin/test", nil)
	if code != http.StatusOK {
		t.Fatalf("the test call answered %d: %s", code, raw)
	}
	var res mediahook.Result
	if err := json.Unmarshal(raw, &res); err != nil {
		t.Fatalf("the body is not a result: %v (%s)", err, raw)
	}
	if res.OK || res.Code != mediahook.CodeRefused {
		t.Errorf("result = %+v, want a refused connection", res)
	}
	if !res.Test {
		t.Error("the call was not recorded as a test")
	}
	// The raw sentence travels for the log, and the value never does.
	if strings.Contains(string(raw), hookToken) {
		t.Fatalf("the header value is in the test result: %s", raw)
	}
	// And the card can draw it afterwards, which is what makes the button worth
	// pressing at all.
	rows := decodeRows(t, listHooks(t, srv))
	if len(rows) != 1 || string(rows[0].Last) == "null" {
		t.Errorf("the last call was not recorded: %s", rows[0].Last)
	}
}

func TestTestingSomethingThatIsNotStoredIs404(t *testing.T) {
	_, srv := mediaHookServer(t)
	code, raw := postJSON(t, http.MethodPost, srv.URL+"/api/mediahooks/kodi/test", nil)
	if code != http.StatusNotFound {
		t.Fatalf("the test call answered %d: %s", code, raw)
	}
}

// TestTheSecondAddressDoesNotReplaceTheFirst, and the order is the order the
// picker on the Categories page offers: an edited address that jumped to the
// bottom of that menu would read as a different address to whoever was looking
// at it.
func TestTheSecondAddressDoesNotReplaceTheFirst(t *testing.T) {
	_, srv := mediaHookServer(t)
	storeHook(t, srv, jellyfinBody())
	storeHook(t, srv, map[string]any{
		"id": "plex", "url": "https://plex.example.org/library/sections/3/refresh",
		"method": "GET", "headerValue": "",
	})
	edited := jellyfinBody()
	edited["name"] = "Jellyfin im Keller"
	edited["headerValue"] = accounts.Redacted
	storeHook(t, srv, edited)

	rows := decodeRows(t, listHooks(t, srv))
	if len(rows) != 2 {
		t.Fatalf("the listing has %d rows, want two", len(rows))
	}
	if rows[0].ID != "jellyfin" || rows[1].ID != "plex" {
		t.Errorf("the order changed: %s, %s", rows[0].ID, rows[1].ID)
	}
	if rows[0].Name != "Jellyfin im Keller" {
		t.Errorf("the edit did not land: %q", rows[0].Name)
	}
}
