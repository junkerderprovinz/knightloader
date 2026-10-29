package federation

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
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
	// Apps is every phone and browser seen in the group, by appID, so one
	// that has gone away still has a card saying when it was last there.
	Apps map[string]KnownApp `json:"apps,omitempty"`
	// Members is every other instance approved in the group, by its id, as
	// it last announced itself, so one that is closed or away keeps its card.
	Members map[string]KnownMember `json:"members,omitempty"`
	// Removed is every phone and browser taken out of the group, by appID
	// and when. Its calls are turned away; scanning the phrase again gives the
	// app a new id, so it comes back as a new device. An instance's id is
	// never in it.
	Removed map[string]time.Time `json:"removed,omitempty"`
	// Keys is the member key pinned to every member id that signed a call
	// here, and Revoked every key taken out of the group, by its hex, with
	// when. See relay/member.go.
	Keys    map[string]PinnedKey `json:"keys,omitempty"`
	Revoked map[string]time.Time `json:"revoked,omitempty"`
}

// PinnedKey is the key a member signs with, pinned on its first signed call.
type PinnedKey struct {
	Key  []byte `json:"key"`
	Kind string `json:"kind"`
	// Used is when the key last signed a call here, kept to within
	// lastSeenStep.
	Used time.Time `json:"used"`
	// Approved is whether somebody at an instance let the member in. A key
	// that is pinned but not approved is refused.
	Approved bool `json:"approved"`
}

// KnownApp is a phone or a browser extension that joined the group with the
// phrase.
type KnownApp struct {
	Name string `json:"name"`
	// Deployment is "mobile" or "extension", what the client announced.
	Deployment string    `json:"deployment,omitempty"`
	LastSeen   time.Time `json:"lastSeen"`
}

// KnownMember is another instance of the group as it last announced itself.
type KnownMember struct {
	// Name is "" when the member announced none of its own.
	Name       string    `json:"name,omitempty"`
	Deployment string    `json:"deployment,omitempty"`
	Address    string    `json:"address,omitempty"`
	LastSeen   time.Time `json:"lastSeen"`
}

// AwayMember is an instance approved in the group that is not reachable now.
type AwayMember struct {
	ID string
	KnownMember
}

// App is one phone or browser extension of the group as the Instances page
// lists it.
type App struct {
	ID         string
	Name       string
	Deployment string
	Connected  bool
	// Approved is whether its pinned key was let in. A browser extension
	// holds its relay socket only for the length of a call, so between calls
	// this is what says it is still part of the group.
	Approved bool
	LastSeen time.Time
}

// clientDeployments are the clients that join a group without being an
// instance: the Android app and the browser extension.
var clientDeployments = map[string]bool{relay.KindMobile: true, relay.KindExtension: true}

// lastSeenStep is how far a connected phone's last-seen time may lag before
// it is written again, so an open page does not rewrite the file on every
// poll.
const lastSeenStep = time.Minute

// maxApps bounds the phones group.json remembers. Every member can announce
// a phone, so without it one could grow the file with invented ones; past
// the cap the phone seen longest ago makes room.
const maxApps = 32

// GroupJoined reports whether b, a group.json read from a backup, belongs to
// an instance that was in a group.
func GroupJoined(b []byte) bool {
	var st GroupState
	return json.Unmarshal(b, &st) == nil && !st.JoinedAt.IsZero()
}

// loadGroupFile reads path. A file that is there but does not read is an
// error rather than an empty group, which would drop every approval, removal
// and revoked key and let in again whoever they kept out.
func loadGroupFile(path string) (*groupFile, error) {
	g := &groupFile{path: path}
	b, err := os.ReadFile(path)
	switch {
	case errors.Is(err, os.ErrNotExist):
		return g, nil
	case err != nil:
		return nil, err
	}
	if err := json.Unmarshal(b, &g.state); err != nil {
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
		err = os.Rename(tmp, g.path)
	}
	if err != nil {
		os.Remove(tmp)
	}
	return err
}

// SetJoined records that this instance entered a group at at, or left its
// group when at is zero. Nobody has been seen in the group that follows, and
// every member that signs in has to be approved here, the instances that were
// there before this one included.
func (m *Manager) SetJoined(at time.Time) error {
	m.mu.Lock()
	m.awaiting, m.heard, m.greeted, m.shared = nil, nil, nil, nil
	m.mu.Unlock()
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

// AwayMembers records the members approved here that are reachable at now and
// returns the ones that are not, as each last announced itself. A desktop app
// that was closed stays in the group until somebody takes it out.
func (m *Manager) AwayMembers(now time.Time) ([]AwayMember, error) {
	there := map[string]Instance{}
	for _, in := range m.Members() {
		there[in.RelayID] = in
	}

	m.group.mu.Lock()
	defer m.group.mu.Unlock()
	st := &m.group.state
	if st.JoinedAt.IsZero() {
		return nil, nil
	}
	dirty := false
	for id, in := range there {
		if !approvedInstanceLocked(st, id) {
			continue
		}
		seen := KnownMember{Name: in.DisplayName, Deployment: in.Deployment, Address: in.Address, LastSeen: now}
		known, ok := st.Members[id]
		if ok && known.Name == seen.Name && known.Deployment == seen.Deployment && known.Address == seen.Address &&
			now.Sub(known.LastSeen) < lastSeenStep {
			continue
		}
		if st.Members == nil {
			st.Members = map[string]KnownMember{}
		}
		st.Members[id] = seen
		dirty = true
	}
	var err error
	if dirty {
		err = m.group.flushLocked()
	}

	var out []AwayMember
	for id, known := range st.Members {
		if _, on := there[id]; on {
			continue
		}
		if used := st.Keys[id].Used; used.After(known.LastSeen) {
			known.LastSeen = used
		}
		out = append(out, AwayMember{ID: id, KnownMember: known})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Name != out[j].Name {
			return out[i].Name < out[j].Name
		}
		return out[i].ID < out[j].ID
	})
	return out, err
}

// approvedInstanceLocked reports whether id is an instance whose key was let
// into the group here. Only such a member is remembered while it is away, so
// a stranger holding the words leaves nothing behind.
func approvedInstanceLocked(st *GroupState, id string) bool {
	pin, ok := st.Keys[id]
	return ok && pin.Approved && pin.Kind == relay.KindInstance
}

// Apps returns every phone and browser of the group, connected ones first,
// and records the ones connected at now. A card is kept only under an id
// shaped like the ones its kind mints, so no announce puts one under an
// instance's id. A device counts as seen whenever its key last signed a call
// here, which catches the calls made while nobody had the page open.
func (m *Manager) Apps(now time.Time) ([]App, error) {
	m.mu.Lock()
	rt := m.rt
	m.mu.Unlock()
	connected := map[string]relay.Announce{}
	if rt != nil {
		for _, sib := range rt.Siblings() {
			if id := appID(sib); sib.Client && clientDeployments[sib.Deployment] && kindOfID(id) == sib.Deployment {
				sib.Name = relay.ClipName(sib.Name)
				connected[id] = sib
			}
		}
	}

	m.group.mu.Lock()
	defer m.group.mu.Unlock()
	st := &m.group.state
	var err error
	if !st.JoinedAt.IsZero() {
		dirty := false
		for id, known := range st.Apps {
			if kindOfID(id) != deploymentOf(known) {
				delete(st.Apps, id)
				dirty = true
			}
		}
		for id, sib := range connected {
			if _, gone := st.Removed[id]; gone {
				continue
			}
			known, ok := st.Apps[id]
			if !ok || known.Name != sib.Name || known.Deployment != sib.Deployment || now.Sub(known.LastSeen) >= lastSeenStep {
				if st.Apps == nil {
					st.Apps = map[string]KnownApp{}
				}
				if !ok && len(st.Apps) >= maxApps {
					forgetStalestApp(st.Apps)
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
		pin := st.Keys[id]
		seen := known.LastSeen
		if pin.Used.After(seen) {
			seen = pin.Used
		}
		out = append(out, App{ID: id, Name: known.Name, Deployment: deploymentOf(known), Connected: on, Approved: pin.Approved, LastSeen: seen})
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

// appID is what a phone or browser is known by in the group: the lasting
// member id it announces, or its relay id when it has none.
func appID(sib relay.Announce) string {
	if sib.Member != "" {
		return sib.Member
	}
	return sib.InstanceID
}

// deploymentOf is the kind of device a card is for. One stored without a kind
// is a phone's.
func deploymentOf(known KnownApp) string {
	if known.Deployment == "" {
		return relay.KindMobile
	}
	return known.Deployment
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

// ErrIsInstance is what RemoveApp answers for an id that is not shaped like a
// phone's or a browser's. An instance leaves through RemoveMember instead.
var ErrIsInstance = errors.New("federation: that id belongs to an instance of the group, not to a phone or a browser")

// RemoveApp takes phone or browser id out of the group at now: its card goes,
// its pinned key is revoked and its calls are turned away from then on. The id
// decides the kind, never a card or a key under it, so nothing a device says
// about itself can have an instance taken out this way.
func (m *Manager) RemoveApp(id string, now time.Time) error {
	if kind := kindOfID(id); kind != relay.KindMobile && kind != relay.KindExtension {
		return ErrIsInstance
	}
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
	revokeLocked(st, id, now)
	return m.group.flushLocked()
}

// ErrNotInstance is what RemoveMember answers for a phone or a browser, which
// leave through RemoveApp instead.
var ErrNotInstance = errors.New("federation: that id belongs to a phone or a browser of the group, not to an instance")

// RemoveMember revokes the key pinned to instance id at now and forgets its
// card. Its id stays free, so the instance can enter the phrase again and join
// under a new key; the old key never speaks here again.
func (m *Manager) RemoveMember(id string, now time.Time) error {
	if !relay.ValidInstanceID(id) {
		return ErrNotInstance
	}
	m.group.mu.Lock()
	defer m.group.mu.Unlock()
	st := &m.group.state
	_, known := st.Members[id]
	delete(st.Members, id)
	if !revokeLocked(st, id, now) && !known {
		return nil
	}
	return m.group.flushLocked()
}

// revokeLocked moves the key pinned to id onto the revoked list and reports
// whether there was one.
func revokeLocked(st *GroupState, id string, now time.Time) bool {
	pin, ok := st.Keys[id]
	if !ok {
		return false
	}
	delete(st.Keys, id)
	if st.Revoked == nil {
		st.Revoked = map[string]time.Time{}
	}
	if len(st.Revoked) >= maxRevoked {
		oldest := ""
		for key, at := range st.Revoked {
			if oldest == "" || at.Before(st.Revoked[oldest]) {
				oldest = key
			}
		}
		delete(st.Revoked, oldest)
	}
	st.Revoked[hex.EncodeToString(pin.Key)] = now
	return true
}

// Removed reports whether phone or browser id was taken out of the group.
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
