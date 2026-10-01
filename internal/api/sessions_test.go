package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/url"
	"testing"

	"github.com/junkerderprovinz/knightloader/internal/auth"
)

// sessionStatus asks for the task list with only cookie, the way a copy of it
// taken from another browser would.
func sessionStatus(t *testing.T, base, cookie string) int {
	t.Helper()
	req, _ := http.NewRequest(http.MethodGet, base+"/api/tasks", nil)
	req.AddCookie(&http.Cookie{Name: auth.CookieName, Value: cookie})
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	return resp.StatusCode
}

// signedIn returns a client holding a fresh session and the cookie it holds.
func signedIn(t *testing.T, base, password string) (*http.Client, string) {
	t.Helper()
	c := &http.Client{Jar: newJar(t)}
	if code := login(t, c, base, password); code != http.StatusOK {
		t.Fatalf("login answered %d", code)
	}
	u, _ := url.Parse(base)
	for _, ck := range c.Jar.Cookies(u) {
		if ck.Name == auth.CookieName {
			return c, ck.Value
		}
	}
	t.Fatal("the login set no session cookie")
	return nil, ""
}

func TestChangingThePasswordSignsOutEveryOtherBrowser(t *testing.T) {
	t.Parallel()
	srv, a := testServer(t)
	defer srv.Close()
	if err := a.Auth.SetPassword("", "a-good-password"); err != nil {
		t.Fatal(err)
	}
	owner, _ := signedIn(t, srv.URL, "a-good-password")
	_, stolen := signedIn(t, srv.URL, "a-good-password")

	body, _ := json.Marshal(map[string]string{"current": "a-good-password", "new": "another-good-password"})
	req, _ := http.NewRequest(http.MethodPut, srv.URL+"/api/auth/password", bytes.NewReader(body))
	resp, err := owner.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("changing the password answered %d", resp.StatusCode)
	}

	if code := sessionStatus(t, srv.URL, stolen); code != http.StatusUnauthorized {
		t.Errorf("a session from before the change answered %d, want 401", code)
	}
	resp, err = owner.Get(srv.URL + "/api/tasks")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("the browser that changed the password answered %d afterwards, want 200", resp.StatusCode)
	}
}

func TestLoggingOutEndsACopyOfTheCookieToo(t *testing.T) {
	t.Parallel()
	srv, a := testServer(t)
	defer srv.Close()
	if err := a.Auth.SetPassword("", "a-good-password"); err != nil {
		t.Fatal(err)
	}
	c, cookie := signedIn(t, srv.URL, "a-good-password")
	_, other := signedIn(t, srv.URL, "a-good-password")

	resp, err := c.Post(srv.URL+"/api/auth/logout", "application/json", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()

	if code := sessionStatus(t, srv.URL, cookie); code != http.StatusUnauthorized {
		t.Errorf("a copy of the logged-out cookie answered %d, want 401", code)
	}
	if code := sessionStatus(t, srv.URL, other); code != http.StatusOK {
		t.Errorf("another browser's session answered %d after this one logged out, want 200", code)
	}
}

func TestLoggingOutEverywhereNeedsASessionAndEndsThemAll(t *testing.T) {
	t.Parallel()
	srv, a := testServer(t)
	defer srv.Close()
	if err := a.Auth.SetPassword("", "a-good-password"); err != nil {
		t.Fatal(err)
	}
	c, cookie := signedIn(t, srv.URL, "a-good-password")
	_, other := signedIn(t, srv.URL, "a-good-password")

	everywhere := func(client *http.Client) int {
		resp, err := client.Post(srv.URL+"/api/auth/logout", "application/json", bytes.NewReader([]byte(`{"everywhere":true}`)))
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		return resp.StatusCode
	}
	if code := everywhere(http.DefaultClient); code != http.StatusUnauthorized {
		t.Fatalf("an anonymous sign-out everywhere answered %d, want 401", code)
	}
	if code := sessionStatus(t, srv.URL, other); code != http.StatusOK {
		t.Fatalf("an anonymous caller signed everybody out: a session answered %d", code)
	}
	if code := everywhere(c); code != http.StatusNoContent {
		t.Fatalf("signing out everywhere answered %d, want 204", code)
	}
	for _, s := range []string{cookie, other} {
		if code := sessionStatus(t, srv.URL, s); code != http.StatusUnauthorized {
			t.Errorf("a session answered %d after signing out everywhere, want 401", code)
		}
	}
}
