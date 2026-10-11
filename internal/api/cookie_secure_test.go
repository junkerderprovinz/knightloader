package api

import (
	"bytes"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/junkerderprovinz/knightloader/internal/auth"
)

// cookieFrom sends req and returns the cookie named name that the answer sets.
func cookieFrom(t *testing.T, req *http.Request, name string) *http.Cookie {
	t.Helper()
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	for _, c := range resp.Cookies() {
		if c.Name == name {
			return c
		}
	}
	t.Fatalf("%s %s set no %s cookie (status %d)", req.Method, req.URL.Path, name, resp.StatusCode)
	return nil
}

func TestTheSessionCookieIsSecureBehindAProxyThatEndsTLS(t *testing.T) {
	t.Parallel()
	srv, a := testServer(t)
	defer srv.Close()
	if err := a.Auth.SetPassword("", "a-good-password"); err != nil {
		t.Fatal(err)
	}
	signIn := func(proto string) *http.Cookie {
		req, _ := http.NewRequest(http.MethodPost, srv.URL+"/api/auth/login", strings.NewReader(`{"password":"a-good-password"}`))
		req.Header.Set("Content-Type", "application/json")
		if proto != "" {
			req.Header.Set("X-Forwarded-Proto", proto)
		}
		return cookieFrom(t, req, auth.CookieName)
	}
	if c := signIn("https"); !c.Secure {
		t.Error("the session cookie for a browser on https is not Secure")
	}
	if c := signIn(""); c.Secure {
		t.Error("the session cookie for a browser on plain http is Secure, so the browser would never send it back")
	}

	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/api/auth/logout", bytes.NewReader(nil))
	req.Header.Set("X-Forwarded-Proto", "https")
	if c := cookieFrom(t, req, auth.CookieName); !c.Secure || c.MaxAge >= 0 {
		t.Errorf("the cookie that clears the session on https is %+v, want it Secure and expired", c)
	}
}

func TestTheQbittorrentCookieIsSecureBehindAProxyThatEndsTLS(t *testing.T) {
	t.Parallel()
	_, srv, secret, _ := qbitServer(t, nil)
	req, _ := http.NewRequest(http.MethodPost, srv.URL+qbitPrefix+"auth/login",
		strings.NewReader(url.Values{"password": {secret}}.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("X-Forwarded-Proto", "https")
	if c := cookieFrom(t, req, qbitCookie); !c.Secure {
		t.Error("the qBittorrent session cookie for a client on https is not Secure")
	}
}
