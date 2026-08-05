package netproxy

import (
	"crypto/tls"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/throttle"
)

// TestForwardsPlainHTTP proves the proxy is a working proxy: a client that is
// configured to use it gets the origin's bytes back unchanged.
func TestForwardsPlainHTTP(t *testing.T) {
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Origin", "yes")
		_, _ = w.Write([]byte("hello from the origin"))
	}))
	defer origin.Close()

	px, err := Start(throttle.New())
	if err != nil {
		t.Fatal(err)
	}
	defer px.Close()

	body, hdr := getVia(t, px.Addr(), origin.URL, nil)
	if body != "hello from the origin" {
		t.Errorf("body = %q", body)
	}
	if hdr.Get("X-Origin") != "yes" {
		t.Error("origin headers were not passed through")
	}
}

// TestTunnelsHTTPS covers the CONNECT path, which is how every real hoster
// download reaches us.
func TestTunnelsHTTPS(t *testing.T) {
	origin := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("secure payload"))
	}))
	defer origin.Close()

	px, err := Start(throttle.New())
	if err != nil {
		t.Fatal(err)
	}
	defer px.Close()

	body, _ := getVia(t, px.Addr(), origin.URL, origin.Client().Transport.(*http.Transport).TLSClientConfig)
	if body != "secure payload" {
		t.Errorf("body = %q", body)
	}
}

// TestLimitAppliesThroughProxy is the point of the whole package: bytes that
// travel through the proxy are metered.
func TestLimitAppliesThroughProxy(t *testing.T) {
	payload := make([]byte, 256*1024)
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(payload)
	}))
	defer origin.Close()

	lim := throttle.New()
	lim.Set(128 * 1024)
	px, err := Start(lim)
	if err != nil {
		t.Fatal(err)
	}
	defer px.Close()

	start := time.Now()
	body, _ := getVia(t, px.Addr(), origin.URL, nil)
	took := time.Since(start)
	if len(body) != len(payload) {
		t.Fatalf("got %d bytes, want %d", len(body), len(payload))
	}
	if took < 700*time.Millisecond {
		t.Errorf("256 KiB at 128 KiB/s came through in %v; the limit did not apply", took)
	}
}

// getVia fetches target through the proxy at addr and returns body + headers.
func getVia(t *testing.T, addr, target string, tlsCfg *tls.Config) (string, http.Header) {
	t.Helper()
	pu, err := url.Parse("http://" + addr)
	if err != nil {
		t.Fatal(err)
	}
	c := &http.Client{
		Timeout:   20 * time.Second,
		Transport: &http.Transport{Proxy: http.ProxyURL(pu), TLSClientConfig: tlsCfg},
	}
	resp, err := c.Get(target)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return string(b), resp.Header
}
