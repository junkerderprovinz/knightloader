package federation

import (
	"encoding/json"
	"os"
	"sync"
	"time"
)

// groupFile keeps when this instance entered its phrase group and when another
// member first showed up there. The Pairing tab tells a group still waiting
// for its second instance from two instances that each generated a phrase of
// their own by it, on the server's clock rather than the browser's.
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
	// MemberSeenAt is when another instance of the group was first visible
	// after JoinedAt, zero until one has been.
	MemberSeenAt time.Time `json:"memberSeenAt"`
}

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

// Members returns the other instances of the group the relay shows right now.
// Instances added by address are not members: they share no phrase.
func (m *Manager) Members() []Instance {
	var out []Instance
	for _, in := range m.List() {
		if in.RelayID != "" {
			out = append(out, in)
		}
	}
	return out
}

// Group reports where this instance stands in its group. members is who is
// there at now, and the first time it holds anyone is kept as MemberSeenAt.
func (m *Manager) Group(members []Instance, now time.Time) (GroupState, error) {
	m.group.mu.Lock()
	defer m.group.mu.Unlock()
	st := m.group.state
	if len(members) > 0 && !st.JoinedAt.IsZero() && st.MemberSeenAt.IsZero() {
		m.group.state.MemberSeenAt = now
		if err := m.group.flushLocked(); err != nil {
			return st, err
		}
		st = m.group.state
	}
	return st, nil
}
