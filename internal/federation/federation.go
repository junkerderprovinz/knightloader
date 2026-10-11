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
	// RelayID is the instance ID of a member of this instance's phrase group,
	// reached directly on this network or through the relay, and empty for
	// stored peers. It is never persisted, since it is only true while the
	// member is reachable.
	RelayID string `json:"relayId,omitempty"`
	// Address is where a member's web interface is, as it announced it: its
	// first known domain, else its address on its network.
	Address string `json:"address,omitempty"`
	// Deployment is what a member announced itself as, "container" or
	// "desktop" (buildinfo.Deployment), so its card can say which it is. A
	// stored peer never announced anything and leaves it empty.
	Deployment string `json:"deployment,omitempty"`

	// directURL is where a member on this network takes direct calls, and
	// viaRelay whether the relay reaches it too.
	directURL string
	viaRelay  bool
}

// Direct reports whether the member announced itself on this network.
func (in Instance) Direct() bool { return in.directURL != "" }

// ViaRelay reports whether the member is connected to the same relay.
func (in Instance) ViaRelay() bool { return in.viaRelay }

// RelayTransport is the relay client as this package uses it. It is an
// interface so tests can run relay peers without a socket; *relay.Client
// satisfies it as is.
type RelayTransport interface {
	// Siblings is the instances the relay makes visible now; empty while the
	// relay is unreachable.
	Siblings() []relay.Announce
	// Proxy calls one sibling by instance ID and returns its answer once the
	// sibling's key signed it.
	Proxy(ctx context.Context, target, method, path string, body []byte) ([]byte, int, error)
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
	// retired is the connection to the group this instance left at a
	// password reset, nil when there is none (see SetRetired).
	retired RelayTransport

	local  LocalNetwork // nil where discovery does not run
	keys   *groupKeys   // nil outside a phrase group
	selfID string

	group  *groupFile
	replay *relay.ReplayGuard
	limits *memberLimits
	// awaiting is when each member last answered that this instance waits
	// for approval there, heard which members served a call from it, and
	// greeted when Greet last called each.
	awaiting map[string]time.Time
	heard    map[string]bool
	greeted  map[string]time.Time
}

// Load reads instances.json and group.json from dir; a missing file is an
// empty list and no group, and a damaged group.json is an error.
func Load(dir string) (*Manager, error) {
	group, err := loadGroupFile(filepath.Join(dir, "group.json"))
	if err != nil {
		return nil, err
	}
	m := &Manager{
		path:   filepath.Join(dir, "instances.json"),
		hc:     httpx.New(httpx.Options{Timeout: peerTimeout}),
		list:   map[string]Instance{},
		group:  group,
		replay: relay.NewReplayGuard(),
		limits: newMemberLimits(),
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

// SetRetired installs the relay connection an instance keeps to the group it
// left at a password reset, or clears it with nil, and closes the one it
// replaces. The connection closes by itself at until.
func (m *Manager) SetRetired(rt RelayTransport, until time.Time) {
	m.mu.Lock()
	prev := m.retired
	m.retired = rt
	m.mu.Unlock()
	if prev != nil {
		_ = prev.Close()
	}
	if rt == nil {
		return
	}
	time.AfterFunc(time.Until(until), func() {
		m.mu.Lock()
		current := m.retired == rt
		if current {
			m.retired = nil
		}
		m.mu.Unlock()
		if current {
			_ = rt.Close()
		}
	})
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
// when other peers come and go. The announcer chooses that id, so a member is
// listed only under one shaped like an InstanceID and never in place of a
// stored peer, whose calls and credential would otherwise go to it.
func (m *Manager) reachable() (map[string]Instance, RelayTransport) {
	local := m.localMembers()
	m.mu.Lock()
	rt := m.rt
	out := make(map[string]Instance, len(m.list))
	for name, in := range m.list {
		out[name] = in
	}
	m.mu.Unlock()
	member := func(id, name string) Instance {
		in := Instance{Name: id, RelayID: id}
		if name != "" && name != id {
			in.DisplayName = name
		}
		return in
	}
	admit := func(id string) bool {
		in, taken := out[id]
		return relay.ValidInstanceID(id) && (!taken || in.RelayID != "")
	}
	for _, p := range local {
		if !admit(p.ID) {
			continue
		}
		in := member(p.ID, p.Name)
		in.directURL = p.URL
		in.Address = p.Address
		in.Deployment = p.Deployment
		out[p.ID] = in
	}
	if rt == nil {
		return out, nil
	}
	for _, sib := range rt.Siblings() {
		// A client-only sibling (the mobile app) calls instances but is not
		// one; see relay.Announce.Client.
		if sib.Client || !admit(sib.InstanceID) {
			continue
		}
		in, ok := out[sib.InstanceID]
		if !ok {
			in = member(sib.InstanceID, sib.Name)
		}
		if in.Address == "" {
			in.Address = sib.Address
		}
		if in.Deployment == "" {
			in.Deployment = sib.Deployment
		}
		in.viaRelay = true
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
	in.Address = ""
	in.Deployment = ""
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
	// A member of the group is called with no credential: its key signs
	// the call, and an answer counts only once the member's key signed it
	// in turn, so a stranger holding the words that takes the member's place
	// learns nothing it could use and cannot answer for it.
	if in.RelayID != "" {
		out, code, err := m.proxyMember(ctx, in, rt, method, path, body)
		if err == nil {
			m.noteAnswer(in.RelayID, code, out, time.Now())
		}
		return out, code, err
	}
	auth := ""
	if tok := m.tokenFor(in.Name); tok != "" {
		auth = "Bearer " + tok
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

// proxyMember calls a member of the group: directly when it is on this
// network, and through the relay when that fails or the member is elsewhere.
// rt is set whenever viaRelay is, since both came from the same snapshot.
func (m *Manager) proxyMember(ctx context.Context, in Instance, rt RelayTransport, method, path string, body []byte) ([]byte, int, error) {
	if in.directURL != "" {
		out, code, err := m.callDirect(ctx, in.directURL, in.RelayID, method, path, body)
		if err == nil || !in.viaRelay {
			return out, code, err
		}
	}
	return rt.Proxy(ctx, in.RelayID, method, path, body)
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
