package httpx

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

const secret = "Bearer router-password"

// echoAuth answers with whatever Authorization header reached it, so a test can
// see what a redirect actually carried rather than what it hoped.
func echoAuth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain")
	_, _ = w.Write([]byte(r.Header.Get("Authorization")))
}

func get(t *testing.T, c *http.Client, raw string) (int, string) {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, raw, nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", secret)
	resp, err := c.Do(req)
	if err != nil {
		t.Fatalf("GET %s: %v", raw, err)
	}
	defer resp.Body.Close()
	buf := make([]byte, 256)
	n, _ := resp.Body.Read(buf)
	return resp.StatusCode, string(buf[:n])
}

// TestRedirectDropsCredentialsCrossHost is the reason this package owns a
// redirect policy at all: a hoster that answers with a redirect to somewhere
// else must not be handed the credential the request carried for the first
// host. Both servers here are on 127.0.0.1, which is precisely the case Go's
// own same-domain rule waves through.
func TestRedirectDropsCredentialsCrossHost(t *testing.T) {
	other := httptest.NewServer(http.HandlerFunc(echoAuth))
	defer other.Close()

	first := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, other.URL+"/landed", http.StatusFound)
	}))
	defer first.Close()

	code, body := get(t, New(Options{}), first.URL+"/start")
	if code != http.StatusOK {
		t.Fatalf("status %d, want 200", code)
	}
	if body != "" {
		t.Errorf("the credential followed the redirect to another host: %q", body)
	}
}

// TestRedirectKeepsCredentialsSameHost pins the other direction. A policy that
// strips everything is not a policy, it is a broken client: a login that
// redirects to its own landing page has to keep working.
func TestRedirectKeepsCredentialsSameHost(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/landed", echoAuth)
	mux.HandleFunc("/start", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/landed", http.StatusFound)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	code, body := get(t, New(Options{}), srv.URL+"/start")
	if code != http.StatusOK {
		t.Fatalf("status %d, want 200", code)
	}
	if body != secret {
		t.Errorf("same-host redirect lost the credential: got %q, want %q", body, secret)
	}
}

// TestSameOriginRules covers the pairs no pair of test servers can produce: a
// scheme downgrade and a port change are both a different origin, and the host
// comparison is case-insensitive because DNS is.
func TestSameOriginRules(t *testing.T) {
	cases := []struct {
		name string
		a, b string
		want bool
	}{
		{"identical", "https://example.com/a", "https://example.com/b", true},
		{"implicit port", "https://example.com/a", "https://example.com:443/b", true},
		{"host case", "https://Example.COM/a", "https://example.com/b", true},
		{"other port", "http://127.0.0.1:9090/a", "http://127.0.0.1:7070/b", false},
		{"downgrade", "https://example.com/a", "http://example.com/b", false},
		{"subdomain", "https://example.com/a", "https://api.example.com/b", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			a, err := url.Parse(c.a)
			if err != nil {
				t.Fatal(err)
			}
			b, err := url.Parse(c.b)
			if err != nil {
				t.Fatal(err)
			}
			if got := sameOrigin(a, b); got != c.want {
				t.Errorf("sameOrigin(%s, %s) = %v, want %v", c.a, c.b, got, c.want)
			}
		})
	}
}

// TestRedirectPolicyStripsOnCrossOrigin exercises the policy directly, because
// the header deletion has to happen on the request that is about to be sent and
// nowhere else.
func TestRedirectPolicyStripsOnCrossOrigin(t *testing.T) {
	policy := checkRedirect(DefaultMaxRedirects)
	first, _ := http.NewRequest(http.MethodGet, "https://example.com/start", nil)

	next, _ := http.NewRequest(http.MethodGet, "https://elsewhere.example/landed", nil)
	next.Header.Set("Authorization", secret)
	next.Header.Set("Cookie", "session=1")
	next.Header.Set("Accept", "text/html")
	if err := policy(next, []*http.Request{first}); err != nil {
		t.Fatal(err)
	}
	if got := next.Header.Get("Authorization"); got != "" {
		t.Errorf("Authorization survived a cross-origin hop: %q", got)
	}
	if got := next.Header.Get("Cookie"); got != "" {
		t.Errorf("Cookie survived a cross-origin hop: %q", got)
	}
	if got := next.Header.Get("Accept"); got != "text/html" {
		t.Errorf("a harmless header was dropped too: Accept = %q", got)
	}
}

// TestRedirectChainIsBounded proves a loop ends with an error rather than with
// a goroutine going round forever.
func TestRedirectChainIsBounded(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/again", http.StatusFound)
	}))
	defer srv.Close()

	_, err := New(Options{MaxRedirects: 3}).Get(srv.URL)
	if err == nil {
		t.Fatal("an endless redirect chain returned no error")
	}
	if !strings.Contains(err.Error(), "stopped after 3 redirects") {
		t.Errorf("error was %v, want the redirect ceiling", err)
	}
}

// TestNoRedirectHandsBackThe3xx is the mode a resolver needs: the redirect
// target is the answer, so following it would throw the answer away.
func TestNoRedirectHandsBackThe3xx(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "https://cdn.example/file.bin", http.StatusFound)
	}))
	defer srv.Close()

	resp, err := New(Options{MaxRedirects: -1}).Get(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("status %d, want 302", resp.StatusCode)
	}
	if got := resp.Header.Get("Location"); got != "https://cdn.example/file.bin" {
		t.Errorf("Location = %q", got)
	}
}

// TestTransportCeilingsAreSet checks the numbers are on the transport and not
// merely in the constants. A ceiling that is declared and never wired is worse
// than none: it reads as covered in review.
func TestTransportCeilingsAreSet(t *testing.T) {
	tr := NewTransport(Options{})
	if tr.TLSHandshakeTimeout != DefaultTLSHandshakeTimeout {
		t.Errorf("TLSHandshakeTimeout = %v", tr.TLSHandshakeTimeout)
	}
	if tr.ResponseHeaderTimeout != DefaultResponseHeaderTimeout {
		t.Errorf("ResponseHeaderTimeout = %v", tr.ResponseHeaderTimeout)
	}
	if tr.ExpectContinueTimeout != expectContinueTimeout {
		t.Errorf("ExpectContinueTimeout = %v", tr.ExpectContinueTimeout)
	}
	if tr.IdleConnTimeout != idleConnTimeout {
		t.Errorf("IdleConnTimeout = %v", tr.IdleConnTimeout)
	}
	if tr.MaxIdleConns != maxIdleConns || tr.MaxIdleConnsPerHost != maxIdleConnsPerHost || tr.MaxConnsPerHost != maxConnsPerHost {
		t.Errorf("pool bounds are %d/%d/%d", tr.MaxIdleConns, tr.MaxIdleConnsPerHost, tr.MaxConnsPerHost)
	}
	if tr.MaxResponseHeaderBytes != maxResponseHeaderBytes {
		t.Errorf("MaxResponseHeaderBytes = %d", tr.MaxResponseHeaderBytes)
	}
	if tr.DialContext == nil {
		t.Error("DialContext is nil, so the dial timeout is whatever the OS says")
	}
	if d := dialer(Options{}.withDefaults()); d.Timeout != DefaultDialTimeout || d.KeepAlive != keepAlive {
		t.Errorf("dialer = %+v", d)
	}
	if tr.Proxy == nil {
		t.Error("Proxy is nil, so HTTP_PROXY would be ignored")
	}
	if u, err := NewTransport(Options{Proxy: NoProxy}).Proxy(&http.Request{}); err != nil || u != nil {
		t.Errorf("NoProxy resolved to %v, %v", u, err)
	}
}

func TestClientTimeoutIsSet(t *testing.T) {
	if got := New(Options{}).Timeout; got != DefaultTimeout {
		t.Errorf("Timeout = %v, want %v", got, DefaultTimeout)
	}
	if got := New(Options{Timeout: NoTimeout}).Timeout; got != 0 {
		t.Errorf("NoTimeout became %v, want 0", got)
	}
	if got := New(Options{Timeout: 5 * time.Second}).Timeout; got != 5*time.Second {
		t.Errorf("Timeout = %v", got)
	}
}

// TestTimeoutsActuallyFire is the behavioural half: a server that accepts the
// connection and then goes quiet must not hold the caller.
func TestTimeoutsActuallyFire(t *testing.T) {
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-release:
		case <-r.Context().Done():
		}
	}))
	defer srv.Close()
	defer close(release)

	t.Run("whole request", func(t *testing.T) {
		start := time.Now()
		_, err := New(Options{Timeout: 150 * time.Millisecond}).Get(srv.URL)
		if err == nil {
			t.Fatal("a silent server returned no error")
		}
		if took := time.Since(start); took > 5*time.Second {
			t.Errorf("gave up after %v, so the request ceiling did not fire", took)
		}
	})

	t.Run("response header", func(t *testing.T) {
		// The whole-request ceiling is off, so only ResponseHeaderTimeout can end
		// this: the case a plain Timeout would otherwise be covering up.
		start := time.Now()
		_, err := New(Options{Timeout: NoTimeout, ResponseHeaderTimeout: 150 * time.Millisecond}).Get(srv.URL)
		if err == nil {
			t.Fatal("a silent server returned no error")
		}
		if took := time.Since(start); took > 5*time.Second {
			t.Errorf("gave up after %v, so the header ceiling did not fire", took)
		}
		var ue *url.Error
		if errors.As(err, &ue) && !ue.Timeout() {
			t.Errorf("error was %v, want a timeout", err)
		}
	})
}

// TestUserAgentIsStamped covers the three cases: nothing set gets ours, a
// caller's own agent is left alone, and an explicitly empty one stays empty
// because that is how net/http spells "send no agent".
func TestUserAgentIsStamped(t *testing.T) {
	seen := make(chan string, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen <- r.Header.Get("User-Agent")
	}))
	defer srv.Close()

	c := New(Options{})
	if _, err := c.Get(srv.URL); err != nil {
		t.Fatal(err)
	}
	if got := <-seen; got != UserAgent() {
		t.Errorf("User-Agent = %q, want %q", got, UserAgent())
	}
	if !strings.HasPrefix(UserAgent(), "KnightLoader/") {
		t.Errorf("the agent does not name the app: %q", UserAgent())
	}

	req, _ := http.NewRequest(http.MethodGet, srv.URL, nil)
	req.Header.Set("User-Agent", "curl/8")
	if _, err := c.Do(req); err != nil {
		t.Fatal(err)
	}
	if got := <-seen; got != "curl/8" {
		t.Errorf("caller's agent became %q", got)
	}
	if got := req.Header.Get("User-Agent"); got != "curl/8" {
		t.Errorf("the caller's own request was modified: %q", got)
	}

	req, _ = http.NewRequest(http.MethodGet, srv.URL, nil)
	req.Header["User-Agent"] = []string{""}
	if _, err := c.Do(req); err != nil {
		t.Fatal(err)
	}
	if got := <-seen; got != "" {
		t.Errorf("an opted-out agent became %q", got)
	}
}
