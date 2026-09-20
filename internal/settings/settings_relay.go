package settings

// Where this instance's own relay lives. A relay is the third point two
// instances behind different NATs can both reach: each dials out to it, so
// neither has to be reachable from the other, which is the only way two desktop
// installs on separate networks can see each other.
//
// Only the relay's address lives here, "https://relay.example.com" or
// "ws://192.168.20.11:8760". That is public identity in the sense KnownDomains
// next door is: it names infrastructure without unlocking it.
//
// The relay key does not live here. Possession of the key is the whole
// authorization check the relay makes, with no account and no password behind
// it, so it is a credential and is sealed in internal/accounts under
// relay.AccountService beside the TorBox and debrid keys. That is why this hook
// has one field to clean.
//
// RelayServe is the other direction: this instance being the relay, on its own
// address, for instances carrying the same key. It removes the second binary
// and the second address, since the relay lives under /relay/connect on the
// address this instance already answers on, behind the same reverse proxy and
// certificate. It does not remove the requirement that something be reachable
// from both sides, so switching it on inside a desktop install nothing outside
// can reach changes nothing.
//
// It carries no address of its own, because the address is this instance's,
// which it already knows and shows on the same page, and a second copy would go
// stale the first time a domain changed. Nor is it a second key: it admits the
// key this instance already stores.

import "strings"

// sanitizeRelay normalises the relay address the same way federation.Add
// already normalises a peer's, and for the same reason: whitespace off both
// ends because a pasted URL carries it, and no trailing slash because the
// relay client appends its own path to this value, and "…example.com/" plus
// "/relay/connect" is a double slash some reverse proxies redirect and
// others simply 404.
func sanitizeRelay(n Settings) Settings {
	n.RelayURL = strings.TrimRight(strings.TrimSpace(n.RelayURL), "/")
	// A bare host gets https:// put in front of it. relay.connectURL refuses an
	// address with no scheme, so somebody typing the domain they gave their
	// reverse proxy, which is what "Adresse" reads as, would otherwise get a
	// field that saved fine and a relay that never dialled. https rather than
	// http, because a relay carries a credential and the insecure guess is the
	// one that costs something.
	if n.RelayURL != "" && !strings.Contains(n.RelayURL, "://") {
		n.RelayURL = "https://" + n.RelayURL
	}
	return n
}
