package api

// Route-level tests for the header profiles against a real app, since what
// matters is what crosses the wire and what lands in the encrypted store.

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/junkerderprovinz/knightloader/internal/app"
	"github.com/junkerderprovinz/knightloader/internal/resolver/hostheaders"
)

// planted is the distinctive value every leak assertion in this file looks
// for.
const planted = "SECRET-h7k2-no-response-may-carry-this"

// OriginOf spells out the port, so asserting on the stored form catches a
// route that echoed the request instead of re-reading the store.
const (
	forumTyped  = "https://forum.example.org"
	forumStored = "https://forum.example.org:443"
)

func hostHeaderServer(t *testing.T) (*app.App, *httptest.Server) {
	t.Helper()
	a := testApp(t)
	reg := newRegistry()
	registerHostHeaders(reg, a)
	mux := http.NewServeMux()
	reg.attach(mux, http.NotFoundHandler())
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return a, srv
}

// plantProfile stores a profile carrying planted through the route itself.
func plantProfile(t *testing.T, srv *httptest.Server, id, origin string) {
	t.Helper()
	code, raw := postJSON(t, http.MethodPost, srv.URL+"/api/hostheaders", map[string]any{
		"id":     id,
		"origin": origin,
		"headers": []map[string]string{
			{"name": "Cookie", "value": "session=" + planted},
			{"name": "X-Auth-Token", "value": planted},
		},
	})
	if code != http.StatusOK {
		t.Fatalf("POST /api/hostheaders answered %d: %s", code, raw)
	}
}

// listProfiles returns the raw listing bytes.
func listProfiles(t *testing.T, srv *httptest.Server) []byte {
	t.Helper()
	resp, err := srv.Client().Get(srv.URL + "/api/hostheaders")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /api/hostheaders answered %d", resp.StatusCode)
	}
	out, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// listedIDs is the names the listing carries, decoded because "forum" is a
// substring of "forum-2".
func listedIDs(t *testing.T, srv *httptest.Server) []string {
	t.Helper()
	var list []hostheaders.Listing
	if err := json.Unmarshal(listProfiles(t, srv), &list); err != nil {
		t.Fatal(err)
	}
	out := make([]string, 0, len(list))
	for _, p := range list {
		out = append(out, p.ID)
	}
	return out
}

// sealedNames is the sorted header names a profile holds, read through
// Set.Attach. Names only, so a failing assertion prints no session into a CI
// log.
func sealedNames(t *testing.T, a *app.App, id, origin string) []string {
	t.Helper()
	out := make([]string, 0, 4)
	for name := range sealedProfile(t, a, id, origin) {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

func sealedProfile(t *testing.T, a *app.App, id, origin string) map[string]string {
	t.Helper()
	return hostheaders.NewStore(a.Accounts).Get(id).Attach(origin)
}

// TestTheProfileListingCarriesNoHeaderValue checks that the listing a settings
// page loads carries no header value.
func TestTheProfileListingCarriesNoHeaderValue(t *testing.T) {
	a, srv := hostHeaderServer(t)
	plantProfile(t, srv, "forum", forumTyped)

	body := listProfiles(t, srv)
	if strings.Contains(string(body), planted) {
		t.Fatalf("the listing carries a header value: %s", body)
	}

	var list []hostheaders.Listing
	if err := json.Unmarshal(body, &list); err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0].ID != "forum" || list[0].Origin != forumStored {
		t.Fatalf("the listing is %v, want one entry for the forum profile on %s", list, forumStored)
	}
	// The names still come through; an empty listing would pass the check
	// above too.
	if got := list[0].Headers; len(got) != 2 {
		t.Errorf("the listing names %v, want the two header names", got)
	}
	// The values really are stored, or the test would pass on a save that did
	// nothing.
	if got := sealedProfile(t, a, "forum", forumStored)["X-Auth-Token"]; got != planted {
		t.Fatal("the profile did not survive the save, so nothing above proves anything")
	}
}

// TestNoDiagnosticsBundleCarriesAHeaderValue checks that neither the settings
// nor the log tail in the bundle picks up a value, including from refusals,
// whose natural wording would quote what was wrong.
func TestNoDiagnosticsBundleCarriesAHeaderValue(t *testing.T) {
	a, srv := hostHeaderServer(t)
	plantProfile(t, srv, "forum", forumTyped)

	refusals := []map[string]any{
		{"id": "forum", "origin": "ftp://forum.example.org/x?token=" + planted,
			"headers": []map[string]string{{"name": "Cookie", "value": planted}}},
		{"id": "forum", "origin": forumTyped,
			"headers": []map[string]string{{"name": "Cookie", "value": planted + strings.Repeat("x", hostheaders.MaxValueLen)}}},
		{"id": "forum", "origin": forumTyped,
			"headers": []map[string]string{{"name": "Cookie", "value": planted + "\r\nX-Smuggled: 1"}}},
	}
	for _, body := range refusals {
		code, raw := postJSON(t, http.MethodPost, srv.URL+"/api/hostheaders", body)
		if code != http.StatusBadRequest {
			t.Fatalf("a refusable save answered %d, want %d", code, http.StatusBadRequest)
		}
		if strings.Contains(string(raw), planted) {
			t.Errorf("a refusal quotes what it refused: %s", raw)
		}
	}

	bundle, err := json.Marshal(buildDiagnostics(a))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(bundle), planted) {
		t.Fatal("the diagnostics bundle carries a header value")
	}
}

// TestChangingAProfileDoesNotMeanRetypingItsHeaders checks that sending the
// placeholder keeps the stored values, and that a save without an id edits the
// profile its origin already has.
func TestChangingAProfileDoesNotMeanRetypingItsHeaders(t *testing.T) {
	a, srv := hostHeaderServer(t)
	plantProfile(t, srv, "forum", forumTyped)

	code, raw := postJSON(t, http.MethodPost, srv.URL+"/api/hostheaders", map[string]any{
		"origin": forumTyped,
		"headers": []map[string]string{
			{"name": "cookie", "value": hostheaders.Redacted},
			{"name": "X-Auth-Token", "value": hostheaders.Redacted},
			{"name": "Referer", "value": forumTyped + "/threads/1"},
		},
	})
	if code != http.StatusOK {
		t.Fatalf("editing the profile answered %d: %s", code, raw)
	}

	stored := sealedProfile(t, a, "forum", forumStored)
	if stored["Cookie"] != "session="+planted {
		t.Error("the stored Cookie did not survive an edit that sent the placeholder for it")
	}
	if stored["X-Auth-Token"] != planted {
		t.Error("the stored X-Auth-Token did not survive an edit that sent the placeholder for it")
	}
	if stored["Referer"] != forumTyped+"/threads/1" {
		t.Error("the header the edit actually added is not stored")
	}
}

// TestAStoredHeaderDoesNotFollowAProfileToAnotherOrigin checks that re-pointing
// a profile at another host with the placeholder cannot file a forum's session
// under that host.
func TestAStoredHeaderDoesNotFollowAProfileToAnotherOrigin(t *testing.T) {
	a, srv := hostHeaderServer(t)
	plantProfile(t, srv, "forum", forumTyped)

	const cdn = "https://cdn.example.net"
	code, raw := postJSON(t, http.MethodPost, srv.URL+"/api/hostheaders", map[string]any{
		"id":      "forum",
		"origin":  cdn,
		"headers": []map[string]string{{"name": "Cookie", "value": hostheaders.Redacted}},
	})
	// The store is checked first, whatever the status, since a 400 that moved
	// the credential anyway would be the same leak.
	if names := sealedNames(t, a, "forum", cdn+":443"); len(names) != 0 {
		t.Fatalf("the stored headers followed the profile to another origin: %v", names)
	}
	if code != http.StatusBadRequest {
		t.Errorf("moving a profile with the placeholder answered %d, want %d: %s", code, http.StatusBadRequest, raw)
	}
	if got := sealedProfile(t, a, "forum", forumStored)["Cookie"]; got != "session="+planted {
		t.Error("the refusal did not leave the original profile alone")
	}
}

// TestASavedProfileIsRoutableWithoutARestart checks that the routes write
// through the live resolver's store, whose origin index only its own writes
// reset.
func TestASavedProfileIsRoutableWithoutARestart(t *testing.T) {
	a, srv := hostHeaderServer(t)
	link := forumTyped + "/attachments/1/x.rar"

	// Builds the lazy index before the save, as an ordinary paste would;
	// otherwise the test would pass whichever store did the writing.
	if claimsLink(a, link) {
		t.Fatal("the resolver already claims the link, so the assertion below proves nothing")
	}

	plantProfile(t, srv, "forum", forumTyped)

	if !claimsLink(a, link) {
		t.Fatal("the saved profile does not claim its own origin: routing will ignore it until the " +
			"next rewire, so the headers are stored and the download still gets a 403")
	}
}

// claimsLink reports whether the registered header-profile resolver would take
// this link, which is the question Registry.All asks on every paste.
func claimsLink(a *app.App, link string) bool {
	for _, res := range a.Registry.All(link) {
		if res.Info().ID == hostheaders.ResolverID {
			return true
		}
	}
	return false
}

// TestDeletingAProfileRemovesIt also checks that an unknown name is a 404, so
// a typo cannot look like a removed session.
func TestDeletingAProfileRemovesIt(t *testing.T) {
	a, srv := hostHeaderServer(t)
	plantProfile(t, srv, "forum", forumTyped)

	code, raw := postJSON(t, http.MethodDelete, srv.URL+"/api/hostheaders/forum", nil)
	if code != http.StatusNoContent {
		t.Fatalf("DELETE answered %d, want %d: %s", code, http.StatusNoContent, raw)
	}
	if names := sealedNames(t, a, "forum", forumStored); len(names) != 0 {
		t.Errorf("the profile is still stored after a 204: %v", names)
	}
	if got := listedIDs(t, srv); slices.Contains(got, "forum") {
		t.Errorf("the deleted profile is still listed: %v", got)
	}

	code, _ = postJSON(t, http.MethodDelete, srv.URL+"/api/hostheaders/forum", nil)
	if code != http.StatusNotFound {
		t.Errorf("deleting a profile that is not there answered %d, want %d", code, http.StatusNotFound)
	}
}

// TestASaveThatWouldQuietlyDestroyAProfileIsRefused covers bodies a page can
// send by accident that would otherwise lose a working profile.
func TestASaveThatWouldQuietlyDestroyAProfileIsRefused(t *testing.T) {
	a, srv := hostHeaderServer(t)
	plantProfile(t, srv, "forum", forumTyped)

	cases := []struct {
		what string
		body map[string]any
		want int
	}{
		{"a form that lost its rows", map[string]any{
			"id": "forum", "origin": forumTyped, "headers": []map[string]string{},
		}, http.StatusBadRequest},
		{"every header blanked", map[string]any{
			"id": "forum", "origin": forumTyped, "headers": []map[string]string{{"name": "Cookie", "value": ""}},
		}, http.StatusBadRequest},
		{"a second profile for one origin", map[string]any{
			"id": "forum-2", "origin": forumTyped, "headers": []map[string]string{{"name": "Cookie", "value": "x"}},
		}, http.StatusConflict},
		{"a name nothing can address", map[string]any{
			"id": "forum/2", "origin": "https://other.example.org", "headers": []map[string]string{{"name": "Cookie", "value": "x"}},
		}, http.StatusBadRequest},
		{"a new origin with no name", map[string]any{
			"origin": "https://other.example.org", "headers": []map[string]string{{"name": "Cookie", "value": "x"}},
		}, http.StatusBadRequest},
	}
	for _, c := range cases {
		code, raw := postJSON(t, http.MethodPost, srv.URL+"/api/hostheaders", c.body)
		if code != c.want {
			t.Errorf("%s answered %d, want %d: %s", c.what, code, c.want, raw)
		}
	}

	if got := listedIDs(t, srv); !slices.Contains(got, "forum") {
		t.Fatalf("a refused save took the stored profile with it: %v", got)
	}
	if got := sealedProfile(t, a, "forum", forumStored)["Cookie"]; got != "session="+planted {
		t.Error("a refused save emptied the stored profile")
	}
}

func TestHeaderProfileRoutesNeedASession(t *testing.T) {
	reg := newRegistry()
	registerHostHeaders(reg, testApp(t))
	for _, r := range reg.Routes() {
		if r.Open {
			t.Errorf("%s %s answers without a session", r.Method, r.Path)
		}
	}
}
