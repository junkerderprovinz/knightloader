package api

import (
	"net/http"
	"strings"
	"testing"
)

// requestOn sends method path to the test server with Host set to host and,
// when origin is set, the Origin a page on that name would send.
func requestOn(t *testing.T, url, method, path, host, origin string, header map[string]string) int {
	t.Helper()
	req, err := http.NewRequest(method, url+path, strings.NewReader("{}"))
	if err != nil {
		t.Fatal(err)
	}
	req.Host = host
	if origin != "" {
		req.Header.Set("Origin", origin)
	}
	for k, v := range header {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	return resp.StatusCode
}

// TestAnInstanceWithoutAPasswordAnswersOnEveryName: a reverse proxy or a
// dynamic DNS name nobody listed yet reaches the whole app.
func TestAnInstanceWithoutAPasswordAnswersOnEveryName(t *testing.T) {
	t.Parallel()
	srv, _ := testServer(t)
	defer srv.Close()

	for _, host := range []string{
		"192.168.1.20:8749",
		"tower:8749",
		"kl.dyndns.example",
		"kl.example.com:443",
	} {
		if code := requestOn(t, srv.URL, http.MethodGet, "/api/tasks", host, "", nil); code != http.StatusOK {
			t.Errorf("GET /api/tasks on %s answered %d, want 200", host, code)
		}
		if code := requestOn(t, srv.URL, http.MethodGet, "/", host, "", nil); code != http.StatusOK {
			t.Errorf("GET / on %s answered %d, want 200", host, code)
		}
	}
}

// TestTheRelayKnowsTheInstancesOwnNames: a relay hello proves an id only to
// the relay it names, so the relay takes the names nobody outside the local
// network can point here, and the domains the owner listed.
func TestTheRelayKnowsTheInstancesOwnNames(t *testing.T) {
	t.Parallel()
	srv, a := testServer(t)
	defer srv.Close()
	cfg := a.Settings.Get()
	cfg.KnownDomains = []string{"https://kl.example.com/kl", "plain.example.org"}
	cfg.RelayURL = "wss://relay.example.net"
	if _, err := a.Settings.Set(cfg); err != nil {
		t.Fatal(err)
	}

	for _, host := range []string{
		"",
		"192.168.1.20:8749",
		"[::1]:8749",
		"localhost:8749",
		// The desktop window on Windows and on Linux.
		"wails.localhost",
		"wails",
		// A machine name, and Sonarr calling the container by its name.
		"tower:8749",
		"knightloader:8749",
		"tower.local:8749",
		"nas.lan",
		"nas.home.arpa",
		"host.docker.internal:8749",
		"KL.Example.COM",
		"kl.example.com.",
		"plain.example.org",
		"relay.example.net",
	} {
		if !ownHost(a, host) {
			t.Errorf("ownHost(%q) = false, want true", host)
		}
	}
	for _, host := range []string{"evil.example:8749", "kl.dyndns.example"} {
		if ownHost(a, host) {
			t.Errorf("ownHost(%q) = true, want false", host)
		}
	}
}

// TestAPasswordLetsEveryNameThrough: with a password the session cookie is
// what keeps a rebound page out, and a reverse proxy on a name nobody listed
// yet still reaches the login.
func TestAPasswordLetsEveryNameThrough(t *testing.T) {
	t.Parallel()
	srv, a := testServer(t)
	defer srv.Close()
	if err := a.Auth.SetPassword("", "a-good-password"); err != nil {
		t.Fatal(err)
	}
	if code := requestOn(t, srv.URL, http.MethodGet, "/api/auth", "kl.example.com", "", nil); code != http.StatusOK {
		t.Errorf("GET /api/auth on an unlisted domain answered %d, want 200", code)
	}
	if code := requestOn(t, srv.URL, http.MethodGet, "/api/tasks", "kl.example.com", "", nil); code != http.StatusUnauthorized {
		t.Errorf("GET /api/tasks on an unlisted domain without a session answered %d, want 401", code)
	}
}

// TestATokenPassesOnAnyName: a phone or a script calls the address it was
// given, and a made-up token gets no further than no token at all.
func TestATokenPassesOnAnyName(t *testing.T) {
	t.Parallel()
	srv, a := testServer(t)
	defer srv.Close()
	if err := a.Auth.SetPassword("", "a-good-password"); err != nil {
		t.Fatal(err)
	}
	_, secret, err := a.APITokens.Create("phone")
	if err != nil {
		t.Fatal(err)
	}
	auth := map[string]string{"Authorization": "Bearer " + secret}
	if code := requestOn(t, srv.URL, http.MethodGet, "/api/tasks", "kl.dyndns.example", "", auth); code != http.StatusOK {
		t.Errorf("GET /api/tasks with a token on an unlisted domain answered %d, want 200", code)
	}
	wrong := map[string]string{"Authorization": "Bearer not-a-token"}
	if code := requestOn(t, srv.URL, http.MethodGet, "/api/tasks", "kl.dyndns.example", "", wrong); code != http.StatusUnauthorized {
		t.Errorf("GET /api/tasks with a made-up token on an unlisted domain answered %d, want 401", code)
	}
}
