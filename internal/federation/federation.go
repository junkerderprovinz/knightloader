// Package federation lets one KnightLoader act as the dashboard for others:
// peer instances are stored locally, and their REST APIs are proxied so the UI
// can view and control every instance from one place.
//
// A peer is reached over HTTP at a stored address, or as a member of this
// instance's phrase group through kit/group, which calls it directly on this
// network or through a relay. List and Proxy hide which.
package federation

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
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
	"github.com/junkerderprovinz/parleyport/kit/group"
	"github.com/junkerderprovinz/parleyport/kit/member"
	"github.com/junkerderprovinz/parleyport/kit/relay"
)

// Instance is a peer KnightLoader, either reachable over HTTP at URL or a
// member of this instance's group under RelayID.
type Instance struct {
	// Name is the stable key routes address a peer by
	// (/api/instances/{name}/...): the chosen name for a stored peer, the
	// member id for a member of the group. A member's announced name is not
	// used, since members may share or change it; see DisplayName.
	Name string `json:"name"`
	URL  string `json:"url"`
	// DisplayName is what a member calls itself, set only when it differs
	// from Name. The UI shows it; nothing addresses a peer by it.
	DisplayName string `json:"displayName,omitempty"`
	// RelayID is the member id of an instance of this instance's group,
	// reached directly on this network or through the relay, and empty for
	// stored peers. It is never persisted, since it is only true while the
	// member is reachable.
	RelayID string `json:"relayId,omitempty"`
	// Address is where a member's web interface is, as it announced it: its
	// first known domain, else its address on its network.
	Address string `json:"address,omitempty"`
	// Deployment is what a member announced itself as, "container", "server"
	// or "desktop" (api.instanceKind), so its card can say which it is. A
	// stored peer never announced anything and leaves it empty.
	Deployment string `json:"deployment,omitempty"`

	direct, viaRelay bool
}

// Direct reports whether the member is reachable on this network.
func (in Instance) Direct() bool { return in.direct }

// ViaRelay reports whether the member is connected to the same relay.
func (in Instance) ViaRelay() bool { return in.viaRelay }

// GroupView is the part of *group.Group the Manager lists and calls members
// through, so tests can stand in for a whole group.
type GroupView interface {
	Members() []group.Member
	Call(ctx context.Context, id, method, path string, body []byte) (int, []byte, error)
	RelayConnected() bool
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

	// opening keeps one group open at a time, as Reopen says.
	opening sync.Mutex

	mu   sync.Mutex
	list map[string]Instance // by name
	grp  GroupView           // nil outside a group
	pt   PeerTokens          // nil means peers are called unauthenticated
	// retired is the connection to the group this instance left at a
	// password reset, nil when there is none (see SetRetired).
	retired io.Closer
	// shared is when SyncLegacy last asked each member for its roster.
	shared map[string]time.Time

	group *groupFile
}

// Load reads instances.json and group.json from dir; a missing file is an
// empty list and no group, and a damaged group.json is an error.
func Load(dir string) (*Manager, error) {
	group, err := loadGroupFile(filepath.Join(dir, "group.json"))
	if err != nil {
		return nil, err
	}
	m := &Manager{
		path:  filepath.Join(dir, "instances.json"),
		hc:    httpx.New(httpx.Options{Timeout: peerTimeout}),
		list:  map[string]Instance{},
		group: group,
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

// RelayConnected reports whether this instance is in a group and its relay
// socket is up.
func (m *Manager) RelayConnected() bool {
	m.mu.Lock()
	g := m.grp
	m.mu.Unlock()
	return g != nil && g.RelayConnected()
}

// SetGroup installs the group members are listed and called through, or
// leaves it with nil, and closes the group it replaces.
func (m *Manager) SetGroup(g GroupView) {
	m.mu.Lock()
	prev := m.grp
	m.grp = g
	m.shared = nil
	m.mu.Unlock()
	if prev != nil {
		if err := prev.Close(); err != nil {
			log.Printf("federation: could not save the group it left: %v", err)
		}
	}
}

// Reopen closes the group this instance is in and installs the one open
// returns, nil for none, even alongside an error. Only one runs at a time:
// two groups open at once would each take over the network announce and the
// relay connection of this instance.
func (m *Manager) Reopen(open func() (*group.Group, error)) error {
	m.opening.Lock()
	defer m.opening.Unlock()
	m.SetGroup(nil)
	g, err := open()
	if g != nil {
		m.SetGroup(g)
	}
	return err
}

// Group is the group this instance is in, nil outside one.
func (m *Manager) Group() *group.Group {
	m.mu.Lock()
	defer m.mu.Unlock()
	g, _ := m.grp.(*group.Group)
	return g
}

// SetRetired installs the relay connection an instance keeps to the group it
// left at a password reset, or clears it with nil, and closes the one it
// replaces. The connection closes by itself at until.
func (m *Manager) SetRetired(c io.Closer, until time.Time) {
	m.mu.Lock()
	prev := m.retired
	m.retired = c
	m.mu.Unlock()
	if prev != nil {
		_ = prev.Close()
	}
	if c == nil {
		return
	}
	time.AfterFunc(time.Until(until), func() {
		m.mu.Lock()
		current := m.retired == c
		if current {
			m.retired = nil
		}
		m.mu.Unlock()
		if current {
			_ = c.Close()
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

// List returns the stored peers and the members of the group reachable now in
// one list, sorted by what a person reads: DisplayName where there is one,
// else Name.
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
// the group, from one snapshot so a member never outlives its group.
//
// A member is always keyed by its id, so its address never shifts when other
// peers come and go. It is listed only under an id shaped like an instance's
// and never in place of a stored peer, whose calls and credential would
// otherwise go to it.
func (m *Manager) reachable() (map[string]Instance, GroupView) {
	m.mu.Lock()
	g := m.grp
	out := make(map[string]Instance, len(m.list))
	for name, in := range m.list {
		out[name] = in
	}
	m.mu.Unlock()
	if g == nil {
		return out, nil
	}
	for _, mb := range g.Members() {
		if mb.Kind != member.KindInstance || !mb.Online() || !relay.ValidInstanceID(mb.ID) {
			continue
		}
		if _, taken := out[mb.ID]; taken {
			continue
		}
		in := Instance{Name: mb.ID, RelayID: mb.ID, Address: mb.Address, Deployment: mb.Deployment, direct: mb.Direct, viaRelay: mb.Relay}
		if mb.Name != "" && mb.Name != mb.ID {
			in.DisplayName = mb.Name
		}
		out[mb.ID] = in
	}
	return out, g
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
	all, g := m.reachable()
	in, ok := all[name]
	if !ok {
		return nil, http.StatusNotFound, fmt.Errorf("federation: unknown instance %q", name)
	}
	// A member of the group is called with no credential: its key signs
	// the call, and an answer counts only once the member's key signed it
	// in turn, so a stranger holding the words that takes the member's place
	// learns nothing it could use and cannot answer for it.
	if in.RelayID != "" {
		code, out, err := g.Call(ctx, in.RelayID, method, path, body)
		if err != nil {
			return nil, http.StatusBadGateway, fmt.Errorf("federation: %s: %w", in.RelayID, err)
		}
		return out, code, nil
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
