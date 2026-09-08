package api

// Route-level tests for the header profiles, against a real app on a throwaway
// data directory - the shape routes_ytdlpcookies_test.go uses next door, and
// for the same reason: what is asserted here is what actually crosses the wire
// and what actually lands in the encrypted store, and a stubbed app can answer
// for neither.

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

// planted is the one string every assertion in this file hunts for, the same
// arrangement internal/resolver/hostheaders' own leak_test.go uses: a single
// distinctive needle, so a hit anywhere is unambiguous and a miss is not a
// coincidence.
const planted = "SECRET-h7k2-no-response-may-carry-this"

// The origin is written one way and stored another. OriginOf spells the port
// out on everything it normalises, so asserting against the stored form is what
// catches a route that echoed the request instead of re-reading the store.
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

// plantProfile stores a profile carrying the needle THROUGH THE ROUTE, so every
// assertion below is about what this HTTP surface did rather than about a store
// a test filled in behind its back.
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

// listProfiles is the raw listing bytes, because what several of these tests are
// about is the bytes and not the decoded shape.
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

// listedIDs is the names the listing carries. Decoded rather than matched as a
// substring: "forum" is inside "forum-2", so a test asking whether the forum
// profile is still there would be answered by a different profile entirely.
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

// sealedNames is what a profile actually holds, read the one way that package
// lets a value out at all (Set.Attach). Only a test asks this; no route does.
//
// It answers NAMES and never the values, sorted, so that a failing assertion
// can print what it found without this file becoming the leak it is guarding
// against - a t.Fatalf of a map[string]string prints the sessions in the clear,
// into a CI log.
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

// TestTheProfileListingCarriesNoHeaderValue is the guard this whole surface is
// built around. The listing is what a settings page loads, so a value in it is
// a live session in a browser, in a screenshot, and in whatever bug report that
// screenshot ends up attached to.
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
	// The names still come through, or the page cannot say what the profile
	// holds - and they are what makes the assertion above mean anything: a
	// listing of nothing would satisfy it too.
	if got := list[0].Headers; len(got) != 2 {
		t.Errorf("the listing names %v, want the two header names", got)
	}
	// And the values really are stored. Without this the whole test passes on
	// a save that quietly did nothing at all.
	if got := sealedProfile(t, a, "forum", forumStored)["X-Auth-Token"]; got != planted {
		t.Fatal("the profile did not survive the save, so nothing above proves anything")
	}
}

// TestNoDiagnosticsBundleCarriesAHeaderValue is the other exposure, and it is
// the reason these values are sealed in accounts.Store under a pseudo service
// id rather than kept in settings.json: the bundle serialises the settings and
// this process's own log tail, and it is the file people attach to public bug
// reports. A handler that logged what it was saving, or an error that quoted
// what it refused, would put a session straight back into it.
func TestNoDiagnosticsBundleCarriesAHeaderValue(t *testing.T) {
	a, srv := hostHeaderServer(t)
	plantProfile(t, srv, "forum", forumTyped)

	// The refusals too. They are the paths where a value escapes most easily,
	// because the natural sentence to write names the thing that was wrong.
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

// TestChangingAProfileDoesNotMeanRetypingItsHeaders is what the placeholder is
// for. The listing carries no values, so a form re-sending what it was given
// sends empty ones - and empty means "clear this" in this store as it does in
// every other one here. Without the put-back, adding one header to a profile
// deletes the two that were already in it, and the user finds out at the next
// download.
//
// It also covers the save being keyed by the origin: this body names no id at
// all, and still edits the profile that origin already has.
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

// TestAStoredHeaderDoesNotFollowAProfileToAnotherOrigin is the cross-origin
// rule reaching this file. redirect.go stops a header from crossing an origin
// boundary during a fetch; the same crossing is available here by re-pointing a
// stored profile at another host and sending the placeholder for its values,
// which would file a forum's session under a CDN nobody ever logged in to.
func TestAStoredHeaderDoesNotFollowAProfileToAnotherOrigin(t *testing.T) {
	a, srv := hostHeaderServer(t)
	plantProfile(t, srv, "forum", forumTyped)

	const cdn = "https://cdn.example.net"
	code, raw := postJSON(t, http.MethodPost, srv.URL+"/api/hostheaders", map[string]any{
		"id":      "forum",
		"origin":  cdn,
		"headers": []map[string]string{{"name": "Cookie", "value": hostheaders.Redacted}},
	})
	// What was stored comes first, and it is asserted whatever the status was:
	// a 200 that moved the credential and a 400 that moved it anyway are the
	// same leak, and only one of the two is visible in the status line.
	if names := sealedNames(t, a, "forum", cdn+":443"); len(names) != 0 {
		t.Fatalf("the stored headers followed the profile to another origin: %v", names)
	}
	if code != http.StatusBadRequest {
		t.Errorf("moving a profile with the placeholder answered %d, want %d: %s", code, http.StatusBadRequest, raw)
	}
	// The profile it was moved away from is untouched, rather than half-saved
	// or emptied by the refusal.
	if got := sealedProfile(t, a, "forum", forumStored)["Cookie"]; got != "session="+planted {
		t.Error("the refusal did not leave the original profile alone")
	}
}

// TestASavedProfileIsRoutableWithoutARestart guards which store these routes
// write through.
//
// hostheaders.Store keeps an origin index because Resolver.Match is called
// under the app's lock, and that index is dropped only by a write through the
// same store. Writing through a second store of our own would seal the profile,
// list it, and leave routing ignoring it until internal/app next rewires its
// backends - an account change, or six hours.
func TestASavedProfileIsRoutableWithoutARestart(t *testing.T) {
	a, srv := hostHeaderServer(t)
	link := forumTyped + "/attachments/1/x.rar"

	// The index has to be built COLD, before the save. It is built lazily, so
	// an index first built afterwards is correct whichever store did the
	// writing, and this test would pass without asserting anything. This call
	// is what an ordinary paste before the save does, and it is what leaves a
	// stale index behind.
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

// TestDeletingAProfileRemovesIt, and answers an unknown name rather than
// cheerfully claiming to have removed one. Somebody deleting a profile is
// removing their own logged-in session from this machine; a 204 on a name that
// stored nothing leaves them believing a live session is gone while it is still
// sealed under the name they meant to type.
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

// TestASaveThatWouldQuietlyDestroyAProfileIsRefused collects the four bodies
// that must not be stored. Each one is a shape a page can send by accident, and
// each one would otherwise take a working profile away: a save with no headers
// left in it reads as a delete, a second profile for one origin loses a coin
// toss it is never told about, and a name the store cannot address is a profile
// no rule can ever name.
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

	// The profile all of that was aimed at is still there, values and all.
	if got := listedIDs(t, srv); !slices.Contains(got, "forum") {
		t.Fatalf("a refused save took the stored profile with it: %v", got)
	}
	if got := sealedProfile(t, a, "forum", forumStored)["Cookie"]; got != "session="+planted {
		t.Error("a refused save emptied the stored profile")
	}
}

// TestHeaderProfileRoutesNeedASession. The listing names the sites somebody has
// logins for, and the writes reach the credential store: neither may answer
// without a session on a password-protected instance.
func TestHeaderProfileRoutesNeedASession(t *testing.T) {
	reg := newRegistry()
	registerHostHeaders(reg, testApp(t))
	for _, r := range reg.Routes() {
		if r.Open {
			t.Errorf("%s %s answers without a session", r.Method, r.Path)
		}
	}
}
