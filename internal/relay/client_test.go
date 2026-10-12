package relay

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/json"
	"net"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"
)

// testBackoff is the reconnect cadence the tests run at, instead of the real
// one-second minimum.
const testBackoff = 20 * time.Millisecond

// testFrameKey is the frame key shared by every client and hand-rolled peer in
// these tests, as if they had entered the same connection phrase. It goes
// through the real DeriveFrameKey so the tests follow any change to it.
var testFrameKey = DeriveFrameKey([]byte("relay package tests"))

// The member keys of the instances in these tests, and the ids they make. A
// client drops an instance's announce under an id of any other shape, and
// takes an answer only when the target's key signed it.
var (
	alphaKey   = memberKeyFrom('a')
	bravoKey   = memberKeyFrom('b')
	charlieKey = memberKeyFrom('c')
	alphaID    = MemberID(KindInstance, alphaKey.Public().(ed25519.PublicKey))
	bravoID    = MemberID(KindInstance, bravoKey.Public().(ed25519.PublicKey))
	charlieID  = MemberID(KindInstance, charlieKey.Public().(ed25519.PublicKey))
	nasID      = strings.Repeat("d", 40)
	memberKeys = map[string]ed25519.PrivateKey{alphaID: alphaKey, bravoID: bravoKey, charlieID: charlieKey}
)

func memberKeyFrom(b byte) ed25519.PrivateKey {
	return ed25519.NewKeyFromSeed(bytes.Repeat([]byte{b}, ed25519.SeedSize))
}

// sealFor, sealResultFor, openFrom and openResultFrom seal and open payloads
// for the hand-rolled peers, keeping the additional data in one place.
func sealFor(t *testing.T, requestID, target string, call ProxyCall) []byte {
	t.Helper()
	sealed, err := SealCall(testFrameKey, requestID, target, call)
	if err != nil {
		t.Fatalf("seal call: %v", err)
	}
	return sealed
}

func sealResultFor(t *testing.T, requestID string, res ProxyResult) []byte {
	t.Helper()
	sealed, err := SealResult(testFrameKey, requestID, res)
	if err != nil {
		t.Fatalf("seal result: %v", err)
	}
	return sealed
}

func openFrom(t *testing.T, requestID, target string, sealed []byte) ProxyCall {
	t.Helper()
	call, err := OpenCall(testFrameKey, requestID, target, sealed)
	if err != nil {
		t.Fatalf("open call: %v", err)
	}
	return call
}

func openResultFrom(t *testing.T, requestID string, sealed []byte) ProxyResult {
	t.Helper()
	res, err := OpenResult(testFrameKey, requestID, sealed)
	if err != nil {
		t.Fatalf("open result: %v", err)
	}
	return res
}

// tracking remembers every connection it accepted so they can all be killed at
// once. http.Server.Close cannot do it, because an upgraded WebSocket is
// hijacked out of the server's bookkeeping.
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
// every live connection, as if the relay process died. Calling relayOn again
// on the same address then looks like a restarted relay, which an
// httptest.Server cannot do because it never gives its port back.
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

// startClient builds a Client against addr with the fast test backoff and
// starts it. It passes the http:// form of the address so every test also
// exercises the scheme and path rewriting.
func startClient(t *testing.T, addr, key, id string, serve ProxyHandler) *Client {
	t.Helper()
	c, err := NewClient(ClientOptions{
		URL:      "http://" + addr,
		Key:      key,
		FrameKey: testFrameKey,
		Self:     Announce{InstanceID: id, Name: id, Deployment: "desktop"},
		Member:   memberKeys[id],
		Serve:    serve,
	})
	if err != nil {
		t.Fatalf("new client %s: %v", id, err)
	}
	c.minBackoff, c.maxBackoff = testBackoff, 4*testBackoff
	c.Start()
	t.Cleanup(func() { _ = c.Close() })
	return c
}

// waitFor polls until cond holds, failing the test if it never does. The
// frames being waited on cross a real socket into a background goroutine the
// test has no handle on.
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

func TestClientAnnouncesItselfAndTracksSiblings(t *testing.T) {
	addr, _ := relayOn(t, "127.0.0.1:0")
	alpha := startClient(t, addr, "shared-relay-test-key-0123456789ab", alphaID, nil)
	waitFor(t, "alpha to connect", alpha.Connected)

	bravo := dialInstance(t, "ws://"+addr+connectPath, "shared-relay-test-key-0123456789ab", bravoID)

	// bravo is a raw socket, so it reads the wire form a relay operator sees:
	// the id in the clear, the name and deployment only sealed.
	var seen Announce
	if err := readFrame(t, bravo, TypeAnnounce).Into(&seen); err != nil {
		t.Fatalf("announce: %v", err)
	}
	if seen.InstanceID != alphaID {
		t.Errorf("bravo was introduced to %+v, want alpha's id in the clear", seen)
	}
	if seen.Name != "" || seen.Deployment != "" {
		t.Errorf("the wire announce still carries identity in the clear: %+v", seen)
	}
	id, err := OpenIdentity(testFrameKey, seen.InstanceID, seen.Sealed)
	if err != nil {
		t.Fatalf("the sealed identity did not open: %v", err)
	}
	if id.Name != alphaID || id.Deployment != "desktop" {
		t.Errorf("sealed identity = %+v, want alpha/desktop", id)
	}

	waitFor(t, "alpha to see bravo", func() bool {
		sibs := alpha.Siblings()
		return len(sibs) == 1 && sibs[0].InstanceID == bravoID
	})

	_ = bravo.CloseNow()
	waitFor(t, "alpha to see bravo go", func() bool { return len(alpha.Siblings()) == 0 })
}

func TestClientProxiesToASibling(t *testing.T) {
	addr, _ := relayOn(t, "127.0.0.1:0")
	alpha := startClient(t, addr, "shared-relay-test-key-0123456789ab", alphaID, nil)
	bravo := dialInstance(t, "ws://"+addr+connectPath, "shared-relay-test-key-0123456789ab", bravoID)
	readFrame(t, bravo, TypeAnnounce)
	waitFor(t, "alpha to see bravo", func() bool { return len(alpha.Siblings()) == 1 })

	type result struct {
		body   []byte
		status int
		err    error
	}
	done := make(chan result, 1)
	go func() {
		body, status, err := alpha.Proxy(context.Background(), bravoID, http.MethodPost, "/api/links", []byte(`{"url":"x"}`))
		done <- result{body, status, err}
	}()

	var req ProxyRequest
	if err := readFrame(t, bravo, TypeProxyRequest).Into(&req); err != nil {
		t.Fatalf("proxy-request: %v", err)
	}
	if req.Target != bravoID {
		t.Fatalf("bravo received %+v, want alpha's call unchanged", req)
	}
	call := openFrom(t, req.RequestID, bravoID, req.Sealed)
	if call.Method != http.MethodPost || call.Path != "/api/links" {
		t.Fatalf("bravo opened %+v, want alpha's call unchanged", call)
	}
	if string(call.Body) != `{"url":"x"}` {
		t.Errorf("body arrived as %s, want it byte for byte", call.Body)
	}
	writeFrame(t, bravo, TypeProxyResponse, ProxyResponse{
		RequestID: req.RequestID,
		Sealed:    sealResultFor(t, req.RequestID, SignResult(bravoKey, req.RequestID, bravoID, req.Sealed, ProxyResult{Status: 201, Body: []byte(`{"added":1}`)})),
	})

	got := <-done
	if got.err != nil {
		t.Fatalf("proxy: %v", got.err)
	}
	if got.status != 201 || string(got.body) != `{"added":1}` {
		t.Errorf("got %d %s, want bravo's own answer", got.status, got.body)
	}
}

func TestClientAnswersASiblingsCall(t *testing.T) {
	tests := []struct {
		name       string
		serve      ProxyHandler
		wantStatus int
		wantBody   string
	}{
		{
			name: "the handler answers",
			serve: func(_ context.Context, call ProxyCall) (int, []byte) {
				return http.StatusOK, []byte(call.Method + " " + call.Path)
			},
			wantStatus: http.StatusOK,
			wantBody:   "GET /api/tasks",
		},
		{
			name:       "no handler configured",
			serve:      nil,
			wantStatus: http.StatusNotImplemented,
			wantBody:   "",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			addr, _ := relayOn(t, "127.0.0.1:0")
			alpha := startClient(t, addr, "shared-relay-test-key-0123456789ab", alphaID, tc.serve)
			waitFor(t, "alpha to connect", alpha.Connected)
			bravo := dialInstance(t, "ws://"+addr+connectPath, "shared-relay-test-key-0123456789ab", bravoID)
			readFrame(t, bravo, TypeAnnounce)

			sealed := sealFor(t, "r1", alphaID, ProxyCall{Method: http.MethodGet, Path: "/api/tasks"})
			writeFrame(t, bravo, TypeProxyRequest, ProxyRequest{RequestID: "r1", Target: alphaID, Sealed: sealed})
			var resp ProxyResponse
			if err := readFrame(t, bravo, TypeProxyResponse).Into(&resp); err != nil {
				t.Fatalf("proxy-response: %v", err)
			}
			if resp.RequestID != "r1" {
				t.Fatalf("got %+v, want the answer to r1", resp)
			}
			res := openResultFrom(t, "r1", resp.Sealed)
			if res.Status != tc.wantStatus || string(res.Body) != tc.wantBody {
				t.Errorf("got %+v, want status %d body %q", res, tc.wantStatus, tc.wantBody)
			}
			if !VerifyResult("r1", alphaID, sealed, res) {
				t.Error("alpha's answer is not signed by its member key")
			}
		})
	}
}

// TestACallSentAgainOrLateIsNotRun has a handler use a ReplayGuard the way
// federation.Manager.Guard does, and checks that a call it drops gets no
// answer at all.
func TestACallSentAgainOrLateIsNotRun(t *testing.T) {
	addr, _ := relayOn(t, "127.0.0.1:0")
	var mu sync.Mutex
	var ran []string
	guard := NewReplayGuard()
	alpha := startClient(t, addr, "shared-relay-test-key-0123456789ab", alphaID, func(_ context.Context, call ProxyCall) (int, []byte) {
		if !guard.Admit(call) {
			return StatusDrop, nil
		}
		mu.Lock()
		ran = append(ran, call.ID)
		mu.Unlock()
		return http.StatusOK, nil
	})
	waitFor(t, "alpha to connect", alpha.Connected)
	bravo := dialInstance(t, "ws://"+addr+connectPath, "shared-relay-test-key-0123456789ab", bravoID)
	readFrame(t, bravo, TypeAnnounce)

	member := ed25519.NewKeyFromSeed([]byte(strings.Repeat("m", ed25519.SeedSize)))
	signed := func(id string, call ProxyCall) []byte {
		sealed, err := SealSignedCall(testFrameKey, member, id, alphaID, call)
		if err != nil {
			t.Fatal(err)
		}
		return sealed
	}
	call := ProxyCall{Method: http.MethodPost, Path: "/api/queue/pause", From: bravoID, Kind: KindInstance}
	first := ProxyRequest{RequestID: "r1", Target: alphaID, Sealed: signed("r1", call)}
	late := call
	late.Sent = time.Now().Add(-10 * time.Minute).Unix()
	writeFrame(t, bravo, TypeProxyRequest, first)
	writeFrame(t, bravo, TypeProxyRequest, first)
	writeFrame(t, bravo, TypeProxyRequest, ProxyRequest{RequestID: "r2", Target: alphaID, Sealed: signed("r2", late)})
	writeFrame(t, bravo, TypeProxyRequest, ProxyRequest{RequestID: "r3", Target: alphaID, Sealed: signed("r3", call)})

	// Each call is answered on its own goroutine, so the answers to r1 and
	// r3 may come in either order.
	answered := map[string]bool{}
	for range 2 {
		var resp ProxyResponse
		if err := readFrame(t, bravo, TypeProxyResponse).Into(&resp); err != nil {
			t.Fatalf("proxy-response: %v", err)
		}
		answered[resp.RequestID] = true
	}
	if !answered["r1"] || !answered["r3"] {
		t.Fatalf("answered %v, want r1 and r3", answered)
	}
	quiet, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	if _, frame, err := bravo.Read(quiet); err == nil {
		t.Fatalf("a third answer came: %s", frame)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(ran) != 2 {
		t.Fatalf("ran %v, want r1 and r3 once each", ran)
	}
}

func TestACallWithoutItsIDAndTimeIsRefused(t *testing.T) {
	plain, err := json.Marshal(struct {
		Method string `json:"method"`
		Path   string `json:"path"`
	}{http.MethodGet, "/api/tasks"})
	if err != nil {
		t.Fatal(err)
	}
	sealed, err := seal(testFrameKey, requestAAD("r1", alphaID), plain)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := OpenCall(testFrameKey, "r1", alphaID, sealed); err == nil {
		t.Fatal("a call sealed without its request id opened")
	}
}

func TestReplayGuardAdmitsACallOnceWithinTheSkew(t *testing.T) {
	g := NewReplayGuard()
	now := time.Unix(1_800_000_000, 0)
	g.now = func() time.Time { return now }
	key := keyNumber(1)
	call := ProxyCall{ID: "a", Sent: now.Unix(), Key: key}
	if !g.Admit(call) || g.Admit(call) {
		t.Fatal("a fresh call must pass once and only once")
	}
	for name, c := range map[string]ProxyCall{
		"old":       {ID: "b", Sent: now.Add(-ClockSkew - time.Second).Unix(), Key: key},
		"future":    {ID: "c", Sent: now.Add(ClockSkew + time.Second).Unix(), Key: key},
		"unstamped": {ID: "", Sent: now.Unix(), Key: key},
	} {
		if g.Admit(c) {
			t.Errorf("an %s call was admitted", name)
		}
	}
	now = now.Add(3 * ClockSkew)
	if !g.Admit(ProxyCall{ID: "d", Sent: now.Unix(), Key: key}) || g.Len() != 1 {
		t.Fatalf("remembered %d ids, want only the one from inside the window", len(g.seen))
	}
}

// TestProxyFailsFastRatherThanWaiting: a call that cannot reach its peer fails
// at once, not at the end of proxyTimeout.
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
			target:  bravoID,
			wantErr: "not connected",
		},
		{
			// The relay answers this one, and the client has to pass its
			// text on.
			name:    "nobody is connected as the target",
			connect: true,
			target:  "nobody",
			wantErr: "no instance nobody is connected",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			addr, stop := relayOn(t, "127.0.0.1:0")
			// Stopped before the client exists, so it cannot briefly connect.
			if !tc.connect {
				stop()
			}
			alpha := startClient(t, addr, "shared-relay-test-key-0123456789ab", alphaID, nil)
			if tc.connect {
				waitFor(t, "alpha to connect", alpha.Connected)
			}

			began := time.Now()
			_, _, err := alpha.Proxy(context.Background(), tc.target, http.MethodGet, "/api/tasks", nil)
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

// TestReconnectsAfterTheRelayDrops: an outage costs the instance its relay
// peers, and they come back on their own once the relay does.
func TestReconnectsAfterTheRelayDrops(t *testing.T) {
	addr, stop := relayOn(t, "127.0.0.1:0")
	alpha := startClient(t, addr, "shared-relay-test-key-0123456789ab", alphaID, nil)
	bravo := dialInstance(t, "ws://"+addr+connectPath, "shared-relay-test-key-0123456789ab", bravoID)
	waitFor(t, "alpha to see bravo", func() bool { return len(alpha.Siblings()) == 1 })

	stop()
	waitFor(t, "alpha to notice the outage", func() bool {
		return !alpha.Connected() && len(alpha.Siblings()) == 0
	})
	_ = bravo.CloseNow()

	// A fresh relay on the same address shares no state with the old one, so
	// everything the client sees now it re-established itself.
	relayOn(t, addr)
	waitFor(t, "alpha to reconnect", alpha.Connected)

	charlie := dialInstance(t, "ws://"+addr+connectPath, "shared-relay-test-key-0123456789ab", charlieID)
	var seen Announce
	if err := readFrame(t, charlie, TypeAnnounce).Into(&seen); err != nil {
		t.Fatalf("announce after reconnect: %v", err)
	}
	if seen.InstanceID != alphaID {
		t.Errorf("charlie was introduced to %+v, want the reconnected alpha", seen)
	}
	waitFor(t, "alpha to see charlie", func() bool {
		sibs := alpha.Siblings()
		return len(sibs) == 1 && sibs[0].InstanceID == charlieID
	})
}

func TestCallInFlightFailsWhenTheConnectionDies(t *testing.T) {
	addr, stop := relayOn(t, "127.0.0.1:0")
	alpha := startClient(t, addr, "shared-relay-test-key-0123456789ab", alphaID, nil)
	bravo := dialInstance(t, "ws://"+addr+connectPath, "shared-relay-test-key-0123456789ab", bravoID)
	readFrame(t, bravo, TypeAnnounce)
	waitFor(t, "alpha to see bravo", func() bool { return len(alpha.Siblings()) == 1 })

	failed := make(chan error, 1)
	go func() {
		_, _, err := alpha.Proxy(context.Background(), bravoID, http.MethodGet, "/api/tasks", nil)
		failed <- err
	}()
	// bravo receives the call and never answers it.
	readFrame(t, bravo, TypeProxyRequest)
	stop()

	select {
	case err := <-failed:
		if err == nil {
			t.Fatal("the call succeeded, want it to fail with the connection")
		}
		// Two paths race and either is correct: alpha notices its own
		// connection died ("dropped"), or the relay notices bravo left first
		// and fails the call through failPending.
		if !strings.Contains(err.Error(), "dropped") && !strings.Contains(err.Error(), "disconnected before it replied") {
			t.Errorf("got %v, want it to say the connection dropped or that the target disconnected first", err)
		}
	case <-time.After(wsTimeout):
		t.Fatal("the call is still waiting; a dead connection must fail it at once")
	}
}

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
		{"an instance under a path has the relay below it", "https://example.com/kl", "wss://example.com/kl/relay/connect"},
		{"and with a trailing slash", "https://example.com/apps/kl/", "wss://example.com/apps/kl/relay/connect"},
		{"a full address under a path is kept", "https://example.com/kl/relay/connect", "wss://example.com/kl/relay/connect"},
		{"a slash after the socket goes", "https://example.com/kl/relay/connect/", "wss://example.com/kl/relay/connect"},
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

// TestRelayHost: the relay a hello's proof names. The app's port is held to
// these rows by mobile/check-relay-url.mjs.
func TestRelayHost(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"a dialled address", "wss://relay.example.com/relay/connect", "relay.example.com"},
		{"the port goes", "ws://192.168.20.30:8760/relay/connect", "192.168.20.30"},
		{"in lower case", "https://Relay.Example.COM", "relay.example.com"},
		{"without the dot of a full name", "https://relay.example.com./kl", "relay.example.com"},
		{"an IPv6 literal without its brackets", "ws://[::1]:8760/relay/connect", "::1"},
		{"a bare host", "relay.example.com", "relay.example.com"},
		{"a bare host and a port", "127.0.0.1:8760", "127.0.0.1"},
		{"whitespace from a setting", "  https://relay.example.com  ", "relay.example.com"},
		{"empty", "", ""},
	}
	for _, tc := range tests {
		if got := RelayHost(tc.in); got != tc.want {
			t.Errorf("%s: RelayHost(%q) = %q, want %q", tc.name, tc.in, got, tc.want)
		}
	}
}

func TestNewClientRejectsMisconfiguration(t *testing.T) {
	valid := ClientOptions{
		URL:      "https://relay.example.com",
		Key:      "shared-relay-test-key-0123456789ab",
		FrameKey: testFrameKey,
		Self:     Announce{InstanceID: alphaID},
	}
	tests := []struct {
		name   string
		mangle func(o *ClientOptions)
	}{
		{"no address", func(o *ClientOptions) { o.URL = "" }},
		{"no key", func(o *ClientOptions) { o.Key = "   " }},
		{"no instance id", func(o *ClientOptions) { o.Self.InstanceID = "" }},
		{"no frame key", func(o *ClientOptions) { o.FrameKey = nil }},
		{"a frame key of the wrong length", func(o *ClientOptions) { o.FrameKey = []byte("too short") }},
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

// TestAnAnswerNotSignedByTheTargetIsNoAnswer: bravo is on the relay, and
// somebody else holding the words answers alpha's calls to it, sealed under the
// frame key, as one who took bravo's place on the relay would. Neither an
// unsigned answer, one signed by another key, bravo's own answer to another
// call nor its answer to alpha's call altered on the way reaches alpha's
// caller, and a "removed" least of all.
func TestAnAnswerNotSignedByTheTargetIsNoAnswer(t *testing.T) {
	addr, _ := relayOn(t, "127.0.0.1:0")
	alpha := startClient(t, addr, "shared-relay-test-key-0123456789ab", alphaID, nil)
	bravo := dialInstance(t, "ws://"+addr+connectPath, "shared-relay-test-key-0123456789ab", bravoID)
	readFrame(t, bravo, TypeAnnounce)
	waitFor(t, "alpha to see bravo", func() bool { return len(alpha.Siblings()) == 1 })

	removed := ProxyResult{Status: http.StatusGone, Body: []byte("removed")}
	// Whoever holds the words can call bravo under the request id of alpha's
	// call and get bravo's signed answer to that call instead.
	another := sealFor(t, "another", bravoID, SignCall(charlieKey, "another", bravoID, ProxyCall{Method: http.MethodGet, Path: "/api/tasks", From: charlieID, Kind: KindInstance}))
	// Or change what alpha's call asks and keep its id, key and signature,
	// which bravo refuses with a signed answer of its own.
	altered := func(id string, sealed []byte) []byte {
		call := openFrom(t, id, bravoID, sealed)
		call.Body = []byte(`{"urls":["https://example.com/other"]}`)
		return mustSeal(t, id, bravoID, call)
	}
	for name, answer := range map[string]func(requestID string, sealed []byte) ProxyResult{
		"unsigned": func(string, []byte) ProxyResult { return removed },
		"signed by a stranger": func(id string, sealed []byte) ProxyResult {
			return SignResult(charlieKey, id, bravoID, sealed, removed)
		},
		"signed by bravo for another call": func(id string, _ []byte) ProxyResult { return SignResult(bravoKey, id, bravoID, another, removed) },
		"signed by bravo for the call altered on the way": func(id string, sealed []byte) ProxyResult {
			return SignResult(bravoKey, id, bravoID, altered(id, sealed), ProxyResult{Status: http.StatusForbidden, Body: SignatureRefused})
		},
		"signed by bravo": func(id string, sealed []byte) ProxyResult { return SignResult(bravoKey, id, bravoID, sealed, removed) },
	} {
		done := make(chan error, 1)
		go func() {
			_, _, err := alpha.Proxy(context.Background(), bravoID, http.MethodGet, "/api/tasks", nil)
			done <- err
		}()
		var req ProxyRequest
		if err := readFrame(t, bravo, TypeProxyRequest).Into(&req); err != nil {
			t.Fatal(err)
		}
		writeFrame(t, bravo, TypeProxyResponse, ProxyResponse{RequestID: req.RequestID, Sealed: sealResultFor(t, req.RequestID, answer(req.RequestID, req.Sealed))})
		err := <-done
		if want := name == "signed by bravo"; (err == nil) != want {
			t.Errorf("an answer %s: err %v, want it taken %v", name, err, want)
		}
	}
}
