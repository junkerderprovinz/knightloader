package relay

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"net/http"
	"testing"
	"time"
)

// The member-call vector the app's and the extension's ports are held to
// (mobile/check-member-key.mjs, extension/check-relay-seal.mjs). Ed25519 is
// deterministic, so a fixed seed and a fixed call give one signature.
var (
	vectorSeed   = bytes.Repeat([]byte{0x2a}, ed25519.SeedSize)
	vectorTarget = "office"
	vectorCall   = ProxyCall{
		ID: "req-7", Method: "POST", Path: "/api/links", Body: []byte(`{"urls":["https://example.com/a"]}`),
		Authorization: "Bearer t0k", From: "phone-member", Kind: KindMobile, Sent: 1760000000,
	}
)

const (
	vectorKey = "GX9rI+FshTLGq8g4+s1ep4m+DHaykgM0A5v6iz02jWE="
	vectorSig = "kQ04zLmeEVum6FSrH9e85KeTQfmAqfHo+7DEmJ7v124FUwniOjZQp2BTTiFkRn3zKmIIkdPKT7Sj2igK/+WaBQ=="
)

func TestTheMemberCallVectorStaysTheSame(t *testing.T) {
	member := ed25519.NewKeyFromSeed(vectorSeed)
	sig := ed25519.Sign(member, CallMessage(vectorTarget, vectorCall))
	if got := base64.StdEncoding.EncodeToString(member.Public().(ed25519.PublicKey)); got != vectorKey {
		t.Errorf("public key = %s, want %s", got, vectorKey)
	}
	if got := base64.StdEncoding.EncodeToString(sig); got != vectorSig {
		t.Errorf("signature = %s, want %s; the app and the extension sign the old message", got, vectorSig)
	}
}

func TestEveryFieldOfACallIsSigned(t *testing.T) {
	base := CallMessage(vectorTarget, vectorCall)
	changed := map[string]func(*ProxyCall){
		"id":            func(c *ProxyCall) { c.ID = "req-8" },
		"method":        func(c *ProxyCall) { c.Method = "GET" },
		"path":          func(c *ProxyCall) { c.Path = "/api/tasks" },
		"body":          func(c *ProxyCall) { c.Body = []byte(`{}`) },
		"authorization": func(c *ProxyCall) { c.Authorization = "" },
		"from":          func(c *ProxyCall) { c.From = "phone-other" },
		"kind":          func(c *ProxyCall) { c.Kind = KindInstance },
		"time":          func(c *ProxyCall) { c.Sent++ },
		// A byte moved from one field to the next must not give the same
		// message.
		"boundary": func(c *ProxyCall) { c.Method, c.Path = "POST/", "api/links" },
	}
	for name, change := range changed {
		c := vectorCall
		change(&c)
		if bytes.Equal(CallMessage(vectorTarget, c), base) {
			t.Errorf("changing the %s leaves the signed message as it was", name)
		}
	}
	if bytes.Equal(CallMessage("bravo", vectorCall), base) {
		t.Error("the target is not part of the signed message")
	}
}

func TestASignedCallOpensAndAnAlteredOneDoesNot(t *testing.T) {
	member := ed25519.NewKeyFromSeed(vectorSeed)
	call := ProxyCall{Method: "GET", Path: "/api/tasks", From: "phone-member", Kind: KindMobile}
	sealed, err := SealSignedCall(testFrameKey, member, "r1", alphaID, call)
	if err != nil {
		t.Fatal(err)
	}
	opened, err := OpenCall(testFrameKey, "r1", alphaID, sealed)
	if err != nil || !opened.Signed() || !bytes.Equal(opened.Key, member.Public().(ed25519.PublicKey)) {
		t.Fatalf("opened %+v, %v; want the call with its key", opened, err)
	}

	// Anybody holding the phrase can seal a call again, so an altered copy
	// with the old signature is what a member would try.
	altered := opened
	altered.Path = "/api/settings"
	plain := mustSeal(t, "r1", alphaID, altered)
	if _, err := OpenCall(testFrameKey, "r1", alphaID, plain); !errors.Is(err, ErrSignature) {
		t.Errorf("an altered signed call opened with %v, want ErrSignature", err)
	}
	other := opened
	other.Key = ed25519.NewKeyFromSeed(bytes.Repeat([]byte{1}, 32)).Public().(ed25519.PublicKey)
	if _, err := OpenCall(testFrameKey, "r1", alphaID, mustSeal(t, "r1", alphaID, other)); !errors.Is(err, ErrSignature) {
		t.Errorf("a signature presented with another key opened with %v, want ErrSignature", err)
	}
	short := opened
	short.Key = short.Key[:10]
	if _, err := OpenCall(testFrameKey, "r1", alphaID, mustSeal(t, "r1", alphaID, short)); !errors.Is(err, ErrSignature) {
		t.Errorf("a key of the wrong length opened with %v, want ErrSignature", err)
	}
}

// mustSeal seals call as it is, signature and all, the way a member that
// alters a call it captured would.
func mustSeal(t *testing.T, requestID, target string, call ProxyCall) []byte {
	t.Helper()
	plain, err := json.Marshal(call)
	if err != nil {
		t.Fatal(err)
	}
	sealed, err := seal(testFrameKey, requestAAD(requestID, target), plain)
	if err != nil {
		t.Fatal(err)
	}
	return sealed
}

func TestOneMembersCallsCannotUseUpAnothersIDs(t *testing.T) {
	g := NewReplayGuard()
	now := time.Unix(1760000000, 0)
	g.now = func() time.Time { return now }
	real := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{2}, 32)).Public().(ed25519.PublicKey)
	forger := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{3}, 32)).Public().(ed25519.PublicKey)

	if !g.Admit(ProxyCall{ID: "next", Sent: now.Unix(), Key: forger}) {
		t.Fatal("the forger's own call was refused")
	}
	if !g.Admit(ProxyCall{ID: "next", Sent: now.Unix(), Key: real}) {
		t.Error("a member's call was refused because another key sent its id first")
	}
	if g.Admit(ProxyCall{ID: "next", Sent: now.Unix(), Key: real}) {
		t.Error("the same member's call ran twice")
	}

	for i := 0; i < maxSeenPerMember; i++ {
		g.Admit(ProxyCall{ID: string(rune(0x1000 + i)), Sent: now.Unix(), Key: forger})
	}
	if g.Admit(ProxyCall{ID: "one more", Sent: now.Unix(), Key: forger}) {
		t.Error("one key kept more ids on record than maxSeenPerMember")
	}
	if !g.Admit(ProxyCall{ID: "after the flood", Sent: now.Unix(), Key: real}) {
		t.Error("a key that filled its own record kept another member out")
	}
}

// keyNumber is a distinct 32-byte value for each n, which is all the guard
// reads of a key.
func keyNumber(n int) []byte {
	k := make([]byte, ed25519.PublicKeySize)
	binary.BigEndian.PutUint64(k, uint64(n))
	return k
}

func TestACallWithoutAKeyIsNeverRecorded(t *testing.T) {
	g := NewReplayGuard()
	now := time.Unix(1760000000, 0)
	g.now = func() time.Time { return now }
	for i := 0; i < 10; i++ {
		if g.Admit(ProxyCall{ID: string(rune(0x1000 + i)), Sent: now.Unix(), From: "phone-flood"}) {
			t.Fatal("a call without a key was admitted")
		}
	}
	if n := g.Len(); n != 0 {
		t.Errorf("the guard kept %d ids of calls without a key, want none", n)
	}
}

// TestManyKeysCostTheGuardLittle admits one call from each of many keys. A
// guard that looks at every key on every call takes seconds for this.
func TestManyKeysCostTheGuardLittle(t *testing.T) {
	g := NewReplayGuard()
	now := time.Unix(1760000000, 0)
	g.now = func() time.Time { return now }
	const keys = 40000
	start := time.Now()
	for i := range keys {
		if !g.Admit(ProxyCall{ID: "only", Sent: now.Unix(), Key: keyNumber(i)}) {
			t.Fatalf("call %d of a new key was refused", i)
		}
	}
	if took := time.Since(start); took > 2*time.Second {
		t.Errorf("admitting %d keys took %s", keys, took)
	}
	now = now.Add(2*ClockSkew + time.Second)
	if !g.Admit(ProxyCall{ID: "later", Sent: now.Unix(), Key: keyNumber(0)}) || len(g.seen) != 1 {
		t.Errorf("%d keys on record once the old ids ran out, want only the newest", len(g.seen))
	}
}

// TestManyFullRecordsKeepNoOtherKeyOut fills the records of more keys than
// it once took to fill the whole guard. Each full record refuses only its own
// key's calls.
func TestManyFullRecordsKeepNoOtherKeyOut(t *testing.T) {
	g := NewReplayGuard()
	now := time.Unix(1760000000, 0)
	g.now = func() time.Time { return now }
	for k := range 17 {
		for i := range maxSeenPerMember {
			g.Admit(ProxyCall{ID: string(rune(0x1000 + i)), Sent: now.Unix(), Key: keyNumber(k)})
		}
	}
	if g.Admit(ProxyCall{ID: "over", Sent: now.Unix(), Key: keyNumber(0)}) {
		t.Error("a full record took one more id instead of refusing it")
	}
	if !g.Admit(ProxyCall{ID: "member", Sent: now.Unix(), Key: keyNumber(1 << 20)}) {
		t.Error("another key's call was refused because other records were full")
	}
	now = now.Add(2*ClockSkew + time.Second)
	if !g.Admit(ProxyCall{ID: "over", Sent: now.Unix(), Key: keyNumber(0)}) {
		t.Error("the guard still refused a key's calls once its ids had run out")
	}
}

func TestABrokenSignatureIsAnsweredAndNeverServed(t *testing.T) {
	addr, _ := relayOn(t, "127.0.0.1:0")
	served := make(chan string, 2)
	alpha := startClient(t, addr, "shared-relay-test-key-0123456789ab", alphaID, func(_ context.Context, call ProxyCall) (int, []byte) {
		served <- call.Path
		return http.StatusOK, nil
	})
	waitFor(t, "alpha to connect", alpha.Connected)
	bravo := dialInstance(t, "ws://"+addr+connectPath, "shared-relay-test-key-0123456789ab", bravoID)
	readFrame(t, bravo, TypeAnnounce)

	member := ed25519.NewKeyFromSeed(vectorSeed)
	sealed, err := SealSignedCall(testFrameKey, member, "r1", alphaID, ProxyCall{Method: http.MethodGet, Path: "/api/tasks", From: bravoID, Kind: KindInstance})
	if err != nil {
		t.Fatal(err)
	}
	call := openFrom(t, "r1", alphaID, sealed)
	call.Path = "/api/queue/stop"
	writeFrame(t, bravo, TypeProxyRequest, ProxyRequest{RequestID: "r1", Target: alphaID, Sealed: mustSeal(t, "r1", alphaID, call)})

	var resp ProxyResponse
	if err := readFrame(t, bravo, TypeProxyResponse).Into(&resp); err != nil {
		t.Fatal(err)
	}
	res := openResultFrom(t, "r1", resp.Sealed)
	if res.Status != http.StatusForbidden || !bytes.Contains(res.Body, []byte(`"memberSignature"`)) {
		t.Fatalf("a broken signature got %d %s, want 403 with its code", res.Status, res.Body)
	}
	select {
	case path := <-served:
		t.Fatalf("the altered call to %s was served", path)
	case <-time.After(100 * time.Millisecond):
	}
}

// TestACallIsServedWithTheConnectionThatSentIt checks that the instance
// serving a call learns which relay connection it came through, as the relay
// says and not as the call claims.
func TestACallIsServedWithTheConnectionThatSentIt(t *testing.T) {
	addr, _ := relayOn(t, "127.0.0.1:0")
	sources := make(chan string, 1)
	alpha := startClient(t, addr, "shared-relay-test-key-0123456789ab", alphaID, func(ctx context.Context, _ ProxyCall) (int, []byte) {
		sources <- SourceOf(ctx)
		return http.StatusOK, nil
	})
	waitFor(t, "alpha to connect", alpha.Connected)
	bravo := dialInstance(t, "ws://"+addr+connectPath, "shared-relay-test-key-0123456789ab", bravoID)
	readFrame(t, bravo, TypeAnnounce)
	call := ProxyCall{Method: http.MethodGet, Path: "/api/tasks", ID: "r1", Sent: time.Now().Unix()}
	writeFrame(t, bravo, TypeProxyRequest, ProxyRequest{RequestID: "r1", Target: alphaID, Sender: charlieID, Sealed: mustSeal(t, "r1", alphaID, call)})
	select {
	case got := <-sources:
		if got != "relay\x00"+bravoID {
			t.Errorf("the call was served as from %q, want bravo's connection", got)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the call was never served")
	}
}
