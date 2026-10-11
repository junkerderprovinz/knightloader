package api

import (
	"encoding/json"
	"net/http"
	"net/netip"
	"strings"
	"testing"

	"github.com/junkerderprovinz/knightloader/internal/app"
)

func TestTheThrottleKeyIsTheCallerBehindATrustedProxy(t *testing.T) {
	t.Parallel()
	proxy := []netip.Prefix{netip.MustParsePrefix("10.0.0.0/8")}
	cases := []struct {
		name    string
		peer    string
		trusted []netip.Prefix
		xff     []string
		want    string
	}{
		{"a direct caller's header is not read", "198.51.100.7:4000", proxy, []string{"203.0.113.5"}, "198.51.100.7"},
		{"without a trusted proxy the peer counts", "10.0.0.2:4000", nil, []string{"203.0.113.5"}, "10.0.0.2"},
		{"behind a trusted proxy the caller counts", "10.0.0.2:4000", proxy, []string{"203.0.113.5"}, "203.0.113.5"},
		{"what the caller wrote to the left is passed over", "10.0.0.2:4000", proxy, []string{"198.51.100.1, 203.0.113.5, 10.0.0.3"}, "203.0.113.5"},
		{"several headers read as one list", "10.0.0.2:4000", proxy, []string{"198.51.100.1", "203.0.113.5"}, "203.0.113.5"},
		{"a port the proxy added is dropped", "10.0.0.2:4000", proxy, []string{"203.0.113.5:61234"}, "203.0.113.5"},
		{"a trusted proxy without the header counts itself", "10.0.0.2:4000", proxy, nil, "10.0.0.2"},
		{"proxies all the way stop at the left-most", "10.0.0.2:4000", proxy, []string{"10.0.0.9, 10.0.0.3"}, "10.0.0.9"},
		{"a mapped address matches its IPv4 range", "[::ffff:10.0.0.2]:4000", proxy, []string{"203.0.113.5"}, "203.0.113.5"},
		{"an IPv6 caller", "10.0.0.2:4000", proxy, []string{"2001:db8::7"}, "2001:db8::7"},
	}
	for _, c := range cases {
		r, _ := http.NewRequest(http.MethodPost, "http://kl.lan/api/auth/login", nil)
		r.RemoteAddr = c.peer
		for _, v := range c.xff {
			r.Header.Add("X-Forwarded-For", v)
		}
		if got := loginClientKey(r, c.trusted); got != c.want {
			t.Errorf("%s: the key is %q, want %q", c.name, got, c.want)
		}
	}
}

func trustLoopback(t *testing.T, a *app.App) {
	t.Helper()
	if _, err := a.PatchSettings(map[string]json.RawMessage{"trustedProxies": json.RawMessage(`["127.0.0.1","::1"]`)}); err != nil {
		t.Fatal(err)
	}
}

func loginFrom(t *testing.T, base, password, from string) int {
	t.Helper()
	body, _ := json.Marshal(map[string]string{"password": password})
	req, _ := http.NewRequest(http.MethodPost, base+"/api/auth/login", strings.NewReader(string(body)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Forwarded-For", from)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	return resp.StatusCode
}

func TestBehindATrustedProxyOneGuesserMakesOnlyThemselvesWait(t *testing.T) {
	t.Parallel()
	for _, trusted := range []bool{false, true} {
		srv, a := testServer(t)
		if err := a.Auth.SetPassword("", "a-good-password"); err != nil {
			t.Fatal(err)
		}
		if trusted {
			trustLoopback(t, a)
		}
		var last int
		for range loginFailLimit + 1 {
			last = loginFrom(t, srv.URL, "not-the-password", "203.0.113.5")
		}
		if last != http.StatusTooManyRequests {
			t.Fatalf("trusted %v: after %d wrong passwords the login answered %d, want 429", trusted, loginFailLimit+1, last)
		}
		got := loginFrom(t, srv.URL, "a-good-password", "203.0.113.6")
		if trusted && got != http.StatusOK {
			t.Errorf("behind a trusted proxy, somebody else answered %d while one caller was throttled, want 200", got)
		}
		if !trusted && got != http.StatusTooManyRequests {
			t.Errorf("with no trusted proxy, a header naming another caller answered %d, want the shared 429", got)
		}
		srv.Close()
	}
}

func TestWrongResetCodesBehindATrustedProxyCountPerCaller(t *testing.T) {
	srv, a, _ := resetServer(t)
	trustLoopback(t, a)
	finish := func(from string) int {
		req, _ := http.NewRequest(http.MethodPost, srv.URL+"/api/auth/reset/finish",
			strings.NewReader(`{"code":"2222-2222-2222","password":"another-good-password"}`))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Forwarded-For", from)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		return resp.StatusCode
	}
	var last int
	for range loginFailLimit + 1 {
		last = finish("203.0.113.5")
	}
	if last != http.StatusTooManyRequests {
		t.Fatalf("after %d wrong codes the route answered %d, want 429", loginFailLimit+1, last)
	}
	if got := finish("203.0.113.6"); got != http.StatusUnauthorized {
		t.Errorf("another caller's wrong code answered %d, want 401 rather than the other caller's 429", got)
	}
}
