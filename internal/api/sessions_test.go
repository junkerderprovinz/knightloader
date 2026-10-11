package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/junkerderprovinz/knightloader/internal/auth"
	"github.com/junkerderprovinz/knightloader/internal/store"
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
	return c, cookieOf(t, c, base)
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

// dialStream opens the live stream with cookie and waits for its snapshot.
func dialStream(t *testing.T, base, cookie string) *websocket.Conn {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	c, _, err := websocket.Dial(ctx, base+"/api/ws", &websocket.DialOptions{HTTPHeader: sessionHeader(cookie)})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.CloseNow() })
	readOneOfType(t, c, "snapshot")
	return c
}

// closedByServer reports whether the server closes c within ten seconds.
func closedByServer(c *websocket.Conn) bool {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	for {
		if _, _, err := c.Read(ctx); err != nil {
			return ctx.Err() == nil
		}
	}
}

func cookieOf(t *testing.T, c *http.Client, base string) string {
	t.Helper()
	u, _ := url.Parse(base)
	for _, ck := range c.Jar.Cookies(u) {
		if ck.Name == auth.CookieName {
			return ck.Value
		}
	}
	t.Fatal("the client holds no session cookie")
	return ""
}

func TestChangingThePasswordClosesTheLiveStreamsAndTheOwnerReconnects(t *testing.T) {
	t.Parallel()
	srv, a := testServer(t)
	defer srv.Close()
	if err := a.Auth.SetPassword("", "a-good-password"); err != nil {
		t.Fatal(err)
	}
	owner, before := signedIn(t, srv.URL, "a-good-password")
	_, other := signedIn(t, srv.URL, "a-good-password")
	own, others := dialStream(t, srv.URL, before), dialStream(t, srv.URL, other)

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

	if !closedByServer(others) {
		t.Error("another browser's stream was still open ten seconds after the change")
	}
	if !closedByServer(own) {
		t.Error("the owner's stream from before the change was still open ten seconds after it")
	}
	dialStream(t, srv.URL, cookieOf(t, owner, srv.URL))
}

func TestSigningOutEverywhereClosesTheLiveStreams(t *testing.T) {
	t.Parallel()
	srv, a := testServer(t)
	defer srv.Close()
	if err := a.Auth.SetPassword("", "a-good-password"); err != nil {
		t.Fatal(err)
	}
	c, _ := signedIn(t, srv.URL, "a-good-password")
	_, other := signedIn(t, srv.URL, "a-good-password")
	stream := dialStream(t, srv.URL, other)

	resp, err := c.Post(srv.URL+"/api/auth/logout", "application/json", bytes.NewReader([]byte(`{"everywhere":true}`)))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("signing out everywhere answered %d", resp.StatusCode)
	}
	if !closedByServer(stream) {
		t.Error("a stream was still open ten seconds after signing out everywhere")
	}
}

func TestRemovingThePasswordRemovesEveryPasskey(t *testing.T) {
	t.Parallel()
	srv, a := testServer(t)
	defer srv.Close()
	if err := a.Auth.SetPassword("", "a-good-password"); err != nil {
		t.Fatal(err)
	}
	if _, err := a.Store.AddPasskey(store.Passkey{
		Name: "my phone", RPID: "kl.example.com",
		CredentialID: []byte{1, 2, 3}, PublicKey: []byte{4, 5, 6},
	}); err != nil {
		t.Fatal(err)
	}
	owner, _ := signedIn(t, srv.URL, "a-good-password")

	body, _ := json.Marshal(map[string]string{"current": "a-good-password", "new": ""})
	req, _ := http.NewRequest(http.MethodPut, srv.URL+"/api/auth/password", bytes.NewReader(body))
	resp, err := owner.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK || a.Auth.Enabled() {
		t.Fatalf("removing the password answered %d", resp.StatusCode)
	}

	if left, _ := a.Store.ListPasskeys(); len(left) != 0 {
		t.Errorf("%d passkey(s) outlived the password and would sign in once another one is set", len(left))
	}
}
