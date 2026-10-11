package federation

import (
	"bytes"
	"context"
	"encoding/hex"
	"net/http"
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
	// maxNewPins is how many keys may be pinned within pinWindow. A whole
	// group pins its keys within minutes of an update, so it is generous; it
	// only keeps somebody minting keys from filling the group.
	maxNewPins = 32
	pinWindow  = time.Hour
	// maxRevoked bounds the revoked keys kept; the oldest goes first.
	maxRevoked = 1024
	// perMemberCalls is how many calls one member key has served at once, and
	// unsignedCalls how many every caller that does not sign has together.
	perMemberCalls = 16
	unsignedCalls  = 32
)

// acceptUnsigned serves calls that carry no member key, from members on a
// release before keys, as long as they claim no id with a pinned key. Turning
// it off refuses every unsigned call.
const acceptUnsigned = true

// Caller is the member a call came from, as its pinned key says.
type Caller struct {
	ID   string
	Kind string
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

// memberLimits counts what each member key has in flight and when keys were
// pinned. It lives in memory and is set by this instance alone.
type memberLimits struct {
	mu       sync.Mutex
	inFlight map[string]int
	pinned   []time.Time
}

func newMemberLimits() *memberLimits {
	return &memberLimits{inFlight: map[string]int{}}
}

func (l *memberLimits) acquire(bucket string) bool {
	most := perMemberCalls
	if bucket == "" {
		most = unsignedCalls
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.inFlight[bucket] >= most {
		return false
	}
	l.inFlight[bucket]++
	return true
}

func (l *memberLimits) release(bucket string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.inFlight[bucket]--; l.inFlight[bucket] <= 0 {
		delete(l.inFlight, bucket)
	}
}

// mayPin reports whether another key may be pinned at now, and counts it.
func (l *memberLimits) mayPin(now time.Time) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	kept := l.pinned[:0]
	for _, at := range l.pinned {
		if now.Sub(at) < pinWindow {
			kept = append(kept, at)
		}
	}
	l.pinned = kept
	if len(l.pinned) >= maxNewPins {
		return false
	}
	l.pinned = append(l.pinned, now)
	return true
}

// Guard wraps serve so every call from another member passes the group's key
// rules and limits first. Both transports serve through the same guard.
func (m *Manager) Guard(serve relay.ProxyHandler) relay.ProxyHandler {
	return func(ctx context.Context, call relay.ProxyCall) (int, []byte) {
		caller, r := m.admit(call, time.Now())
		if r != nil {
			return r.status, r.body
		}
		bucket := string(call.Key)
		if !m.limits.acquire(bucket) {
			return http.StatusTooManyRequests, relay.RefusalBody("memberBusy",
				"this device already has as many calls waiting on this instance as it may; try again in a moment")
		}
		defer m.limits.release(bucket)
		if caller.ID != "" {
			ctx = context.WithValue(ctx, callerKey{}, caller)
		}
		return serve(ctx, call)
	}
}

// admit decides whose call this is. call comes from relay.OpenCall, so a
// signature it carries already holds; what is left is whether its key may
// speak for the id it names.
func (m *Manager) admit(call relay.ProxyCall, now time.Time) (Caller, *refusal) {
	m.group.mu.Lock()
	defer m.group.mu.Unlock()
	st := &m.group.state
	_, removed := st.Removed[call.From]
	if !call.Signed() {
		switch {
		case !acceptUnsigned:
			return Caller{}, refuse(http.StatusForbidden, "memberUnsigned",
				"this instance only takes signed calls; update the device, or remove it on the Pairing page and pair it again")
		case call.From == "":
			return Caller{}, nil
		case removed:
			return Caller{}, removedRefusal
		}
		if _, pinned := st.Keys[call.From]; pinned {
			return Caller{}, refuse(http.StatusForbidden, "memberUnsigned",
				"this device signs its calls, so a call in its name without a signature is refused; update it, or remove it on the Pairing page and pair it again")
		}
		// A member calling without a key is on a release from before keys,
		// so it was let in before there was anything to approve.
		if noteLegacyLocked(st, call.From) {
			_ = m.group.flushLocked()
		}
		return Caller{}, nil
	}

	if _, gone := st.Revoked[hex.EncodeToString(call.Key)]; gone || removed {
		return Caller{}, removedRefusal
	}
	if call.From == "" {
		return Caller{}, refuse(http.StatusForbidden, "memberUnnamed", "a signed call has to name the member it comes from")
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
		return Caller{ID: call.From, Kind: pin.Kind}, nil
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
	if !m.limits.mayPin(now) {
		return Caller{}, refuse(http.StatusTooManyRequests, "groupJoinsBusy",
			"too many new devices joined this group within the hour; try again later")
	}
	if st.Keys == nil {
		st.Keys = map[string]PinnedKey{}
	}
	kind := call.Kind
	if kind != relay.KindInstance && kind != relay.KindMobile && kind != relay.KindExtension {
		kind = ""
	}
	approved := st.Legacy[call.From] || (kind == relay.KindInstance && now.Before(st.TrustUntil))
	st.Keys[call.From] = PinnedKey{Key: bytes.Clone(call.Key), Kind: kind, Used: now, Approved: approved}
	if err := m.group.flushLocked(); err != nil {
		delete(st.Keys, call.From)
		return Caller{}, refuse(http.StatusInternalServerError, "memberNotPinned", "this instance could not record the device's key")
	}
	if !approved {
		return Caller{}, pendingRefusal
	}
	return Caller{ID: call.From, Kind: kind}, nil
}

// pendingRefusal answers a member whose key is pinned but nobody approved yet.
var pendingRefusal = refuse(http.StatusForbidden, "memberPending",
	"this device waits for approval; approve it on an instance of the group under Settings, Pairing")

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
