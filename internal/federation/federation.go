// Package federation lets one KnightLoader act as the dashboard for others:
// peer instances are stored locally, and their REST APIs are proxied so the UI
// can view and control every instance from one place.
//
// A peer is reached over HTTP at a stored address, or through a self-hosted
// relay both sides dial out to when neither accepts inbound connections.
// List and Proxy hide which. The relay is optional.
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
	// Name is the stable key routes address a peer by
	// (/api/instances/{name}/...): the chosen name for a stored peer, the
	// InstanceID for a relay peer. A relay peer's announced name is not used,
	// since peers may share or change it; see DisplayName.
	Name string `json:"name"`
	URL  string `json:"url"`
	// DisplayName is what a relay peer calls itself, set only when it differs
	// from Name. The UI shows it; nothing addresses a peer by it.
	DisplayName string `json:"displayName,omitempty"`
	// RelayID is the instance ID to address when the peer is only reachable
	// through the relay, and empty for stored peers. It is never persisted,
	// since it is only true while the relay connection lasts.
	RelayID string `json:"relayId,omitempty"`
}

// RelayTransport is the relay client as this package uses it. It is an
// interface so tests can run relay peers without a socket; *relay.Client
// satisfies it as is.
type RelayTransport interface {
	// Siblings is the instances the relay makes visible now; empty while the
	// relay is unreachable.
	Siblings() []relay.Announce
	// Proxy calls one sibling by instance ID. authorization is the
	// Authorization header value the target should see, or "".
	Proxy(ctx context.Context, target, method, path string, body []byte, authorization string) ([]byte, int, error)
	// Connected reports whether the socket to the relay is up. Siblings alone
	// cannot tell an unreachable relay from one where nobody else is on the
	// key.
	Connected() bool
	// Close stops the connection for good.
	Close() error
}

var nameRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9 _.-]{0,31}$`)

// peerTimeout bounds one call to another instance, for a peer that is
// reachable but wedged.
const peerTimeout = 15 * time.Second

// Manager persists the peer list and talks to peers.
type Manager struct {
	path string
	hc   *http.Client

	mu   sync.Mutex
	list map[string]Instance // by name
	rt   RelayTransport      // nil while no relay is configured
	pt   PeerTokens          // nil means peers are called unauthenticated
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

// RelayConnected reports whether a relay is configured and its socket is up.
func (m *Manager) RelayConnected() bool {
	m.mu.Lock()
	rt := m.rt
	m.mu.Unlock()
	return rt != nil && rt.Connected()
}

// SetRelay installs the transport for relay peers, or clears it with nil, and
// closes the transport it replaces so repeated settings saves do not leak
// connections. Relay peers are never stored.
func (m *Manager) SetRelay(rt RelayTransport) {
	m.mu.Lock()
	prev := m.rt
	m.rt = rt
	m.mu.Unlock()
	if prev != nil {
		_ = prev.Close()
	}
}

// PeerTokens supplies the credential a call to one peer must carry. It is a
// hook rather than an Instance field because instances.json is plaintext and
// the token is a secret kept in the encrypted store. An empty token means the
// peer is called unauthenticated.
type PeerTokens interface {
	TokenFor(peer string) string
}

// SetPeerTokens installs the lookup; nil calls peers unauthenticated.
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

// List returns the stored peers and the relay-visible ones in one list,
// sorted by what a person reads: DisplayName where there is one, else Name.
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

// reachable returns every peer addressable now, keyed by name, together with
// the relay transport, from one snapshot so a relay peer never outlives its
// transport.
//
// A relay peer is always keyed by its InstanceID, so its address never shifts
// when other peers come and go. Stored names are at most 32 characters and
// InstanceIDs are 40 hex characters, so the two cannot collide.
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
		// A client-only sibling (the mobile app) calls instances but is not
		// one; see relay.Announce.Client.
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
	// A stored peer is an HTTP peer. The route decodes the request body
	// straight into an Instance, so these read-only fields are cleared.
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

// Proxy forwards an API call to a peer and returns its response body. method
// and path are the peer-local route (e.g. GET /api/tasks). The transport
// follows from the peer, so callers never learn which one was used.
func (m *Manager) Proxy(ctx context.Context, name, method, path string, body []byte) ([]byte, int, error) {
	all, rt := m.reachable()
	in, ok := all[name]
	if !ok {
		return nil, http.StatusNotFound, fmt.Errorf("federation: unknown instance %q", name)
	}
	// The token is looked up under the key the peer is addressed by: the
	// pairing name for a stored peer, the InstanceID for a relay peer. The
	// pairing exchange files it under the same key (routes_pairing.go).
	auth := ""
	if tok := m.tokenFor(name); tok != "" {
		auth = "Bearer " + tok
	}
	if in.RelayID != "" {
		// rt is set, since this entry came from it in the same snapshot.
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

// ErrUnauthorized is what Ping reports when a peer answered but refused the
// call. It is kept apart from being unreachable because the fix differs:
// pairing supplies the missing credential.
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
