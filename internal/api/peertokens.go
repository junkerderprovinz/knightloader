package api

// Where the credential for calling one peer lives, and how the two sides give
// each other one during pairing.
//
// A peer token is a normal apitoken: named after the peer, revocable from the
// Access tab and hashed on the issuing instance. The receiving side keeps it
// sealed in internal/accounts rather than in instances.json, which is a
// plaintext file of public identity.
//
// A peer token carries every scope, since a peer drives this instance as fully
// as its owner's browser does. The outbound allowlist bounds what a peer can
// ask this instance to forward, not what the token could do if taken off the
// peer; naming and revoking it individually is the mitigation available.

import (
	"regexp"
	"sort"

	"github.com/junkerderprovinz/knightloader/internal/accounts"
	"github.com/junkerderprovinz/knightloader/internal/apitoken"
	"github.com/junkerderprovinz/knightloader/internal/app"
	"github.com/junkerderprovinz/knightloader/internal/federation"
)

// peerTokenService is the accounts service the per-peer credentials are filed
// under, with the peer's name as the account. It is not in accounts.Catalogue,
// since the Accounts page must not offer it as a row to fill in.
const peerTokenService = "federation-peer"

// peerTokens adapts the sealed credential store to what federation.Manager
// asks for, so neither package learns about the other.
type peerTokens struct{ a *app.App }

func (p peerTokens) TokenFor(peer string) string {
	cred, err := p.a.Accounts.GetCredential(peerTokenService, peer)
	if err != nil {
		return ""
	}
	return cred.APIKey
}

// storePeerToken files the credential this instance uses when calling peer.
// An empty token deletes the entry.
func storePeerToken(a *app.App, peer, token string) error {
	return a.Accounts.SetCredential(peerTokenService, peer, accounts.Credential{APIKey: token})
}

// storePeerTokens files one credential under every key the peer can be
// addressed by.
func storePeerTokens(a *app.App, id peerIdentity, token string) error {
	for _, k := range id.keys() {
		if err := storePeerToken(a, k, token); err != nil {
			return err
		}
	}
	return nil
}

// instanceIDRe is the shape of an InstanceID: 20 random bytes as lowercase
// hex. A relay id is used as a credential key and arrives on the
// unauthenticated /complete route, so an unchecked one could name an existing
// peer and take over its credential. Pairing names are capped at 32
// characters, so the two key spaces cannot collide.
var instanceIDRe = regexp.MustCompile(`^[0-9a-f]{40}$`)

// peerIdentity is everything one peer can be addressed by. Over HTTP a peer is
// addressed by its pairing name, over the relay by its instance id, and which
// one gets used is not known at pairing time, so the credential is filed under
// both.
type peerIdentity struct {
	// Name is the pairing name, empty when the peer offered no address.
	Name string
	// RelayID is the peer's instance id, empty unless it is relay-visible.
	RelayID string
}

// newPeerIdentity builds one from what arrived on the wire, dropping a relay id
// that is not shaped like one.
func newPeerIdentity(name, relayID string) peerIdentity {
	if !instanceIDRe.MatchString(relayID) {
		relayID = ""
	}
	return peerIdentity{Name: name, RelayID: relayID}
}

// keys is every key this peer's credential has to be findable under.
func (p peerIdentity) keys() []string {
	var out []string
	if p.Name != "" {
		out = append(out, p.Name)
	}
	if p.RelayID != "" {
		out = append(out, p.RelayID)
	}
	return out
}

// canonical is the one key that minting and superseding tokens both use. The
// relay id wins because it cannot change; if the two disagreed, nothing would
// ever be retired.
func (p peerIdentity) canonical() string {
	if p.RelayID != "" {
		return p.RelayID
	}
	return p.Name
}

func (p peerIdentity) empty() bool { return p.Name == "" && p.RelayID == "" }

// peerTokenName is what a minted peer token is called on the Access tab. It is
// not unique, so tokens are always addressed by ID.
func peerTokenName(peer string) string { return "peer: " + peer }

// mintPeerToken creates the token the other side will use to call this one
// and returns its ID and the plaintext, which exists only now. The caller keeps
// the ID so a failed re-pair can revoke exactly this token and not the one
// from a pairing that worked.
func mintPeerToken(a *app.App, peer string) (id, secret string, err error) {
	t, secret, err := a.APITokens.Create(peerTokenName(peer))
	if err != nil {
		return "", "", err
	}
	return t.ID, secret, nil
}

// addPeer registers in, first dropping any credential held under that name
// when its address changed. Otherwise the old peer's bearer token would be
// sent to whoever the name now points at. A real re-pairing stores a fresh
// credential right after this.
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

// forgetPeerCredentials ends the credential relationship with peer in both
// directions: the token used to call it, and the tokens minted for it. Either
// one left behind would stay live. Best effort, since it runs while another
// error is being reported.
func forgetPeerCredentials(a *app.App, peer string) {
	_ = storePeerToken(a, peer, "")
	want := peerTokenName(peer)
	for _, t := range a.APITokens.List() {
		if t.Name == want {
			_ = a.APITokens.Revoke(t.ID)
		}
	}
}

// revokeMintedToken drops the token minted for a pairing that did not
// complete, so failed attempts do not pile up live tokens. Best effort, since
// the pairing failure is already being reported.
func revokeMintedToken(a *app.App, id string) {
	if id == "" {
		return
	}
	_ = a.APITokens.Revoke(id)
}

// keepPerPeer bounds how many credentials one peer may have outstanding.
// supersedePeerTokens only runs once a pairing is proven, and a peer that is
// asleep or reachable one way only never proves it, so re-pairing would
// otherwise run into apitoken.MaxTokens. Three leaves room for two unproven
// attempts beside the one that worked.
const keepPerPeer = 3

// trimPeerTokens keeps keepID and the newest few others, revoking the rest.
func trimPeerTokens(a *app.App, peer, keepID string) {
	want := peerTokenName(peer)
	var others []apitoken.Token
	for _, t := range a.APITokens.List() {
		if t.Name == want && t.ID != keepID {
			others = append(others, t)
		}
	}
	sort.Slice(others, func(i, j int) bool { return others[i].CreatedAt.After(others[j].CreatedAt) })
	for i := keepPerPeer - 1; i < len(others); i++ {
		_ = a.APITokens.Revoke(others[i].ID)
	}
}

// supersedePeerTokens revokes every other token issued for peer once a pairing
// has succeeded. It runs after success rather than before minting, so a failed
// re-pair cannot destroy the working credential.
func supersedePeerTokens(a *app.App, peer, keepID string) {
	want := peerTokenName(peer)
	for _, t := range a.APITokens.List() {
		if t.Name == want && t.ID != keepID {
			_ = a.APITokens.Revoke(t.ID)
		}
	}
}
