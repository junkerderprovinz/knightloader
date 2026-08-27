package relay

// Turning the secret behind a connection phrase into the key that groups
// instances on a relay.
//
// The two are deliberately not the same value. The secret is what a person
// holds - it is what their phrase decodes to, and whoever has it can join
// their group anywhere. The relay key is what travels: every instance sends
// it in the clear in its hello frame, so the relay operator sees it, and so
// would anyone who ever got at the relay's memory or its logs.
//
// Deriving one from the other costs a hash and buys the difference between
// those two facts. A relay only ever learns an identifier for a group; it
// never learns the thing that would let it produce that group's phrase, or
// rejoin the same people through some other relay later. That distinction
// matters most for exactly the deployment this exists for: one relay that
// jdp operates for other people, where "the operator cannot recover a
// user's phrase" should be a property of the design and not of his good
// behaviour.
//
// That later work is now done: see DeriveFrameKey below and seal.go. Proxy
// frames are sealed with a SECOND key derived from the same secret under its
// own domain, so a relay learns the group identifier and nothing else about
// what travels through it. Deriving the relay key separately is exactly what
// made that possible without changing a phrase anybody already holds.

import (
	"crypto/sha256"
	"encoding/hex"
)

// DefaultRelayURL is the relay every instance dials unless its owner points
// it somewhere else. A compiled-in constant rather than a setting, and that
// is the decision the whole seed-phrase design rests on: because the
// address is already known, the phrase has to carry only the secret, which
// is what makes it twelve words instead of a URL plus a key, and what
// removes the last thing there would otherwise be to log into.
//
// It follows that this value can never change and can never lapse. Every
// released binary carries it, so a copy installed today must still find a
// working relay here years from now - see the deployment spec for why the
// domain was chosen with that in mind rather than reusing one that already
// existed.
const DefaultRelayURL = "wss://relay.knightloader.app/relay/connect"

// SeedAccountService is where the seed-phrase SECRET is sealed, beside the
// debrid keys in internal/accounts - not in settings.json, for the reason
// settings_relay.go's own comment gives about the relay key: possession of
// it is the entire authorization, so it is a credential.
//
// Kept separate from AccountService (which holds a hand-entered relay key)
// because the two are different things stored at different layers: this is
// the secret a person's phrase decodes to, and what reaches the relay is
// DeriveKey of it, never this value itself.
const SeedAccountService = "relay-seed"

// keyDomain separates this use of the secret from any other that might be
// derived from it later (an encryption key, an instance identity). Without
// a domain separator, two features that both hash the same secret would
// produce the same bytes and quietly become the same credential.
const keyDomain = "knightloader/relay/group-key/v1"

// DeriveKey returns the relay key for a secret - the value an instance puts
// in its hello frame. Hex rather than base64 so it survives being copied
// through a log, a URL, or a config file without an encoding argument, at
// the cost of length nobody has to type: this value is never shown to a
// person, the phrase is.
func DeriveKey(secret []byte) string {
	h := sha256.New()
	h.Write([]byte(keyDomain))
	h.Write(secret)
	return hex.EncodeToString(h.Sum(nil))
}

// frameDomain is keyDomain's counterpart for the key that ENCRYPTS proxy
// frames. Two domains over one secret, and the separation is the entire
// point: the relay is handed DeriveKey's output in every hello frame, so a
// frame key derived from that value - or from the same domain - would be a
// key the relay already holds. Deriving it from the secret under a different
// domain means the relay can hold one and never compute the other.
const frameDomain = "knightloader/relay/frame-key/v1"

// DeriveFrameKey returns the 32-byte AES-256-GCM key that seals proxy frames
// between instances that share a connection phrase.
//
// Raw bytes rather than DeriveKey's hex, because nothing ever writes this one
// down: it is derived on both ends from a secret they already share, used,
// and dropped. It never travels, never reaches settings.json, and is never
// shown to anybody.
//
// This is the key the security claim in the UI rests on. A relay operator
// sees the group key, the target instance id, a request id, and the size and
// timing of what passes. They do not see the method, the path, the body, or
// the bearer token a phone attaches - and holding the group key gets them no
// closer to this one, because SHA-256 does not run backwards.
func DeriveFrameKey(secret []byte) []byte {
	h := sha256.New()
	h.Write([]byte(frameDomain))
	h.Write(secret)
	return h.Sum(nil)
}

// manualFrameDomain is the third domain, for the one configuration that has
// no secret to derive from: a hand-entered relay key (AccountService),
// predating the phrase or preferred by somebody running their own relay.
const manualFrameDomain = "knightloader/relay/frame-key-manual/v1"

// FrameKeyFromRelayKey derives a frame key for the hand-entered-key path.
//
// WHAT THIS IS AND IS NOT, because the difference matters and is easy to
// overstate: the input here is the very value that travels to the relay in
// the hello frame, so an operator who wanted to read these frames could
// derive this key themselves. This is NOT the end-to-end guarantee
// DeriveFrameKey provides, and no UI text claims it is - the connection-phrase
// card is the only place that makes a claim, and the phrase path never comes
// through here.
//
// It is still worth having. It seals the traffic against everything BETWEEN
// the two instances and the relay - a reverse proxy, a TLS-terminating load
// balancer, a captured log - and this path exists specifically for somebody
// running the relay themselves, where operator and owner are the same person.
// The alternative was to leave one of the two paths in the clear, which would
// mean the protocol had two shapes and every reader had to know which one
// they were looking at.
func FrameKeyFromRelayKey(key string) []byte {
	h := sha256.New()
	h.Write([]byte(manualFrameDomain))
	h.Write([]byte(key))
	return h.Sum(nil)
}
