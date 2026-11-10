package federation

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/fsretry"
	"github.com/junkerderprovinz/parleyport/kit/group"
	"github.com/junkerderprovinz/parleyport/kit/member"
)

// groupFile keeps when this instance entered its phrase group and when
// another member first showed up there. The Pairing card tells a group still
// waiting for its second member from two instances that each generated a
// phrase of their own by it, on the server's clock rather than the browser's.
// Who is in the group is kept by kit/group.
type groupFile struct {
	path string

	mu    sync.Mutex
	state GroupState
	// old is the group as releases before kit/group kept it in this file,
	// until the group is opened and takes it over.
	old oldGroup
}

// GroupState is where this instance stands in its group.
type GroupState struct {
	// JoinedAt is when this instance generated or entered its phrase, zero
	// outside a group.
	JoinedAt time.Time `json:"joinedAt"`
	// MemberSeenAt is when another instance or a phone of the group was first
	// there after JoinedAt, zero until one has been.
	MemberSeenAt time.Time `json:"memberSeenAt"`
	// RelayKeyDropped says the update took away the relay key typed in by
	// hand that this instance was paired by, until it pairs again with the
	// phrase.
	RelayKeyDropped bool `json:"relayKeyDropped,omitempty"`
}

// oldGroup is what group.json held of the group's members before kit/group.
type oldGroup struct {
	Apps    map[string]oldApp       `json:"apps,omitempty"`
	Members map[string]oldMember    `json:"members,omitempty"`
	Removed map[string]time.Time    `json:"removed,omitempty"`
	Keys    map[string]oldPinnedKey `json:"keys,omitempty"`
	Revoked map[string]time.Time    `json:"revoked,omitempty"`
}

type oldPinnedKey struct {
	Key      []byte      `json:"key"`
	Kind     member.Kind `json:"kind"`
	Used     time.Time   `json:"used"`
	Approved bool        `json:"approved"`
}

type oldApp struct {
	Name       string    `json:"name"`
	Deployment string    `json:"deployment,omitempty"`
	LastSeen   time.Time `json:"lastSeen"`
}

type oldMember struct {
	Name       string    `json:"name,omitempty"`
	Deployment string    `json:"deployment,omitempty"`
	Address    string    `json:"address,omitempty"`
	LastSeen   time.Time `json:"lastSeen"`
}

// GroupJoined reports whether b, a group.json read from a backup, belongs to
// an instance that was in a group.
func GroupJoined(b []byte) bool {
	var st GroupState
	return json.Unmarshal(b, &st) == nil && !st.JoinedAt.IsZero()
}

// loadGroupFile reads path. A file that is there but does not read is an
// error rather than an empty group, which would drop every approval, removal
// and revoked key it still holds from before kit/group.
func loadGroupFile(path string) (*groupFile, error) {
	g := &groupFile{path: path}
	b, err := os.ReadFile(path)
	switch {
	case errors.Is(err, os.ErrNotExist):
		return g, nil
	case err != nil:
		return nil, err
	}
	err = json.Unmarshal(b, &g.state)
	if err == nil {
		err = json.Unmarshal(b, &g.old)
	}
	if err != nil {
		return nil, fmt.Errorf("federation: %s is damaged; move it aside to start the group over: %w", path, err)
	}
	return g, nil
}

// flushLocked replaces the file through a synced temporary file in the same
// folder, so a crash leaves the old state or the new one and never a cut-off
// file.
func (g *groupFile) flushLocked() error {
	b, err := json.Marshal(g.state)
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(g.path), "."+filepath.Base(g.path)+".tmp-*")
	if err != nil {
		return err
	}
	tmp := f.Name()
	_, err = f.Write(b)
	if err == nil {
		err = f.Sync()
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = fsretry.Rename(tmp, g.path)
	}
	if err != nil {
		os.Remove(tmp)
	}
	return err
}

// SetJoined records that this instance entered a group at at, or left its
// group when at is zero. Nobody has been seen in the group that follows.
func (m *Manager) SetJoined(at time.Time) error {
	m.group.mu.Lock()
	defer m.group.mu.Unlock()
	m.group.state = GroupState{JoinedAt: at}
	m.group.old = oldGroup{}
	return m.group.flushLocked()
}

// DropRelayKey records that this instance lost the group a relay key typed in
// by hand made, so the Pairing card can say why it stands alone.
func (m *Manager) DropRelayKey() error {
	m.group.mu.Lock()
	defer m.group.mu.Unlock()
	m.group.state = GroupState{RelayKeyDropped: true}
	m.group.old = oldGroup{}
	return m.group.flushLocked()
}

// Joined reports where this instance stands in its group. anyone is whether
// another instance or a phone is there at now, and the first time one is,
// is kept as MemberSeenAt.
func (m *Manager) Joined(anyone bool, now time.Time) (GroupState, error) {
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

// OldGroup is the group as releases before kit/group kept it, for
// group.Group.Import, and whether there was one. Every pinned key becomes a
// member under its approval and last use, named as its card was, and the
// removed ids and revoked keys go over as they are.
func (m *Manager) OldGroup() (group.Roster, bool) {
	m.group.mu.Lock()
	defer m.group.mu.Unlock()
	old := m.group.old
	if len(old.Keys) == 0 && len(old.Removed) == 0 && len(old.Revoked) == 0 {
		return group.Roster{}, false
	}
	r := group.Roster{Removed: old.Removed, Revoked: old.Revoked}
	for id, pin := range old.Keys {
		mb := group.Member{ID: id, Kind: pin.Kind, Key: pin.Key, State: group.StatePending, LastSeen: pin.Used}
		if pin.Approved {
			mb.State = group.StateApproved
		}
		if app, ok := old.Apps[id]; ok {
			mb.Name, mb.Deployment = app.Name, app.Deployment
			if app.LastSeen.After(mb.LastSeen) {
				mb.LastSeen = app.LastSeen
			}
		}
		if in, ok := old.Members[id]; ok {
			mb.Name, mb.Deployment, mb.Address = in.Name, in.Deployment, in.Address
			if in.LastSeen.After(mb.LastSeen) {
				mb.LastSeen = in.LastSeen
			}
		}
		r.Members = append(r.Members, mb)
	}
	return r, true
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

// GroupMembers lists the members of the group this instance is in, none
// outside one.
func (m *Manager) GroupMembers() []group.Member {
	m.mu.Lock()
	g := m.grp
	m.mu.Unlock()
	if g == nil {
		return nil
	}
	return g.Members()
}

// StillMember reports whether id may still act in the group here: approved
// and not taken out.
func (m *Manager) StillMember(id string) bool {
	for _, mb := range m.GroupMembers() {
		if mb.ID == id {
			return mb.State == group.StateApproved
		}
	}
	return false
}
