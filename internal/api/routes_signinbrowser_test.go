package api

import (
	"context"
	"encoding/json"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/junkerderprovinz/knightloader/internal/apitoken"
)

var signInBrowserRoutes = []struct{ method, path string }{
	{http.MethodGet, "/api/signinbrowser"},
	{http.MethodPost, "/api/signinbrowser/patreon/open"},
	{http.MethodGet, "/api/signinbrowser/stream"},
	{http.MethodPost, "/api/signinbrowser/close"},
}

func TestTheServerSaysWhetherItHasASignInBrowser(t *testing.T) {
	t.Setenv("KL_CHROMIUM", filepath.Join(t.TempDir(), "no-chromium"))
	_, _, _, _, owner := groupServer(t)
	status, body := owner(http.MethodGet, "/api/signinbrowser", "")
	var got struct {
		Available *bool   `json:"available"`
		Site      *string `json:"site"`
	}
	if status != http.StatusOK || json.Unmarshal(body, &got) != nil || got.Available == nil || *got.Available || got.Site == nil {
		t.Errorf("GET /api/signinbrowser = %d %s, want it unavailable and no site open", status, body)
	}
}

func TestOpeningWithoutChromiumPointsToTheExtension(t *testing.T) {
	t.Setenv("KL_CHROMIUM", filepath.Join(t.TempDir(), "no-chromium"))
	_, _, _, _, owner := groupServer(t)
	status, body := owner(http.MethodPost, "/api/signinbrowser/patreon/open", `{"width":800,"height":600,"scale":1}`)
	if status != http.StatusServiceUnavailable || !strings.Contains(string(body), "extension") {
		t.Errorf("opening = %d %s, want 503 that points to the extension", status, body)
	}
	if status, _ := owner(http.MethodPost, "/api/signinbrowser/nowhere/open", `{}`); status != http.StatusNotFound {
		t.Errorf("opening an unknown site = %d, want 404", status)
	}
}

func TestTheGroupsPhonesCannotReachTheSignInBrowser(t *testing.T) {
	_, _, _, member, _ := groupServer(t)
	for _, r := range signInBrowserRoutes {
		if status, _ := member(r.method, r.path, `{}`); status != http.StatusForbidden {
			t.Errorf("a member's %s %s = %d, want 403", r.method, r.path, status)
		}
	}
}

func TestOnlyAnAdminTokenReachesTheSignInBrowser(t *testing.T) {
	for _, r := range signInBrowserRoutes {
		pattern := r.method + " " + strings.Replace(r.path, "/patreon/", "/{id}/", 1)
		if got := scopeFor(pattern); got != apitoken.ScopeAdmin {
			t.Errorf("%s needs %q, want admin", pattern, got)
		}
		if _, named := routeScopes[pattern]; !named {
			t.Errorf("%s is missing from the scope table", pattern)
		}
	}
}

func TestTheStreamClosesWhenNoWindowIsOpen(t *testing.T) {
	srv, _ := testServer(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	c, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(srv.URL, "http")+"/api/signinbrowser/stream", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer c.CloseNow()
	_, _, err = c.Read(ctx)
	if websocket.CloseStatus(err) != websocket.StatusNormalClosure || !strings.Contains(err.Error(), "no sign-in window") {
		t.Errorf("the stream ended with %v, want a normal close that says no window is open", err)
	}
}

func TestTheStreamRefusesAnotherSite(t *testing.T) {
	srv, _ := testServer(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_, resp, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(srv.URL, "http")+"/api/signinbrowser/stream",
		&websocket.DialOptions{HTTPHeader: http.Header{"Origin": {"https://evil.example"}}})
	if err == nil || resp == nil || resp.StatusCode != http.StatusForbidden {
		t.Errorf("a stream opened from another site was answered %v, %v; want 403", resp, err)
	}
}
