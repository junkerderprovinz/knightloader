package relay

import (
	"context"
	"encoding/json"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/websocket"
)

// fakeConn is a connection whose Write can be held open on demand, which is
// how these tests model an instance on a bad link without a real socket.
type fakeConn struct {
	writes chan []byte
	block  chan struct{} // if non-nil, Write waits for it to be closed
	closed atomic.Bool
}

func newFakeConn() *fakeConn {
	return &fakeConn{writes: make(chan []byte, 4*queueDepth)}
}

func (f *fakeConn) Write(ctx context.Context, _ websocket.MessageType, p []byte) error {
	if f.block != nil {
		select {
		case <-f.block:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	f.writes <- append([]byte(nil), p...)
	return nil
}

func (f *fakeConn) CloseNow() error {
	f.closed.Store(true)
	return nil
}

// next takes one frame off a fake connection or fails, so a missing broadcast
// shows up as a named failure instead of a hung test.
func next(t *testing.T, f *fakeConn, msg string) Envelope {
	t.Helper()
	select {
	case raw := <-f.writes:
		env, err := Decode(raw)
		if err != nil {
			t.Fatalf("%s: frame is not an envelope (%v): %s", msg, err, raw)
		}
		return env
	case <-time.After(5 * time.Second):
		t.Fatal(msg)
		return Envelope{}
	}
}

// nothing asserts a connection stays quiet, which is the only way to state
// "these two never saw each other".
func nothing(t *testing.T, f *fakeConn, msg string) {
	t.Helper()
	select {
	case extra := <-f.writes:
		t.Fatalf("%s: %s", msg, extra)
	case <-time.After(100 * time.Millisecond):
	}
}

func announceOf(t *testing.T, env Envelope, msg string) Announce {
	t.Helper()
	if env.Type != TypeAnnounce {
		t.Fatalf("%s: got a %q frame, want an announce", msg, env.Type)
	}
	var a Announce
	if err := env.Into(&a); err != nil {
		t.Fatalf("%s: %v", msg, err)
	}
	return a
}

func join(t *testing.T, s *Server, key, id string) *fakeConn {
	t.Helper()
	c := newFakeConn()
	s.Join(key, c, Announce{InstanceID: id, Name: id, Deployment: "container"})
	t.Cleanup(func() { s.Leave(c) })
	return c
}

// send routes one frame as if the connection had written it up its socket.
func send(t *testing.T, s *Server, c Conn, typ string, data any) {
	t.Helper()
	frame, err := Encode(typ, data)
	if err != nil {
		t.Fatalf("encode %s: %v", typ, err)
	}
	s.Route(c, frame)
}

// TestJoinIntroducesBothWays is the "log in once, see everything" promise: a
// late joiner has to learn about the instances that were already up, not only
// about the ones that connect after it.
func TestJoinIntroducesBothWays(t *testing.T) {
	s := New()
	a := join(t, s, "key-1", "alpha")
	b := join(t, s, "key-1", "bravo")

	if got := announceOf(t, next(t, b, "the joiner never heard about its sibling"), "sibling announce"); got.InstanceID != "alpha" {
		t.Errorf("joiner was told about %q, want alpha", got.InstanceID)
	}
	if got := announceOf(t, next(t, a, "the sibling never heard about the joiner"), "arrival announce"); got.InstanceID != "bravo" {
		t.Errorf("sibling was told about %q, want bravo", got.InstanceID)
	}
	nothing(t, b, "the joiner received its own arrival announce back")
}

// TestKeysNeverSeeEachOther pins the only isolation boundary this relay has.
// If it broke, one person's instances would appear on another person's
// Instances page.
func TestKeysNeverSeeEachOther(t *testing.T) {
	s := New()
	mine := join(t, s, "key-mine", "alpha")
	theirs := join(t, s, "key-theirs", "bravo")

	nothing(t, mine, "a connection on another key was announced")
	nothing(t, theirs, "a connection on another key was announced")

	send(t, s, mine, TypeProxyRequest, ProxyRequest{RequestID: "r1", Target: "bravo"})
	nothing(t, theirs, "a proxy-request crossed a key boundary")

	env := next(t, mine, "no answer for a target on another key")
	var resp ProxyResponse
	if env.Type != TypeProxyResponse || env.Into(&resp) != nil {
		t.Fatalf("got a %q frame, want a proxy-response", env.Type)
	}
	if resp.Error == "" {
		t.Error("a target on another key was answered without an error")
	}
}

// TestLeaveTellsSiblingsTheInstanceWentOffline: the Instances page shows live
// status from these frames instead of guessing from a poll, so a disconnect
// that broadcast nothing would leave a dead instance looking online forever.
func TestLeaveTellsSiblingsTheInstanceWentOffline(t *testing.T) {
	s := New()
	a := join(t, s, "key-1", "alpha")
	b := join(t, s, "key-1", "bravo")
	next(t, a, "no arrival announce")
	next(t, b, "no sibling announce")

	s.Leave(b)

	env := next(t, a, "no presence frame after a sibling disconnected")
	var p Presence
	if env.Type != TypePresence || env.Into(&p) != nil {
		t.Fatalf("got a %q frame, want a presence", env.Type)
	}
	if p.InstanceID != "bravo" || p.Online {
		t.Errorf("got %+v, want bravo offline", p)
	}
	if s.Len() != 1 {
		t.Errorf("%d connections still registered, want 1", s.Len())
	}
}

// TestProxyRoundTripRoutesByRequestID covers the forwarding path in both
// directions, including that the frame reaches the target byte for byte - the
// relay must not re-encode a payload it cannot look inside.
//
// The sealed blobs here are deliberately not real ciphertext. Whether they
// open is the client's business (client_test.go covers that end to end); what
// this test asserts is that the relay carries whatever it is handed, opaque
// and unaltered, which is exactly the property the encryption rests on.
func TestProxyRoundTripRoutesByRequestID(t *testing.T) {
	s := New()
	a := join(t, s, "key-1", "alpha")
	b := join(t, s, "key-1", "bravo")
	next(t, a, "no arrival announce")
	next(t, b, "no sibling announce")

	sealed := []byte("sealed-call-bytes-the-relay-cannot-read")
	send(t, s, a, TypeProxyRequest, ProxyRequest{
		RequestID: "r1", Target: "bravo", Sealed: sealed,
	})

	env := next(t, b, "the target never received the proxy-request")
	var req ProxyRequest
	if env.Type != TypeProxyRequest || env.Into(&req) != nil {
		t.Fatalf("got a %q frame, want a proxy-request", env.Type)
	}
	if req.RequestID != "r1" || req.Target != "bravo" || string(req.Sealed) != string(sealed) {
		t.Fatalf("target received %+v, want the request unchanged", req)
	}

	answer := []byte("sealed-result-bytes")
	send(t, s, b, TypeProxyResponse, ProxyResponse{RequestID: "r1", Sealed: answer})

	env = next(t, a, "the caller never received the proxy-response")
	var resp ProxyResponse
	if env.Type != TypeProxyResponse || env.Into(&resp) != nil {
		t.Fatalf("got a %q frame, want a proxy-response", env.Type)
	}
	if string(resp.Sealed) != string(answer) || resp.Error != "" {
		t.Errorf("caller received %+v, want the target's own answer", resp)
	}
	nothing(t, b, "the response was echoed back to the responder")
}

// TestUnroutableRequestAnswersImmediately: every one of these would otherwise
// make the caller wait out its own timeout to learn something the relay knew
// at once.
func TestUnroutableRequestAnswersImmediately(t *testing.T) {
	cases := []struct {
		name   string
		target string
	}{
		{"nobody is connected under that id", "charlie"},
		{"the caller addressed itself", "alpha"},
		{"the id is on another key", "bravo"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := New()
			a := join(t, s, "key-1", "alpha")
			join(t, s, "key-2", "bravo")

			send(t, s, a, TypeProxyRequest, ProxyRequest{RequestID: "r1", Target: tc.target})

			env := next(t, a, "no answer for an unroutable request")
			var resp ProxyResponse
			if env.Type != TypeProxyResponse || env.Into(&resp) != nil {
				t.Fatalf("got a %q frame, want a proxy-response", env.Type)
			}
			// Error and nothing else: a relay holds no frame key, so the
			// only thing it can ever author is this field. A sealed blob on
			// a relay-authored refusal would mean the relay could seal.
			if resp.RequestID != "r1" || resp.Error == "" || len(resp.Sealed) != 0 {
				t.Errorf("got %+v, want an unsealed refusal carrying an error", resp)
			}
		})
	}
}

// TestFullQueueDropsOnlyThatConnection is the back-pressure decision: an
// instance that cannot drain is disconnected, and everyone else on the key
// keeps being routed. Without it the queue would only move a stall from the
// router into memory growth.
func TestFullQueueDropsOnlyThatConnection(t *testing.T) {
	s := New()
	stuck := newFakeConn()
	stuck.block = make(chan struct{})
	s.Join("key-1", stuck, Announce{InstanceID: "stuck"})
	healthy := join(t, s, "key-1", "healthy")
	next(t, healthy, "no sibling announce")

	// One frame ends up in flight inside Write and queueDepth more fit in its
	// queue, so anything past that has nowhere to go. A handful of senders -
	// comfortably under maxClientsPerKey - each push as many in-flight
	// proxy-requests at "stuck" as maxPendingPerSender allows; together that
	// is well past queueDepth without needing anywhere near
	// maxClientsPerKey distinct connections to get there.
	need := queueDepth + 5
	for i := 0; need > 0; i++ {
		sender := join(t, s, "key-1", fmt.Sprintf("sender-%d", i))
		batch := maxPendingPerSender
		if batch > need {
			batch = need
		}
		for j := 0; j < batch; j++ {
			send(t, s, sender, TypeProxyRequest, ProxyRequest{
				RequestID: fmt.Sprintf("r-%d-%d", i, j), Target: "stuck",
			})
		}
		need -= batch
	}

	if s.Len() == 0 {
		t.Fatal("dropping the stuck connection took the whole key down")
	}
	found := false
	s.mu.Lock()
	for _, cl := range s.keys["key-1"] {
		if cl.announce.InstanceID == "stuck" {
			found = true
		}
	}
	s.mu.Unlock()
	if found {
		t.Error("the over-full connection is still registered")
	}

	close(stuck.block)
	deadline := time.Now().Add(5 * time.Second)
	for !stuck.closed.Load() && time.Now().Before(deadline) {
		time.Sleep(2 * time.Millisecond)
	}
	if !stuck.closed.Load() {
		t.Fatal("the dropped connection was never closed")
	}

	// The healthy sibling is still being routed to, which is the whole point.
	late := join(t, s, "key-1", "late")
	if got := announceOf(t, next(t, late, "the relay stopped routing after a drop"), "sibling announce"); got.InstanceID == "" {
		t.Error("the late joiner was introduced to nobody")
	}
}

// TestReconnectReplacesTheDeadSocket: an instance whose socket died without a
// close frame reconnects while the relay still holds the corpse. If the old
// entry survived, proxy-requests would be routed to a connection nobody is
// reading.
func TestReconnectReplacesTheDeadSocket(t *testing.T) {
	s := New()
	sib := join(t, s, "key-1", "watcher")
	first := join(t, s, "key-1", "alpha")
	next(t, sib, "no arrival announce")
	next(t, first, "no sibling announce")

	second := join(t, s, "key-1", "alpha")
	next(t, sib, "the reconnect was never announced")
	// No presence(offline) for a replaced socket: the instance is still here.
	env := next(t, second, "the reconnected socket was introduced to nobody")
	if got := announceOf(t, env, "sibling announce"); got.InstanceID != "watcher" {
		t.Errorf("reconnected socket was told about %q, want watcher", got.InstanceID)
	}
	if s.Len() != 2 {
		t.Errorf("%d connections registered, want the dead socket replaced rather than joined beside", s.Len())
	}

	send(t, s, sib, TypeProxyRequest, ProxyRequest{RequestID: "r1", Target: "alpha"})
	got := next(t, second, "the request was not routed to the live socket")
	if got.Type != TypeProxyRequest {
		t.Fatalf("live socket got a %q frame, want the proxy-request", got.Type)
	}
	nothing(t, first, "the request was routed to the replaced socket")
}

// TestGarbageFramesAreIgnored: a client one version ahead sends frame types
// this build has never heard of, and that must not take its connection down.
func TestGarbageFramesAreIgnored(t *testing.T) {
	s := New()
	a := join(t, s, "key-1", "alpha")
	b := join(t, s, "key-1", "bravo")
	next(t, a, "no arrival announce")
	next(t, b, "no sibling announce")

	for _, frame := range [][]byte{
		[]byte("not json at all"),
		[]byte(`{"type":"something-new","data":{"x":1}}`),
		frameOf(TypeProxyRequest, ProxyRequest{Target: "bravo"}),            // no request id
		frameOf(TypeProxyRequest, ProxyRequest{RequestID: "r1"}),            // no target
		frameOf(TypeProxyResponse, ProxyResponse{Sealed: []byte("x")}),      // no request id
		frameOf(TypeProxyResponse, ProxyResponse{RequestID: "unasked-for"}), // nobody is waiting
	} {
		s.Route(a, frame)
	}

	if s.Len() != 2 {
		t.Fatalf("%d connections registered, want both still up after garbage", s.Len())
	}
	nothing(t, a, "a garbage frame produced an answer")
	nothing(t, b, "a garbage frame was forwarded to a sibling")

	// The connection still works afterwards, which is what "ignored" means.
	send(t, s, a, TypeProxyRequest, ProxyRequest{RequestID: "r2", Target: "bravo"})
	if got := next(t, b, "the connection stopped working after a garbage frame"); got.Type != TypeProxyRequest {
		t.Errorf("got a %q frame, want the proxy-request", got.Type)
	}
}

// TestResponseFromWrongConnectionIsIgnored: routeResponse only delivers an
// answer to the connection a request was actually routed to. Without this,
// any connection that learns or guesses a live request ID - a same-key
// sibling that was never asked, or one on an entirely different key, since a
// request ID alone carries no key of its own - could hand the requester a
// forged answer.
func TestResponseFromWrongConnectionIsIgnored(t *testing.T) {
	s := New()
	a := join(t, s, "key-1", "alpha")
	b := join(t, s, "key-1", "bravo")
	impostor := join(t, s, "key-1", "impostor")
	outsider := join(t, s, "key-2", "outsider")
	next(t, a, "no bravo arrival announce")
	next(t, a, "no impostor arrival announce")
	next(t, b, "no alpha sibling announce")
	next(t, b, "no impostor arrival announce")
	next(t, impostor, "no alpha sibling announce")
	next(t, impostor, "no bravo sibling announce")

	send(t, s, a, TypeProxyRequest, ProxyRequest{RequestID: "r1", Target: "bravo"})
	next(t, b, "the real target never received the request")

	send(t, s, impostor, TypeProxyResponse, ProxyResponse{RequestID: "r1", Sealed: []byte("forged")})
	send(t, s, outsider, TypeProxyResponse, ProxyResponse{RequestID: "r1", Sealed: []byte("forged")})
	nothing(t, a, "a forged proxy-response was delivered to the requester")

	send(t, s, b, TypeProxyResponse, ProxyResponse{RequestID: "r1", Sealed: []byte("real")})
	env := next(t, a, "the real answer was never delivered")
	var resp ProxyResponse
	if env.Type != TypeProxyResponse || env.Into(&resp) != nil {
		t.Fatalf("got a %q frame, want a proxy-response", env.Type)
	}
	if string(resp.Sealed) != "real" {
		t.Errorf("requester received %q, want the real target's own answer", resp.Sealed)
	}
}

// TestTooManyInFlightRequestsAreRefused: without this, a connection sending
// proxy-requests faster than its target answers them can pile frames into
// that target's own queue until enqueue's overflow policy evicts the
// TARGET - punishing whichever side is being flooded, not the side flooding
// it. The cap has to bite the sender before any excess frame ever reaches
// the target's own queue, which is what this proves.
func TestTooManyInFlightRequestsAreRefused(t *testing.T) {
	s := New()
	a := join(t, s, "key-1", "alpha")
	b := join(t, s, "key-1", "bravo")
	next(t, a, "no arrival announce")
	next(t, b, "no sibling announce")

	for i := 0; i < maxPendingPerSender; i++ {
		send(t, s, a, TypeProxyRequest, ProxyRequest{
			RequestID: fmt.Sprintf("r%d", i), Target: "bravo",
		})
		next(t, b, "a within-budget request was refused")
	}

	send(t, s, a, TypeProxyRequest, ProxyRequest{RequestID: "over-budget", Target: "bravo"})
	nothing(t, b, "an over-budget request reached the target anyway")
	env := next(t, a, "the sender was never told it was refused")
	var resp ProxyResponse
	if env.Type != TypeProxyResponse || env.Into(&resp) != nil {
		t.Fatalf("got a %q frame, want a proxy-response", env.Type)
	}
	if resp.RequestID != "over-budget" || resp.Error == "" || len(resp.Sealed) != 0 {
		t.Errorf("got %+v, want a 429 carrying an error", resp)
	}
}

// TestKeyGroupSizeIsCapped: a relay is meant to connect one person's own
// instances, not an unbounded crowd on a guessed or leaked key - this proves
// Join actually refuses a connection past maxClientsPerKey rather than only
// documenting that it should.
func TestKeyGroupSizeIsCapped(t *testing.T) {
	s := New()
	for i := 0; i < maxClientsPerKey; i++ {
		if !s.Join("key-1", newFakeConn(), Announce{InstanceID: fmt.Sprintf("i%d", i)}) {
			t.Fatalf("join %d was refused before the cap was reached", i)
		}
	}
	if s.Join("key-1", newFakeConn(), Announce{InstanceID: "one-too-many"}) {
		t.Error("a connection past maxClientsPerKey was admitted")
	}
	if s.Len() != maxClientsPerKey {
		t.Errorf("%d connections registered, want exactly maxClientsPerKey", s.Len())
	}
}

// TestTargetDisconnectFailsThePendingRequestFast: if the connection that
// would have answered leaves mid-request, its requester learns that
// immediately instead of waiting out pendingTTL for an answer that can now
// never arrive.
func TestTargetDisconnectFailsThePendingRequestFast(t *testing.T) {
	s := New()
	a := join(t, s, "key-1", "alpha")
	b := join(t, s, "key-1", "bravo")
	next(t, a, "no arrival announce")
	next(t, b, "no sibling announce")

	send(t, s, a, TypeProxyRequest, ProxyRequest{RequestID: "r1", Target: "bravo"})
	next(t, b, "the target never received the request")

	s.Leave(b)

	env := next(t, a, "the pending request was never failed fast")
	var resp ProxyResponse
	if env.Type != TypeProxyResponse || env.Into(&resp) != nil {
		t.Fatalf("got a %q frame first, want the failed proxy-response ahead of the presence frame", env.Type)
	}
	if resp.RequestID != "r1" || resp.Error == "" || len(resp.Sealed) != 0 {
		t.Errorf("got %+v, want an unsealed refusal carrying an error", resp)
	}

	presenceEnv := next(t, a, "no presence frame after the disconnect")
	var p Presence
	if presenceEnv.Type != TypePresence || presenceEnv.Into(&p) != nil || p.InstanceID != "bravo" || p.Online {
		t.Errorf("got a %+v presence frame, want bravo offline", presenceEnv)
	}
}

// TestReconnectFailsThePendingRequestFast: when a target reconnects (Join
// replacing its stale connection) before answering a request that was routed
// to the old socket, that answer can now never arrive there either - the
// requester is told immediately rather than left to time out.
func TestReconnectFailsThePendingRequestFast(t *testing.T) {
	s := New()
	a := join(t, s, "key-1", "alpha")
	join(t, s, "key-1", "bravo")
	next(t, a, "no arrival announce")

	send(t, s, a, TypeProxyRequest, ProxyRequest{RequestID: "r1", Target: "bravo"})

	join(t, s, "key-1", "bravo") // reconnect, replaces the connection r1 was routed to

	env := next(t, a, "the pending request was never failed fast on reconnect")
	var resp ProxyResponse
	if env.Type != TypeProxyResponse || env.Into(&resp) != nil {
		t.Fatalf("got a %q frame, want the failed proxy-response", env.Type)
	}
	if resp.RequestID != "r1" || resp.Error == "" || len(resp.Sealed) != 0 {
		t.Errorf("got %+v, want an unsealed refusal carrying an error", resp)
	}
}

// TestEnvelopeShapeIsStable pins the JSON other implementations (the mobile
// companion app, a future client package) are written against. A field
// renamed by accident compiles fine and breaks every peer at once, so the
// wire bytes themselves are asserted here rather than only round-tripped
// through this package's own types.
func TestEnvelopeShapeIsStable(t *testing.T) {
	cases := []struct {
		name string
		typ  string
		data any
		want string
	}{
		{
			"hello",
			TypeHello,
			Hello{Key: "s3cret", Announce: Announce{InstanceID: "alpha", Name: "NAS", Deployment: "container"}},
			`{"type":"hello","data":{"key":"s3cret","announce":{"instanceId":"alpha","name":"NAS","deployment":"container"}}}`,
		},
		{
			"announce",
			TypeAnnounce,
			Announce{InstanceID: "alpha", Name: "NAS", Deployment: "container"},
			`{"type":"announce","data":{"instanceId":"alpha","name":"NAS","deployment":"container"}}`,
		},
		{
			"presence",
			TypePresence,
			Presence{InstanceID: "alpha"},
			`{"type":"presence","data":{"instanceId":"alpha","online":false}}`,
		},
		{
			"proxy-request",
			TypeProxyRequest,
			ProxyRequest{RequestID: "r1", Target: "bravo", Sealed: []byte("hi")},
			`{"type":"proxy-request","data":{"requestId":"r1","target":"bravo","sealed":"aGk="}}`,
		},
		{
			"proxy-response",
			TypeProxyResponse,
			ProxyResponse{RequestID: "r1", Sealed: []byte("hi")},
			`{"type":"proxy-response","data":{"requestId":"r1","sealed":"aGk="}}`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := Encode(tc.typ, tc.data)
			if err != nil {
				t.Fatalf("encode: %v", err)
			}
			if string(got) != tc.want {
				t.Errorf("wire shape changed:\n got %s\nwant %s", got, tc.want)
			}
			env, err := Decode(got)
			if err != nil || env.Type != tc.typ {
				t.Fatalf("decode: %v, type %q", err, env.Type)
			}
			if !json.Valid(env.Data) {
				t.Error("payload is not valid JSON on its own")
			}
		})
	}
}
