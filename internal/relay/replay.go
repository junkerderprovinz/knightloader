package relay

import (
	"sync"
	"time"
)

// ClockSkew is how far a call's timestamp may be from this instance's clock.
const ClockSkew = 2 * time.Minute

// maxSeenPerMember bounds the ids one member key, or all unsigned callers
// together, can have on record. A member polling once a second stays far below
// it within the 2*ClockSkew that ids are kept.
const maxSeenPerMember = 4096

// ReplayGuard admits each sealed call once and only while its timestamp is
// recent. An instance keeps one guard for the relay and the direct transport,
// so a call captured on either cannot be run a second time on the other.
//
// Ids are kept per member key, a bucket this instance takes from the verified
// key rather than from anything the caller names, so one member can neither
// use up another's ids nor fill another's record. Unsigned calls share one
// bucket.
type ReplayGuard struct {
	mu   sync.Mutex
	seen map[string]map[string]time.Time
	now  func() time.Time
}

// NewReplayGuard returns a guard that has seen nothing.
func NewReplayGuard() *ReplayGuard {
	return &ReplayGuard{seen: map[string]map[string]time.Time{}, now: time.Now}
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
	for bucket, ids := range g.seen {
		for id, at := range ids {
			if now.Sub(at) > 2*ClockSkew {
				delete(ids, id)
			}
		}
		if len(ids) == 0 {
			delete(g.seen, bucket)
		}
	}
	bucket := string(call.Key)
	ids := g.seen[bucket]
	if ids == nil {
		ids = map[string]time.Time{}
		g.seen[bucket] = ids
	}
	if _, dup := ids[call.ID]; dup || len(ids) >= maxSeenPerMember {
		return false
	}
	ids[call.ID] = now
	return true
}
