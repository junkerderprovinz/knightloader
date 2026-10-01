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
}

// KnownApp is a phone that joined the group with the phrase.
type KnownApp struct {
	Name     string    `json:"name"`
	LastSeen time.Time `json:"lastSeen"`
}

// App is one phone of the group as the Instances page lists it.
type App struct {
	ID        string
	Name      string
	Connected bool
	LastSeen  time.Time
}

// appDeployment is what the Android app announces itself as, which tells it
// from the browser extension, the other client that joins a group.
const appDeployment = "mobile"

// lastSeenStep is how far a connected phone's last-seen time may lag before
// it is written again, so an open page does not rewrite the file on every
// poll.
const lastSeenStep = time.Minute

// maxApps bounds the phones group.json remembers. Every member can announce
// a phone, so without it one could grow the file with invented ones; past
// the cap the phone seen longest ago makes room.
const maxApps = 32

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
	connected := map[string]string{}
	if rt != nil {
		for _, sib := range rt.Siblings() {
			if sib.Client && sib.Deployment == appDeployment {
				connected[sib.InstanceID] = relay.ClipName(sib.Name)
			}
		}
	}

	m.group.mu.Lock()
	defer m.group.mu.Unlock()
	st := &m.group.state
	var err error
	if !st.JoinedAt.IsZero() {
		dirty := false
		for id, name := range connected {
			known, ok := st.Apps[id]
			if !ok || known.Name != name || now.Sub(known.LastSeen) >= lastSeenStep {
				if st.Apps == nil {
					st.Apps = map[string]KnownApp{}
				}
				if !ok && len(st.Apps) >= maxApps {
					forgetStalestApp(st.Apps)
				}
				st.Apps[id] = KnownApp{Name: name, LastSeen: now}
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
		out = append(out, App{ID: id, Name: known.Name, Connected: on, LastSeen: known.LastSeen})
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

func forgetStalestApp(apps map[string]KnownApp) {
	stalest := ""
	for id, known := range apps {
		if stalest == "" || known.LastSeen.Before(apps[stalest].LastSeen) {
			stalest = id
		}
	}
	delete(apps, stalest)
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
