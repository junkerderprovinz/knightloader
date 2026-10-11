package federation

import (
	"bytes"
	"context"
	"encoding/hex"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/relay"
)

// The rules a call from another member passes before it is served, on the
// relay and directly alike: whose key it is (see relay/member.go) and how much
// that key may ask at once.

const (
	// maxPinned is how many member keys a group keeps pinned. Past it a key
	// idle for pinIdle makes room for a new one; otherwise the new one waits.
	maxPinned = 64
	pinIdle   = 30 * 24 * time.Hour
	// maxNewPins is how many keys may be pinned within pinWindow, and
	// maxNewPinsPerSource how many of them may come through one relay
	// connection or one address on this network. A whole group pins its keys
	// within minutes of being paired, each member through its own connection.
	// Once the hour is used up, a new key takes the place of the oldest one
	// pinned in it that still waits for approval, so keys minted to fill the
	// hour keep nobody out.
	maxNewPins          = 64
	maxNewPinsPerSource = 2
	pinWindow           = time.Hour
	// maxRevoked bounds the revoked keys kept; the oldest goes first.
	maxRevoked = 1024
	// perMemberCalls is how many calls one member key has served at once.
	perMemberCalls = 16
)

// Caller is the member a call came from: the id its pinned key speaks for.
type Caller struct {
	ID   string
	Kind string
	Key  []byte
}

type callerKey struct{}

// CallerOf returns the member a call into this instance came from, if it
// signed with a pinned key.
func CallerOf(ctx context.Context) (Caller, bool) {
	c, ok := ctx.Value(callerKey{}).(Caller)
	return c, ok
}

// refusal is how a call that is not served is answered.
type refusal struct {
	status int
	body   []byte
}

func refuse(status int, code, text string) *refusal {
	return &refusal{status: status, body: relay.RefusalBody(code, text)}
}

// removedRefusal stays plain text: every release of the app and the extension
// reads exactly this body as "this device was taken out of the group".
var removedRefusal = &refusal{status: http.StatusGone, body: []byte("removed")}

// dropped is a call that gets no answer at all: one that is old, or one whose
// id its key already used.
var dropped = &refusal{status: relay.StatusDrop}

// memberLimits counts what each member has in flight and when keys were
// pinned, and through which source. It lives in memory and is set by this
// instance alone.
type memberLimits struct {
	mu       sync.Mutex
	inFlight map[string]int
	pinned   []newPin
}

type newPin struct {
	at         time.Time
	source, id string
}

func newMemberLimits() *memberLimits {
	return &memberLimits{inFlight: map[string]int{}}
}

// acquire counts one more call of key in flight and reports whether it may
// run; release ends it.
func (l *memberLimits) acquire(key []byte) (release func(), ok bool) {
	bucket := string(key)
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.inFlight[bucket] >= perMemberCalls {
		return nil, false
	}
	l.inFlight[bucket]++
	return func() {
		l.mu.Lock()
		defer l.mu.Unlock()
		if l.inFlight[bucket]--; l.inFlight[bucket] <= 0 {
			delete(l.inFlight, bucket)
		}
	}, true
}

// mayPin reports whether id may be pinned at now through source, and counts
// it. A transport that cannot tell its sources apart passes "", which only the
// group's hour counts. When the hour is used up, waiting is asked whether an
// id pinned in it still waits for approval; the oldest such id gives up its
// place, and mayPin returns it as evicted for the caller to unpin.
func (l *memberLimits) mayPin(id, source string, now time.Time, waiting func(id string) bool) (evicted string, ok bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	kept := l.pinned[:0]
	fromSource := 0
	for _, p := range l.pinned {
		if now.Sub(p.at) < pinWindow {
			kept = append(kept, p)
			if source != "" && p.source == source {
				fromSource++
			}
		}
	}
	l.pinned = kept
	if fromSource >= maxNewPinsPerSource {
		return "", false
	}
	if len(l.pinned) >= maxNewPins {
		i := 0
		for i < len(l.pinned) && !waiting(l.pinned[i].id) {
			i++
		}
		if i == len(l.pinned) {
			return "", false
		}
		evicted = l.pinned[i].id
		l.pinned = append(l.pinned[:i], l.pinned[i+1:]...)
	}
	l.pinned = append(l.pinned, newPin{at: now, source: source, id: id})
	return evicted, true
}

// Guard wraps serve so every call from another member passes the group's key
// rules and limits first. Both transports serve through the same guard, and
// it is also where a call's id is used up: only once the call is known to
// come from a pinned key, so a stranger fills no record a member needs.
func (m *Manager) Guard(serve relay.ProxyHandler) relay.ProxyHandler {
	return func(ctx context.Context, call relay.ProxyCall) (int, []byte) {
		caller, r := m.admit(call, relay.SourceOf(ctx), time.Now())
		if r != nil {
			return r.status, r.body
		}
		release, ok := m.limits.acquire(call.Key)
		if !ok {
			return http.StatusTooManyRequests, relay.RefusalBody("memberBusy",
				"this device already has as many calls waiting on this instance as it may; try again in a moment")
		}
		defer release()
		return serve(context.WithValue(ctx, callerKey{}, caller), call)
	}
}

// admit decides whose call this is. call comes from relay.OpenCall, so a
// signature it carries already holds; what is left is whether its key may
// speak for the id it names and whether the call ran before. source is where
// the call came in, as relay.SourceOf says.
func (m *Manager) admit(call relay.ProxyCall, source string, now time.Time) (Caller, *refusal) {
	if !relay.Recent(call, now) {
		return Caller{}, dropped
	}
	if !call.Signed() {
		return Caller{}, unsignedRefusal
	}
	m.group.mu.Lock()
	defer m.group.mu.Unlock()
	st := &m.group.state
	_, removed := st.Removed[call.From]
	if _, gone := st.Revoked[hex.EncodeToString(call.Key)]; gone || removed {
		return Caller{}, removedRefusal
	}
	if call.From == "" {
		return Caller{}, refuse(http.StatusForbidden, "memberUnnamed", "a signed call has to name the member it comes from")
	}
	if !claimFits(call.From, call.Kind) {
		return Caller{}, kindRefusal
	}
	if pin, ok := st.Keys[call.From]; ok {
		if !bytes.Equal(pin.Key, call.Key) {
			return Caller{}, refuse(http.StatusForbidden, "memberKeyChanged",
				"this instance knows this device by another key; remove it on the Pairing page and pair it again")
		}
		if !m.replay.Admit(call) {
			return Caller{}, dropped
		}
		if now.Sub(pin.Used) >= lastSeenStep {
			pin.Used = now
			st.Keys[call.From] = pin
			_ = m.group.flushLocked()
		}
		if !pin.Approved {
			return Caller{}, pendingRefusal
		}
		return Caller{ID: call.From, Kind: pin.Kind, Key: pin.Key}, nil
	}
	for _, pin := range st.Keys {
		if bytes.Equal(pin.Key, call.Key) {
			return Caller{}, refuse(http.StatusForbidden, "memberKeyChanged",
				"this key already speaks for another member of the group")
		}
	}
	if len(st.Keys) >= maxPinned && !forgetIdlePinLocked(st, now) {
		return Caller{}, refuse(http.StatusTooManyRequests, "groupFull",
			"this group already knows as many devices as it can; remove one on the Pairing page first")
	}
	evicted, ok := m.limits.mayPin(call.From, source, now, func(id string) bool {
		pin, pinned := st.Keys[id]
		return pinned && !pin.Approved
	})
	if !ok {
		return Caller{}, refuse(http.StatusTooManyRequests, "groupJoinsBusy",
			"too many new devices joined this group within the hour; try again later")
	}
	if st.Keys == nil {
		st.Keys = map[string]PinnedKey{}
	}
	before, hadEvicted := st.Keys[evicted]
	delete(st.Keys, evicted)
	st.Keys[call.From] = PinnedKey{Key: bytes.Clone(call.Key), Kind: call.Kind, Used: now}
	if err := m.group.flushLocked(); err != nil {
		delete(st.Keys, call.From)
		if hadEvicted {
			st.Keys[evicted] = before
		}
		return Caller{}, refuse(http.StatusInternalServerError, "memberNotPinned", "this instance could not record the device's key")
	}
	m.replay.Admit(call)
	return Caller{}, pendingRefusal
}

// claimFits reports whether a signed call's id has the shape the kind it
// claims mints for itself. The shapes never overlap, so no device can be
// listed, pinned, refused or removed under an instance's id, nor an instance
// under a device's.
func claimFits(id, kind string) bool {
	switch kind {
	case relay.KindInstance:
		return relay.ValidInstanceID(id)
	case relay.KindExtension:
		return validBrowserID(id)
	case relay.KindMobile:
		return validPhoneID(id)
	}
	return false
}

// kindOfID is the kind of device whose ids have id's shape, "" for none.
func kindOfID(id string) string {
	for _, kind := range []string{relay.KindInstance, relay.KindExtension, relay.KindMobile} {
		if claimFits(id, kind) {
			return kind
		}
	}
	return ""
}

// maxClientID bounds a phone's or a browser's id, as the relay bounds a
// client's.
const maxClientID = 64

// validPhoneID reports whether id has the shape the app mints: "phone-" and
// then lowercase letters, digits and dashes.
func validPhoneID(id string) bool {
	rest, ok := strings.CutPrefix(id, "phone-")
	if !ok || rest == "" || len(id) > maxClientID {
		return false
	}
	for i := 0; i < len(rest); i++ {
		if c := rest[i]; (c < 'a' || c > 'z') && (c < '0' || c > '9') && c != '-' {
			return false
		}
	}
	return true
}

// validBrowserID reports whether id has the shape the extension mints:
// "browser-" and then lowercase hex.
func validBrowserID(id string) bool {
	rest, ok := strings.CutPrefix(id, "browser-")
	if !ok || rest == "" || len(id) > maxClientID {
		return false
	}
	for i := 0; i < len(rest); i++ {
		if c := rest[i]; (c < 'a' || c > 'f') && (c < '0' || c > '9') {
			return false
		}
	}
	return true
}

// ApprovedInstance reports whether c signed with a key this instance holds
// pinned and approved for an instance of the group. Only such a caller may
// decide who else is in the group.
func (m *Manager) ApprovedInstance(c Caller) bool {
	if len(c.Key) == 0 {
		return false
	}
	m.group.mu.Lock()
	defer m.group.mu.Unlock()
	pin, ok := m.group.state.Keys[c.ID]
	return ok && pin.Approved && pin.Kind == relay.KindInstance && relay.ValidInstanceID(c.ID) && bytes.Equal(pin.Key, c.Key)
}

// StillMember reports whether id may still act in the group here: approved
// with a pinned key and not taken out.
func (m *Manager) StillMember(id string) bool {
	m.group.mu.Lock()
	defer m.group.mu.Unlock()
	st := &m.group.state
	if _, gone := st.Removed[id]; gone {
		return false
	}
	pin, ok := st.Keys[id]
	return ok && pin.Approved
}

// pendingRefusal answers a member whose key is pinned but nobody approved yet.
var pendingRefusal = refuse(http.StatusForbidden, "memberPending",
	"this device waits for approval; approve it on an instance of the group under Settings, Pairing")

// unsignedRefusal answers a call without a member key: a device on a release
// before keys, or one that cannot make a key.
var unsignedRefusal = refuse(http.StatusForbidden, "memberUnsigned",
	"this device does not sign its calls, which every member of the group has to; update it")

// kindRefusal answers a signed call whose id does not fit the kind it claims.
var kindRefusal = refuse(http.StatusForbidden, "memberKind",
	"this call names an id that no device of its kind uses")

// forgetIdlePinLocked makes room for one more pin and reports whether it
// could: it drops the waiting pin used longest ago, else an approved one
// nobody used for pinIdle. A dropped member pins its key again on its next
// call.
func forgetIdlePinLocked(st *GroupState, now time.Time) bool {
	idlest := ""
	for id, pin := range st.Keys {
		cur, ok := st.Keys[idlest]
		switch {
		case !ok:
			idlest = id
		case pin.Approved != cur.Approved:
			if !pin.Approved {
				idlest = id
			}
		case pin.Used.Before(cur.Used):
			idlest = id
		}
	}
	pin, ok := st.Keys[idlest]
	if !ok || (pin.Approved && now.Sub(pin.Used) < pinIdle) {
		return false
	}
	delete(st.Keys, idlest)
	return true
}
