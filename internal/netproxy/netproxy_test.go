package netproxy

import (
	"bufio"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
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

// silentServer accepts connections and never sends a byte, like an upstream
// that took a request and went quiet. Each connection is reported once the
// other side has closed it.
func silentServer(t *testing.T) (addr string, closed <-chan struct{}) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	ch := make(chan struct{}, 8)
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				_, _ = io.Copy(io.Discard, c)
				_ = c.Close()
				ch <- struct{}{}
			}()
		}
	}()
	return ln.Addr().String(), ch
}

// connectVia opens a CONNECT tunnel to target through the proxy at addr.
func connectVia(t *testing.T, addr, target string) net.Conn {
	t.Helper()
	c, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })
	if _, err := fmt.Fprintf(c, "CONNECT %s HTTP/1.1\r\nHost: %s\r\n\r\n", target, target); err != nil {
		t.Fatal(err)
	}
	_ = c.SetReadDeadline(time.Now().Add(5 * time.Second))
	br := bufio.NewReader(c)
	resp, err := http.ReadResponse(br, nil)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("CONNECT answered %s", resp.Status)
	}
	return bufferedConn{Conn: c, r: br}
}

// bufferedConn reads through the reader that parsed the CONNECT answer, which
// may already hold the first bytes of the tunnel.
type bufferedConn struct {
	net.Conn
	r *bufio.Reader
}

func (c bufferedConn) Read(p []byte) (int, error) { return c.r.Read(p) }

// A tunnel whose server has gone silent is closed on both sides, so the
// download engine asks for the rest again instead of waiting for ever.
func TestTunnelClosesAServerThatGoesSilent(t *testing.T) {
	target, closed := silentServer(t)
	px, err := start(throttle.New(), 300*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	defer px.Close()

	c := connectVia(t, px.Addr(), target)
	if _, err := io.WriteString(c, "GET /f.bin HTTP/1.1\r\nHost: x\r\nRange: bytes=1000-\r\n\r\n"); err != nil {
		t.Fatal(err)
	}
	_ = c.SetReadDeadline(time.Now().Add(5 * time.Second))
	began := time.Now()
	_, err = c.Read(make([]byte, 1))
	var ne net.Error
	if errors.As(err, &ne) && ne.Timeout() {
		t.Fatalf("the tunnel was still open after %v of silence", time.Since(began).Round(time.Millisecond))
	}
	select {
	case <-closed:
	case <-time.After(5 * time.Second):
		t.Error("the connection to the silent server was left open")
	}
}

// Bytes the limit holds back are not silence. Here every read after the first
// burst waits two seconds for its allowance, twice the idle timeout, and the
// tunnel still carries it all. The timeout stays well above the pauses a busy
// CI runner puts between arming a deadline and reading, since a deadline that
// has already passed fails the read however much data is waiting.
func TestTunnelKeepsAConnectionTheLimitHoldsBack(t *testing.T) {
	const size = 48 * 1024
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() {
		c, err := ln.Accept()
		if err != nil {
			return
		}
		defer c.Close()
		_, _ = c.Write(make([]byte, size))
		_, _ = io.Copy(io.Discard, c)
	}()

	lim := throttle.New()
	lim.Set(4 * 1024)
	px, err := start(lim, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer px.Close()

	c := connectVia(t, px.Addr(), ln.Addr().String())
	_ = c.SetReadDeadline(time.Now().Add(30 * time.Second))
	n, err := io.ReadFull(c, make([]byte, size))
	if err != nil {
		t.Fatalf("the tunnel ended after %d of %d bytes: %v", n, size, err)
	}
}

// A plain HTTP server that never sends its response headers gets a 502 rather
// than a request that hangs.
func TestForwardGivesUpOnAServerThatSendsNoHeaders(t *testing.T) {
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	defer origin.Close()

	px, err := start(throttle.New(), 300*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	defer px.Close()

	pu, err := url.Parse("http://" + px.Addr())
	if err != nil {
		t.Fatal(err)
	}
	c := &http.Client{Timeout: 5 * time.Second, Transport: &http.Transport{Proxy: http.ProxyURL(pu)}}
	resp, err := c.Get(origin.URL)
	if err != nil {
		t.Fatalf("no answer from the proxy: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadGateway {
		t.Errorf("status = %s, want 502", resp.Status)
	}
}

// A plain HTTP server that sends its headers and then goes quiet has those
// headers passed on at once. Held back until the first body bytes, they leave
// the client waiting for headers, which it does without a limit, rather than
// reading a body, which it gives up on after a while.
func TestForwardPassesHeadersOnBeforeTheBody(t *testing.T) {
	stop := make(chan struct{})
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "1024")
		w.WriteHeader(http.StatusOK)
		w.(http.Flusher).Flush()
		select {
		case <-r.Context().Done():
		case <-stop:
		}
	}))
	defer origin.Close()
	defer close(stop)

	px, err := Start(throttle.New())
	if err != nil {
		t.Fatal(err)
	}
	defer px.Close()

	pu, err := url.Parse("http://" + px.Addr())
	if err != nil {
		t.Fatal(err)
	}
	c := &http.Client{Transport: &http.Transport{Proxy: http.ProxyURL(pu), ResponseHeaderTimeout: 3 * time.Second}}
	resp, err := c.Get(origin.URL)
	if err != nil {
		t.Fatalf("the headers did not come through: %v", err)
	}
	resp.Body.Close()
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
