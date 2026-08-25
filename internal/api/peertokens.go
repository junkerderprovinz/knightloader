package api

// Where the credential for calling ONE peer lives, and how the two sides
// give each other one during pairing.
//
// Until this existed, a federation call carried no credential at all over
// either transport, so a peer with a password set was listed on the Instances
// page and answered 401 to everything - visible and useless, and
// indistinguishable from switched off. See issue #26.
//
// A peer token is a normal apitoken: named after the peer it was minted for,
// individually revocable from the Access tab, and hashed on the instance that
// issued it exactly like any other. What is NOT normal is where the RECEIVING
// side keeps it: sealed in internal/accounts, never in instances.json.
// An Instance is public identity written to a plaintext file, and a bearer
// token is a secret - the same separation settings.RelayURL already keeps
// from the relay key (settings_relay.go's own doc comment).
//
// One honest limitation: apitoken has no scopes, so a peer token is a
// full-power API token, not one restricted to the links/tasks/queue routes
// the outbound proxy allowlist permits. The allowlist bounds what a peer can
// ASK this instance to forward; it does not bound what the token itself could
// do if it were taken off the peer. Being individually revocable and named is
// the mitigation available today.

import (
	"sort"

	"github.com/junkerderprovinz/knightloader/internal/accounts"
	"github.com/junkerderprovinz/knightloader/internal/apitoken"
	"github.com/junkerderprovinz/knightloader/internal/app"
	"github.com/junkerderprovinz/knightloader/internal/federation"
)

// peerTokenService is the accounts service the per-peer credentials are
// filed under, with the peer's own name as the account. Deliberately not in
// accounts.Catalogue, for the same reason relay.AccountService is not: that
// list is what the Accounts page offers as a row to configure, and a peer
// token is issued by machinery, never typed by a person.
const peerTokenService = "federation-peer"

// peerTokens adapts the sealed credential store to what federation.Manager
// asks for. It is deliberately tiny: federation must not learn where secrets
// live, and accounts must not learn what a peer is.
type peerTokens struct{ a *app.App }

func (p peerTokens) TokenFor(peer string) string {
	cred, err := p.a.Accounts.GetCredential(peerTokenService, peer)
	if err != nil {
		return ""
	}
	return cred.APIKey
}

// storePeerToken files the credential this instance will use when calling
// peer. An empty token deletes the entry, which is what accounts already
// treats a zero Credential as.
func storePeerToken(a *app.App, peer, token string) error {
	return a.Accounts.SetCredential(peerTokenService, peer, accounts.Credential{APIKey: token})
}

// peerTokenName is what a minted peer token is called on the Access tab, so
// that list reads as a list of who can reach this instance rather than a row
// of anonymous secrets. It is NOT unique: apitoken.Store.Create does not
// enforce unique names, so pairing twice with the same peer leaves two live
// tokens sharing this name. Everything below therefore addresses a token by
// its ID and never by this string.
func peerTokenName(peer string) string { return "peer: " + peer }

// mintPeerToken creates the token the OTHER side will use to call this one,
// and returns its ID along with the plaintext - the only moment the secret
// exists in readable form, since the issuing side keeps a hash like every
// other API token.
//
// The ID is what the caller must keep. An earlier version revoked "every
// token named after this peer" instead, which is wrong in both directions:
// a re-pair that fails would revoke the token from the pairing that WORKED,
// killing a live federation link on behalf of an attempt that did nothing.
func mintPeerToken(a *app.App, peer string) (id, secret string, err error) {
	t, secret, err := a.APITokens.Create(peerTokenName(peer))
	if err != nil {
		return "", "", err
	}
	return t.ID, secret, nil
}

// addPeer registers in, dropping any credential held under that NAME first
// when the address behind it changed.
//
// federation.Add overwrites by name, and a peer's credential is filed by name
// too - so re-pointing a name at a new address silently re-attached the OLD
// peer's credential to the NEW one. That is a credential-theft vector, not
// just untidiness: the very next call (a Ping from either pairing handler, or
// from POST /api/instances) would put the real peer's bearer token in an
// Authorization header addressed to whoever now owns that name. A pairing
// code names its own peer, and /complete takes one unauthenticated, so the
// attacker's side of that is a code somebody was persuaded to paste, or read
// off a screen.
//
// Dropped rather than refused, because re-pointing a name is a legitimate
// thing to do - a peer moves, a domain changes, an address was typed wrong.
// A real re-pairing hands over a fresh credential in the same exchange, which
// is stored immediately after this; an attacker's does not, and their peer is
// then called with nothing, which is exactly what an unknown peer deserves.
func addPeer(a *app.App, in federation.Instance) error {
	if prev, ok := findPeer(a, in.Name); ok && prev.URL != in.URL {
		forgetPeerCredentials(a, in.Name)
	}
	return a.Federation.Add(in)
}

// findPeer looks one peer up by the name it is addressed as.
func findPeer(a *app.App, name string) (federation.Instance, bool) {
	for _, p := range a.Federation.List() {
		if p.Name == name {
			return p, true
		}
	}
	return federation.Instance{}, false
}

// forgetPeerCredentials ends the credential relationship with peer, in both
// directions: the token this instance uses to CALL it, and the token it was
// given to call this one.
//
// Both halves, because either one left behind is a live secret for a
// relationship that no longer exists. The minted one is a full-power API token
// (see the note at the top of this file) that would keep working forever; the
// stored one gets silently re-attached the moment anything registers a peer
// under the same name again, which on a network of repeated hostnames is not
// a hypothetical.
//
// Best-effort: this runs while something else is already being reported, and a
// cleanup that fails must not replace that report with a different error.
func forgetPeerCredentials(a *app.App, peer string) {
	_ = storePeerToken(a, peer, "")
	want := peerTokenName(peer)
	for _, t := range a.APITokens.List() {
		if t.Name == want {
			_ = a.APITokens.Revoke(t.ID)
		}
	}
}

// revokeMintedToken drops one specific token, for the case the pairing it was
// minted for did not complete.
//
// Without it every failed attempt would leave a live, full-power token on the
// Access tab named after a peer that was never added, and a retry would add
// another. Best-effort by design: the pairing has already failed and is being
// reported, so a revoke that itself fails must not replace that message with
// a different one.
func revokeMintedToken(a *app.App, id string) {
	if id == "" {
		return
	}
	_ = a.APITokens.Revoke(id)
}

// keepPerPeer bounds how many credentials one peer may have outstanding on
// this instance at once.
//
// Exists because supersedePeerTokens can only run when a pairing is PROVEN,
// and proof is not always available: the generator learns its peer works by
// reaching it, and a peer that is asleep, or reachable in one direction only,
// never supplies that. Without a ceiling, re-pairing such a peer would add a
// credential every time and eventually walk into apitoken.MaxTokens, where a
// perfectly valid pairing code starts failing with "too many tokens".
//
// Three rather than one, because the whole point of not superseding on an
// unproven pairing is to keep the credential from the pairing that DID work.
// Three leaves room for two unproven attempts before the oldest is at risk,
// which is well past the point where somebody would have noticed the pairing
// is one-way - the page says so on every attempt.
const keepPerPeer = 3

// trimPeerTokens is the ceiling for the unproven case: keeps keepID and the
// newest few others, revoking the rest oldest-first.
func trimPeerTokens(a *app.App, peer, keepID string) {
	want := peerTokenName(peer)
	var others []apitoken.Token
	for _, t := range a.APITokens.List() {
		if t.Name == want && t.ID != keepID {
			others = append(others, t)
		}
	}
	// Newest first, so what gets revoked is the oldest - the ones least likely
	// to still be the credential the peer is actually using.
	sort.Slice(others, func(i, j int) bool { return others[i].CreatedAt.After(others[j].CreatedAt) })
	for i := keepPerPeer - 1; i < len(others); i++ {
		_ = a.APITokens.Revoke(others[i].ID)
	}
}

// supersedePeerTokens drops every OTHER token issued for peer, keeping only
// keepID. Called once a pairing has actually succeeded.
//
// Without it, tokens accumulate: each re-pair mints another full-power
// credential that nothing ever cleans up, the Access tab fills with
// identically-named rows nobody can tell apart, and at apitoken.MaxTokens a
// perfectly valid pairing code starts failing with "too many tokens" - an
// error about a limit the user never knowingly approached.
//
// Deliberately after success, not before minting: revoking first would mean a
// re-pair that then fails has destroyed the working credential from the
// pairing it was retrying.
func supersedePeerTokens(a *app.App, peer, keepID string) {
	want := peerTokenName(peer)
	for _, t := range a.APITokens.List() {
		if t.Name == want && t.ID != keepID {
			_ = a.APITokens.Revoke(t.ID)
		}
	}
}
