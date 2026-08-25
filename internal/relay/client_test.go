package relay

import (
	"context"
	"net"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"
)

// testBackoff is the reconnect cadence the tests run at. Real minBackoff is a
// second, which would turn every reconnect assertion into a stopwatch; the
// loop's behaviour is identical either way, only the wait is not.
const testBackoff = 20 * time.Millisecond

// tracking remembers every connection it accepted so they can all be killed at
// once. http.Server.Close cannot do it: a WebSocket connection is hijacked out
// of the server's own bookkeeping the moment it is upgraded, so closing the
// server would leave exactly the connections these tests need to break.
type tracking struct {
	net.Listener
	mu    sync.Mutex
	conns []net.Conn
}

func (l *tracking) Accept() (net.Conn, error) {
	c, err := l.Listener.Accept()
	if err == nil {
		l.mu.Lock()
		l.conns = append(l.conns, c)
		l.mu.Unlock()
	}
	return c, err
}

func (l *tracking) closeAll() {
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, c := range l.conns {
		_ = c.Close()
	}
	l.conns = nil
}

// relayOn serves a real relay on addr ("127.0.0.1:0" for a fresh port) and
// returns the address it landed on plus a stop that drops the listener and
// every live connection - the relay process dying, not shutting down politely.
// Stopping and then calling relayOn again with the same address is what a
// restarted relay looks like to a client that was connected to the first one:
// the case the reconnect test exists for, and one an httptest.Server cannot
// produce, since it never gives its port back.
func relayOn(t *testing.T, addr string) (string, func()) {
	t.Helper()
	raw, err := net.Listen("tcp", addr)
	if err != nil {
		t.Fatalf("listen on %s: %v", addr, err)
	}
	l := &tracking{Listener: raw}
	srv := &http.Server{Handler: New()}
	go func() { _ = srv.Serve(l) }()
	var once sync.Once
	stop := func() {
		once.Do(func() {
			_ = srv.Close()
			l.closeAll()
		})
	}
	t.Cleanup(stop)
	return raw.Addr().String(), stop
}

// startClient builds a Client against addr, wires it to the fast test backoff
// and starts it. It is given the http:// form of the address on purpose, so
// every test also exercises the scheme and path rewriting a person's typed-in
// relay address goes through.
func startClient(t *testing.T, addr, key, id string, serve ProxyHandler) *Client {
	t.Helper()
	c, err := NewClient(ClientOptions{
		URL:   "http://" + addr,
		Key:   key,
		Self:  Announce{InstanceID: id, Name: id, Deployment: "desktop"},
		Serve: serve,
	})
	if err != nil {
		t.Fatalf("new client %s: %v", id, err)
	}
	c.minBackoff, c.maxBackoff = testBackoff, 4*testBackoff
	c.Start()
	t.Cleanup(func() { _ = c.Close() })
	return c
}

// waitFor polls until cond holds, failing the test if it never does. Polling
// rather than a synchronisation point because the thing being waited on is a
// frame crossing a real socket into a background goroutine, which the test has
// no handle on by design.
func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(wsTimeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

// TestClientAnnouncesItselfAndTracksSiblings is the whole discovery half: the
// client tells the relay who it is, learns who else is there, and forgets them
// again when they go.
func TestClientAnnouncesItselfAndTracksSiblings(t *testing.T) {
	addr, _ := relayOn(t, "127.0.0.1:0")
	alpha := startClient(t, addr, "shared-relay-test-key-0123", "alpha", nil)
	waitFor(t, "alpha to connect", alpha.Connected)

	bravo := dialInstance(t, "ws://"+addr+connectPath, "shared-relay-test-key-0123", "bravo")

	// bravo hears about alpha, which is only possible if the client's own
	// hello carried its announce.
	var seen Announce
	if err := readFrame(t, bravo, TypeAnnounce).Into(&seen); err != nil {
		t.Fatalf("announce: %v", err)
	}
	if seen.InstanceID != "alpha" || seen.Deployment != "desktop" {
		t.Errorf("bravo was introduced to %+v, want the client's own announce", seen)
	}

	waitFor(t, "alpha to see bravo", func() bool {
		sibs := alpha.Siblings()
		return len(sibs) == 1 && sibs[0].InstanceID == "bravo"
	})

	_ = bravo.CloseNow()
	waitFor(t, "alpha to see bravo go", func() bool { return len(alpha.Siblings()) == 0 })
}

// TestClientProxiesToASibling: a call goes out addressed to a sibling and the
// answer comes back to the caller that made it, matched by request ID.
func TestClientProxiesToASibling(t *testing.T) {
	addr, _ := relayOn(t, "127.0.0.1:0")
	alpha := startClient(t, addr, "shared-relay-test-key-0123", "alpha", nil)
	bravo := dialInstance(t, "ws://"+addr+connectPath, "shared-relay-test-key-0123", "bravo")
	readFrame(t, bravo, TypeAnnounce)
	waitFor(t, "alpha to see bravo", func() bool { return len(alpha.Siblings()) == 1 })

	type result struct {
		body   []byte
		status int
		err    error
	}
	done := make(chan result, 1)
	go func() {
		body, status, err := alpha.Proxy(context.Background(), "bravo", http.MethodPost, "/api/links", []byte(`{"url":"x"}`), "")
		done <- result{body, status, err}
	}()

	var req ProxyRequest
	if err := readFrame(t, bravo, TypeProxyRequest).Into(&req); err != nil {
		t.Fatalf("proxy-request: %v", err)
	}
	if req.Target != "bravo" || req.Method != http.MethodPost || req.Path != "/api/links" {
		t.Fatalf("bravo received %+v, want alpha's call unchanged", req)
	}
	if string(req.Body) != `{"url":"x"}` {
		t.Errorf("body arrived as %s, want it byte for byte", req.Body)
	}
	writeFrame(t, bravo, TypeProxyResponse, ProxyResponse{RequestID: req.RequestID, Status: 201, Body: []byte(`{"added":1}`)})

	got := <-done
	if got.err != nil {
		t.Fatalf("proxy: %v", got.err)
	}
	if got.status != 201 || string(got.body) != `{"added":1}` {
		t.Errorf("got %d %s, want bravo's own answer", got.status, got.body)
	}
}

// TestClientAnswersASiblingsCall covers the inbound direction, which is what
// makes this a transport rather than a one-way remote control: a client is
// something siblings call, not only something that calls.
func TestClientAnswersASiblingsCall(t *testing.T) {
	tests := []struct {
		name       string
		serve      ProxyHandler
		wantStatus int
		wantBody   string
	}{
		{
			name: "the handler answers",
			serve: func(_ context.Context, req ProxyRequest) (int, []byte) {
				return http.StatusOK, []byte(req.Method + " " + req.Path)
			},
			wantStatus: http.StatusOK,
			wantBody:   "GET /api/tasks",
		},
		{
			// A client with nothing to serve says so at once rather than
			// leaving the caller to sit out its own timeout.
			name:       "no handler configured",
			serve:      nil,
			wantStatus: http.StatusNotImplemented,
			wantBody:   "",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			addr, _ := relayOn(t, "127.0.0.1:0")
			alpha := startClient(t, addr, "shared-relay-test-key-0123", "alpha", tc.serve)
			waitFor(t, "alpha to connect", alpha.Connected)
			bravo := dialInstance(t, "ws://"+addr+connectPath, "shared-relay-test-key-0123", "bravo")
			readFrame(t, bravo, TypeAnnounce)

			writeFrame(t, bravo, TypeProxyRequest, ProxyRequest{
				RequestID: "r1", Target: "alpha", Method: http.MethodGet, Path: "/api/tasks",
			})
			var resp ProxyResponse
			if err := readFrame(t, bravo, TypeProxyResponse).Into(&resp); err != nil {
				t.Fatalf("proxy-response: %v", err)
			}
			if resp.RequestID != "r1" || resp.Status != tc.wantStatus || string(resp.Body) != tc.wantBody {
				t.Errorf("got %+v, want status %d body %q", resp, tc.wantStatus, tc.wantBody)
			}
		})
	}
}

// TestProxyFailsFastRatherThanWaiting: every way a call can fail to reach a
// peer has to be reported now, not at the end of proxyTimeout - the Instances
// page's answer to "that instance is offline" is one a caller must not wait
// fifteen seconds for.
func TestProxyFailsFastRatherThanWaiting(t *testing.T) {
	tests := []struct {
		name    string
		connect bool
		target  string
		wantErr string
	}{
		{
			name:    "the relay is not connected",
			connect: false,
			target:  "bravo",
			wantErr: "not connected",
		},
		{
			// The relay itself answers this one, so the error text is the
			// relay's own - proving the client surfaces it rather than
			// reporting a bare status.
			name:    "nobody is connected as the target",
			connect: true,
			target:  "nobody",
			wantErr: "no instance nobody is connected",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			addr, stop := relayOn(t, "127.0.0.1:0")
			// Stopped before the client exists, so there is no window in
			// which it briefly connects and the assertion below tests
			// nothing.
			if !tc.connect {
				stop()
			}
			alpha := startClient(t, addr, "shared-relay-test-key-0123", "alpha", nil)
			if tc.connect {
				waitFor(t, "alpha to connect", alpha.Connected)
			}

			began := time.Now()
			_, _, err := alpha.Proxy(context.Background(), tc.target, http.MethodGet, "/api/tasks", nil, "")
			if err == nil {
				t.Fatal("the call succeeded, want it to fail")
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Errorf("got %v, want it to mention %q", err, tc.wantErr)
			}
			if time.Since(began) >= proxyTimeout {
				t.Errorf("the call took %v, want it to fail without waiting out the timeout", time.Since(began))
			}
		})
	}
}

// TestReconnectsAfterTheRelayDrops is the resilience the whole design hangs
// on: a relay outage costs the instance its relay peers and nothing else, and
// the peers come back on their own once the relay does.
func TestReconnectsAfterTheRelayDrops(t *testing.T) {
	addr, stop := relayOn(t, "127.0.0.1:0")
	alpha := startClient(t, addr, "shared-relay-test-key-0123", "alpha", nil)
	bravo := dialInstance(t, "ws://"+addr+connectPath, "shared-relay-test-key-0123", "bravo")
	waitFor(t, "alpha to see bravo", func() bool { return len(alpha.Siblings()) == 1 })

	stop()
	waitFor(t, "alpha to notice the outage", func() bool {
		return !alpha.Connected() && len(alpha.Siblings()) == 0
	})
	_ = bravo.CloseNow()

	// A different relay process on the same address, exactly as a restarted
	// container looks: no shared state, so everything the client sees now it
	// re-established by itself.
	relayOn(t, addr)
	waitFor(t, "alpha to reconnect", alpha.Connected)

	charlie := dialInstance(t, "ws://"+addr+connectPath, "shared-relay-test-key-0123", "charlie")
	var seen Announce
	if err := readFrame(t, charlie, TypeAnnounce).Into(&seen); err != nil {
		t.Fatalf("announce after reconnect: %v", err)
	}
	if seen.InstanceID != "alpha" {
		t.Errorf("charlie was introduced to %+v, want the reconnected alpha", seen)
	}
	waitFor(t, "alpha to see charlie", func() bool {
		sibs := alpha.Siblings()
		return len(sibs) == 1 && sibs[0].InstanceID == "charlie"
	})
}

// TestCallInFlightFailsWhenTheConnectionDies: a request the relay accepted and
// nobody answered must not hold its caller until proxyTimeout once the socket
// carrying the answer is gone.
func TestCallInFlightFailsWhenTheConnectionDies(t *testing.T) {
	addr, stop := relayOn(t, "127.0.0.1:0")
	alpha := startClient(t, addr, "shared-relay-test-key-0123", "alpha", nil)
	bravo := dialInstance(t, "ws://"+addr+connectPath, "shared-relay-test-key-0123", "bravo")
	readFrame(t, bravo, TypeAnnounce)
	waitFor(t, "alpha to see bravo", func() bool { return len(alpha.Siblings()) == 1 })

	failed := make(chan error, 1)
	go func() {
		_, _, err := alpha.Proxy(context.Background(), "bravo", http.MethodGet, "/api/tasks", nil, "")
		failed <- err
	}()
	// bravo receives the call and deliberately never answers it.
	readFrame(t, bravo, TypeProxyRequest)
	stop()

	select {
	case err := <-failed:
		if err == nil {
			t.Fatal("the call succeeded, want it to fail with the connection")
		}
		// Two legitimate paths race here, and either is a correct answer:
		// alpha's own connection can notice it died first ("...dropped"), or
		// the relay can notice bravo left first and answer alpha with a
		// synthetic failure before alpha's own socket has even reported
		// anything wrong (server.go's own failPending, added once a target
		// disconnecting mid-request started failing the call fast instead of
		// leaving the caller to time out) - which one wins is inherent to
		// killing the whole relay process out from under both connections at
		// once, not something either side controls.
		if !strings.Contains(err.Error(), "dropped") && !strings.Contains(err.Error(), "disconnected before it replied") {
			t.Errorf("got %v, want it to say the connection dropped or that the target disconnected first", err)
		}
	case <-time.After(wsTimeout):
		t.Fatal("the call is still waiting; a dead connection must fail it at once")
	}
}

// TestConnectURL: a person configures the address they gave their reverse
// proxy, not a WebSocket URL, so every reasonable spelling of the same relay
// has to reach the same endpoint.
func TestConnectURL(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"https gets the path appended", "https://relay.example.com", "wss://relay.example.com/relay/connect"},
		{"a trailing slash is a bare host too", "https://relay.example.com/", "wss://relay.example.com/relay/connect"},
		{"http and a port", "http://192.168.20.30:8760", "ws://192.168.20.30:8760/relay/connect"},
		{"whitespace from a settings field", "  https://relay.example.com  ", "wss://relay.example.com/relay/connect"},
		{"a ws url is already one", "ws://127.0.0.1:8760/relay/connect", "ws://127.0.0.1:8760/relay/connect"},
		{"a mounted path is kept", "https://home.example.com/kl-relay/connect", "wss://home.example.com/kl-relay/connect"},
		{"no scheme", "relay.example.com", ""},
		{"a scheme nobody can dial", "ftp://relay.example.com", ""},
		{"empty", "", ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := connectURL(tc.in)
			if tc.want == "" {
				if err == nil {
					t.Fatalf("got %q, want %q rejected", got, tc.in)
				}
				return
			}
			if err != nil {
				t.Fatalf("connectURL(%q): %v", tc.in, err)
			}
			if got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

// TestNewClientRejectsMisconfiguration: all three of these are permanent, and
// the settings page that produced them is the only place they can be fixed, so
// none of them may turn into a connection that quietly never works.
func TestNewClientRejectsMisconfiguration(t *testing.T) {
	valid := ClientOptions{URL: "https://relay.example.com", Key: "shared-relay-test-key-0123", Self: Announce{InstanceID: "alpha"}}
	tests := []struct {
		name   string
		mangle func(o *ClientOptions)
	}{
		{"no address", func(o *ClientOptions) { o.URL = "" }},
		{"no key", func(o *ClientOptions) { o.Key = "   " }},
		{"no instance id", func(o *ClientOptions) { o.Self.InstanceID = "" }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			opts := valid
			tc.mangle(&opts)
			if _, err := NewClient(opts); err == nil {
				t.Fatal("accepted, want an error")
			}
		})
	}
	if _, err := NewClient(valid); err != nil {
		t.Fatalf("a valid configuration was rejected: %v", err)
	}
}
