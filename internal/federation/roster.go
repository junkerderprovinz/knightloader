package federation

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"sync"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/relay"
)

// Sharing the phones and browsers. A browser extension is on the relay only
// for the length of a call, and a decision passed on misses an instance that
// is away, so each instance asks the others what they know and takes the
// cards, approvals and removals it lacks. Only an approved instance is asked,
// and it could pass the same approvals and removals on by itself.

// RosterApp is one phone or browser as an instance of the group knows it.
type RosterApp struct {
	ID         string    `json:"id"`
	Name       string    `json:"name"`
	Deployment string    `json:"deployment"`
	LastSeen   time.Time `json:"lastSeen"`
	// Key is the device's key once it was approved there.
	Key []byte `json:"key,omitempty"`
}

// Roster is what an instance tells the others of the group's phones and
// browsers: the ones it knows and the ones taken out, by id and when.
type Roster struct {
	Apps    []RosterApp          `json:"apps"`
	Removed map[string]time.Time `json:"removed,omitempty"`
}

// Roster returns the phones and browsers known here, each with its key if it
// was approved, and the ones taken out.
func (m *Manager) Roster() Roster {
	m.group.mu.Lock()
	defer m.group.mu.Unlock()
	st := &m.group.state
	r := Roster{Apps: []RosterApp{}, Removed: map[string]time.Time{}}
	for id, known := range st.Apps {
		app := RosterApp{ID: id, Name: known.Name, Deployment: deploymentOf(known), LastSeen: known.LastSeen}
		if pin := st.Keys[id]; pin.Approved {
			app.Key = bytes.Clone(pin.Key)
			if pin.Used.After(app.LastSeen) {
				app.LastSeen = pin.Used
			}
		}
		r.Apps = append(r.Apps, app)
	}
	for id, at := range st.Removed {
		r.Removed[id] = at
	}
	return r
}

// Merge takes from r, the roster of an approved instance of the group, the
// cards, approvals and removals missing here and reports whether it changed
// anything. A key is taken only for the id it makes, so no instance can
// put a key of its own under a device of the group.
func (m *Manager) Merge(r Roster, now time.Time) (bool, error) {
	m.group.mu.Lock()
	defer m.group.mu.Unlock()
	st := &m.group.state
	if st.JoinedAt.IsZero() {
		return false, nil
	}
	changed := false
	for id, at := range r.Removed {
		if !clientDeployments[kindOfID(id)] {
			continue
		}
		if _, done := st.Removed[id]; done {
			continue
		}
		delete(st.Apps, id)
		if st.Removed == nil {
			st.Removed = map[string]time.Time{}
		}
		st.Removed[id] = earliest(at, now)
		revokeLocked(st, id, now)
		changed = true
	}
	for i, app := range r.Apps {
		if i >= maxApps {
			break
		}
		if _, gone := st.Removed[app.ID]; gone || !clientDeployments[app.Deployment] || kindOfID(app.ID) != app.Deployment {
			continue
		}
		seen := earliest(app.LastSeen, now)
		known, ok := st.Apps[app.ID]
		if !ok || seen.After(known.LastSeen) {
			if st.Apps == nil {
				st.Apps = map[string]KnownApp{}
			}
			if !ok && len(st.Apps) >= maxApps {
				forgetStalestApp(st.Apps)
			}
			st.Apps[app.ID] = KnownApp{Name: relay.ClipName(app.Name), Deployment: app.Deployment, LastSeen: seen}
			changed = true
		}
		if len(app.Key) > 0 && approvePassedLocked(st, app.ID, app.Deployment, app.Key, seen) {
			changed = true
		}
	}
	if !changed {
		return false, nil
	}
	return true, m.group.flushLocked()
}

// approvePassedLocked pins key as approved for device id unless id was taken
// out, the key was revoked, another key is pinned or the key does not make id.
// It reports whether it changed anything.
func approvePassedLocked(st *GroupState, id, kind string, key []byte, used time.Time) bool {
	if _, revoked := st.Revoked[hex.EncodeToString(key)]; revoked || !claimFits(id, kind, key) {
		return false
	}
	pin, ok := st.Keys[id]
	switch {
	case ok && !bytes.Equal(pin.Key, key), ok && pin.Approved:
		return false
	case !ok:
		pin = PinnedKey{Key: bytes.Clone(key), Kind: kind, Used: used}
		if st.Keys == nil {
			st.Keys = map[string]PinnedKey{}
		}
	}
	pin.Approved = true
	st.Keys[id] = pin
	return true
}

// earliest keeps a time another instance reports from lying ahead of this
// instance's clock.
func earliest(at, now time.Time) time.Time {
	if at.After(now) {
		return now
	}
	return at
}

// shareEvery is how often SyncApps asks one member for its roster.
const shareEvery = time.Minute

// SyncApps asks every approved instance of the group reachable now for its
// roster, each at most once every shareEvery, merges what comes back and
// reports whether anything changed here. It returns when the calls are done
// or ctx ends.
func (m *Manager) SyncApps(ctx context.Context, now time.Time) bool {
	members := m.Members()
	m.group.mu.Lock()
	approved := map[string]bool{}
	for _, in := range members {
		approved[in.RelayID] = approvedInstanceLocked(&m.group.state, in.RelayID)
	}
	m.group.mu.Unlock()

	var due []string
	m.mu.Lock()
	for _, in := range members {
		if !approved[in.RelayID] || now.Sub(m.shared[in.RelayID]) < shareEvery {
			continue
		}
		if m.shared == nil {
			m.shared = map[string]time.Time{}
		}
		m.shared[in.RelayID] = now
		due = append(due, in.Name)
	}
	m.mu.Unlock()

	var (
		wg      sync.WaitGroup
		mu      sync.Mutex
		changed bool
	)
	for _, name := range due {
		wg.Add(1)
		go func() {
			defer wg.Done()
			body, status, err := m.Proxy(ctx, name, http.MethodGet, "/api/group/roster", nil)
			var r Roster
			if err != nil || status != http.StatusOK || json.Unmarshal(body, &r) != nil {
				return
			}
			got, _ := m.Merge(r, now)
			mu.Lock()
			changed = changed || got
			mu.Unlock()
		}()
	}
	wg.Wait()
	return changed
}
