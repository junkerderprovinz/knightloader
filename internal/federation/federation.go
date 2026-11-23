// Package federation lets one KnightLoader act as the dashboard for the other
// instances of its phrase group: their REST APIs are proxied so the UI can
// view and control every instance from one place. kit/group reaches a member
// directly on this network or through a relay, and List and Proxy hide which.
package federation

import (
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/junkerderprovinz/parleyport/kit/group"
	"github.com/junkerderprovinz/parleyport/kit/member"
	"github.com/junkerderprovinz/parleyport/kit/relay"
)

// Instance is another instance of the group, reachable now.
type Instance struct {
	// Name is the stable key routes address an instance by
	// (/api/instances/{name}/...): its member id. Its announced name is not
	// used, since instances may share or change it; see DisplayName.
	Name string `json:"name"`
	// DisplayName is what the instance calls itself, set only when it differs
	// from Name. The UI shows it; nothing addresses an instance by it.
	DisplayName string `json:"displayName,omitempty"`
	// RelayID is its member id, which Name carries as well.
	RelayID string `json:"relayId"`
	// Address is where its web interface is, as it announced it: its first
	// known domain, else its address on its network.
	Address string `json:"address,omitempty"`
	// Deployment is what it announced itself as, "container", "server" or
	// "desktop" (api.instanceKind), so its card can say which it is.
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

// Manager reaches the other instances of this instance's group.
type Manager struct {
	// opening keeps one group open at a time, as Reopen says.
	opening sync.Mutex

	mu  sync.Mutex
	grp GroupView // nil outside a group
	// retired is the connection to the group this instance left at a
	// password reset, nil when there is none (see SetRetired).
	retired io.Closer
	// shared is when SyncLegacy last asked each member for its roster.
	shared map[string]time.Time

	group *groupFile
}

// Load reads group.json from dir, a missing file being no group and a damaged
// one an error. Instances added by their address in instances.json give way
// to the group: their names are kept for the Instances page to name once,
// and the file goes.
func Load(dir string) (*Manager, error) {
	group, err := loadGroupFile(filepath.Join(dir, "group.json"))
	if err != nil {
		return nil, err
	}
	if err := group.dropInstances(filepath.Join(dir, "instances.json")); err != nil {
		return nil, err
	}
	return &Manager{group: group}, nil
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
func (m *Manager) Reopen(open func() (GroupView, error)) error {
	m.opening.Lock()
	defer m.opening.Unlock()
	m.SetGroup(nil)
	g, err := open()
	if g != nil {
		m.SetGroup(g)
	}
	return err
}

// View is what the members are reached through, nil outside a group.
func (m *Manager) View() GroupView {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.grp
}

// Group is the group this instance is in, nil outside one.
func (m *Manager) Group() *group.Group {
	m.mu.Lock()
	defer m.mu.Unlock()
	if p, ok := m.grp.(interface{ Pairing() *group.Group }); ok {
		return p.Pairing()
	}
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

// List returns the other instances of the group reachable now, sorted by what
// a person reads: DisplayName where there is one, else Name. Phones and
// browsers are left out, since they serve no API to go to.
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

// reachable returns every member addressable now, keyed by its id, together
// with the group, from one snapshot so a member never outlives its group. A
// member is listed only under an id shaped like an instance's.
func (m *Manager) reachable() (map[string]Instance, GroupView) {
	m.mu.Lock()
	g := m.grp
	m.mu.Unlock()
	out := map[string]Instance{}
	if g == nil {
		return out, nil
	}
	for _, mb := range g.Members() {
		if mb.Kind != member.KindInstance || !mb.Online() || !relay.ValidInstanceID(mb.ID) {
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

// Proxy forwards an API call to the member name names and returns its answer.
// method and path are the member-local route (e.g. GET /api/tasks). A member
// is called with no credential: its key signs the call, and an answer counts
// only once the member's key signed it in turn, so a stranger holding the
// words that takes the member's place learns nothing it could use and cannot
// answer for it.
func (m *Manager) Proxy(ctx context.Context, name, method, path string, body []byte) ([]byte, int, error) {
	all, g := m.reachable()
	in, ok := all[name]
	if !ok {
		return nil, http.StatusNotFound, fmt.Errorf("federation: unknown instance %q", name)
	}
	code, out, err := g.Call(ctx, in.RelayID, method, path, body)
	if err != nil {
		return nil, http.StatusBadGateway, fmt.Errorf("federation: %s: %w", in.RelayID, err)
	}
	return out, code, nil
}
