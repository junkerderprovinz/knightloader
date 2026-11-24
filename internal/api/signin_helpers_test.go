package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/junkerderprovinz/knightloader/internal/apitoken"
	"github.com/junkerderprovinz/knightloader/internal/app"
)

// kitCall sends a request to the kit's contract on a, as a browser on the
// same site sends it, with cookie as its Cookie header.
func kitCall(t *testing.T, a *app.App, method, path, cookie string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var b []byte
	if body != nil {
		b, _ = json.Marshal(body)
	}
	req := httptest.NewRequest(method, "/api/signin/v1/"+path, bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Sec-Fetch-Site", "same-origin")
	if cookie != "" {
		req.Header.Set("Cookie", cookie)
	}
	w := httptest.NewRecorder()
	a.SignIn.Guard(a.SignIn.Handler()).ServeHTTP(w, req)
	return w
}

// sessionOf is the session cookie a kit answer set, as a Cookie header.
func sessionOf(t *testing.T, w *httptest.ResponseRecorder) string {
	t.Helper()
	for _, c := range w.Result().Cookies() {
		if strings.HasSuffix(c.Name, "session") && c.Value != "" {
			return c.Name + "=" + c.Value
		}
	}
	t.Fatalf("the kit set no session cookie: %d %s", w.Code, w.Body)
	return ""
}

// setPassword closes a with pw as the owner's first password, the way the
// Security page sets it, and returns the owner's session as a Cookie header.
func setPassword(t *testing.T, a *app.App, pw string) string {
	t.Helper()
	w := kitCall(t, a, http.MethodPut, "me/password", "", map[string]string{"next": pw})
	if w.Code != http.StatusOK {
		t.Fatalf("setting the first password = %d %s", w.Code, w.Body)
	}
	return sessionOf(t, w)
}

// signIn signs the owner in with pw and returns the session as a Cookie
// header.
func signIn(t *testing.T, a *app.App, pw string) string {
	t.Helper()
	w := kitCall(t, a, http.MethodPost, "login", "", map[string]string{"password": pw})
	if w.Code != http.StatusOK {
		t.Fatalf("signing in = %d %s", w.Code, w.Body)
	}
	return sessionOf(t, w)
}

// kitTokenWith makes one of the kit's API tokens at level for the owner
// signed in with session, and returns its secret.
func kitTokenWith(t *testing.T, a *app.App, session, level string) string {
	t.Helper()
	w := kitCall(t, a, http.MethodPost, "me/tokens", session, map[string]any{"name": level, "scopes": []string{level}})
	if w.Code/100 != 2 {
		t.Fatalf("making a %s token = %d %s", level, w.Code, w.Body)
	}
	var out struct {
		Token string `json:"value"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil || out.Token == "" {
		t.Fatalf("the new token's answer %s has no token: %v", w.Body, err)
	}
	return out.Token
}

// scopedToken makes a token with scopes the way an MCP client gets one,
// which every route takes with exactly those rights, as it took the tokens
// of the versions before.
func scopedToken(a *app.App, name string, scopes []apitoken.Scope) (apitoken.Token, string, error) {
	tok, secret, err := a.APITokens.CreateScoped(name, scopes)
	if err != nil {
		return tok, secret, err
	}
	if _, err := a.MCPClients.Add("other", tok.ID); err != nil {
		return tok, secret, err
	}
	return tok, secret, nil
}

// sessionCookie is the name of the kit's session cookie over plain HTTP,
// which every test server speaks.
const kitSession = "kl_session"

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

// login signs in through the kit's contract on the server at base, the way
// the sign-in page does, and returns the answer's status.
func login(t *testing.T, c *http.Client, base, password string) int {
	t.Helper()
	status, _ := postTo(t, c, base+"/api/signin/v1/login", map[string]string{"password": password})
	return status
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

// postTo posts v as JSON, as a page on the same site does.
func postTo(t *testing.T, c *http.Client, url string, v any) (int, []byte) {
	t.Helper()
	body, _ := json.Marshal(v)
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Sec-Fetch-Site", "same-origin")
	resp, err := c.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := readAll(resp)
	if err != nil {
		t.Fatal(err)
	}
	return resp.StatusCode, raw
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

func cookieOf(t *testing.T, c *http.Client, base string) string {
	t.Helper()
	u, _ := url.Parse(base)
	for _, ck := range c.Jar.Cookies(u) {
		if ck.Name == kitSession {
			return ck.Value
		}
	}
	t.Fatal("the client holds no session cookie")
	return ""
}
