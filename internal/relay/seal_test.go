package relay

import (
	"bytes"
	"encoding/base64"
	"encoding/hex"
	"net/http"
	"testing"
)

// TestSealedCallRoundTrips is the baseline: what goes in comes out, byte for
// byte, including the two fields most worth not leaking.
func TestSealedCallRoundTrips(t *testing.T) {
	key := DeriveFrameKey([]byte("a secret"))
	want := ProxyCall{
		Method:        http.MethodPost,
		Path:          "/api/links",
		Body:          []byte(`{"url":"https://example.invalid/holiday-photos.zip"}`),
		Authorization: "Bearer a-real-looking-token",
	}
	sealed, err := SealCall(key, "r1", "bravo", want)
	if err != nil {
		t.Fatalf("seal: %v", err)
	}
	got, err := OpenCall(key, "r1", "bravo", sealed)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if got.Method != want.Method || got.Path != want.Path || got.Authorization != want.Authorization {
		t.Errorf("opened %+v, want %+v", got, want)
	}
	if !bytes.Equal(got.Body, want.Body) {
		t.Errorf("body opened as %s, want it byte for byte", got.Body)
	}
}

// TestSealedFrameHidesItsContents is the actual claim the connection card
// makes, asserted rather than assumed: none of what a relay operator would
// most want appears anywhere in the bytes that cross their machine.
func TestSealedFrameHidesItsContents(t *testing.T) {
	key := DeriveFrameKey([]byte("a secret"))
	sealed, err := SealCall(key, "r1", "bravo", ProxyCall{
		Method:        http.MethodPost,
		Path:          "/api/links",
		Body:          []byte(`{"url":"https://example.invalid/holiday-photos.zip"}`),
		Authorization: "Bearer a-real-looking-token",
	})
	if err != nil {
		t.Fatalf("seal: %v", err)
	}
	for _, secret := range []string{
		"/api/links",
		"holiday-photos",
		"example.invalid",
		"Bearer",
		"a-real-looking-token",
		http.MethodPost,
	} {
		if bytes.Contains(sealed, []byte(secret)) {
			t.Errorf("the sealed frame contains %q in the clear", secret)
		}
	}
}

// TestRelayKeyDoesNotYieldTheFrameKey is the separation the whole design
// rests on. A relay is HANDED DeriveKey's output in every hello frame; if the
// frame key could be computed from it, sealing would be theatre.
func TestRelayKeyDoesNotYieldTheFrameKey(t *testing.T) {
	secret := []byte("a secret")
	relayKey := DeriveKey(secret)
	frameKey := DeriveFrameKey(secret)

	// The obvious wrong implementation: the same hash without a separate
	// domain would make these two the same bytes.
	if hex.EncodeToString(frameKey) == relayKey {
		t.Fatal("the frame key and the relay key are the same value - the relay would hold both")
	}
	// And the second obvious one: deriving the frame key FROM the relay key
	// rather than from the secret.
	if bytes.Equal(frameKey, FrameKeyFromRelayKey(relayKey)) {
		t.Fatal("the frame key is derivable from the relay key the relay is already given")
	}
	if len(frameKey) != 32 {
		t.Fatalf("frame key is %d bytes, want 32 for AES-256", len(frameKey))
	}
}

// TestSealIsBoundToItsRouting: a relay routes on RequestID and Target, which
// therefore travel in the clear. Binding them into the AEAD is what stops a
// relay from delivering a sealed call to an instance it was not addressed to
// and having it open there.
func TestSealIsBoundToItsRouting(t *testing.T) {
	key := DeriveFrameKey([]byte("a secret"))
	sealed, err := SealCall(key, "r1", "bravo", ProxyCall{Method: "GET", Path: "/api/tasks"})
	if err != nil {
		t.Fatalf("seal: %v", err)
	}
	if _, err := OpenCall(key, "r1", "charlie", sealed); err == nil {
		t.Error("a call addressed to bravo opened as one addressed to charlie")
	}
	if _, err := OpenCall(key, "r2", "bravo", sealed); err == nil {
		t.Error("a call opened under a request id it was not sealed with")
	}
}

// TestSealedResultIsNotAcceptedAsACall: the two directions use different
// labels in their additional data, so a captured answer cannot be replayed as
// a question - which would otherwise let a relay turn a peer's own reply into
// a request addressed back at it.
func TestSealedResultIsNotAcceptedAsACall(t *testing.T) {
	key := DeriveFrameKey([]byte("a secret"))
	sealed, err := SealResult(key, "r1", ProxyResult{Status: 200, Body: []byte("ok")})
	if err != nil {
		t.Fatalf("seal: %v", err)
	}
	if _, err := OpenCall(key, "r1", "bravo", sealed); err == nil {
		t.Error("a sealed result opened as a sealed call")
	}
}

// TestWrongKeyAndTamperingFail: a peer on the same relay key that does not
// hold the group's secret, and a relay that edits a frame in flight, must
// both produce nothing openable rather than something plausible.
func TestWrongKeyAndTamperingFail(t *testing.T) {
	key := DeriveFrameKey([]byte("a secret"))
	other := DeriveFrameKey([]byte("a different secret"))
	sealed, err := SealCall(key, "r1", "bravo", ProxyCall{Method: "GET", Path: "/api/tasks"})
	if err != nil {
		t.Fatalf("seal: %v", err)
	}

	if _, err := OpenCall(other, "r1", "bravo", sealed); err == nil {
		t.Error("a frame opened under a key it was not sealed with")
	}

	for _, at := range []int{0, nonceLen, len(sealed) - 1} {
		tampered := bytes.Clone(sealed)
		tampered[at] ^= 0xff
		if _, err := OpenCall(key, "r1", "bravo", tampered); err == nil {
			t.Errorf("a frame with byte %d flipped still opened", at)
		}
	}

	if _, err := OpenCall(key, "r1", "bravo", sealed[:nonceLen-1]); err == nil {
		t.Error("a frame too short to hold a nonce still opened")
	}
	if _, err := OpenCall(key, "r1", "bravo", nil); err == nil {
		t.Error("an absent frame opened - an unsealed call must never look like a valid one")
	}
}

// TestNonceIsNotReused: two seals of identical plaintext under identical key
// and routing must differ. A repeated nonce under AES-GCM is not a weakness,
// it is a break - the keystream repeats and the authentication key falls out.
func TestNonceIsNotReused(t *testing.T) {
	key := DeriveFrameKey([]byte("a secret"))
	call := ProxyCall{Method: "GET", Path: "/api/tasks"}
	seen := map[string]bool{}
	for i := 0; i < 128; i++ {
		sealed, err := SealCall(key, "r1", "bravo", call)
		if err != nil {
			t.Fatalf("seal: %v", err)
		}
		nonce := string(sealed[:nonceLen])
		if seen[nonce] {
			t.Fatal("the same nonce was used twice for the same key")
		}
		seen[nonce] = true
	}
}

// TestOpensAFrameSealedByTheMobilePort is the test that catches the failure
// nothing else here can: the phone carries its own TypeScript implementation
// of this format (mobile/src/api/relayFrame.ts), and every other test in this
// package checks Go against Go.
//
// The frame below was produced by that TypeScript code and pasted in - not
// generated by this package - so it fails if either side changes the framing,
// the additional-data layout, the domain string, or the JSON field names.
// Which is exactly the change that would otherwise ship as "the phone
// connects to the group and every instance ignores it".
//
// Its nonce is a fixed run of 0x03 rather than a random one, because a vector
// has to be reproducible. Nothing in production seals with a fixed nonce; see
// seal() and relayFrame.ts's own seal, which both take one from a real random
// source.
func TestOpensAFrameSealedByTheMobilePort(t *testing.T) {
	key := DeriveFrameKey([]byte("cross-implementation vector"))
	sealed, err := base64.StdEncoding.DecodeString(
		"AwMDAwMDAwMDAwMDAXqxEhwJlzwnjSCTaEIl6yMvi98geuQjUvrQqyH8fBq7GmJaDPGtnxmikd1OKaYoB82eYg==")
	if err != nil {
		t.Fatalf("the vector itself is not valid base64: %v", err)
	}

	call, err := OpenCall(key, "req-2", "alpha", sealed)
	if err != nil {
		t.Fatalf("could not open a frame the mobile port sealed: %v", err)
	}
	if call.Method != "GET" || call.Path != "/api/tasks" {
		t.Errorf("opened %+v, want the GET /api/tasks the phone sealed", call)
	}

	// The binding has to hold across implementations too, or the phone would
	// be producing frames a relay could redirect.
	if _, err := OpenCall(key, "req-2", "charlie", sealed); err == nil {
		t.Error("a mobile-sealed call opened under a target it was not addressed to")
	}
}

// TestFrameKeyVectors pins the derivation to fixed bytes.
//
// This is the one test in the file that would catch the worst possible
// change: the mobile app carries its own TypeScript port of this derivation
// (mobile/src/relay/), and the two have to agree byte for byte or a phone
// joins the group and can talk to nobody. A refactor that "harmlessly"
// reordered the domain and the secret would keep every other test here
// passing while silently splitting the two implementations apart.
//
// The two values below were NOT copied out of a failing run of this test,
// which would make it assert only that the code still does what it does.
// They are the plain SHA-256 of the domain string concatenated with the
// secret, confirmed against sha256sum:
//
//	printf 'knightloader/relay/frame-key/v1' | sha256sum
//	printf 'knightloader/relay/frame-key/v1knightloader' | sha256sum
func TestFrameKeyVectors(t *testing.T) {
	cases := []struct {
		secret string
		want   string
	}{
		{"", "a94ace9821d4aa96f2f8c4bb250e2937a59655f53a14ba74c3e589d5f71f17b9"},
		{"knightloader", "e04113452443266a47195c6b866225201d6312a88a6cfc0e2af1ecf5a4beeb88"},
	}
	for _, tc := range cases {
		got := hex.EncodeToString(DeriveFrameKey([]byte(tc.secret)))
		if got != tc.want {
			t.Errorf("DeriveFrameKey(%q) = %s, want %s", tc.secret, got, tc.want)
		}
	}
}
