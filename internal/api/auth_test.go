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

// TestAPITokenGrantsAccess is the whole point of internal/apitoken: a client
// that has never seen the session cookie, and does not send one, still gets
// in with a Bearer token, and a bad one is refused exactly like a bad
// password would be.
func TestAPITokenGrantsAccess(t *testing.T) {
	srv, a := testServer(t)
	defer srv.Close()
	if err := a.Auth.SetPassword("", "a-good-password"); err != nil {
		t.Fatal(err)
	}
	_, secret, err := a.APITokens.Create("test script")
	if err != nil {
		t.Fatal(err)
	}

	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/api/tasks", nil)
	req.Header.Set("Authorization", "Bearer "+secret)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("a request with no cookie and a valid Bearer token answered %d, want 200", resp.StatusCode)
	}

	req2, _ := http.NewRequest(http.MethodGet, srv.URL+"/api/tasks", nil)
	req2.Header.Set("Authorization", "Bearer kl_not-a-real-token")
	resp2, err := http.DefaultClient.Do(req2)
	if err != nil {
		t.Fatal(err)
	}
	resp2.Body.Close()
	if resp2.StatusCode != http.StatusUnauthorized {
		t.Errorf("a made-up Bearer token answered %d, want 401", resp2.StatusCode)
	}
}

// TestRevokedTokenStopsAuthenticating is named tokens' reason to exist: one
// device's credential can be pulled without touching the shared password or
// any other token.
func TestRevokedTokenStopsAuthenticating(t *testing.T) {
	srv, a := testServer(t)
	defer srv.Close()
	if err := a.Auth.SetPassword("", "a-good-password"); err != nil {
		t.Fatal(err)
	}
	tok, secret, err := a.APITokens.Create("lost phone")
	if err != nil {
		t.Fatal(err)
	}
	if err := a.APITokens.Revoke(tok.ID); err != nil {
		t.Fatal(err)
	}

	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/api/tasks", nil)
	req.Header.Set("Authorization", "Bearer "+secret)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("a revoked token answered %d, want 401", resp.StatusCode)
	}
	// And the password it never touched still works.
	client := &http.Client{Jar: newJar(t)}
	if code := login(t, client, srv.URL, "a-good-password"); code != http.StatusOK {
		t.Error("revoking a token broke the shared password")
	}
}

// TestTokenMintedBeforePasswordIsRevokedWhenPasswordIsSet closes the standing
// bypass apitoken.Store.RevokeAll exists for (see its own doc comment): a
// token minted while the instance had no password protecting it must not go
// on working once one is set. Reproduced live before this fix: exactly this
// token kept authenticating after the very password change meant to lock the
// instance down.
func TestTokenMintedBeforePasswordIsRevokedWhenPasswordIsSet(t *testing.T) {
	srv, a := testServer(t)
	defer srv.Close()

	_, secret, err := a.APITokens.Create("phone")
	if err != nil {
		t.Fatal(err)
	}
	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/api/tasks", nil)
	req.Header.Set("Authorization", "Bearer "+secret)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("token minted before any password answered %d, want 200", resp.StatusCode)
	}

	// Through the real route, not a.Auth.SetPassword directly: the eviction
	// is wired into the HTTP handler (routes_system.go), not into Auth itself.
	body, _ := json.Marshal(map[string]string{"current": "", "new": "a-good-password"})
	putReq, _ := http.NewRequest(http.MethodPut, srv.URL+"/api/auth/password", bytes.NewReader(body))
	putReq.Header.Set("Content-Type", "application/json")
	putResp, err := http.DefaultClient.Do(putReq)
	if err != nil {
		t.Fatal(err)
	}
	putResp.Body.Close()
	if putResp.StatusCode != http.StatusOK {
		t.Fatalf("setting the first password answered %d, want 200", putResp.StatusCode)
	}

	req2, _ := http.NewRequest(http.MethodGet, srv.URL+"/api/tasks", nil)
	req2.Header.Set("Authorization", "Bearer "+secret)
	resp2, err := http.DefaultClient.Do(req2)
	if err != nil {
		t.Fatal(err)
	}
	resp2.Body.Close()
	if resp2.StatusCode != http.StatusUnauthorized {
		t.Errorf("the pre-password token still authenticates after the password was set: %d, want 401", resp2.StatusCode)
	}

	// A token minted after the change is unaffected by that same call.
	_, secret2, err := a.APITokens.Create("new phone")
	if err != nil {
		t.Fatal(err)
	}
	req3, _ := http.NewRequest(http.MethodGet, srv.URL+"/api/tasks", nil)
	req3.Header.Set("Authorization", "Bearer "+secret2)
	resp3, err := http.DefaultClient.Do(req3)
	if err != nil {
		t.Fatal(err)
	}
	resp3.Body.Close()
	if resp3.StatusCode != http.StatusOK {
		t.Errorf("a token minted after the password change answered %d, want 200", resp3.StatusCode)
	}
}

// TestAPITokenNotNeededWithoutAPassword: a fresh, unprotected install answers
// every route already, so a token is neither required nor checked.
func TestAPITokenNotNeededWithoutAPassword(t *testing.T) {
	srv, _ := testServer(t)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/tasks")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("an unprotected instance answered %d, want 200", resp.StatusCode)
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

// TestAssetsRevalidate is the reason a redeploy can leave a browser on an old
// UI. The bundle names carry no content hash and the embedded files have no
// modification time, so without an ETag the browser has no way to tell that
// app.js changed and is free to keep serving the old one from cache.
func TestAssetsRevalidate(t *testing.T) {
	srv, _ := testServer(t)
	defer srv.Close()

	for _, path := range []string{"/", "/assets/app.js"} {
		t.Run(path, func(t *testing.T) {
			resp, err := http.Get(srv.URL + path)
			if err != nil {
				t.Fatal(err)
			}
			resp.Body.Close()
			if resp.StatusCode != http.StatusOK {
				t.Fatalf("status = %d", resp.StatusCode)
			}
			tag := resp.Header.Get("ETag")
			if tag == "" {
				t.Fatal("no ETag, so a stale bundle can survive a redeploy unnoticed")
			}
			if cc := resp.Header.Get("Cache-Control"); cc != "no-cache" {
				t.Errorf("Cache-Control = %q, want no-cache so the browser revalidates", cc)
			}

			// The same bytes must answer 304, or revalidation costs a full
			// download every time and the fix trades one problem for another.
			req, _ := http.NewRequest(http.MethodGet, srv.URL+path, nil)
			req.Header.Set("If-None-Match", tag)
			resp2, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			resp2.Body.Close()
			if resp2.StatusCode != http.StatusNotModified {
				t.Errorf("revalidating with the same ETag = %d, want 304", resp2.StatusCode)
			}
		})
	}
}

// TestManifestServesTheRealContentType is spaHandler's own mime.AddExtensionType
// call: Go's mime package has no built-in mapping for .webmanifest, so
// without it http.FileServer falls through to content sniffing, which reads
// a manifest's leading "{" as plain text - the PWA install prompt and
// several browsers' own manifest parsers expect application/manifest+json,
// not text/plain.
func TestManifestServesTheRealContentType(t *testing.T) {
	srv, _ := testServer(t)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/manifest.webmanifest")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /manifest.webmanifest = %d, want 200", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "application/manifest+json") {
		t.Errorf("Content-Type = %q, want application/manifest+json", ct)
	}
}
