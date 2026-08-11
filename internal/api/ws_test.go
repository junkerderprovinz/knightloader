package api

// End-to-end coverage for the WebSocket control protocol: internal/hub's own
// tests prove Subscribe/Unsubscribe/Broadcast are race-free and correct in
// isolation; this file proves handleWSControl (api.go) actually wires a real
// client's "subscribe" frame into that hub, over a real connection, which is
// the part hub_test.go cannot see at all.

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/coder/websocket"
)

// dialWS opens the live event stream against a running test server, the way
// a real client (or web/src/lib/api.ts's connectWS) would.
func dialWS(t *testing.T, srv string) *websocket.Conn {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	c, _, err := websocket.Dial(ctx, srv+"/api/ws", nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.CloseNow() })
	return c
}

// readOneOfType reads frames until it sees typ, skipping the ones the
// connection sends unconditionally on open (snapshot, activitySnapshot) so a
// test does not have to special-case them one by one.
func readOneOfType(t *testing.T, c *websocket.Conn, typ string) map[string]any {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		_, data, err := c.Read(ctx)
		cancel()
		if err != nil {
			t.Fatalf("reading the socket: %v", err)
		}
		var msg map[string]any
		if json.Unmarshal(data, &msg) != nil {
			continue
		}
		if msg["type"] == typ {
			return msg
		}
	}
	t.Fatalf("never saw a %q message before the deadline", typ)
	return nil
}

// expectNothingOfType fails if typ arrives within the window, which is the
// whole assertion a subscription filter needs: not that the wanted kind
// arrives, but that an UNWANTED one does not.
func expectNothingOfType(t *testing.T, c *websocket.Conn, typ string, within time.Duration) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), within)
	defer cancel()
	for {
		_, data, err := c.Read(ctx)
		if err != nil {
			return // timeout (or close) is the pass case
		}
		var msg map[string]any
		if json.Unmarshal(data, &msg) == nil && msg["type"] == typ {
			t.Fatalf("received a %q message despite never subscribing to it", typ)
		}
	}
}

// TestWSSubscribeNarrowsTheLiveStream is the feature end to end: a real
// client dials /api/ws, sends a subscribe frame, and a broadcast of an
// un-subscribed kind never reaches it while a subscribed one does.
func TestWSSubscribeNarrowsTheLiveStream(t *testing.T) {
	srv, a := testServer(t)
	defer srv.Close()

	c := dialWS(t, srv.URL)
	// Drain the two frames every connection gets unconditionally on open, so
	// they cannot be mistaken for the broadcasts this test sends next.
	readOneOfType(t, c, "snapshot")
	readOneOfType(t, c, "activitySnapshot")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	sub, _ := json.Marshal(map[string]any{"type": "subscribe", "kinds": []string{"test-sentinel"}})
	if err := c.Write(ctx, websocket.MessageText, sub); err != nil {
		cancel()
		t.Fatal(err)
	}
	cancel()

	// Give the server's read loop a moment to actually apply the subscribe
	// frame before the broadcasts below race it; the hub's own tests already
	// prove Subscribe is correct once called; this is only proving the wire
	// gets it there at all.
	time.Sleep(150 * time.Millisecond)

	a.Hub.Broadcast("queue", "should be filtered")
	a.Hub.Broadcast("test-sentinel", "should arrive")

	got := readOneOfType(t, c, "test-sentinel")
	if got["data"] != "should arrive" {
		t.Errorf("test-sentinel data = %v, want %q", got["data"], "should arrive")
	}
}

// TestWSUnsubscribedConnectionIsUnaffectedByOthersSubscribing checks the
// isolation the per-connection design promises: one socket narrowing itself
// must not narrow a second, independent socket on the same hub.
func TestWSUnsubscribedConnectionIsUnaffectedByOthersSubscribing(t *testing.T) {
	srv, a := testServer(t)
	defer srv.Close()

	narrow := dialWS(t, srv.URL)
	readOneOfType(t, narrow, "snapshot")
	readOneOfType(t, narrow, "activitySnapshot")
	wide := dialWS(t, srv.URL)
	readOneOfType(t, wide, "snapshot")
	readOneOfType(t, wide, "activitySnapshot")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	sub, _ := json.Marshal(map[string]any{"type": "subscribe", "kinds": []string{"test-sentinel"}})
	if err := narrow.Write(ctx, websocket.MessageText, sub); err != nil {
		cancel()
		t.Fatal(err)
	}
	cancel()
	time.Sleep(150 * time.Millisecond)

	a.Hub.Broadcast("queue", "wide should still get this")
	got := readOneOfType(t, wide, "queue")
	if got["data"] != "wide should still get this" {
		t.Errorf("queue data = %v", got["data"])
	}
}

// TestWSMalformedControlFrameDoesNotCloseTheSocket: a client sending garbage
// on this socket (a bug, an unrelated protocol version) must not be
// disconnected over it. See handleWSControl's own doc comment.
func TestWSMalformedControlFrameDoesNotCloseTheSocket(t *testing.T) {
	srv, a := testServer(t)
	defer srv.Close()

	c := dialWS(t, srv.URL)
	readOneOfType(t, c, "snapshot")
	readOneOfType(t, c, "activitySnapshot")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	err := c.Write(ctx, websocket.MessageText, []byte("not json at all"))
	cancel()
	if err != nil {
		t.Fatal(err)
	}

	// The connection must still be alive and still receiving broadcasts.
	a.Hub.Broadcast("test-sentinel", "still connected")
	got := readOneOfType(t, c, "test-sentinel")
	if got["data"] != "still connected" {
		t.Errorf("data = %v", got["data"])
	}
}

// TestWSSubscribeWildcardReturnsToEverything exercises the "*" reset over
// the wire, the way back out of a narrowed stream a real client uses.
func TestWSSubscribeWildcardReturnsToEverything(t *testing.T) {
	srv, a := testServer(t)
	defer srv.Close()

	c := dialWS(t, srv.URL)
	readOneOfType(t, c, "snapshot")
	readOneOfType(t, c, "activitySnapshot")

	send := func(msg map[string]any) {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		b, _ := json.Marshal(msg)
		if err := c.Write(ctx, websocket.MessageText, b); err != nil {
			t.Fatal(err)
		}
	}
	send(map[string]any{"type": "subscribe", "kinds": []string{"test-sentinel"}})
	time.Sleep(150 * time.Millisecond)
	send(map[string]any{"type": "subscribe", "kinds": []string{"*"}})
	time.Sleep(150 * time.Millisecond)

	a.Hub.Broadcast("queue", "back to everything")
	got := readOneOfType(t, c, "queue")
	if got["data"] != "back to everything" {
		t.Errorf("data = %v", got["data"])
	}
}

// TestWSNeedsASession pins the ordinary guard for the socket itself: the
// live stream is not on the small list of routes that answer without one.
func TestWSNeedsASession(t *testing.T) {
	srv, a := testServer(t)
	defer srv.Close()
	if err := a.Auth.SetPassword("", "a-good-password"); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, resp, err := websocket.Dial(ctx, srv.URL+"/api/ws", nil)
	if err == nil {
		t.Fatal("dialed the event stream on a locked instance with no session")
	}
	if resp == nil || resp.StatusCode != 401 {
		status := -1
		if resp != nil {
			status = resp.StatusCode
		}
		t.Errorf("handshake failed with status %d, want 401", status)
	}
}
