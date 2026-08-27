package relay

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
)

// wsTimeout bounds every step of the end-to-end test, so a protocol mistake
// fails the run instead of hanging it.
const wsTimeout = 10 * time.Second

// dialInstance opens a real WebSocket to the relay and completes the hello
// handshake, returning the connection an instance would then keep open.
func dialInstance(t *testing.T, url, key, id string) *websocket.Conn {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), wsTimeout)
	defer cancel()
	c, _, err := websocket.Dial(ctx, url, nil)
	if err != nil {
		t.Fatalf("dial %s as %s: %v", url, id, err)
	}
	t.Cleanup(func() { _ = c.CloseNow() })
	writeFrame(t, c, TypeHello, Hello{
		Key:      key,
		Announce: Announce{InstanceID: id, Name: id, Deployment: "container"},
	})
	return c
}

func writeFrame(t *testing.T, c *websocket.Conn, typ string, data any) {
	t.Helper()
	frame, err := Encode(typ, data)
	if err != nil {
		t.Fatalf("encode %s: %v", typ, err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), wsTimeout)
	defer cancel()
	if err := c.Write(ctx, websocket.MessageText, frame); err != nil {
		t.Fatalf("write %s: %v", typ, err)
	}
}

func readFrame(t *testing.T, c *websocket.Conn, want string) Envelope {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), wsTimeout)
	defer cancel()
	_, frame, err := c.Read(ctx)
	if err != nil {
		t.Fatalf("waiting for a %s frame: %v", want, err)
	}
	env, err := Decode(frame)
	if err != nil {
		t.Fatalf("waiting for a %s frame: %s is not an envelope (%v)", want, frame, err)
	}
	if env.Type != want {
		t.Fatalf("got a %q frame, want %q", env.Type, want)
	}
	return env
}

// TestEndToEndOverRealWebSockets runs the whole thing over an actual
// handshake and actual frames: two clients, the real coder/websocket
// implementation on both ends, and the relay's own http.Handler. The registry
// tests above drive Join/Route directly and so cannot catch a wire-format
// mistake - a frame the server writes but no real client can parse, or a
// handshake the library rejects - which is exactly what this covers.
func TestEndToEndOverRealWebSockets(t *testing.T) {
	srv := httptest.NewServer(New())
	t.Cleanup(srv.Close)
	url := "ws" + strings.TrimPrefix(srv.URL, "http") + "/relay/connect"

	alpha := dialInstance(t, url, "shared-relay-test-key-0123456789ab", "alpha")
	bravo := dialInstance(t, url, "shared-relay-test-key-0123456789ab", "bravo")

	// bravo joined last, so it hears about alpha; alpha hears bravo arrive.
	var sib Announce
	if err := readFrame(t, bravo, TypeAnnounce).Into(&sib); err != nil {
		t.Fatalf("sibling announce: %v", err)
	}
	if sib.InstanceID != "alpha" || sib.Deployment != "container" {
		t.Errorf("bravo was introduced to %+v, want alpha", sib)
	}
	var arrival Announce
	if err := readFrame(t, alpha, TypeAnnounce).Into(&arrival); err != nil {
		t.Fatalf("arrival announce: %v", err)
	}
	if arrival.InstanceID != "bravo" {
		t.Errorf("alpha was told about %+v, want bravo", arrival)
	}

	// alpha calls bravo's REST API through the relay.
	writeFrame(t, alpha, TypeProxyRequest, ProxyRequest{
		RequestID: "r1", Target: "bravo", Method: "POST", Path: "/api/links",
		Body: []byte(`{"url":"https://example.invalid/file.bin"}`),
	})
	var req ProxyRequest
	if err := readFrame(t, bravo, TypeProxyRequest).Into(&req); err != nil {
		t.Fatalf("proxy-request: %v", err)
	}
	if req.RequestID != "r1" || req.Method != "POST" || req.Path != "/api/links" {
		t.Fatalf("bravo received %+v, want alpha's request unchanged", req)
	}
	if string(req.Body) != `{"url":"https://example.invalid/file.bin"}` {
		t.Errorf("body arrived as %s, want it byte for byte", req.Body)
	}

	writeFrame(t, bravo, TypeProxyResponse, ProxyResponse{
		RequestID: req.RequestID, Status: 201, Body: []byte(`{"added":1}`),
	})
	var resp ProxyResponse
	if err := readFrame(t, alpha, TypeProxyResponse).Into(&resp); err != nil {
		t.Fatalf("proxy-response: %v", err)
	}
	if resp.RequestID != "r1" || resp.Status != 201 || string(resp.Body) != `{"added":1}` {
		t.Errorf("alpha received %+v, want bravo's own answer", resp)
	}

	// A real socket closing is what the Instances page's live status hangs
	// on, so it is exercised here and not only through Leave.
	_ = bravo.CloseNow()
	var gone Presence
	if err := readFrame(t, alpha, TypePresence).Into(&gone); err != nil {
		t.Fatalf("presence: %v", err)
	}
	if gone.InstanceID != "bravo" || gone.Online {
		t.Errorf("got %+v, want bravo offline", gone)
	}
}

// TestConnectionWithoutAKeyIsRejected: the hello frame is the only credential
// this relay has, so a connection that never presents one must not end up in
// anybody's group.
func TestConnectionWithoutAKeyIsRejected(t *testing.T) {
	relaySrv := New()
	srv := httptest.NewServer(relaySrv)
	t.Cleanup(srv.Close)
	url := "ws" + strings.TrimPrefix(srv.URL, "http") + "/relay/connect"

	ctx, cancel := context.WithTimeout(context.Background(), wsTimeout)
	defer cancel()
	c, _, err := websocket.Dial(ctx, url, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { _ = c.CloseNow() })

	writeFrame(t, c, TypeHello, Hello{Announce: Announce{InstanceID: "alpha"}})
	if _, _, err := c.Read(ctx); err == nil {
		t.Fatal("a hello without a relay key was accepted")
	}
	if relaySrv.Len() != 0 {
		t.Errorf("%d connections registered, want the keyless one rejected", relaySrv.Len())
	}
}

// TestConnectionWithATooShortKeyIsRejected: minKeyLength is a floor against
// the laziest keys, and a floor that only exists in the doc comment is no
// floor at all - this proves readHello actually enforces it over a real
// handshake, not only that a non-empty key is required.
func TestConnectionWithATooShortKeyIsRejected(t *testing.T) {
	relaySrv := New()
	srv := httptest.NewServer(relaySrv)
	t.Cleanup(srv.Close)
	url := "ws" + strings.TrimPrefix(srv.URL, "http") + "/relay/connect"

	ctx, cancel := context.WithTimeout(context.Background(), wsTimeout)
	defer cancel()
	c, _, err := websocket.Dial(ctx, url, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { _ = c.CloseNow() })

	writeFrame(t, c, TypeHello, Hello{Key: "too-short", Announce: Announce{InstanceID: "alpha"}})
	if _, _, err := c.Read(ctx); err == nil {
		t.Fatal("a hello with a key shorter than minKeyLength was accepted")
	}
	if relaySrv.Len() != 0 {
		t.Errorf("%d connections registered, want the short-keyed one rejected", relaySrv.Len())
	}
}
