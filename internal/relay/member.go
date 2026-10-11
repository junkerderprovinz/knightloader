package relay

// Member keys. Every member of a phrase group shares the frame key, so the
// seal alone says only "somebody holding the phrase sent this". Each member
// therefore also holds an Ed25519 key pair of its own and signs every call
// with it:
//
//   - The signature and the public half travel inside the seal (ProxyCall.Key
//     and Sig), so a relay never learns them and an older relay carries them
//     unchanged. It covers the request id, the target, the method, the path,
//     the body, the authorization field, the member id, the kind and the time,
//     each with its length, so no field can be moved into another.
//   - The receiving instance pins the key to the member id the call names the
//     first time it sees that id (trust on first use). From then on only that
//     key speaks for that id: another key, or no signature, is refused with a
//     code that says so. A member already known before keys existed gets its
//     key pinned on its first signed call.
//   - Taking a member out revokes its pinned key on that instance for good.
//     The id stays removed for a phone or a browser; an instance's id is freed,
//     so the instance can join again under a key of its own.
//   - A key pinned for the first time waits for approval: until somebody at an
//     instance approves the device, its calls are refused with a code that
//     says so. The approval, and a refusal, are passed on to the other
//     instances with the key, so they pin it as approved before the device
//     calls them. Members that were in the group before keys count as
//     approved, and so do the instances an instance meets right after its
//     owner entered the words of a group that already existed.
//   - Limits count per pinned key, on buckets the receiving instance chooses
//     from the verified key: the replay guard, the calls served at once, and
//     how many keys may be pinned per hour. Calls without a signature, from
//     members on a release before keys, share one bucket of their own.
//
// A call without a signature is still served during the move to keys, unless
// its id has a pinned key or was removed, so an instance on the release before
// keys and one after it keep talking. federation.acceptUnsigned is the one
// switch that ends this.

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
)

// MemberAccountService is the credential-store service an instance's member
// key seed is sealed under. It belongs to the group: joining one mints a new
// seed and leaving deletes it.
const MemberAccountService = "relay-member"

// The kinds a member signs its calls as. An instance's kind is pinned with its
// key, so a member cannot have an instance taken out as if it were a phone.
const (
	KindInstance  = "instance"
	KindMobile    = "mobile"
	KindExtension = "extension"
)

// callDomain opens every signed call message, so a member key's signature
// over a call can never be taken for one over anything else.
const callDomain = "knightloader/relay/member-call/v1\x00"

// ErrSignature is returned for a call that opens but whose signature does not
// match the key it presents. Only a member holding the phrase can seal such a
// call, so unlike ErrSealed it is answered, and the answer says why.
var ErrSignature = errors.New("relay: the call's signature does not match its member key")

// NewMemberSeed returns the seed of a new member key.
func NewMemberSeed() ([]byte, error) {
	seed := make([]byte, ed25519.SeedSize)
	if _, err := rand.Read(seed); err != nil {
		return nil, fmt.Errorf("relay: no randomness for a member key: %w", err)
	}
	return seed, nil
}

// CallMessage is what a member signs for a call to target. It is exported so
// the app's and the extension's ports can be held to it.
func CallMessage(target string, c ProxyCall) []byte {
	out := []byte(callDomain)
	for _, field := range [][]byte{
		[]byte(c.ID), []byte(target), []byte(c.Method), []byte(c.Path), c.Body,
		[]byte(c.Authorization), []byte(c.From), []byte(c.Kind),
	} {
		out = binary.BigEndian.AppendUint64(out, uint64(len(field)))
		out = append(out, field...)
	}
	return binary.BigEndian.AppendUint64(out, uint64(c.Sent)) //nolint:gosec // G115: the bits of the time are signed as they are
}

// Signed reports whether a call presents a member key at all.
func (c ProxyCall) Signed() bool { return len(c.Key) > 0 || len(c.Sig) > 0 }

// verifyCall reports whether an unsigned call, or a signed one whose signature
// holds, may go on.
func verifyCall(target string, c ProxyCall) bool {
	if !c.Signed() {
		return true
	}
	return len(c.Key) == ed25519.PublicKeySize && len(c.Sig) == ed25519.SignatureSize &&
		ed25519.Verify(c.Key, CallMessage(target, c), c.Sig)
}

// RefusalBody is the answer body of a refused call, in the {error, code} shape
// the API's own refusals have, so the app and the extension can word it.
func RefusalBody(code, text string) []byte {
	b, _ := json.Marshal(map[string]string{"error": text, "code": code})
	return b
}

// SignatureRefused is the answer body for a call whose signature does not hold.
var SignatureRefused = RefusalBody("memberSignature", "the signature on this call does not match the key it came with")
