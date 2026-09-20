package api

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/junkerderprovinz/knightloader/internal/app"
)

// testApp is an app on a throwaway data directory, closed with the test.
func testApp(t *testing.T) *app.App {
	t.Helper()
	a, err := app.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { a.Close() })
	return a
}

// TestNothingRegistersOutsideTheTable keeps every route in the table the index
// is generated from. It reads the package source because a ServeMux does not
// report what was registered on it.
func TestNothingRegistersOutsideTheTable(t *testing.T) {
	// Split so that this test file does not match itself.
	forbidden := []string{"mux." + "HandleFunc", "mux." + "Handle("}

	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		if name == "routes.go" {
			continue // the one file allowed to touch a mux
		}
		b, err := os.ReadFile(filepath.Join(".", name))
		if err != nil {
			t.Fatal(err)
		}
		for _, f := range forbidden {
			if strings.Contains(string(b), f) {
				t.Errorf("%s calls %s directly; register the route through the table in routes.go, "+
					"or the API index will never know it exists", name, f)
			}
		}
	}
}

// TestEveryRouteDescribesItself checks that every route has a summary for the
// help page and lives under /api/ unless it has a listed reason not to.
func TestEveryRouteDescribesItself(t *testing.T) {
	// The relay client dials the relay address plus "/relay/connect"
	// (relay.connectURL), so an instance serving a relay has to answer there.
	outsideAPI := map[string]string{
		"GET /relay/connect": "the relay socket has to sit where relay clients dial, which is the " +
			"address itself plus /relay/connect, not under /api/",
	}
	reg := buildRegistry(t)
	for _, r := range reg.Routes() {
		if strings.TrimSpace(r.Summary) == "" {
			t.Errorf("%s %s has no summary", r.Method, r.Path)
		}
		if !strings.HasPrefix(r.Path, "/api/") {
			if _, ok := outsideAPI[r.Method+" "+r.Path]; !ok {
				t.Errorf("%s %s is not under /api/, where the session guard only protects what is", r.Method, r.Path)
			}
		}
	}
}

// TestOnlyTheseRoutesAreOpen pins the routes that answer without a session, so
// opening another one takes an edit here that states the reason.
func TestOnlyTheseRoutesAreOpen(t *testing.T) {
	want := map[string]string{
		"GET /api/health":       "a container orchestrator has to be able to probe a locked instance",
		"GET /api/auth":         "the login screen asks this before anybody can log in",
		"POST /api/auth/login":  "this is the way in",
		"POST /api/auth/logout": "logging out of a session the server no longer honours must still work",
		"GET /api/auth/passkeys": "the sign-in screen has to know whether to offer the passkey button, and " +
			"whether this address can carry one at all, before anybody is signed in. It answers with counts " +
			"and a verdict; the registered keys themselves are listed only to a caller that already has a " +
			"session, so an open route is not an open list",
		"POST /api/auth/passkey/login/begin": "one half of signing in with a passkey, which by definition " +
			"happens without a session. It refuses outright unless a password is set and a credential is " +
			"registered for this exact address, and it shares the password login's throttle",
		"POST /api/auth/passkey/login/finish": "the other half, and the one that issues the cookie. The " +
			"credential is the signature the authenticator made over this instance's own challenge, which " +
			"is a stronger thing to hold than the password would have been",
		"GET /api/containers/relay/{token}": "the fetch comes from the JD backend on another host, " +
			"with no session; the unguessable single-use token in the path is the credential",
		"GET /api/sabnzbd/api": "Sonarr and Radarr send their credential as ?apikey=, which the session " +
			"guard knows nothing about, so a guarded route would answer 401 to every call. The route " +
			"checks that key against this instance's own API tokens itself, on every request and on " +
			"every instance including one with no password, and answers 404 while the downloadclient " +
			"module is switched off - so an open route is not an open door",
		"POST /api/sabnzbd/api": "the same door for mode=addfile, which is the one call the two apps " +
			"make as a POST; same credential, same switch, same reasoning as the GET above",
		"GET /relay/connect": "the relay socket, when this instance is serving one; every instance " +
			"dialling in is a different machine with no session here, and the relay key in the first " +
			"frame is the only credential there is. relay.Server.Admit lets in exactly the key this " +
			"instance stores, and the route answers 404 while the switch is off, so an open route is " +
			"not an open relay",
	}
	got := map[string]bool{}
	for _, r := range buildRegistry(t).Routes() {
		if r.Open {
			got[r.Method+" "+r.Path] = true
		}
	}
	for key := range got {
		if _, ok := want[key]; !ok {
			t.Errorf("%s answers without a session and nothing here says why it may", key)
		}
	}
	for key := range want {
		if !got[key] {
			t.Errorf("%s is no longer open; the login flow or the container handover is broken", key)
		}
	}
}

// TestSessionGuardCoversWildcardRoutes checks the matching the open list is
// looked up with. The relay route is the only open route with a wildcard in it,
// and a prefix test that was too generous would open everything below it.
func TestSessionGuardCoversWildcardRoutes(t *testing.T) {
	reg := buildRegistry(t)
	cases := []struct {
		path string
		open bool
	}{
		{"/api/containers/relay/abc123", true},
		{"/api/containers", false},
		{"/api/tasks", false},
		{"/api/settings", false},
		{"/", true},
		{"/assets/app.js", true},
	}
	for _, c := range cases {
		if got := reg.open(c.path); got != c.open {
			t.Errorf("open(%q) = %v, want %v", c.path, got, c.open)
		}
	}
}

// buildRegistry assembles the table through the same registerAll that Handler
// uses, so a subsystem cannot be tested but unserved.
func buildRegistry(t *testing.T) *Registry {
	t.Helper()
	reg := newRegistry()
	registerAll(reg, testApp(t))
	return reg
}

// TestEverySubsystemIsRegistered catches a routes_*.go file whose register
// function registerAll never calls: its own tests would pass while the server
// never attached the routes.
func TestEverySubsystemIsRegistered(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	src, err := os.ReadFile("routes.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		name := e.Name()
		if !strings.HasPrefix(name, "routes_") || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		b, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		for _, line := range strings.Split(string(b), "\n") {
			fn, ok := strings.CutPrefix(line, "func register")
			if !ok {
				continue
			}
			fn, _, _ = strings.Cut(fn, "(")
			if !strings.Contains(string(src), "\tregister"+fn+"(reg, a)\n") {
				t.Errorf("%s defines register%s but registerAll never calls it; "+
					"the routes exist in its test and nowhere in the running server", name, fn)
			}
		}
	}
}

// TestAnUnknownApiPathIs404 checks that an unclaimed /api/ path answers 404
// instead of falling through to the single-page app's 200.
func TestAnUnknownApiPathIs404(t *testing.T) {
	reg := buildRegistry(t)
	mux := http.NewServeMux()
	// A fallback that answers 200, as the real one does for the app.
	reg.attach(mux, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte("<!doctype html>"))
	}))

	cases := []struct {
		method, path string
		want         int
		why          string
	}{
		{http.MethodPost, "/api/rules/test", http.StatusNotFound, "a removed route must not answer as the app"},
		{http.MethodGet, "/api/nonsense", http.StatusNotFound, "a route that never existed"},
		{http.MethodGet, "/api/", http.StatusNotFound, "the prefix itself is not an endpoint"},
		{http.MethodGet, "/api/health", http.StatusOK, "a registered route still answers"},
		{http.MethodGet, "/", http.StatusOK, "the interface is still served"},
		{http.MethodGet, "/assets/app.js", http.StatusOK, "so are its assets"},
	}
	for _, c := range cases {
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest(c.method, c.path, nil))
		if rec.Code != c.want {
			t.Errorf("%s %s answered %d, want %d (%s)", c.method, c.path, rec.Code, c.want, c.why)
		}
	}
}
