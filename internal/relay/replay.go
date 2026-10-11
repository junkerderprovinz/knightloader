package relay

import (
	"sync"
	"time"
)

// ClockSkew is how far a call's timestamp may be from this instance's clock.
const ClockSkew = 2 * time.Minute

// maxSeenPerMember bounds the ids one member key, or one member id calling
// without a key, can have on record, and maxSeen the ids of all of them
// together. A member polling once a second stays far below the first within
// the 2*ClockSkew that ids are kept.
const (
	maxSeenPerMember = 4096
	maxSeen          = 1 << 16
)

// ReplayGuard admits each sealed call once and only while its timestamp is
// recent. An instance keeps one guard for the relay and the direct transport,
// so a call captured on either cannot be run a second time on the other.
//
// Ids are kept per member key, a bucket this instance takes from the verified
// key rather than from anything the caller names, so one member can neither
// use up another's ids nor fill another's record. Unsigned calls are kept per
// member id they name. Once the whole record is full, new calls wait until
// old ids run out rather than push out ids that could still be played again.
type ReplayGuard struct {
	mu   sync.Mutex
	seen map[string]map[string]struct{}
	// order holds every id on record from order[head] on, oldest first, so
	// the ids that ran out are always at its front.
	order []seenID
	head  int
	now   func() time.Time
}

type seenID struct {
	bucket, id string
	at         time.Time
}

// NewReplayGuard returns a guard that has seen nothing.
func NewReplayGuard() *ReplayGuard {
	return &ReplayGuard{seen: map[string]map[string]struct{}{}, now: time.Now}
}

// Admit reports whether call is recent and new, and records it. Ids are kept
// for twice the skew, which covers every call whose timestamp would still
// pass. The call has to come from OpenCall, which checked its signature.
func (g *ReplayGuard) Admit(call ProxyCall) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	now := g.now()
	if d := now.Sub(time.Unix(call.Sent, 0)); call.ID == "" || d > ClockSkew || d < -ClockSkew {
		return false
	}
	g.forgetLocked(now)
	bucket := replayBucket(call)
	ids := g.seen[bucket]
	if _, dup := ids[call.ID]; dup || len(ids) >= maxSeenPerMember || len(g.order)-g.head >= maxSeen {
		return false
	}
	if ids == nil {
		ids = map[string]struct{}{}
		g.seen[bucket] = ids
	}
	ids[call.ID] = struct{}{}
	g.order = append(g.order, seenID{bucket: bucket, id: call.ID, at: now})
	return true
}

// forgetLocked drops the ids that ran out, which all sit at the front of
// order, so each id costs one step however many members there are.
func (g *ReplayGuard) forgetLocked(now time.Time) {
	for g.head < len(g.order) && now.Sub(g.order[g.head].at) > 2*ClockSkew {
		old := g.order[g.head]
		g.order[g.head] = seenID{}
		g.head++
		ids := g.seen[old.bucket]
		delete(ids, old.id)
		if len(ids) == 0 {
			delete(g.seen, old.bucket)
		}
	}
	if g.head > len(g.order)/2 {
		n := copy(g.order, g.order[g.head:])
		clear(g.order[n:])
		g.order, g.head = g.order[:n], 0
	}
}

// replayBucket is the record a call's id is kept in: its verified key, or for
// a call without one the member id it names. The prefixes keep a key from
// ever reading as an id.
func replayBucket(call ProxyCall) string {
	if call.Signed() {
		return "k" + string(call.Key)
	}
	return "u" + call.From
}
