// Package federation lets one KnightLoader act as the dashboard for others:
// peer instances are stored locally, and their REST APIs are proxied so the UI
// can view and control every instance from one place.
//
// A peer is reached one of two ways, and List/Proxy hide which: over plain
// HTTP to an address somebody stored (the original path - a LAN IP, or a
// domain behind a reverse proxy), or through a self-hosted relay both sides
// dial out to when neither can accept an inbound connection. The relay is
// additive and optional; with none configured this package behaves exactly as
// it did before one existed.
package federation

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/httpx"
	"github.com/junkerderprovinz/knightloader/internal/relay"
)

// Instance is a peer KnightLoader, either reachable over HTTP at URL or
// visible through the relay under RelayID.
type Instance struct {
	// Name is the stable address every route below keys on
	// (/api/instances/{name}/...) - for a stored peer, the name somebody
	// chose when adding it; for a relay peer, always its InstanceID. Never
	// the relay's own announced display name, which two different
	// instances are free to share and which any instance is free to
	// change at any time - see DisplayName.
	Name string `json:"name"`
	URL  string `json:"url"`
	// DisplayName is what a relay peer calls itself, set only when it
	// differs from Name (i.e. only for relay peers, and only once they have
	// announced a name at all). A stored peer never sets it: Name already
	// is what the person who added it chose to see. The UI shows this over
	// Name when present; nothing here or on the wire ever addresses a peer
	// by it, which is what makes it safe for two peers to share one.
	DisplayName string `json:"displayName,omitempty"`
	// RelayID is the instance ID a call is addressed to when this peer is only
	// reachable through the relay, and is empty for every stored peer. It is
	// never written to instances.json, because it is never true for longer
	// than the relay connection that produced it: a relay peer comes and goes
	// live, and one remembered across a restart would be a peer this instance
	// cannot reach and cannot explain.
	//
	// The UI reads it to say how a peer is reached; the API layer does not
	// need to, which is the point of it being on the same struct.
	RelayID string `json:"relayId,omitempty"`
}

// RelayTransport is the relay client seen from here: the sibling list it keeps
// live, one call over it, and the ability to shut the connection down.
// Declared as an interface rather than taking *relay.Client so this package's
// tests can exercise a relay peer without a socket - *relay.Client satisfies
// it as it stands, with no adapter.
type RelayTransport interface {
	// Siblings is the instances the relay makes visible right now. It is empty
	// while the relay is unreachable, which is the whole of the outage
	// handling here: no relay, no relay peers, nothing else affected.
	Siblings() []relay.Announce
	// Proxy calls one sibling, addressed by its instance ID. authorization is
	// the Authorization header value the target should see, or "" for none.
	Proxy(ctx context.Context, target, method, path string, body []byte, authorization string) ([]byte, int, error)
	// Connected reports whether the socket to the relay is up right now.
	//
	// Siblings() alone cannot answer that: it is empty both when the relay is
	// unreachable and when it is perfectly fine but nobody else is on the key.
	// Those need telling apart - one is a configuration mistake to go fix, the
	// other is normal - and nothing above this could tell them apart before.
	Connected() bool
	// Close stops the connection for good. SetRelay calls it on whatever
	// transport it is replacing, which is what makes reconfiguring the relay
	// (a new address, a new key, or switching it off) safe to call as often
	// as a settings save happens, rather than leaking one socket per save.
	Close() error
}

var nameRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9 _.-]{0,31}$`)

// peerTimeout bounds one call to another instance. A peer that is switched off
// answers instantly; one that is reachable but wedged is the case this exists
// for, and the dashboard polls every peer, so a generous ceiling here is paid
// once per peer per tick.
const peerTimeout = 15 * time.Second

// Manager persists the peer list and talks to peers.
type Manager struct {
	path string
	hc   *http.Client

	mu   sync.Mutex
	list map[string]Instance // by name
	rt   RelayTransport      // nil until SetRelay, and again after it is cleared
	pt   PeerTokens          // nil until SetPeerTokens: peers are then called unauthenticated
}

// Load reads instances.json from dir (missing file = empty list).
func Load(dir string) (*Manager, error) {
	m := &Manager{
		path: filepath.Join(dir, "instances.json"),
		hc:   httpx.New(httpx.Options{Timeout: peerTimeout}),
		list: map[string]Instance{},
	}
	if b, err := os.ReadFile(m.path); err == nil {
		var arr []Instance
		if json.Unmarshal(b, &arr) == nil {
			for _, in := range arr {
				m.list[in.Name] = in
			}
		}
	}
	return m, nil
}

// RelayConnected reports whether a relay is configured AND its socket is up.
// False means either no relay at all or one that cannot be reached - the
// caller that wants to tell those apart has the stored config to check.
func (m *Manager) RelayConnected() bool {
	m.mu.Lock()
	rt := m.rt
	m.mu.Unlock()
	return rt != nil && rt.Connected()
}

// SetRelay installs the transport that reaches relay-visible peers, or clears
// it with nil when relay mode is switched off - and closes whatever transport
// it is replacing, so calling this again (a new address, a new key, an
// application restart's own first call) never leaks the previous connection.
// Peers the relay reports are not stored and not persisted: they appear and
// disappear with the relay connection itself, so there is nothing here to
// save and nothing to load back.
func (m *Manager) SetRelay(rt RelayTransport) {
	m.mu.Lock()
	prev := m.rt
	m.rt = rt
	m.mu.Unlock()
	if prev != nil {
		_ = prev.Close()
	}
}

// PeerTokens supplies the credential a call to one peer must carry.
//
// A hook rather than a field on Instance, because an Instance is written to
// instances.json in plaintext and a bearer token is a secret - the same
// separation settings.RelayURL and the relay key already keep (see
// settings_relay.go's own doc comment). The implementation lives with the
// encrypted store; this package only asks.
//
// Empty is a valid answer and means "call it unauthenticated", which is what
// every peer call did before peer tokens existed and what a manually added
// peer still does.
type PeerTokens interface {
	TokenFor(peer string) string
}

// SetPeerTokens installs the lookup. Safe to call with nil, which restores the
// old credential-free behaviour.
func (m *Manager) SetPeerTokens(pt PeerTokens) {
	m.mu.Lock()
	m.pt = pt
	m.mu.Unlock()
}

func (m *Manager) tokenFor(peer string) string {
	m.mu.Lock()
	pt := m.pt
	m.mu.Unlock()
	if pt == nil {
		return ""
	}
	return pt.TokenFor(peer)
}

// List returns the peers sorted by what a person reads as their name: the
// stored ones plus whatever the relay currently makes visible, in one list,
// because the Instances page shows one list. Sorted by DisplayName where a
// relay peer has one, not by Name - Name is now always that peer's raw
// InstanceID (reachable's own doc comment explains why), and sorting on a
// column nobody sees would scatter relay peers through the list in an order
// that looks arbitrary next to the friendly names right beside them.
func (m *Manager) List() []Instance {
	all, _ := m.reachable()
	out := make([]Instance, 0, len(all))
	for _, in := range all {
		out = append(out, in)
	}
	label := func(in Instance) string {
		if in.DisplayName != "" {
			return in.DisplayName
		}
		return in.Name
	}
	sort.Slice(out, func(i, j int) bool { return label(out[i]) < label(out[j]) })
	return out
}

// reachable is every peer addressable right now, keyed by the name the API
// layer addresses it as, together with the transport the relay ones need. Both
// come out of one call so a relay that disconnects between resolving a name
// and using it cannot leave a relay peer with no way to reach it.
//
// A relay peer is always keyed by its InstanceID, never by the name it
// announced. An earlier version tried to key it by that name when nothing
// else had it yet, falling back to the ID only on a collision - which let
// the same peer's address change on its own: removing an unrelated stored
// peer, or an unrelated sibling connecting or disconnecting, could flip a
// peer already in use from one key to the other with nothing about that
// peer itself having changed. A stored peer's Name is validated against
// nameRe (max 32 chars); an InstanceID is 40 hex characters
// (newInstanceID, internal/settings), so the two key spaces can never
// collide - every peer's address is decided once, by what kind of peer it
// is, and never moves again for as long as it is reachable at all.
func (m *Manager) reachable() (map[string]Instance, RelayTransport) {
	m.mu.Lock()
	rt := m.rt
	out := make(map[string]Instance, len(m.list))
	for name, in := range m.list {
		out[name] = in
	}
	m.mu.Unlock()
	if rt == nil {
		return out, nil
	}
	for _, sib := range rt.Siblings() {
		// A client-only sibling (the mobile app) is on the key to CALL
		// instances, not to be one - listing it would offer a peer that
		// answers 501 to every route. See relay.Announce.Client.
		if sib.Client {
			continue
		}
		in := Instance{Name: sib.InstanceID, RelayID: sib.InstanceID}
		if sib.Name != "" && sib.Name != sib.InstanceID {
			in.DisplayName = sib.Name
		}
		out[sib.InstanceID] = in
	}
	return out, rt
}

// Add validates and stores a peer (overwrites the same name).
func (m *Manager) Add(in Instance) error {
	in.Name = strings.TrimSpace(in.Name)
	in.URL = strings.TrimRight(strings.TrimSpace(in.URL), "/")
	// A stored peer is an HTTP peer by definition, addressed by the Name it
	// is given here. Dropping these rather than rejecting them keeps the
	// route that decodes an Instance straight off a request body
	// (routes_federation.go) from turning a field the UI only reads into a
	// way to store a peer that claims a relay identity or a display name
	// that disagrees with its own address.
	in.RelayID = ""
	in.DisplayName = ""
	if !nameRe.MatchString(in.Name) {
		return errors.New("federation: invalid instance name")
	}
	u, err := url.Parse(in.URL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return errors.New("federation: instance URL must be http(s)")
	}
	m.mu.Lock()
	m.list[in.Name] = in
	err = m.flushLocked()
	m.mu.Unlock()
	return err
}

// Remove deletes a peer by name.
func (m *Manager) Remove(name string) error {
	m.mu.Lock()
	delete(m.list, name)
	err := m.flushLocked()
	m.mu.Unlock()
	return err
}

func (m *Manager) flushLocked() error {
	arr := make([]Instance, 0, len(m.list))
	for _, in := range m.list {
		arr = append(arr, in)
	}
	sort.Slice(arr, func(i, j int) bool { return arr[i].Name < arr[j].Name })
	b, err := json.MarshalIndent(arr, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(m.path, b, 0o600)
}

// Proxy forwards an API call to a peer and returns its response body.
// method + path are the peer-local API route (e.g. GET /api/tasks).
//
// The transport is chosen from the peer, not from the caller: a relay-visible
// peer goes over the relay, a stored one over HTTP, and the route that calls
// this (and the page behind it) never learns which.
func (m *Manager) Proxy(ctx context.Context, name, method, path string, body []byte) ([]byte, int, error) {
	all, rt := m.reachable()
	in, ok := all[name]
	if !ok {
		return nil, http.StatusNotFound, fmt.Errorf("federation: unknown instance %q", name)
	}
	// rt is never nil here when RelayID is set: the entry came from that same
	// transport in the same call, above.
	//
	// The credential is looked up by the name this peer is ADDRESSED as, which
	// is not the same key for both transports and is worth being precise
	// about, because an earlier version of this comment claimed it was.
	//
	// A stored peer is addressed by its pairing name; a RELAY peer by its
	// 40-hex InstanceID (see reachable). Both are looked up here by the key
	// they are actually addressed as, which is the key the pairing exchange
	// files the credential under - routes_pairing.go picks one or the other
	// depending on how the pairing travelled.
	//
	// That symmetry is newer than it looks. While pairing was HTTP-only, a
	// relay peer's credential could only ever be filed under a name, the lookup
	// here asked for an id, and nothing matched - so relay peers were called
	// unauthenticated and a password-protected one refused everything, which is
	// issue #26 surviving in exactly the deployment the relay exists for.
	auth := ""
	if tok := m.tokenFor(name); tok != "" {
		auth = "Bearer " + tok
	}
	if in.RelayID != "" {
		return rt.Proxy(ctx, in.RelayID, method, path, body, auth)
	}
	var rd io.Reader
	if len(body) > 0 {
		rd = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, in.URL+path, rd)
	if err != nil {
		return nil, http.StatusInternalServerError, err
	}
	if len(body) > 0 {
		req.Header.Set("Content-Type", "application/json")
	}
	if auth != "" {
		req.Header.Set("Authorization", auth)
	}
	resp, err := m.hc.Do(req)
	if err != nil {
		return nil, http.StatusBadGateway, fmt.Errorf("federation: %s unreachable: %w", in.Name, err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
	return b, resp.StatusCode, nil
}

// ErrUnauthorized is what Ping reports when a peer was REACHED and refused the
// call. Distinguished from every other failure because the fix is completely
// different and the two look identical from outside: unreachable means check
// the address or the network, refused means this instance holds no credential
// the peer accepts, which pairing is what supplies. Reporting both as "offline"
// is what made a password-protected peer indistinguishable from a switched-off
// one - the state issue #26 was about.
var ErrUnauthorized = errors.New("federation: the peer refused this instance's credentials")

// Ping checks a peer by listing its tasks.
func (m *Manager) Ping(ctx context.Context, name string) error {
	_, code, err := m.Proxy(ctx, name, http.MethodGet, "/api/tasks", nil)
	if err != nil {
		return err
	}
	if code == http.StatusUnauthorized || code == http.StatusForbidden {
		return ErrUnauthorized
	}
	if code != http.StatusOK {
		return fmt.Errorf("federation: peer answered HTTP %d", code)
	}
	return nil
}
