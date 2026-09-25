package api

// Route-level tests for the cookie jars against a real app, since what matters
// is what crosses the wire and what lands in the encrypted store.

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

// theJar carries a distinctive session value the leak checks look for.
const (
	cookieSecret = "SESSION-b7f1c2d4-never-leaves-the-store"
	theJar       = "# Netscape HTTP Cookie File\n.youtube.com\tTRUE\t/\tTRUE\t0\tSID\t" + cookieSecret + "\n"
)

// cookieServer attaches this file's routes and nothing else.
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

// postCookieJSON sends a body to one of the write routes and returns the
// status and the raw response bytes.
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

// getCookieHosts returns the listing both raw and decoded.
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

// TestCookieListNeverCarriesTheJar searches the raw bytes of all three answers
// for the session, since a leak added later would arrive in a field this test
// does not know.
func TestCookieListNeverCarriesTheJar(t *testing.T) {
	t.Parallel()
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
	// An empty answer would pass the check above too.
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

// TestCookieJarIsStoredUnderTheHostALookupUses checks that whatever form of
// the host is pasted, the jar lands under the lower-cased, "www."-stripped key
// CookieStore.Text looks up.
func TestCookieJarIsStoredUnderTheHostALookupUses(t *testing.T) {
	t.Parallel()
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
			t.Fatalf("%q was filed as %v, want [youtube.com], the key every lookup builds", typed, hosts)
		}
		// The call the yt-dlp backend makes on every spawn.
		if got := ytdlp.NewCookieStore(a.Accounts).Text("https://www.youtube.com/watch?v=1"); got != theJar {
			t.Fatalf("after storing for %q the backend's own lookup found %q, want the jar", typed, got)
		}
		// Removed under the same typed string, so the remove route's
		// normalisation is exercised too.
		if code, body := postCookieJSON(t, srv, "/api/ytdlp/cookies/remove", map[string]any{"host": typed}); code != http.StatusOK {
			t.Fatalf("removing the jar stored for %q answered %d (%s)", typed, code, body)
		}
	}
}

// TestCookieHostThatIsNotAHostIsRefused covers an address pasted without its
// scheme, which accounts.Store would otherwise seal under a key nothing
// matches.
func TestCookieHostThatIsNotAHostIsRefused(t *testing.T) {
	t.Parallel()
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

// TestSavingWithNoTextKeepsTheStoredJar checks that a save without the text
// field is refused rather than read as a clear, since the page can never send
// a stored jar back.
func TestSavingWithNoTextKeepsTheStoredJar(t *testing.T) {
	t.Parallel()
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

// TestEmptyTextClearsTheJar checks that an empty text clears the jar, as in
// CookieStore.Set, and that the listing agrees at once.
func TestEmptyTextClearsTheJar(t *testing.T) {
	t.Parallel()
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

// TestRemovingAHostWithNoJarIs404 checks that a typo cannot look like a
// removed session.
func TestRemovingAHostWithNoJarIs404(t *testing.T) {
	t.Parallel()
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

// TestAnEmptyCookieListIsAnEmptyArray checks that a fresh instance answers []
// rather than null.
func TestAnEmptyCookieListIsAnEmptyArray(t *testing.T) {
	t.Parallel()
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
