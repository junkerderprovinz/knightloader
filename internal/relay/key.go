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
// What this does NOT provide: the relay still forwards proxy frames in the
// clear, so it can read the traffic it routes. Fixing that needs end-to-end
// encryption keyed on the secret, which is a separate piece of work and is
// noted as a non-goal in the current spec. Deriving the key is what makes
// that work possible later without changing the phrase people already hold.

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
