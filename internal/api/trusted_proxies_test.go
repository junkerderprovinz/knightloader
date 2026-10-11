package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/netip"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/junkerderprovinz/knightloader/internal/app"
	"github.com/junkerderprovinz/knightloader/internal/relay"
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

// TestTheServedRelayBelievesOnlyTheTrustedProxies: the relay an instance
// serves counts failed handshakes per caller, and a caller on the same
// network is not a proxy unless the list says so.
func TestTheServedRelayBelievesOnlyTheTrustedProxies(t *testing.T) {
	t.Parallel()
	for _, trusted := range []bool{false, true} {
		srv, a := testServer(t)
		if code, put := putRelayConfig(t, srv.URL, `{"relayUrl":"","key":"a-key-long-enough-to-pass","serve":true}`); code != http.StatusOK || !put.Serve {
			t.Fatalf("PUT /api/relay/config = %d %+v", code, put)
		}
		if trusted {
			trustLoopback(t, a)
		}
		url := "ws" + strings.TrimPrefix(srv.URL, "http") + "/relay/connect"
		dial := func(from string) (*websocket.Conn, int) {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			c, resp, err := websocket.Dial(ctx, url, &websocket.DialOptions{HTTPHeader: http.Header{"X-Forwarded-For": {from}}})
			if err != nil {
				if resp == nil {
					t.Fatalf("dial: %v", err)
				}
				return nil, resp.StatusCode
			}
			return c, http.StatusSwitchingProtocols
		}
		blocked := false
		for range 30 {
			c, code := dial("203.0.113.66")
			if code == http.StatusTooManyRequests {
				blocked = true
				break
			}
			hello, _ := relay.Encode(relay.TypeHello, relay.Hello{Key: "not-this-relays-key"})
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			_ = c.Write(ctx, websocket.MessageText, hello)
			_, _, _ = c.Read(ctx)
			cancel()
			_ = c.CloseNow()
		}
		if !blocked {
			t.Fatal("the failing caller was never blocked, so this test proves nothing")
		}
		c, code := dial("198.51.100.7")
		if c != nil {
			_ = c.CloseNow()
		}
		if trusted && code == http.StatusTooManyRequests {
			t.Error("behind a trusted proxy, another caller was refused with the failing caller's 429")
		}
		if !trusted && code != http.StatusTooManyRequests {
			t.Errorf("with no trusted proxy, a header naming another caller answered %d, want the shared 429", code)
		}
		srv.Close()
	}
}
