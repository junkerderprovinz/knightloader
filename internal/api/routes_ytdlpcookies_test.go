package api

// Route-level tests for the cookie jars, against a real app on a throwaway
// data directory - the shape routes_downloadclient_test.go uses, and for the
// same reason: what is being asserted here is what actually crosses the wire
// and what actually lands in the encrypted store, neither of which a stubbed
// app could answer for.

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/junkerderprovinz/knightloader/internal/app"
	"github.com/junkerderprovinz/knightloader/internal/resolver/ytdlp"
)

// theJar carries a string that appears nowhere else, so a test can look for it
// in a response body and know that finding it means the response carried the
// session and not a coincidence.
const (
	cookieSecret = "SESSION-b7f1c2d4-never-leaves-the-store"
	theJar       = "# Netscape HTTP Cookie File\n.youtube.com\tTRUE\t/\tTRUE\t0\tSID\t" + cookieSecret + "\n"
)

// cookieServer attaches this file's routes and nothing else. Not testServer,
// which builds the whole table through registerAll: the registration line in
// routes.go is added separately, and TestEverySubsystemIsRegistered is already
// the guard for that line being there. This helper is about the handlers.
func cookieServer(t *testing.T) (*httptest.Server, *app.App) {
	t.Helper()
	a := testApp(t)
	reg := newRegistry()
	registerYtdlpCookies(reg, a)
	mux := http.NewServeMux()
	reg.attach(mux, http.NotFoundHandler())
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv, a
}

// postCookieJSON sends any body to one of the two write routes and hands back
// the status with the RAW response bytes, because what several of these tests
// are about is the bytes rather than the decoded shape.
func postCookieJSON(t *testing.T, srv *httptest.Server, path string, body any) (int, []byte) {
	t.Helper()
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.Post(srv.URL+path, "application/json", bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	out, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return resp.StatusCode, out
}

// getCookieHosts is the listing, raw and decoded: raw because the guard below
// is about what the bytes contain, decoded because every other test wants the
// names.
func getCookieHosts(t *testing.T, srv *httptest.Server) (int, []byte, []string) {
	t.Helper()
	resp, err := http.Get(srv.URL + "/api/ytdlp/cookies")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	var hosts []string
	if resp.StatusCode == http.StatusOK {
		if err := json.Unmarshal(raw, &hosts); err != nil {
			t.Fatalf("the listing is not a JSON array of names: %v (%s)", err, raw)
		}
	}
	return resp.StatusCode, raw, hosts
}

// TestCookieListNeverCarriesTheJar is the guard this file exists for.
//
// A cookies.txt is a live session, and the listing is what a settings page
// renders on every visit - so the moment any of these three routes answers
// with the text, the session is in a browser tab's memory, in whatever proxy
// log sits between, and one screenshot away from a public bug report. The
// store itself refuses to make this easy (CookieStore.Text is deliberately not
// wired to any route), and this asserts that the HTTP layer did not undo that
// on the way out.
//
// It reads the RAW bytes of all three answers rather than a decoded field: a
// leak added later would arrive in a field nothing here knows the name of, and
// a test that only checked the fields it already knows about would report
// nothing at all.
func TestCookieListNeverCarriesTheJar(t *testing.T) {
	srv, _ := cookieServer(t)

	code, saved := postCookieJSON(t, srv, "/api/ytdlp/cookies", map[string]any{
		"host": "https://www.youtube.com/watch?v=x",
		"text": theJar,
	})
	if code != http.StatusOK {
		t.Fatalf("storing a jar answered %d, want 200 (%s)", code, saved)
	}
	if bytes.Contains(saved, []byte(cookieSecret)) {
		t.Errorf("the answer to storing a jar handed the session straight back:\n%s", saved)
	}

	code, raw, hosts := getCookieHosts(t, srv)
	if code != http.StatusOK {
		t.Fatalf("GET /api/ytdlp/cookies answered %d (%s)", code, raw)
	}
	if bytes.Contains(raw, []byte(cookieSecret)) {
		t.Errorf("the listing shipped the stored session to the client:\n%s", raw)
	}
	// The other half of the same assertion: a route that answered nothing at
	// all would pass the check above while telling the page nothing, so the
	// name has to be there for the absence of the text to mean anything.
	if len(hosts) != 1 || hosts[0] != "youtube.com" {
		t.Fatalf("the listing = %v, want the one host that has a jar", hosts)
	}

	code, removed := postCookieJSON(t, srv, "/api/ytdlp/cookies/remove", map[string]any{"host": "youtube.com"})
	if code != http.StatusOK {
		t.Fatalf("removing the jar answered %d, want 200 (%s)", code, removed)
	}
	if bytes.Contains(removed, []byte(cookieSecret)) {
		t.Errorf("the answer to removing a jar carried the session out with it:\n%s", removed)
	}
}

// TestCookieJarIsStoredUnderTheHostALookupUses is the normalisation the whole
// feature turns on. yt-dlp is handed a jar by CookieStore.Text, which walks
// cookieHostChain - lower-cased and "www."-stripped - so a jar filed under
// what a person actually pasted ("https://www.YouTube.com/watch?v=x") is
// stored, listed on the page, and never once read.
func TestCookieJarIsStoredUnderTheHostALookupUses(t *testing.T) {
	srv, a := cookieServer(t)

	for _, typed := range []string{
		"https://www.YouTube.com/watch?v=x",
		"WWW.YouTube.com",
		"  youtube.com  ",
	} {
		if code, body := postCookieJSON(t, srv, "/api/ytdlp/cookies", map[string]any{
			"host": typed,
			"text": theJar,
		}); code != http.StatusOK {
			t.Fatalf("storing a jar for %q answered %d (%s)", typed, code, body)
		}
		_, _, hosts := getCookieHosts(t, srv)
		if len(hosts) != 1 || hosts[0] != "youtube.com" {
			t.Fatalf("%q was filed as %v, want [youtube.com] - the key every lookup builds", typed, hosts)
		}
		// The lookup itself, not just the name: this is the call the yt-dlp
		// backend makes on every spawn.
		if got := ytdlp.NewCookieStore(a.Accounts).Text("https://www.youtube.com/watch?v=1"); got != theJar {
			t.Fatalf("after storing for %q the backend's own lookup found %q, want the jar", typed, got)
		}
		// Removed under the SAME string it was stored under, so that the
		// remove route's own normalisation is exercised too: it looks the host
		// up in the listing before it deletes anything (that is what makes a
		// typo a 404), and a key normalised on the way in but not on the way
		// out would answer "nothing is stored for it" about a jar that is
		// sitting right there.
		if code, body := postCookieJSON(t, srv, "/api/ytdlp/cookies/remove", map[string]any{"host": typed}); code != http.StatusOK {
			t.Fatalf("removing the jar stored for %q answered %d (%s)", typed, code, body)
		}
	}
}

// TestCookieHostThatIsNotAHostIsRefused: accounts.Store seals whatever it is
// given and validates nothing, so a pasted address that lost its scheme would
// be filed as "youtube.com/watch?v=x", listed as though it were a site, and
// matched by nothing. The 400 is the only moment anybody can be told.
func TestCookieHostThatIsNotAHostIsRefused(t *testing.T) {
	srv, _ := cookieServer(t)

	for _, typed := range []string{"", "   ", "youtube.com/watch?v=x", "youtube.com:443"} {
		code, body := postCookieJSON(t, srv, "/api/ytdlp/cookies", map[string]any{"host": typed, "text": theJar})
		if code != http.StatusBadRequest {
			t.Errorf("storing a jar for host %q answered %d, want 400", typed, code)
		}
		if !bytes.Contains(body, []byte("host:")) {
			t.Errorf("the refusal for %q does not name the field that failed: %s", typed, body)
		}
		if _, _, hosts := getCookieHosts(t, srv); len(hosts) != 0 {
			t.Fatalf("a refused save still stored something: %v", hosts)
		}
	}
}

// TestSavingWithNoTextKeepsTheStoredJar is the silent-wipe guard. A stored jar
// is never sent back to the page, so a form cannot round-trip one: a save that
// left the field out - a UI saving the row after an edit to some other column,
// a client written from an older shape - must not be read as "clear it". The
// download that would break afterwards fails as "sign in to confirm you are
// not a bot", which reads as the site changing its mind rather than as this
// endpoint having deleted the answer to it.
func TestSavingWithNoTextKeepsTheStoredJar(t *testing.T) {
	srv, a := cookieServer(t)

	if code, body := postCookieJSON(t, srv, "/api/ytdlp/cookies", map[string]any{
		"host": "youtube.com",
		"text": theJar,
	}); code != http.StatusOK {
		t.Fatalf("storing the jar answered %d (%s)", code, body)
	}

	code, body := postCookieJSON(t, srv, "/api/ytdlp/cookies", map[string]any{"host": "youtube.com"})
	if code != http.StatusBadRequest {
		t.Errorf("a save carrying no text at all answered %d, want 400", code)
	}
	if !bytes.Contains(body, []byte("text:")) {
		t.Errorf("the refusal does not name the field that failed: %s", body)
	}
	if got := ytdlp.NewCookieStore(a.Accounts).Text("https://www.youtube.com/watch?v=1"); got != theJar {
		t.Fatal("a save with no text wiped the stored session; the next download will look like the site blocking it")
	}
}

// TestEmptyTextClearsTheJar is the other half of that decision: an empty text
// is the deliberate clear, which is the contract CookieStore.Set already has,
// and the listing has to agree immediately.
func TestEmptyTextClearsTheJar(t *testing.T) {
	srv, a := cookieServer(t)

	if code, body := postCookieJSON(t, srv, "/api/ytdlp/cookies", map[string]any{
		"host": "youtube.com",
		"text": theJar,
	}); code != http.StatusOK {
		t.Fatalf("storing the jar answered %d (%s)", code, body)
	}
	code, body := postCookieJSON(t, srv, "/api/ytdlp/cookies", map[string]any{"host": "youtube.com", "text": ""})
	if code != http.StatusOK {
		t.Fatalf("clearing the jar answered %d, want 200 (%s)", code, body)
	}
	var hosts []string
	if err := json.Unmarshal(body, &hosts); err != nil {
		t.Fatalf("the answer to a clear is not a JSON array of names: %v (%s)", err, body)
	}
	if len(hosts) != 0 {
		t.Errorf("the site is still listed after its jar was cleared: %v", hosts)
	}
	if got := ytdlp.NewCookieStore(a.Accounts).Text("https://www.youtube.com/watch?v=1"); got != "" {
		t.Error("the backend's own lookup still finds a jar that was cleared")
	}
}

// TestRemovingAHostWithNoJarIs404 stops somebody believing a typo took their
// logged-in session off this machine while it is still sealed under the name
// they meant to type - the same answer DELETE /api/tokens/{id} gives an
// unknown id, for a sharper version of the same reason.
func TestRemovingAHostWithNoJarIs404(t *testing.T) {
	srv, _ := cookieServer(t)

	if code, body := postCookieJSON(t, srv, "/api/ytdlp/cookies", map[string]any{
		"host": "youtube.com",
		"text": theJar,
	}); code != http.StatusOK {
		t.Fatalf("storing the jar answered %d (%s)", code, body)
	}
	code, body := postCookieJSON(t, srv, "/api/ytdlp/cookies/remove", map[string]any{"host": "youtub.com"})
	if code != http.StatusNotFound {
		t.Errorf("removing a jar that was never stored answered %d, want 404", code)
	}
	if !bytes.Contains(body, []byte("youtub.com")) {
		t.Errorf("the refusal does not name the host it could not find: %s", body)
	}
	if _, _, hosts := getCookieHosts(t, srv); len(hosts) != 1 {
		t.Errorf("the real jar did not survive a failed remove: %v", hosts)
	}
}

// TestAnEmptyCookieListIsAnEmptyArray: encoding/json writes a nil slice as
// null, CookieStore.Hosts answers nil until something is stored, and a page
// mapping over the answer would throw on every fresh instance - the state in
// which the settings page is most likely to be opened.
func TestAnEmptyCookieListIsAnEmptyArray(t *testing.T) {
	srv, _ := cookieServer(t)

	code, raw, hosts := getCookieHosts(t, srv)
	if code != http.StatusOK {
		t.Fatalf("GET /api/ytdlp/cookies on a fresh instance answered %d (%s)", code, raw)
	}
	if got := string(bytes.TrimSpace(raw)); got != "[]" {
		t.Errorf("a fresh instance answers %s, want []", got)
	}
	if len(hosts) != 0 {
		t.Errorf("a fresh instance already lists %v", hosts)
	}
}
