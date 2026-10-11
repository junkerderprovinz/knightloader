package federation

import (
	"encoding/hex"
	"encoding/json"
	"errors"
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
	// Apps is every phone seen in the group, by appID, so one that has
	// gone away still has a card saying when it was last there.
	Apps map[string]KnownApp `json:"apps,omitempty"`
	// Removed is every phone taken out of the group, by appID and when.
	// Its calls are turned away; scanning the phrase again gives the app a
	// new id, so it comes back as a new phone.
	Removed map[string]time.Time `json:"removed,omitempty"`
	// Keys is the member key pinned to every member id that signed a call
	// here, and Revoked every key taken out of the group, by its hex, with
	// when. See relay/member.go.
	Keys    map[string]PinnedKey `json:"keys,omitempty"`
	Revoked map[string]time.Time `json:"revoked,omitempty"`
	// KeysSince is when this instance first held the group with member keys.
	// Members on a release before keys were never approved, since there was
	// nothing to approve: Legacy holds the ids of the phones and browsers known
	// at that moment, and nothing that arrives later adds to it, or anybody
	// holding the words could name themselves a member. Their keys count as
	// approved when they are pinned. An id taken out stays in it as false.
	KeysSince time.Time       `json:"keysSince"`
	Legacy    map[string]bool `json:"legacy,omitempty"`
	// TrustUntil is set when this instance entered the words of an existing
	// group: an instance that signs in before then and is already listed in
	// the group, on this network or on the relay, was there before this one
	// and counts as approved here.
	TrustUntil time.Time `json:"trustUntil"`
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

// maxApps bounds the phones group.json remembers. Every member can announce
// a phone, so without it one could grow the file with invented ones; past
// the cap the phone seen longest ago makes room.
const maxApps = 32

// trustWindow is how long an instance that entered the words of an existing
// group takes the instances it meets as members already there.
const trustWindow = 10 * time.Minute

func loadGroupFile(path string) *groupFile {
	g := &groupFile{path: path}
	if b, err := os.ReadFile(path); err == nil {
		_ = json.Unmarshal(b, &g.state)
	}
	if st := &g.state; !st.JoinedAt.IsZero() && st.KeysSince.IsZero() {
		beforeKeysLocked(st, time.Now())
		_ = g.flushLocked()
	}
	return g
}

// beforeKeysLocked marks a group this instance held before member keys: the
// phones and browsers it knows are taken as approved.
func beforeKeysLocked(st *GroupState, now time.Time) {
	st.KeysSince = now
	st.Legacy = map[string]bool{}
	for id := range st.Apps {
		st.Legacy[id] = true
	}
}

func (g *groupFile) flushLocked() error {
	b, err := json.Marshal(g.state)
	if err != nil {
		return err
	}
	return os.WriteFile(g.path, b, 0o600)
}

// SetJoined records that this instance entered a group at at, or left its
// group when at is zero. Nobody has been seen in the group that follows, and
// every member that signs in has to be approved, apart from the instances met
// within trustWindow when this instance entered the words of a group that
// already existed.
func (m *Manager) SetJoined(at time.Time, entered bool) error {
	m.group.mu.Lock()
	defer m.group.mu.Unlock()
	m.group.state = GroupState{JoinedAt: at, KeysSince: at}
	if entered {
		m.group.state.TrustUntil = at.Add(trustWindow)
	}
	return m.group.flushLocked()
}

// SetJoinedBeforeKeys records a group this instance held before it knew when
// it joined, and so before member keys.
func (m *Manager) SetJoinedBeforeKeys(at time.Time) error {
	m.group.mu.Lock()
	defer m.group.mu.Unlock()
	m.group.state = GroupState{JoinedAt: at}
	beforeKeysLocked(&m.group.state, at)
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
				sib.Name = relay.ClipName(sib.Name)
				connected[appID(sib)] = sib
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

// appID is what a phone or browser is known by in the group: the lasting
// member id it announces, or its relay id when it has none.
func appID(sib relay.Announce) string {
	if sib.Member != "" {
		return sib.Member
	}
	return sib.InstanceID
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

// ErrIsInstance is what RemoveApp answers for an id whose pinned key signs as
// an instance. An instance leaves through RemoveMember instead.
var ErrIsInstance = errors.New("federation: that id belongs to an instance of the group, not to a phone or a browser")

// RemoveApp takes phone id out of the group at now: its card goes, its pinned
// key is revoked and its calls are turned away from then on. An id with a
// phone's or a browser's card is one, whatever kind its key was pinned as.
func (m *Manager) RemoveApp(id string, now time.Time) error {
	m.group.mu.Lock()
	defer m.group.mu.Unlock()
	st := &m.group.state
	_, isApp := st.Apps[id]
	if pin, ok := st.Keys[id]; ok && pin.Kind == relay.KindInstance && !isApp {
		return ErrIsInstance
	}
	if _, done := st.Removed[id]; done {
		return nil
	}
	delete(st.Apps, id)
	if st.Removed == nil {
		st.Removed = map[string]time.Time{}
	}
	st.Removed[id] = now
	if st.Legacy != nil {
		st.Legacy[id] = false
	}
	revokeLocked(st, id, now)
	return m.group.flushLocked()
}

// ErrNotInstance is what RemoveMember answers for a phone or a browser, which
// leave through RemoveApp instead.
var ErrNotInstance = errors.New("federation: that id belongs to a phone or a browser of the group, not to an instance")

// RemoveMember revokes the key pinned to instance id at now. Its id stays
// free, so the instance can enter the phrase again and join under a new key;
// the old key never speaks here again.
func (m *Manager) RemoveMember(id string, now time.Time) error {
	m.group.mu.Lock()
	defer m.group.mu.Unlock()
	st := &m.group.state
	_, isApp := st.Apps[id]
	if pin, ok := st.Keys[id]; isApp || (ok && pin.Kind != relay.KindInstance) {
		return ErrNotInstance
	}
	legacy := st.Legacy[id]
	if legacy {
		st.Legacy[id] = false
	}
	if !revokeLocked(st, id, now) && !legacy {
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
