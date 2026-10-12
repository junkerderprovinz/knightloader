package federation

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/relay"
)

// Approving members. A device that enters the words is pinned on its first
// signed call but refused until somebody at an instance approves it there.
// The approval, or the refusal, is passed on to the other instances, which
// pin the approved key even before the device has called them.

// Pending is a member whose key is pinned and waits for approval.
type Pending struct {
	ID          string
	Kind        string
	Key         []byte
	Fingerprint string
}

// ErrKeyMismatch is what Approve answers when the key it was given is not the
// one this instance pinned for the member.
var ErrKeyMismatch = errors.New("federation: this instance pinned another key for that member")

// ErrGone is what Approve answers for a member that was taken out, or a key
// that was revoked.
var ErrGone = errors.New("federation: that member was taken out of the group")

// ErrNoSuchPending is what Approve answers locally for an id with no pinned key.
var ErrNoSuchPending = errors.New("federation: no device with that id waits for approval")

// ErrKindMismatch is what Approve answers for a key passed on for an id that
// the key does not make for the kind it names.
var ErrKindMismatch = errors.New("federation: that id is not the one the key makes for the kind of device named")

// Fingerprint is how a person compares a member key: the start of its hash in
// four groups.
func Fingerprint(key []byte) string {
	sum := sha256.Sum256(key)
	h := hex.EncodeToString(sum[:8])
	return strings.Join([]string{h[0:4], h[4:8], h[8:12], h[12:16]}, " ")
}

// PendingMembers lists the members that wait for approval here.
func (m *Manager) PendingMembers() []Pending {
	m.group.mu.Lock()
	defer m.group.mu.Unlock()
	var out []Pending
	for id, pin := range m.group.state.Keys {
		if !pin.Approved {
			out = append(out, Pending{ID: id, Kind: pin.Kind, Key: bytes.Clone(pin.Key), Fingerprint: Fingerprint(pin.Key)})
		}
	}
	return out
}

// Approve lets member id in. With a nil key it approves the key pinned here,
// which is what a person at this instance does; with a key, as passed on by
// another instance, it pins that key as approved if nothing is pinned yet.
// It returns the key and kind it approved, for passing on.
func (m *Manager) Approve(id string, key []byte, kind string) ([]byte, string, error) {
	m.group.mu.Lock()
	defer m.group.mu.Unlock()
	st := &m.group.state
	if _, gone := st.Removed[id]; gone {
		return nil, "", ErrGone
	}
	pin, ok := st.Keys[id]
	switch {
	case !ok && key == nil:
		return nil, "", ErrNoSuchPending
	case !ok:
		if _, revoked := st.Revoked[hex.EncodeToString(key)]; revoked {
			return nil, "", ErrGone
		}
		if !claimFits(id, kind, key) {
			return nil, "", ErrKindMismatch
		}
		pin = PinnedKey{Key: bytes.Clone(key), Kind: kind}
		if st.Keys == nil {
			st.Keys = map[string]PinnedKey{}
		}
	case key != nil && !bytes.Equal(pin.Key, key):
		return nil, "", ErrKeyMismatch
	}
	if pin.Approved {
		return pin.Key, pin.Kind, nil
	}
	// Approving counts as use, so a key approved elsewhere and passed on
	// here is not taken for idle before the device first calls.
	pin.Approved = true
	pin.Used = time.Now()
	st.Keys[id] = pin
	return pin.Key, pin.Kind, m.group.flushLocked()
}

// Refuse turns member id away: an instance as RemoveMember does, a phone or a
// browser as RemoveApp does, told apart by the shape of the id. Its key is
// revoked either way. An id no device mints is ignored.
func (m *Manager) Refuse(id string, now time.Time) error {
	switch kindOfID(id) {
	case relay.KindInstance:
		return m.RemoveMember(id, now)
	case relay.KindMobile, relay.KindExtension:
		return m.RemoveApp(id, now)
	}
	return nil
}

// awaitWindow is how long one answer saying "waits for approval" is taken to
// stand for this instance.
const awaitWindow = 2 * time.Minute

// noteAnswer records whether a member just told this instance that it waits
// for approval there.
func (m *Manager) noteAnswer(member string, status int, body []byte, now time.Time) {
	pending := status == http.StatusForbidden && bytes.Contains(body, []byte(`"memberPending"`))
	m.mu.Lock()
	defer m.mu.Unlock()
	if pending {
		if m.awaiting == nil {
			m.awaiting = map[string]time.Time{}
		}
		m.awaiting[member] = now
	} else {
		delete(m.awaiting, member)
	}
	if status >= 200 && status < 300 {
		if m.heard == nil {
			m.heard = map[string]bool{}
		}
		m.heard[member] = true
	}
}

// greetEvery is how often Greet calls a member that has not let this instance
// in yet.
const greetEvery = 30 * time.Second

// Greet calls every instance of the group that has not yet served a call from
// this one, at most once every greetEvery, and returns when the calls are done
// or ctx ends. A first call pins this instance's key there, so two instances
// that meet list each other as waiting for approval on their Pairing cards,
// and each owner approves the other, whichever of them entered the words.
func (m *Manager) Greet(ctx context.Context, now time.Time) {
	members := m.Members()
	var due []string
	m.mu.Lock()
	for _, in := range members {
		if m.heard[in.RelayID] || now.Sub(m.greeted[in.RelayID]) < greetEvery {
			continue
		}
		if m.greeted == nil {
			m.greeted = map[string]time.Time{}
		}
		m.greeted[in.RelayID] = now
		due = append(due, in.Name)
	}
	m.mu.Unlock()
	var wg sync.WaitGroup
	for _, name := range due {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _, _ = m.Proxy(ctx, name, http.MethodGet, "/api/auth", nil)
		}()
	}
	wg.Wait()
}

// AwaitingApproval reports whether another instance of the group said
// recently that this instance waits for approval there.
func (m *Manager) AwaitingApproval(now time.Time) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, at := range m.awaiting {
		if now.Sub(at) < awaitWindow {
			return true
		}
	}
	return false
}
