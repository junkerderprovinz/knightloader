package federation

import (
	"encoding/json"
	"os"
	"sort"
	"sync"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/relay"
)

// groupFile keeps when this instance entered its phrase group, when another
// member first showed up there, and the phones that joined it. The Pairing
// card tells a group still waiting for its second member from two instances
// that each generated a phrase of their own by it, on the server's clock
// rather than the browser's.
type groupFile struct {
	path string

	mu    sync.Mutex
	state GroupState
}

// GroupState is where this instance stands in its group.
type GroupState struct {
	// JoinedAt is when this instance generated or entered its phrase, zero
	// outside a group.
	JoinedAt time.Time `json:"joinedAt"`
	// MemberSeenAt is when another instance or a phone of the group was first
	// there after JoinedAt, zero until one has been.
	MemberSeenAt time.Time `json:"memberSeenAt"`
	// Apps is every phone seen in the group, by its relay id, so one that has
	// gone away still has a card saying when it was last there.
	Apps map[string]KnownApp `json:"apps,omitempty"`
	// Removed is every phone taken out of the group, by relay id and when.
	// Its calls are turned away; scanning the phrase again gives the app a
	// new id, so it comes back as a new phone.
	Removed map[string]time.Time `json:"removed,omitempty"`
}

// KnownApp is a phone or a browser extension that joined the group with the
// phrase.
type KnownApp struct {
	Name string `json:"name"`
	// Deployment is "mobile" or "extension", what the client announced.
	Deployment string    `json:"deployment,omitempty"`
	LastSeen   time.Time `json:"lastSeen"`
}

// App is one phone or browser extension of the group as the Instances page
// lists it.
type App struct {
	ID         string
	Name       string
	Deployment string
	Connected  bool
	LastSeen   time.Time
}

// clientDeployments are the clients that join a group without being an
// instance: the Android app and the browser extension.
var clientDeployments = map[string]bool{"mobile": true, "extension": true}

// lastSeenStep is how far a connected phone's last-seen time may lag before
// it is written again, so an open page does not rewrite the file on every
// poll.
const lastSeenStep = time.Minute

func loadGroupFile(path string) *groupFile {
	g := &groupFile{path: path}
	if b, err := os.ReadFile(path); err == nil {
		_ = json.Unmarshal(b, &g.state)
	}
	return g
}

func (g *groupFile) flushLocked() error {
	b, err := json.Marshal(g.state)
	if err != nil {
		return err
	}
	return os.WriteFile(g.path, b, 0o600)
}

// SetJoined records that this instance entered a group at at, or left its
// group when at is zero. Nobody has been seen in the group that follows.
func (m *Manager) SetJoined(at time.Time) error {
	m.group.mu.Lock()
	defer m.group.mu.Unlock()
	m.group.state = GroupState{JoinedAt: at}
	return m.group.flushLocked()
}

// Members returns the other instances of the group reachable right now,
// directly or through the relay. Instances added by address are not members:
// they share no phrase.
func (m *Manager) Members() []Instance {
	var out []Instance
	for _, in := range m.List() {
		if in.RelayID != "" {
			out = append(out, in)
		}
	}
	return out
}

// Apps returns every phone of the group, connected ones first, and records
// the ones connected at now.
func (m *Manager) Apps(now time.Time) ([]App, error) {
	m.mu.Lock()
	rt := m.rt
	m.mu.Unlock()
	connected := map[string]relay.Announce{}
	if rt != nil {
		for _, sib := range rt.Siblings() {
			if sib.Client && clientDeployments[sib.Deployment] {
				connected[sib.InstanceID] = sib
			}
		}
	}

	m.group.mu.Lock()
	defer m.group.mu.Unlock()
	st := &m.group.state
	var err error
	if !st.JoinedAt.IsZero() {
		dirty := false
		for id, sib := range connected {
			if _, gone := st.Removed[id]; gone {
				continue
			}
			known, ok := st.Apps[id]
			if !ok || known.Name != sib.Name || known.Deployment != sib.Deployment || now.Sub(known.LastSeen) >= lastSeenStep {
				if st.Apps == nil {
					st.Apps = map[string]KnownApp{}
				}
				st.Apps[id] = KnownApp{Name: sib.Name, Deployment: sib.Deployment, LastSeen: now}
				dirty = true
			}
		}
		if dirty {
			err = m.group.flushLocked()
		}
	}
	out := make([]App, 0, len(st.Apps))
	for id, known := range st.Apps {
		_, on := connected[id]
		deployment := known.Deployment
		if deployment == "" {
			// An entry stored without a kind is a phone's.
			deployment = "mobile"
		}
		out = append(out, App{ID: id, Name: known.Name, Deployment: deployment, Connected: on, LastSeen: known.LastSeen})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Connected != out[j].Connected {
			return out[i].Connected
		}
		if out[i].Name != out[j].Name {
			return out[i].Name < out[j].Name
		}
		return out[i].ID < out[j].ID
	})
	return out, err
}

// RemoveApp takes phone id out of the group at now: its card goes and its
// calls are turned away from then on.
func (m *Manager) RemoveApp(id string, now time.Time) error {
	m.group.mu.Lock()
	defer m.group.mu.Unlock()
	st := &m.group.state
	if _, done := st.Removed[id]; done {
		return nil
	}
	delete(st.Apps, id)
	if st.Removed == nil {
		st.Removed = map[string]time.Time{}
	}
	st.Removed[id] = now
	return m.group.flushLocked()
}

// Removed reports whether phone id was taken out of the group.
func (m *Manager) Removed(id string) bool {
	m.group.mu.Lock()
	defer m.group.mu.Unlock()
	_, gone := m.group.state.Removed[id]
	return gone
}

// Group reports where this instance stands in its group. anyone is whether
// another instance or a phone is there at now, and the first time one is,
// is kept as MemberSeenAt.
func (m *Manager) Group(anyone bool, now time.Time) (GroupState, error) {
	m.group.mu.Lock()
	defer m.group.mu.Unlock()
	st := m.group.state
	if anyone && !st.JoinedAt.IsZero() && st.MemberSeenAt.IsZero() {
		m.group.state.MemberSeenAt = now
		if err := m.group.flushLocked(); err != nil {
			return st, err
		}
		st = m.group.state
	}
	return st, nil
}
