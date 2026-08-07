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

// TestNothingRegistersOutsideTheTable is the gate the whole registration table
// exists for. The self-describing index is generated from that table, so a route
// attached to the mux by hand is a route the index will never mention and that
// nothing can notice — and after eleven waves of that, the index is a lie and
// auditing it means reading every file in the package.
//
// It reads the package's own source rather than inspecting the mux, because a
// ServeMux does not hand back what was registered on it, and because the failure
// this prevents is somebody writing the line, not the line misbehaving.
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

// TestEveryRouteDescribesItself keeps the table worth generating an index from.
// A route with no summary renders as a blank line in the help page, which is
// worse than an absent one: it looks like the page is broken rather than like
// somebody forgot a string.
func TestEveryRouteDescribesItself(t *testing.T) {
	reg := buildRegistry(t)
	for _, r := range reg.Routes() {
		if strings.TrimSpace(r.Summary) == "" {
			t.Errorf("%s %s has no summary", r.Method, r.Path)
		}
		if !strings.HasPrefix(r.Path, "/api/") {
			t.Errorf("%s %s is not under /api/, where the session guard only protects what is", r.Method, r.Path)
		}
	}
}

// TestOnlyTheseRoutesAreOpen pins the list of doors that are not locked. Every
// wave adds routes; one of them will eventually mark something open because it
// was convenient during development, and nothing else in the app would ever
// mention it again. Adding a route here has to be a deliberate edit to a test
// that says so.
func TestOnlyTheseRoutesAreOpen(t *testing.T) {
	want := map[string]string{
		"GET /api/health":       "a container orchestrator has to be able to probe a locked instance",
		"GET /api/auth":         "the login screen asks this before anybody can log in",
		"POST /api/auth/login":  "this is the way in",
		"POST /api/auth/logout": "logging out of a session the server no longer honours must still work",
		"GET /api/containers/relay/{token}": "the fetch comes from the JD backend on another host, " +
			"with no session; the unguessable single-use token in the path is the credential",
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

// buildRegistry assembles the table the way Handler does, through the same
// registerAll — not a copy of its call list. A copy is how a subsystem ends up
// tested but unserved.
func buildRegistry(t *testing.T) *Registry {
	t.Helper()
	reg := newRegistry()
	registerAll(reg, testApp(t))
	return reg
}

// TestEverySubsystemIsRegistered is the guard for the failure that got past the
// two-copies arrangement: a routes_*.go file whose register function nothing
// calls. The subsystem's own test file registers it by hand and passes, the
// server never attaches it, and the page that calls it receives the SPA's HTML
// with a 200 — so the client fails on parsing, not on the status, and the error
// never names the route.
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

// TestAnUnknownApiPathIs404 closes the one hole the registration table did not
// cover. Everything the table does not claim used to fall through to the
// single-page app, which answers 200 with index.html — so a call to a route that
// had been renamed, removed or never registered came back successful, failed
// while the client parsed HTML as JSON, and produced an error naming neither the
// route nor the status.
func TestAnUnknownApiPathIs404(t *testing.T) {
	reg := buildRegistry(t)
	mux := http.NewServeMux()
	// A fallback that answers 200, exactly as the real one does for the app.
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
