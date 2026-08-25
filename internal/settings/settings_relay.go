package settings

// Where this instance's own relay lives. A relay is the third point two
// instances behind different NATs can both reach: each one dials OUT to it,
// so neither has to be reachable from the other, which is the only way two
// desktop installs on separate networks can ever see each other (see the
// self-hosted relay design spec for the full reasoning, and for why this
// project ships the relay as a component people run themselves rather than
// as a service it operates for them).
//
// What lives here is only the relay's ADDRESS - "https://relay.example.com",
// "ws://192.168.20.11:8760". That is public identity in exactly the sense
// KnownDomains next door is: it names infrastructure, it does not unlock it,
// and it is no more sensitive in settings.json than the domain this instance
// already answers on.
//
// The relay KEY deliberately does NOT live here. Possession of the key is
// the entire authorization check the relay makes - there is no account and
// no password behind it - so it is a credential, and it is stored the way
// this app already stores credentials: sealed in internal/accounts under
// relay.AccountService, beside the TorBox and debrid keys. Public identity
// and a secret do not belong in the same file, let alone the same sanitize
// path, which is why this hook has exactly one field to clean.
//
// RelayServe is the other direction: this instance BEING the relay, on its
// own address, for instances carrying the same key. It is worth being plain
// about what it moves and what it does not. It removes the second binary and
// the second address: the relay lives under /relay/connect on the address
// this instance already answers on, behind the same reverse proxy and the
// same certificate, and the other instances point at that. What it cannot
// remove is the requirement that SOMETHING be reachable from both sides. A
// relay is the third point two NATed instances both dial out to, so the one
// hosting it has to be reachable, and switching it on inside a desktop
// install that nothing outside can reach changes nothing about what can
// reach it.
//
// It carries no address of its own. The address is this instance's, which it
// already knows and already shows on the same page, and a second copy here
// would be a field that goes stale the first time a domain changes. Nor is
// it a second key: it admits exactly the key this instance already stores,
// so "my relay" is one key rather than two that have to be kept in step.

import "strings"

// sanitizeRelay normalises the relay address the same way federation.Add
// already normalises a peer's, and for the same reason: whitespace off both
// ends because a pasted URL carries it, and no trailing slash because the
// relay client appends its own path to this value, and "…example.com/" plus
// "/relay/connect" is a double slash some reverse proxies redirect and
// others simply 404.
func sanitizeRelay(n Settings) Settings {
	n.RelayURL = strings.TrimRight(strings.TrimSpace(n.RelayURL), "/")
	return n
}
