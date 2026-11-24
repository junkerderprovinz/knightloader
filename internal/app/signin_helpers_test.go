package app

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// kitCall sends a request to the kit's contract on a, as a browser on the
// same site sends it.
func kitCall(a *App, method, path, cookie string, body any) *httptest.ResponseRecorder {
	b, _ := json.Marshal(body)
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

// setPassword sets the owner's first password and returns the session as a
// Cookie header.
func setPassword(t *testing.T, a *App, pw string) string {
	t.Helper()
	w := kitCall(a, http.MethodPut, "me/password", "", map[string]string{"next": pw})
	if w.Code != http.StatusOK {
		t.Fatalf("setting the first password = %d %s", w.Code, w.Body)
	}
	for _, c := range w.Result().Cookies() {
		if strings.HasSuffix(c.Name, "session") {
			return c.Name + "=" + c.Value
		}
	}
	t.Fatal("no session cookie")
	return ""
}

// signsIn reports whether pw signs the owner in.
func signsIn(a *App, pw string) bool {
	return kitCall(a, http.MethodPost, "login", "", map[string]string{"password": pw}).Code == http.StatusOK
}

// kitToken makes an API token of the kit for the owner signed in with session.
func kitToken(t *testing.T, a *App, session string) string {
	t.Helper()
	w := kitCall(a, http.MethodPost, "me/tokens", session, map[string]any{"name": "ci", "scopes": []string{"admin"}})
	var out struct {
		Token string `json:"value"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil || out.Token == "" {
		t.Fatalf("making a token = %d %s", w.Code, w.Body)
	}
	return out.Token
}

func tokenCounts(a *App, token string) bool {
	_, ok := a.SignIn.TokenVisitor(context.Background(), token)
	return ok
}
