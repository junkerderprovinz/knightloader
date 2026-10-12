package relay

// Member keys. Every member of a phrase group shares the frame key, so the
// seal alone says only "somebody holding the phrase sent this". Each member
// therefore also holds an Ed25519 key pair of its own and signs every call
// with it:
//
//   - A member's id is made from its key (MemberID), so nobody can speak for
//     an id without holding the key behind it, and the shape of the id says
//     which kind of device it is: 40 hex digits for an instance, "browser-"
//     and hex for the extension, "phone-" and hex for the app. No key is
//     pinned, and no device is listed, refused or removed, under an instance's
//     id, nor the other way round.
//   - The signature and the public half travel inside the seal (ProxyCall.Key
//     and Sig), so a relay never learns them. It covers the request id, the
//     target, the method, the path, the body, the member id, the kind and the
//     time, each with its length, so no field can be moved into another.
//   - The receiving instance pins the key on the first signed call and refuses
//     a call without a signature with a code that tells the device to update.
//   - Taking a member out revokes its pinned key on that instance for good.
//     The id stays removed for a phone or a browser; an instance's id is freed,
//     so the instance can join again under a key of its own.
//   - A key pinned for the first time waits for approval: until somebody at an
//     instance approves the device, its calls are refused with a code that
//     says so. That holds for an instance too, in both directions: the
//     instances already in a group approve a new one, and the new one
//     approves each of them. The approval, and a refusal, are passed on to the
//     other instances with the key, so they pin it as approved before the
//     device calls them.
//   - The instance that answers signs the answer too (ResultMessage), and the
//     caller acts on it only once the signature holds for the key the
//     target's id is made from. Whoever holds the words can seal an answer,
//     but only the target can sign one, so a forged "removed" or "reset"
//     reaches nobody. The answer names the call it answers by a hash of the
//     call as it arrived, sealed, so one the target gave to somebody else's
//     call, or to this call altered on the way, counts for nobody.
//   - Limits count per pinned key, on buckets the receiving instance chooses
//     from the verified key: the replay guard and the calls served at once.
//     How many keys may be pinned per hour counts per sender as well as for
//     the group (see WithSource).

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

type sourceKey struct{}

// WithSource marks ctx with where a call came in: the relay connection that
// sent it, or the address a direct call came from. Whoever holds the words can
// open as many of either as it likes, so a source proves nothing; it only
// keeps one of them from using up a limit the whole group shares.
func WithSource(ctx context.Context, source string) context.Context {
	return context.WithValue(ctx, sourceKey{}, source)
}

// SourceOf returns what WithSource put on ctx, "" when the transport could not
// tell.
func SourceOf(ctx context.Context) string {
	s, _ := ctx.Value(sourceKey{}).(string)
	return s
}

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

// MemberID is the id a member of kind goes by with key: the hex of the key's
// SHA-256 after "phone-" for the app and "browser-" for the extension, and for
// an instance the first 20 bytes of it, the 40 hex digits an instance id has.
// It is "" for a key of the wrong length or an unknown kind.
func MemberID(kind string, key []byte) string {
	if len(key) != ed25519.PublicKeySize {
		return ""
	}
	sum := sha256.Sum256(key)
	switch kind {
	case KindInstance:
		return hex.EncodeToString(sum[:20])
	case KindMobile:
		return "phone-" + hex.EncodeToString(sum[:])
	case KindExtension:
		return "browser-" + hex.EncodeToString(sum[:])
	}
	return ""
}

// Fingerprint is how a person compares a member key: the start of its hash in
// four groups. Each device shows its own, and an instance shows it beside a
// device that waits for approval.
func Fingerprint(key []byte) string {
	sum := sha256.Sum256(key)
	h := hex.EncodeToString(sum[:8])
	return strings.Join([]string{h[0:4], h[4:8], h[8:12], h[12:16]}, " ")
}

// The domains that open every signed message, so a member key's signature over
// one kind of message can never be taken for one over another.
const (
	callDomain   = "knightloader/relay/member-call/v2\x00"
	resultDomain = "knightloader/relay/member-result/v3\x00"
	helloDomain  = "knightloader/relay/member-hello/v2\x00"
)

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

// signedMessage is domain followed by every field with its length and then n,
// the one layout every signed message has.
func signedMessage(domain string, n uint64, fields ...[]byte) []byte {
	out := []byte(domain)
	for _, field := range fields {
		out = binary.BigEndian.AppendUint64(out, uint64(len(field)))
		out = append(out, field...)
	}
	return binary.BigEndian.AppendUint64(out, n)
}

// CallMessage is what a member signs for a call to target. It is exported so
// the app's and the extension's ports can be held to it.
func CallMessage(target string, c ProxyCall) []byte {
	return signedMessage(callDomain, uint64(c.Sent), //nolint:gosec // G115: the bits of the time are signed as they are
		[]byte(c.ID), []byte(target), []byte(c.Method), []byte(c.Path), c.Body, []byte(c.From), []byte(c.Kind))
}

// ResultMessage is what the member from signs when it answers, with r, the
// call that arrived sealed as sealedCall under requestID. The call is named by
// the hash of its sealed bytes, which covers every field it carries.
func ResultMessage(requestID, from string, sealedCall []byte, r ProxyResult) []byte {
	call := sha256.Sum256(sealedCall)
	return signedMessage(resultDomain, uint64(int64(r.Status)), //nolint:gosec // G115: a status is signed as its bits
		[]byte(requestID), []byte(from), call[:], r.Body)
}

// SignResult is r signed by member as the answer from from to the call that
// arrived sealed as sealedCall.
func SignResult(member ed25519.PrivateKey, requestID, from string, sealedCall []byte, r ProxyResult) ProxyResult {
	r.Key = member.Public().(ed25519.PublicKey)
	r.Sig = ed25519.Sign(member, ResultMessage(requestID, from, sealedCall, r))
	return r
}

// VerifyResult reports whether r is target's answer to the caller's own call,
// sent sealed as sealedCall under requestID: signed by the key target's
// instance id is made from.
func VerifyResult(requestID, target string, sealedCall []byte, r ProxyResult) bool {
	return len(r.Sig) == ed25519.SignatureSize && MemberID(KindInstance, r.Key) == target &&
		ed25519.Verify(r.Key, ResultMessage(requestID, target, sealedCall, r), r.Sig)
}

// Signed reports whether a call presents a member key at all.
func (c ProxyCall) Signed() bool { return len(c.Key) > 0 || len(c.Sig) > 0 }

// verifyCall reports whether an unsigned call, or a signed one whose signature
// holds, may go on. An unsigned one only goes on to be refused with a reason
// (see federation.Manager.Guard).
func verifyCall(target string, c ProxyCall) bool {
	if !c.Signed() {
		return true
	}
	return len(c.Key) == ed25519.PublicKeySize && len(c.Sig) == ed25519.SignatureSize &&
		ed25519.Verify(c.Key, CallMessage(target, c), c.Sig)
}

// IDProof shows that a connection holds the key its announced id is made
// from. Only a connection that shows it takes the place of one already
// connected under that id. It signs the challenge the relay sent on that
// connection, so a hello somebody saw proves nothing on another one.
type IDProof struct {
	Kind string `json:"kind"`
	Key  []byte `json:"key"`
	Sig  []byte `json:"sig"`
}

// HelloMessage is what a member signs to prove that id is its own on the
// relay key key, on the connection the relay sent challenge on.
func HelloMessage(key, id, kind string, challenge []byte) []byte {
	return signedMessage(helloDomain, 0, []byte(key), []byte(id), []byte(kind), challenge)
}

// ProveID signs the proof that member, a member of kind, holds id on the relay
// key key, for the connection the relay sent challenge on.
func ProveID(member ed25519.PrivateKey, kind, key, id string, challenge []byte) *IDProof {
	return &IDProof{
		Kind: kind,
		Key:  member.Public().(ed25519.PublicKey),
		Sig:  ed25519.Sign(member, HelloMessage(key, id, kind, challenge)),
	}
}

// proves reports whether h shows the key its announced id is made from, on the
// connection the relay sent challenge on.
func (h Hello) proves(challenge []byte) bool {
	p := h.Proof
	if p == nil || MemberID(p.Kind, p.Key) != h.Announce.InstanceID || len(p.Sig) != ed25519.SignatureSize {
		return false
	}
	return ed25519.Verify(p.Key, HelloMessage(h.Key, h.Announce.InstanceID, p.Kind, challenge), p.Sig)
}

// RefusalBody is the answer body of a refused call, in the {error, code} shape
// the API's own refusals have, so the app and the extension can word it.
func RefusalBody(code, text string) []byte {
	b, _ := json.Marshal(map[string]string{"error": text, "code": code})
	return b
}

// SignatureRefused is the answer body for a call whose signature does not hold.
var SignatureRefused = RefusalBody("memberSignature", "the signature on this call does not match the key it came with")
