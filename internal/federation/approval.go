package federation

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"strings"
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
		if kind != relay.KindInstance && kind != relay.KindMobile && kind != relay.KindExtension {
			kind = ""
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
	pin.Approved = true
	st.Keys[id] = pin
	return pin.Key, pin.Kind, m.group.flushLocked()
}

// Refuse turns member id away: an instance as RemoveMember does, a phone or a
// browser as RemoveApp does. Its key is revoked either way.
func (m *Manager) Refuse(id string, now time.Time) error {
	m.group.mu.Lock()
	pin := m.group.state.Keys[id]
	m.group.mu.Unlock()
	if pin.Kind == relay.KindInstance {
		return m.RemoveMember(id, now)
	}
	return m.RemoveApp(id, now)
}

// noteLegacy adds members seen announcing without member keys to the ones
// taken as approved once they sign: they were in the group before keys. It
// ends with unsigned calls, since then such a member cannot act anyway.
func (m *Manager) noteLegacy(ids []string) {
	if !acceptUnsigned || len(ids) == 0 {
		return
	}
	m.group.mu.Lock()
	defer m.group.mu.Unlock()
	st := &m.group.state
	if st.JoinedAt.IsZero() {
		return
	}
	added := false
	for _, id := range ids {
		added = noteLegacyLocked(st, id) || added
	}
	if added {
		_ = m.group.flushLocked()
	}
}

// maxLegacy bounds the ids noted as members from before keys. Anybody holding
// the words can name new ones while unsigned calls are served.
const maxLegacy = 256

// noteLegacyLocked notes id as a member from before keys, unless it was noted
// or taken out already, and reports whether it did.
func noteLegacyLocked(st *GroupState, id string) bool {
	if _, known := st.Legacy[id]; known || len(st.Legacy) >= maxLegacy {
		return false
	}
	if st.Legacy == nil {
		st.Legacy = map[string]bool{}
	}
	st.Legacy[id] = true
	return true
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
