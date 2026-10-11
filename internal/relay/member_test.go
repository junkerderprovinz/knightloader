package relay

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/base64"
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

func TestAnAnnounceSaysWhetherItsMemberSigns(t *testing.T) {
	for _, signs := range []bool{true, false} {
		wire, err := sealAnnounce(testFrameKey, Announce{InstanceID: alphaID, Name: "nas", Signs: signs})
		if err != nil {
			t.Fatal(err)
		}
		if wire.Signs {
			t.Error("the flag travels in the clear, where a relay can set it")
		}
		if got := openAnnounce(testFrameKey, wire); got.Signs != signs {
			t.Errorf("an announce sealed with signs=%v opened as %v", signs, got.Signs)
		}
	}
}
