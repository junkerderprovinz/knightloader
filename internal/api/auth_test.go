package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/junkerderprovinz/knightloader/internal/app"
	"github.com/junkerderprovinz/knightloader/internal/auth"
)

// TestUnprotectedByDefault keeps a fresh install working exactly as before: no
// password, no login, nothing in the way.
func TestUnprotectedByDefault(t *testing.T) {
	srv, _ := testServer(t)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/tasks")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("GET /api/tasks without a password = %d, want 200", resp.StatusCode)
	}
}

// TestPasswordLocksTheApi is the whole point: once a password is set, an
// unauthenticated caller is turned away, and a correct login gets back in.
func TestPasswordLocksTheApi(t *testing.T) {
	srv, a := testServer(t)
	defer srv.Close()

	if err := a.Auth.SetPassword("", "a-good-password"); err != nil {
		t.Fatal(err)
	}

	resp, err := http.Get(srv.URL + "/api/tasks")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("locked instance answered %d, want 401", resp.StatusCode)
	}

	// A wrong password must not hand out a session.
	client := &http.Client{Jar: newJar(t)}
	if code := login(t, client, srv.URL, "not-the-password"); code != http.StatusUnauthorized {
		t.Fatalf("wrong password answered %d, want 401", code)
	}
	resp, err = client.Get(srv.URL + "/api/tasks")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("after a failed login the API answered %d, want 401", resp.StatusCode)
	}

	// The right one does.
	if code := login(t, client, srv.URL, "a-good-password"); code != http.StatusOK {
		t.Fatalf("correct password answered %d, want 200", code)
	}
	resp, err = client.Get(srv.URL + "/api/tasks")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("after logging in the API answered %d, want 200", resp.StatusCode)
	}

	// Logging out closes it again.
	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/api/auth/logout", nil)
	if _, err := client.Do(req); err != nil {
		t.Fatal(err)
	}
	resp, err = client.Get(srv.URL + "/api/tasks")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("after logout the API answered %d, want 401", resp.StatusCode)
	}
}

// TestForgedSessionRejected checks the cookie is actually signed and not just
// present.
func TestForgedSessionRejected(t *testing.T) {
	srv, a := testServer(t)
	defer srv.Close()
	if err := a.Auth.SetPassword("", "a-good-password"); err != nil {
		t.Fatal(err)
	}

	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/api/tasks", nil)
	req.AddCookie(&http.Cookie{Name: auth.CookieName, Value: "99999999999.deadbeef"})
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("a forged cookie answered %d, want 401", resp.StatusCode)
	}
}

// TestCrossOriginRefused stops another website from driving this instance
// through the visitor's browser.
func TestCrossOriginRefused(t *testing.T) {
	srv, _ := testServer(t)
	defer srv.Close()

	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/api/links",
		strings.NewReader(`{"links":"https://example.com/x.bin"}`))
	req.Header.Set("Origin", "https://evil.example")
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("cross-origin POST answered %d, want 403", resp.StatusCode)
	}

	// The UI's own origin still works.
	req, _ = http.NewRequest(http.MethodGet, srv.URL+"/api/tasks", nil)
	req.Header.Set("Origin", srv.URL)
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("same-origin GET answered %d, want 200", resp.StatusCode)
	}
}

// TestChangePasswordNeedsCurrent stops a stolen session from locking the owner
// out of their own instance.
func TestChangePasswordNeedsCurrent(t *testing.T) {
	srv, a := testServer(t)
	defer srv.Close()
	if err := a.Auth.SetPassword("", "a-good-password"); err != nil {
		t.Fatal(err)
	}
	client := &http.Client{Jar: newJar(t)}
	if code := login(t, client, srv.URL, "a-good-password"); code != http.StatusOK {
		t.Fatal("login failed")
	}

	body, _ := json.Marshal(map[string]string{"current": "wrong", "new": "another-password"})
	req, _ := http.NewRequest(http.MethodPut, srv.URL+"/api/auth/password", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("changing the password without the current one answered %d, want 400", resp.StatusCode)
	}
	if !a.Auth.Check("a-good-password") {
		t.Error("the original password stopped working")
	}
}

func testServer(t *testing.T) (*httptest.Server, *app.App) {
	t.Helper()
	a, err := app.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { a.Close() })
	srv := httptest.NewServer(Handler(a))
	return srv, a
}

func login(t *testing.T, c *http.Client, base, password string) int {
	t.Helper()
	body, _ := json.Marshal(map[string]string{"password": password})
	resp, err := c.Post(base+"/api/auth/login", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	return resp.StatusCode
}

// newJar gives a test client somewhere to keep the session cookie.
func newJar(t *testing.T) http.CookieJar {
	t.Helper()
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	return jar
}
