package api

import (
	"net/http"
	"strings"
	"testing"
)

// TestHelpListsEveryRegisteredRoute is the point of generating this index
// from the registration table instead of writing it by hand: it cannot list
// fewer routes than the server actually answers, and it cannot list a route
// that was renamed or removed.
func TestHelpListsEveryRegisteredRoute(t *testing.T) {
	srv, _ := testServer(t)
	defer srv.Close()

	var idx HelpIndex
	if code := getJSON(t, srv.URL+"/api/help", &idx); code != http.StatusOK {
		t.Fatalf("GET /api/help answered %d", code)
	}

	want := buildRegistry(t).Routes()
	if len(idx.Routes) != len(want) {
		t.Fatalf("the index lists %d routes, the table has %d", len(idx.Routes), len(want))
	}
	found := false
	for _, r := range idx.Routes {
		if r.Method == http.MethodGet && r.Path == "/api/help" {
			found = true
		}
		if strings.TrimSpace(r.Summary) == "" {
			t.Errorf("%s %s is in the index with no summary", r.Method, r.Path)
		}
	}
	if !found {
		t.Error("GET /api/help does not list itself")
	}
}

// TestHelpExplainsWhyThereIsNoMyJDShim is the specific sentence section 8's
// Wave 11 amendment asks for by name: a future contributor reading this
// route learns the decision instead of finding silence and half-building a
// shim that would not even work, since MyJDownloader's own clients speak to
// AppWork's relay and cannot be pointed at a plain server instead of it.
func TestHelpExplainsWhyThereIsNoMyJDShim(t *testing.T) {
	srv, _ := testServer(t)
	defer srv.Close()

	var idx HelpIndex
	if code := getJSON(t, srv.URL+"/api/help", &idx); code != http.StatusOK {
		t.Fatalf("GET /api/help answered %d", code)
	}
	for _, want := range []string{"My.JDownloader", "relay"} {
		if !strings.Contains(idx.Vocabulary, want) {
			t.Errorf("Vocabulary does not mention %q: %s", want, idx.Vocabulary)
		}
	}
}

// TestHelpExplainsNoHostedRelay is the same discipline for 11C's own remote
// access page: the absence of a pairing/relay feature reads as a decision on
// this route, not only on a settings page a script never looks at.
func TestHelpExplainsNoHostedRelay(t *testing.T) {
	srv, _ := testServer(t)
	defer srv.Close()

	var idx HelpIndex
	if code := getJSON(t, srv.URL+"/api/help", &idx); code != http.StatusOK {
		t.Fatalf("GET /api/help answered %d", code)
	}
	for _, want := range []string{"no hosted relay", "/api/remote-access", "/api/tokens"} {
		if !strings.Contains(idx.RemoteAccess, want) {
			t.Errorf("RemoteAccess does not mention %q: %s", want, idx.RemoteAccess)
		}
	}
}

// TestHelpNeedsASession: the index names internal route paths and their
// guard requirements, which is exactly the kind of quiet recon an
// unauthenticated LAN visitor on a password-protected instance should not
// get for free. See routes.go's own two justifications for what "open"
// means, neither of which this route qualifies for.
func TestHelpNeedsASession(t *testing.T) {
	srv, a := testServer(t)
	defer srv.Close()
	if err := a.Auth.SetPassword("", "a-good-password"); err != nil {
		t.Fatal(err)
	}

	resp, err := http.Get(srv.URL + "/api/help")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("GET /api/help with no session answered %d, want 401", resp.StatusCode)
	}
}

// TestHelpReportsVersionAndDeployment: a bug report or an integration script
// reading this index should not have to make a second call just to learn
// which build it is talking to.
func TestHelpReportsVersionAndDeployment(t *testing.T) {
	srv, _ := testServer(t)
	defer srv.Close()

	var idx HelpIndex
	if code := getJSON(t, srv.URL+"/api/help", &idx); code != http.StatusOK {
		t.Fatalf("GET /api/help answered %d", code)
	}
	if idx.Version == "" {
		t.Error("no version in the index")
	}
	if idx.Deployment == "" {
		t.Error("no deployment in the index")
	}
}
