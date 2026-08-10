package captcha

// Store is the in-memory map of currently active challenges - "active"
// meaning the last List() call answered with them. It is 7A's file inside a
// package 7F otherwise owns this wave (build-plan.md section 3's Wave 7
// table, section 8's amendment), the same "small store a reconciler owns"
// shape internal/hosterauth/reconcile.go's own states map already is: a pure
// Sync step that decides what changed, kept apart from anything that talks
// to a network or a browser.
//
// Deliberately NOT persisted, and that is not a gap - see NewStore. A
// restart loses whatever was in flight exactly as a JD restart already does
// (jdsource.go's own Source.List answers "everything pending" fresh from JD
// every time, never from a local memory of what used to be pending), so
// there is nothing honest to write to disk: a challenge this store forgot
// about is reacquired from JD itself on the very next poll, or it is
// genuinely gone.
//
// core.Task gained no new field for any of this. A task's Reason already
// carries core.ReasonCaptcha (internal/core/task.go), already the
// classification app_errors.go gives this exact condition. The one fact
// neither of those can answer - which challenge, if any, task T is waiting
// on right now - is this Store's whole job, via byTask below; see
// internal/app/app_captcha.go for how the App wires a poll loop and the hub
// around it.

import (
	"reflect"
	"sort"
	"sync"
	"time"
)

// Store is one session's view of every challenge the last List() call
// answered with. The zero value is not usable; build one with NewStore. Safe
// for concurrent use.
type Store struct {
	mu     sync.Mutex
	active map[string]Challenge // id -> last-seen snapshot
	byTask map[string]string    // taskID -> id, for ByTask
}

// NewStore returns an empty Store.
func NewStore() *Store {
	return &Store{active: map[string]Challenge{}, byTask: map[string]string{}}
}

// Sync reconciles a fresh List() result against what Store held before, and
// returns exactly what changed:
//
//   - added is a challenge this Store has never seen (a new id).
//   - changed is one already known whose visible fields moved - a later
//     ExpiresAt most often, since JD recomputes it fresh on every list() call
//     from its own live countdown (see jdsource.go's own doc comment on
//     Challenge.ExpiresAt); that is expected motion, not a bug, and callers
//     that only care about it as a live countdown are free to ignore this
//     slice entirely.
//   - removed is every challenge that WAS active and is not in current any
//     more, carrying its LAST-KNOWN snapshot rather than only its id - a
//     caller deciding what a disappearance means (solved, timed out,
//     aborted elsewhere) needs to know which task and host it was, and this
//     is the only place that snapshot still exists once JD has stopped
//     mentioning it.
//
// Store's own state is fully replaced by current before returning, so two
// calls never have to run back to back to converge, and a challenge removed
// out of band (see Remove) simply does not reappear here - it is already
// absent from what Sync is diffing against.
func (s *Store) Sync(current []Challenge) (added, changed, removed []Challenge) {
	s.mu.Lock()
	defer s.mu.Unlock()

	nextActive := make(map[string]Challenge, len(current))
	nextByTask := make(map[string]string, len(current))
	for _, c := range current {
		nextActive[c.ID] = c
		if c.TaskID != "" {
			nextByTask[c.TaskID] = c.ID
		}
		if prev, ok := s.active[c.ID]; !ok {
			added = append(added, c)
		} else if !sameChallenge(prev, c) {
			changed = append(changed, c)
		}
	}
	for id, prev := range s.active {
		if _, ok := nextActive[id]; !ok {
			removed = append(removed, prev)
		}
	}
	s.active = nextActive
	s.byTask = nextByTask
	return added, changed, removed
}

// sameChallenge reports whether two snapshots of the same id carry the same
// externally visible facts, treating an ExpiresAt move under one second as
// noise rather than a real change.
//
// The tolerance is load-bearing, not cosmetic: ExpiresAt is recomputed fresh
// on every JD list() call from its own live countdown (jdsource.go), so
// without it two polls a couple of milliseconds apart would both read as
// "changed" from clock jitter alone, and Sync's changed slice - the signal a
// caller uses to decide whether to re-broadcast - would fire on every single
// tick regardless of whether anything a person can see actually moved.
func sameChallenge(a, b Challenge) bool {
	return a.Host == b.Host && a.TaskID == b.TaskID && a.Kind == b.Kind &&
		a.Prompt == b.Prompt &&
		expiresAtClose(a.ExpiresAt, b.ExpiresAt) &&
		reflect.DeepEqual(a.Payload, b.Payload)
}

// expiresAtClose reports whether two ExpiresAt readings are close enough to
// count as the same deadline rather than a real move.
//
// A Truncate(time.Second)-then-Equal comparison looks like the obvious way
// to write this and is subtly wrong: two instants 400ms apart that straddle
// a wall-clock second boundary (…28.900 and …29.300) truncate to two
// DIFFERENT seconds, so the bucket edge itself becomes a source of the exact
// false "changed" report the tolerance exists to absorb - caught by
// TestStoreSyncIgnoresSubSecondExpiresAtJitter, which failed against that
// version. Comparing the actual gap between the two instants has no such
// edge.
func expiresAtClose(a, b time.Time) bool {
	if a.IsZero() != b.IsZero() {
		return false
	}
	d := a.Sub(b)
	if d < 0 {
		d = -d
	}
	return d < time.Second
}

// Remove drops one challenge out of band, ahead of the next Sync - the
// caller who just learned the outcome directly (a POST .../answer or
// .../abort that got its own definitive answer from JD) rather than by
// noticing an absence on the next poll. It reports whether id was present,
// and is a no-op otherwise: a challenge already gone is the state Remove
// exists to reach, the same idempotent-on-gone rule Source.Abort's own doc
// comment states for the same reason.
func (s *Store) Remove(id string) (Challenge, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	c, ok := s.active[id]
	if !ok {
		return Challenge{}, false
	}
	delete(s.active, id)
	if s.byTask[c.TaskID] == id {
		delete(s.byTask, c.TaskID)
	}
	return c, true
}

// Get returns one challenge by id.
func (s *Store) Get(id string) (Challenge, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	c, ok := s.active[id]
	return c, ok
}

// ByTask returns the challenge taskID is currently waiting on, if any - the
// lookup internal/app/app_captcha.go's dispatchLocked seam and onUpdate
// wiring need to tell a captcha-waiting task apart from a merely queued one,
// without core.Task carrying a pointer back to a challenge it does not own
// the lifecycle of.
func (s *Store) ByTask(taskID string) (Challenge, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	id, ok := s.byTask[taskID]
	if !ok {
		return Challenge{}, false
	}
	c, ok := s.active[id]
	return c, ok
}

// List returns every currently active challenge, nearest expiry first.
//
// A zero ExpiresAt - "the Source could not say", per Challenge's own doc
// comment - sorts LAST, not first: an unknown deadline is not the most
// urgent challenge to show, it is the least informative one, and a consumer
// that shows one challenge at a time (the prompt modal) wants the one most
// likely to lapse first in front of the one most likely to sit quietly.
// Ties (including two zero deadlines) break on id, so the order is stable
// from one call to the next when nothing has actually changed.
func (s *Store) List() []Challenge {
	s.mu.Lock()
	out := make([]Challenge, 0, len(s.active))
	for _, c := range s.active {
		out = append(out, c)
	}
	s.mu.Unlock()
	sort.Slice(out, func(i, j int) bool {
		ei, ej := out[i].ExpiresAt, out[j].ExpiresAt
		switch {
		case ei.IsZero() && ej.IsZero():
			return out[i].ID < out[j].ID
		case ei.IsZero():
			return false
		case ej.IsZero():
			return true
		case !ei.Equal(ej):
			return ei.Before(ej)
		default:
			return out[i].ID < out[j].ID
		}
	})
	return out
}
