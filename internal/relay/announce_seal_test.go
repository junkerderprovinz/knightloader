package relay

// The announce seal, and the promise it exists to make true.
//
// jdp, 2026-09-03, having read the previous day's audit: "Können wir nicht
// alle daten die das relay erreichen verschlüsseln?" Most of them, and this
// is the part that had been travelling in the clear for no reason at all: the
// instance's name, which falls back to os.Hostname(), so a machine named
// after its owner introduced its owner to the relay operator by name on every
// single connection.
//
// The server was read before the change rather than assumed about. It touches
// Announce.InstanceID in exactly three places - matching a reconnect,
// addressing a presence frame, and picking a proxy target - and has never
// read Name, Deployment or Client. That is what makes this safe to seal and
// what these tests pin: the relay keeps what it routes on, and gets nothing
// else.

import (
	"encoding/base64"
	"encoding/json"
	"testing"
	"time"
)

// TestAnnounceRoundTripsThroughTheSeal is the base case: what goes in comes
// out, and the wire form in between carries none of it.
func TestAnnounceRoundTripsThroughTheSeal(t *testing.T) {
	in := Announce{InstanceID: "alpha", Name: "BOTTICH", Deployment: "container", Client: true}

	wire, err := sealAnnounce(testFrameKey, in)
	if err != nil {
		t.Fatalf("sealAnnounce: %v", err)
	}
	if wire.Name != "" || wire.Deployment != "" || wire.Client {
		t.Errorf("wire form still carries identity: %+v", wire)
	}
	if wire.InstanceID != "alpha" {
		t.Errorf("wire form lost the routing field: %+v", wire)
	}
	if len(wire.Sealed) == 0 {
		t.Fatal("nothing was sealed")
	}

	got := openAnnounce(testFrameKey, wire)
	if got.InstanceID != in.InstanceID || got.Name != in.Name ||
		got.Deployment != in.Deployment || got.Client != in.Client {
		t.Errorf("round trip = %+v, want %+v", got, in)
	}
	if len(got.Sealed) != 0 {
		t.Errorf("the opened form still carries the blob: %+v", got)
	}
}

// TestTheSealedIdentityIsNotInTheEncodedFrame is the assertion that would
// have caught the original leak, and it is deliberately made against the
// BYTES rather than against the struct.
//
// A struct comparison passes whenever the fields are empty, which is also
// what a forgotten json tag produces. Searching the marshalled frame for the
// hostname is the only form of this check that cannot pass for the wrong
// reason.
func TestTheSealedIdentityIsNotInTheEncodedFrame(t *testing.T) {
	wire, err := sealAnnounce(testFrameKey, Announce{
		InstanceID: "alpha",
		Name:       "jdp-workstation",
		Deployment: "desktop",
	})
	if err != nil {
		t.Fatalf("sealAnnounce: %v", err)
	}
	frame, err := Encode(TypeAnnounce, wire)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	for _, secret := range []string{"jdp-workstation", "desktop"} {
		if containsBytes(frame, secret) {
			t.Errorf("the encoded announce contains %q in the clear:\n%s", secret, frame)
		}
	}
	if !containsBytes(frame, "alpha") {
		t.Errorf("the encoded announce lost the id the relay routes on:\n%s", frame)
	}
}

// containsBytes is a substring search over a frame, written out rather than
// pulled from strings so the test reads at the level it asserts at: is this
// text present in what goes over the wire.
func containsBytes(frame []byte, want string) bool {
	w := []byte(want)
	if len(w) == 0 || len(frame) < len(w) {
		return false
	}
	for i := 0; i+len(w) <= len(frame); i++ {
		match := true
		for j := range w {
			if frame[i+j] != w[j] {
				match = false
				break
			}
		}
		if match {
			return true
		}
	}
	return false
}

// TestARelayCannotMoveAnIdentityToAnotherInstance is what binding the seal to
// the instance id buys.
//
// Without it, a relay could take the sealed blob announcing "this is the NAS"
// and attach it to a different connection, so the Instances page would offer
// a machine under a name that belongs to something else - and a person would
// then send a download to it. The additional data makes that a tag failure
// rather than a believable lie.
func TestARelayCannotMoveAnIdentityToAnotherInstance(t *testing.T) {
	wire, err := sealAnnounce(testFrameKey, Announce{InstanceID: "alpha", Name: "the NAS"})
	if err != nil {
		t.Fatalf("sealAnnounce: %v", err)
	}

	// A hostile relay forwards alpha's blob under bravo's id.
	moved := Announce{InstanceID: "bravo", Sealed: wire.Sealed}
	got := openAnnounce(testFrameKey, moved)
	if got.Name != "" {
		t.Errorf("a moved identity opened as %+v, want nothing usable", got)
	}
	if got.InstanceID != "bravo" {
		t.Errorf("the peer was dropped entirely (%+v); it should still be listed, just unnamed", got)
	}
}

// TestAWrongFrameKeyLeavesThePeerListedButUnnamed pins the third case in
// openAnnounce, and the reasoning is the part worth keeping.
//
// A peer whose seal will not open is on a different frame key, which means it
// could not exchange a single proxy call with this instance either. Dropping
// it would be defensible and is still wrong: the relay says something is
// there, and an instance list that silently disagrees with the relay hides
// the one symptom that would let anybody diagnose a key mismatch.
func TestAWrongFrameKeyLeavesThePeerListedButUnnamed(t *testing.T) {
	wire, err := sealAnnounce(testFrameKey, Announce{InstanceID: "alpha", Name: "the NAS"})
	if err != nil {
		t.Fatalf("sealAnnounce: %v", err)
	}
	other := make([]byte, 32)
	for i := range other {
		other[i] = 0x5a
	}

	got := openAnnounce(other, wire)
	if got.InstanceID != "alpha" {
		t.Errorf("the peer vanished: %+v", got)
	}
	if got.Name != "" {
		t.Errorf("a foreign key produced a name: %+v", got)
	}
}

// TestAnAnnounceFromBeforeTheSealIsStillReadable is the rollout case. jdp
// runs more than one instance and they do not update in the same second; an
// old one keeps sending its identity in the clear, and a new one must go on
// showing it rather than listing a nameless box for the length of the
// upgrade.
func TestAnAnnounceFromBeforeTheSealIsStillReadable(t *testing.T) {
	// Exactly what the previous version put on the wire.
	legacy := []byte(`{"instanceId":"alpha","name":"BOTTICH","deployment":"container"}`)
	var a Announce
	if err := json.Unmarshal(legacy, &a); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	got := openAnnounce(testFrameKey, a)
	if got.Name != "BOTTICH" || got.Deployment != "container" {
		t.Errorf("an old peer read as %+v, want its plaintext identity", got)
	}
}

// TestTwoRealClientsStillSeeEachOthersNames is the end-to-end one, and the
// only test here that would have caught a seal applied on one side and not
// the other. Everything above tests the conversion; this tests that the
// feature the conversion protects still works.
func TestTwoRealClientsStillSeeEachOthersNames(t *testing.T) {
	addr, _ := relayOn(t, "127.0.0.1:0")
	const key = "shared-relay-test-key-0123456789ab"

	alpha := startClient(t, addr, key, "alpha", nil)
	bravo := startClient(t, addr, key, "bravo", nil)
	waitFor(t, "alpha to connect", alpha.Connected)
	waitFor(t, "bravo to connect", bravo.Connected)

	named := func(c *Client, id string) func() bool {
		return func() bool {
			for _, s := range c.Siblings() {
				if s.InstanceID == id {
					// startClient announces Name == id and "desktop".
					return s.Name == id && s.Deployment == "desktop"
				}
			}
			return false
		}
	}
	waitFor(t, "alpha to see bravo by name", named(alpha, "bravo"))
	waitFor(t, "bravo to see alpha by name", named(bravo, "alpha"))
}

// TestOpensAnIdentitySealedByTheMobilePort is the cross-implementation check,
// the same shape TestOpensAFrameSealedByTheMobilePort already has for a proxy
// call - and the one test here that can catch the failure every Go-against-Go
// test above is blind to.
//
// Three implementations now speak this frame: this package, the phone's
// TypeScript port (mobile/src/api/relayFrame.ts) and the extension's
// JavaScript one (extension/src/relay.js). A change to the domain string, the
// separator, the JSON field names or the nonce framing would leave all the
// tests in this file green and ship as "every instance shows the phone with
// no name".
//
// The vector below was produced with @noble/ciphers - the phone's own cipher
// library, out of mobile's node_modules - through relayFrame.ts's own
// announceAAD and JSON shape. Its nonce is a fixed run of 0x07 because a
// vector has to be reproducible; nothing in production seals with one.
func TestOpensAnIdentitySealedByTheMobilePort(t *testing.T) {
	key := DeriveFrameKey([]byte("cross-implementation vector"))
	sealed, err := base64.StdEncoding.DecodeString(
		"BwcHBwcHBwcHBwcHKowzi1tX9hS/RbpFD36F1jz5pHjOvE8p9pXq7oULX/Xf0cCMQxbXtTdsUR7tCWHualBstwHbZzUY0vpg/urebU1me215Eg==")
	if err != nil {
		t.Fatalf("the vector itself is not valid base64: %v", err)
	}

	id, err := OpenIdentity(key, "phone", sealed)
	if err != nil {
		t.Fatalf("could not open an identity the mobile port sealed: %v", err)
	}
	if id.Name != "Pixel 8" || id.Deployment != "mobile" || !id.Client {
		t.Errorf("opened %+v, want the phone's own announce", id)
	}

	// The binding has to hold across implementations too, or a phone would be
	// producing announces a relay could reattach to another connection.
	if _, err := OpenIdentity(key, "nas", sealed); err == nil {
		t.Error("a mobile-sealed identity opened under an id it was not bound to")
	}
}

// TestAClientFlagSurvivesTheSeal: the mobile app's "route to me, do not list
// me" marker travels inside the seal like the rest of the identity, and a
// phone that lost it would appear on every Instances page as somewhere to go
// and answer 501 to everything asked of it.
func TestAClientFlagSurvivesTheSeal(t *testing.T) {
	addr, _ := relayOn(t, "127.0.0.1:0")
	const key = "shared-relay-test-key-0123456789ab"

	instance := startClient(t, addr, key, "nas", nil)
	waitFor(t, "the instance to connect", instance.Connected)

	phone, err := NewClient(ClientOptions{
		URL:      "http://" + addr,
		Key:      key,
		FrameKey: testFrameKey,
		Self:     Announce{InstanceID: "phone", Name: "Pixel", Deployment: "mobile", Client: true},
	})
	if err != nil {
		t.Fatalf("new phone: %v", err)
	}
	phone.minBackoff, phone.maxBackoff = testBackoff, 4*testBackoff
	phone.Start()
	defer func() { _ = phone.Close() }()

	deadline := time.Now().Add(wsTimeout)
	for time.Now().Before(deadline) {
		for _, s := range instance.Siblings() {
			if s.InstanceID == "phone" {
				if !s.Client {
					t.Fatalf("the phone arrived as a browsable instance: %+v", s)
				}
				if s.Name != "Pixel" {
					t.Fatalf("the phone arrived as %+v, want its name through the seal", s)
				}
				return
			}
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("the instance never saw the phone at all")
}
