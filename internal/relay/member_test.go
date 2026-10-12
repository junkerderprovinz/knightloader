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
		From: "phone-member", Kind: KindMobile, Sent: 1760000000,
	}
)

const (
	vectorKey = "GX9rI+FshTLGq8g4+s1ep4m+DHaykgM0A5v6iz02jWE="
	vectorSig = "2DRpY1IMpnl6W0mqYgRv6Nq2SDimJFXhcYv2I4d0LDAPGzf3vdgfuf2ty0nb1bchk9xZXmMdHkTGFQOHhGvhDg=="
)

// The ids the vector seed's key makes, which the app's and the extension's
// ports are held to as well.
const (
	vectorInstanceID = "b600306cfa76723fdec395e53a9b3d9fdb78b1e2"
	vectorPhoneID    = "phone-b600306cfa76723fdec395e53a9b3d9fdb78b1e2d7a23c32fcbcd2dc6d0c4092"
	vectorBrowserID  = "browser-b600306cfa76723fdec395e53a9b3d9fdb78b1e2d7a23c32fcbcd2dc6d0c4092"
)

func TestAMembersIDIsMadeFromItsKey(t *testing.T) {
	key := ed25519.NewKeyFromSeed(vectorSeed).Public().(ed25519.PublicKey)
	for kind, want := range map[string]string{KindInstance: vectorInstanceID, KindMobile: vectorPhoneID, KindExtension: vectorBrowserID} {
		if got := MemberID(kind, key); got != want {
			t.Errorf("MemberID(%s) = %s, want %s", kind, got, want)
		}
	}
	if !ValidInstanceID(vectorInstanceID) || len(vectorBrowserID) > maxClientIDBytes {
		t.Error("a member id does not fit the shape its kind is listed under")
	}
	other := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{1}, ed25519.SeedSize)).Public().(ed25519.PublicKey)
	if MemberID(KindMobile, other) == vectorPhoneID {
		t.Error("two keys make one id")
	}
	if MemberID("robot", key) != "" || MemberID(KindMobile, key[:10]) != "" {
		t.Error("an unknown kind or a short key made an id")
	}
}

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
		"id":     func(c *ProxyCall) { c.ID = "req-8" },
		"method": func(c *ProxyCall) { c.Method = "GET" },
		"path":   func(c *ProxyCall) { c.Path = "/api/tasks" },
		"body":   func(c *ProxyCall) { c.Body = []byte(`{}`) },
		"from":   func(c *ProxyCall) { c.From = "phone-other" },
		"kind":   func(c *ProxyCall) { c.Kind = KindInstance },
		"time":   func(c *ProxyCall) { c.Sent++ },
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

// TestManyFullRecordsKeepNoOtherKeyOut fills the records of seventeen keys.
// Each full record refuses only its own key's calls.
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

// The answer and the hello the vector seed signs, which the app's and the
// extension's ports check and make. The answer is to the call that arrived as
// vectorSealedCall.
var (
	vectorResult   = ProxyResult{Status: 410, Body: []byte("removed")}
	vectorRelayKey = "relay-key-of-the-vector-0123456789"
	// vectorChallenge is the challenge the vector hello answers, in base64.
	vectorChallenge = "XFxcXFxcXFxcXFxcXFxcXFxcXFxcXFxcXFxcXFxcXFw="
	// vectorSealedCall stands for a call as it arrived sealed, in base64. The
	// answer names it by the hash of these bytes alone, so any bytes serve.
	vectorSealedCall = "MzMzMzMzMzMzMzMzMzMzMzMzMzMzMzMzMzMzMzMzMzMzMzMzMzMzMw=="
)

const (
	vectorResultSig = "efDaJoYEIIwKg5Z98dMHRSb3VtLTEx/lrphppJjTcHvDTTdEHRa9WZuRGNfTzdqgIXFzVed4Hq/TdRjWFncHBQ=="
	vectorHelloSig  = "vIshHdbAJQWFYoDinfE3ip4WFg11J4hsA7194C9+mS8w8HK58oRwEsCg4NubHVA/C+tYThl57pRl25tQNKHsBA=="
)

// decoded is the bytes of base64 text s.
func decoded(t *testing.T, s string) []byte {
	t.Helper()
	b, err := base64.StdEncoding.DecodeString(s)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestTheAnswerAndHelloVectorsStayTheSame(t *testing.T) {
	member := ed25519.NewKeyFromSeed(vectorSeed)
	signed := SignResult(member, vectorCall.ID, vectorInstanceID, decoded(t, vectorSealedCall), vectorResult)
	if got := base64.StdEncoding.EncodeToString(signed.Sig); got != vectorResultSig {
		t.Errorf("answer signature = %s, want %s; the app and the extension check the old message", got, vectorResultSig)
	}
	proof := ProveID(member, KindMobile, vectorRelayKey, vectorPhoneID, decoded(t, vectorChallenge))
	if got := base64.StdEncoding.EncodeToString(proof.Sig); got != vectorHelloSig {
		t.Errorf("hello signature = %s, want %s; the app proves its id with the old message", got, vectorHelloSig)
	}
}

// TestAnAnswerCountsOnlySignedByTheTargetsKey: whoever holds the words can
// seal an answer, such as a "removed" that makes a phone forget its group, but
// only the target can sign one.
func TestAnAnswerCountsOnlySignedByTheTargetsKey(t *testing.T) {
	target := ed25519.NewKeyFromSeed(vectorSeed)
	forger := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{9}, ed25519.SeedSize))
	call := decoded(t, vectorSealedCall)
	alteredCall := bytes.Clone(call)
	alteredCall[len(alteredCall)-1] ^= 1
	signed := SignResult(target, "r1", vectorInstanceID, call, vectorResult)
	if !VerifyResult("r1", vectorInstanceID, call, signed) {
		t.Fatal("the target's own answer does not verify")
	}
	restated := signed
	restated.Status = http.StatusOK
	for name, r := range map[string]ProxyResult{
		"unsigned":                vectorResult,
		"signed by another key":   SignResult(forger, "r1", vectorInstanceID, call, vectorResult),
		"with its status changed": restated,
		"from another target":     SignResult(forger, "r1", MemberID(KindInstance, forger.Public().(ed25519.PublicKey)), call, vectorResult),
		"to another request":      SignResult(target, "r2", vectorInstanceID, call, vectorResult),
		"to another call":         SignResult(target, "r1", vectorInstanceID, []byte("another call"), vectorResult),
		"to the call altered":     SignResult(target, "r1", vectorInstanceID, alteredCall, vectorResult),
	} {
		if VerifyResult("r1", vectorInstanceID, call, r) {
			t.Errorf("an answer %s verifies as the target's", name)
		}
	}
}

func TestAHelloProvesOnlyTheIDItsKeyMakes(t *testing.T) {
	member := ed25519.NewKeyFromSeed(vectorSeed)
	challenge := decoded(t, vectorChallenge)
	hello := func(id string, p *IDProof) Hello {
		return Hello{Key: vectorRelayKey, Announce: Announce{InstanceID: id}, Proof: p}
	}
	if !hello(vectorPhoneID, ProveID(member, KindMobile, vectorRelayKey, vectorPhoneID, challenge)).proves(challenge) {
		t.Fatal("a phone's proof of its own id does not hold")
	}
	other := MemberID(KindMobile, keyNumber(7))
	for name, h := range map[string]Hello{
		"without a proof":         hello(vectorPhoneID, nil),
		"for another id":          hello(other, ProveID(member, KindMobile, vectorRelayKey, other, challenge)),
		"on another relay key":    hello(vectorPhoneID, ProveID(member, KindMobile, "another-relay-key-0123456789abcdef", vectorPhoneID, challenge)),
		"for another connection":  hello(vectorPhoneID, ProveID(member, KindMobile, vectorRelayKey, vectorPhoneID, make([]byte, challengeSize))),
		"under another kind's id": hello(vectorInstanceID, ProveID(member, KindMobile, vectorRelayKey, vectorInstanceID, challenge)),
	} {
		if h.proves(challenge) {
			t.Errorf("a hello %s proves its id", name)
		}
	}
}

// vectorFingerprint is the vector key's fingerprint, which the app and the
// extension show for their own key in the same form.
const vectorFingerprint = "b600 306c fa76 723f"

func TestTheFingerprintVectorStaysTheSame(t *testing.T) {
	if got := Fingerprint(ed25519.NewKeyFromSeed(vectorSeed).Public().(ed25519.PublicKey)); got != vectorFingerprint {
		t.Errorf("fingerprint = %q, want %q; the app and the extension show their keys in another form", got, vectorFingerprint)
	}
}
