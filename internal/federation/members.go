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
	// within minutes of an update, each member through its own connection,
	// so both are generous; they keep somebody minting keys from filling the
	// group or using up the hour for everybody else.
	maxNewPins          = 64
	maxNewPinsPerSource = 4
	pinWindow           = time.Hour
	// maxRevoked bounds the revoked keys kept; the oldest goes first.
	maxRevoked = 1024
	// perMemberCalls is how many calls one member key has served at once.
	// Callers that do not sign have unsignedCalls together, and each member
	// id they name unsignedPerMember of them.
	perMemberCalls    = 16
	unsignedCalls     = 32
	unsignedPerMember = 8
)

// acceptUnsigned serves calls that carry no member key from the phones and
// browsers this instance knew before keys, as long as their id has no pinned
// key and was not taken out. Turning it off refuses every unsigned call.
const acceptUnsigned = true

// Caller is the member a call came from: the id its pinned key speaks for, or
// for an unsigned call from before keys the id it names, with no Kind or Key.
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

// memberLimits counts what each member has in flight and when keys were
// pinned, and through which source. It lives in memory and is set by this
// instance alone.
type memberLimits struct {
	mu       sync.Mutex
	inFlight map[string]int
	pinned   []newPin
}

type newPin struct {
	at     time.Time
	source string
}

func newMemberLimits() *memberLimits {
	return &memberLimits{inFlight: map[string]int{}}
}

// acquire counts one more call of call's member in flight and reports whether
// it may run; release ends it. A signed call counts against its key, an
// unsigned one against the share of all unsigned calls and against the id it
// names.
func (l *memberLimits) acquire(call relay.ProxyCall) (release func(), ok bool) {
	type bucket struct {
		name string
		most int
	}
	buckets := []bucket{{"k" + string(call.Key), perMemberCalls}}
	if !call.Signed() {
		buckets = []bucket{{"u", unsignedCalls}, {"u" + call.From, unsignedPerMember}}
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, b := range buckets {
		if l.inFlight[b.name] >= b.most {
			return nil, false
		}
	}
	for _, b := range buckets {
		l.inFlight[b.name]++
	}
	return func() {
		l.mu.Lock()
		defer l.mu.Unlock()
		for _, b := range buckets {
			if l.inFlight[b.name]--; l.inFlight[b.name] <= 0 {
				delete(l.inFlight, b.name)
			}
		}
	}, true
}

// mayPin reports whether another key may be pinned at now through source, and
// counts it. A transport that cannot tell its sources apart passes "", which
// only the group's hour counts.
func (l *memberLimits) mayPin(source string, now time.Time) bool {
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
	if len(l.pinned) >= maxNewPins || fromSource >= maxNewPinsPerSource {
		return false
	}
	l.pinned = append(l.pinned, newPin{at: now, source: source})
	return true
}

// Guard wraps serve so every call from another member passes the group's key
// rules and limits first. Both transports serve through the same guard.
func (m *Manager) Guard(serve relay.ProxyHandler) relay.ProxyHandler {
	return func(ctx context.Context, call relay.ProxyCall) (int, []byte) {
		caller, r := m.admit(call, relay.SourceOf(ctx), time.Now())
		if r != nil {
			return r.status, r.body
		}
		release, ok := m.limits.acquire(call)
		if !ok {
			return http.StatusTooManyRequests, relay.RefusalBody("memberBusy",
				"this device already has as many calls waiting on this instance as it may; try again in a moment")
		}
		defer release()
		if caller.ID != "" {
			ctx = context.WithValue(ctx, callerKey{}, caller)
		}
		return serve(ctx, call)
	}
}

// admit decides whose call this is. call comes from relay.OpenCall, so a
// signature it carries already holds; what is left is whether its key may
// speak for the id it names. source is where the call came in, as
// relay.SourceOf says.
func (m *Manager) admit(call relay.ProxyCall, source string, now time.Time) (Caller, *refusal) {
	// Looked up before the group's lock, since it asks the transports.
	trusted := call.Signed() && call.Kind == relay.KindInstance && m.trusting(now) && m.instanceThere(call.From)
	m.group.mu.Lock()
	defer m.group.mu.Unlock()
	st := &m.group.state
	_, removed := st.Removed[call.From]
	if !call.Signed() {
		return admitUnsigned(st, call.From, removed)
	}

	if _, gone := st.Revoked[hex.EncodeToString(call.Key)]; gone || removed {
		return Caller{}, removedRefusal
	}
	if call.From == "" {
		return Caller{}, refuse(http.StatusForbidden, "memberUnnamed", "a signed call has to name the member it comes from")
	}
	if !claimFits(st, call.From, call.Kind) {
		return Caller{}, kindRefusal
	}
	if pin, ok := st.Keys[call.From]; ok {
		if !bytes.Equal(pin.Key, call.Key) {
			return Caller{}, refuse(http.StatusForbidden, "memberKeyChanged",
				"this instance knows this device by another key; remove it on the Pairing page and pair it again")
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
	if !m.limits.mayPin(source, now) {
		return Caller{}, refuse(http.StatusTooManyRequests, "groupJoinsBusy",
			"too many new devices joined this group within the hour; try again later")
	}
	if st.Keys == nil {
		st.Keys = map[string]PinnedKey{}
	}
	// The list from before keys holds phones and browsers only, so it never
	// approves an instance.
	approved := (st.Legacy[call.From] && call.Kind != relay.KindInstance) || trusted
	key := bytes.Clone(call.Key)
	st.Keys[call.From] = PinnedKey{Key: key, Kind: call.Kind, Used: now, Approved: approved}
	if err := m.group.flushLocked(); err != nil {
		delete(st.Keys, call.From)
		return Caller{}, refuse(http.StatusInternalServerError, "memberNotPinned", "this instance could not record the device's key")
	}
	if !approved {
		return Caller{}, pendingRefusal
	}
	return Caller{ID: call.From, Kind: call.Kind, Key: key}, nil
}

// admitUnsigned decides on a call without a member key. In a phrase group it
// is served only for a phone or a browser this instance knew before keys
// whose id has no pinned key and was not taken out. A relay key typed in by
// hand makes no phrase group: there are no member keys, nothing to approve,
// and holding the key is all any caller can prove.
func admitUnsigned(st *GroupState, from string, removed bool) (Caller, *refusal) {
	switch {
	case !acceptUnsigned:
		return Caller{}, unsignedRefusal
	case removed:
		return Caller{}, removedRefusal
	case st.JoinedAt.IsZero():
		return Caller{}, nil
	}
	if _, pinned := st.Keys[from]; pinned {
		return Caller{}, refuse(http.StatusForbidden, "memberUnsigned",
			"this device signs its calls, so a call in its name without a signature is refused; update it, or remove it on the Pairing page and pair it again")
	}
	if !st.Legacy[from] {
		return Caller{}, unsignedRefusal
	}
	return Caller{ID: from}, nil
}

// claimFits reports whether a signed call's id has the shape the kind it
// claims mints for itself, and for an instance, whether that id is not a
// phone's or a browser's of the group.
func claimFits(st *GroupState, id, kind string) bool {
	switch kind {
	case relay.KindInstance:
		_, isApp := st.Apps[id]
		return relay.ValidInstanceID(id) && !isApp
	case relay.KindExtension:
		// A browser mints its id the way an instance does.
		return relay.ValidInstanceID(id)
	case relay.KindMobile:
		return validPhoneID(id)
	}
	return false
}

// maxPhoneID bounds a phone's id, as the relay bounds a client's.
const maxPhoneID = 64

// validPhoneID reports whether id has the shape the app mints: "phone-" and
// then lowercase letters, digits and dashes.
func validPhoneID(id string) bool {
	rest, ok := strings.CutPrefix(id, "phone-")
	if !ok || rest == "" || len(id) > maxPhoneID {
		return false
	}
	for i := 0; i < len(rest); i++ {
		if c := rest[i]; (c < 'a' || c > 'z') && (c < '0' || c > '9') && c != '-' {
			return false
		}
	}
	return true
}

// trusting reports whether now falls in the window after this instance's
// owner entered the words of a group that already existed.
func (m *Manager) trusting(now time.Time) bool {
	m.group.mu.Lock()
	defer m.group.mu.Unlock()
	return now.Before(m.group.state.TrustUntil)
}

// instanceThere reports whether id is an instance this one sees in the group
// right now, on this network or on the relay.
func (m *Manager) instanceThere(id string) bool {
	for _, p := range m.localMembers() {
		if p.ID == id {
			return true
		}
	}
	m.mu.Lock()
	rt := m.rt
	m.mu.Unlock()
	if rt == nil {
		return false
	}
	for _, sib := range rt.Siblings() {
		if !sib.Client && sib.InstanceID == id {
			return true
		}
	}
	return false
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
	st := &m.group.state
	pin, ok := st.Keys[c.ID]
	_, isApp := st.Apps[c.ID]
	return ok && pin.Approved && pin.Kind == relay.KindInstance && !isApp && bytes.Equal(pin.Key, c.Key)
}

// StillMember reports whether id may still act in the group here: approved
// with a pinned key, or a phone or a browser from before keys, and not taken
// out.
func (m *Manager) StillMember(id string) bool {
	m.group.mu.Lock()
	defer m.group.mu.Unlock()
	st := &m.group.state
	if _, gone := st.Removed[id]; gone {
		return false
	}
	if pin, ok := st.Keys[id]; ok {
		return pin.Approved
	}
	return st.Legacy[id]
}

// pendingRefusal answers a member whose key is pinned but nobody approved yet.
var pendingRefusal = refuse(http.StatusForbidden, "memberPending",
	"this device waits for approval; approve it on an instance of the group under Settings, Pairing")

// unsignedRefusal answers a call without a member key that names no phone or
// browser this instance knew before keys.
var unsignedRefusal = refuse(http.StatusForbidden, "memberUnsigned",
	"this instance only takes signed calls; update the device, or remove it on the Pairing page and pair it again")

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
