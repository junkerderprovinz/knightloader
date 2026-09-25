package captcha

// The in-memory map of the challenges the last List call answered with: a pure
// Sync step that decides what changed, kept apart from anything that talks to a
// network or a browser.
//
// Nothing here is persisted. Source.List answers "everything pending" fresh
// from JD on every call, so a challenge this store forgot is picked up again on
// the next poll or is genuinely gone.
//
// A task's Reason already carries core.ReasonCaptcha. Which challenge a given
// task is waiting on is what byTask below answers; internal/app/app_captcha.go
// wires the poll loop and the hub around it.

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

// Sync reconciles a fresh List result against what the Store held before and
// reports what changed:
//
//   - added is a challenge under an id this Store has not seen.
//   - changed is a known one whose visible fields moved, most often a later
//     ExpiresAt, which JD recomputes on every list call.
//   - removed is every challenge that was active and is gone, carrying its
//     last known snapshot: a caller deciding what a disappearance means needs
//     the task and host, and this is the last place that snapshot exists.
//
// The Store's state is replaced by current before returning, so one call
// converges. A known challenge keeps the SolverReport it had, since a Source
// never fills one.
func (s *Store) Sync(current []Challenge) (added, changed, removed []Challenge) {
	s.mu.Lock()
	defer s.mu.Unlock()

	nextActive := make(map[string]Challenge, len(current))
	nextByTask := make(map[string]string, len(current))
	for _, c := range current {
		prev, known := s.active[c.ID]
		if known {
			c.Solver = prev.Solver
		}
		nextActive[c.ID] = c
		if c.TaskID != "" {
			nextByTask[c.TaskID] = c.ID
		}
		if !known {
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
// visible facts, treating an ExpiresAt move under one second as noise.
//
// JD recomputes ExpiresAt from its live countdown on every list call, so
// without the tolerance two polls milliseconds apart would read as changed from
// jitter alone and a caller would re-broadcast on every tick.
func sameChallenge(a, b Challenge) bool {
	return a.Host == b.Host && a.TaskID == b.TaskID && a.Kind == b.Kind &&
		a.Prompt == b.Prompt &&
		expiresAtClose(a.ExpiresAt, b.ExpiresAt) &&
		reflect.DeepEqual(a.Payload, b.Payload)
}

// expiresAtClose reports whether two ExpiresAt readings are close enough to
// count as the same deadline.
//
// It compares the gap rather than truncating both to whole seconds: two
// instants 400ms apart can straddle a second boundary and truncate to different
// seconds, which is the false "changed" report the tolerance exists to absorb.
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

// Remove drops one challenge ahead of the next Sync, for a caller that learned
// the outcome directly rather than by noticing an absence on the next poll. It
// reports whether id was present and is a no-op otherwise, the same
// idempotent-on-gone rule Source.Abort follows.
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

// Report sets the SolverReport of one challenge and returns the updated
// challenge for a caller to publish. It reports false for an id the Store does
// not hold, a challenge answered or gone meanwhile.
func (s *Store) Report(id string, r SolverReport) (Challenge, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	c, ok := s.active[id]
	if !ok {
		return Challenge{}, false
	}
	c.Solver = &r
	s.active[id] = c
	return c, true
}

// Get returns one challenge by id.
func (s *Store) Get(id string) (Challenge, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	c, ok := s.active[id]
	return c, ok
}

// ByTask returns the challenge taskID is waiting on, if any. It lets the app
// tell a captcha-waiting task from a merely queued one without core.Task
// carrying a pointer to a challenge whose lifecycle it does not own.
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

// List returns every active challenge, nearest expiry first.
//
// A zero ExpiresAt, meaning the Source could not say, sorts last: a consumer
// showing one challenge at a time wants the one most likely to lapse in front
// of the one it knows nothing about. Ties break on id, so the order is stable
// while nothing changes.
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
