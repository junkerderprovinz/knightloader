package captcha

import (
	"testing"
	"time"
)

func TestStoreSyncAddedChangedRemoved(t *testing.T) {
	s := NewStore()

	// First sync: everything is new.
	added, changed, removed := s.Sync([]Challenge{
		{ID: "1", Host: "a.example", TaskID: "t1", Kind: KindImage},
		{ID: "2", Host: "b.example", TaskID: "t2", Kind: KindImage},
	})
	if len(added) != 2 || len(changed) != 0 || len(removed) != 0 {
		t.Fatalf("first sync: added=%d changed=%d removed=%d, want 2/0/0", len(added), len(changed), len(removed))
	}

	// Second sync: id 1 unchanged, id 2 gets a later ExpiresAt (changed), id 3
	// is new, id 2's sibling is gone... wait id 2 stays, nothing removed yet.
	exp := time.Now().Add(5 * time.Minute)
	added, changed, removed = s.Sync([]Challenge{
		{ID: "1", Host: "a.example", TaskID: "t1", Kind: KindImage},
		{ID: "2", Host: "b.example", TaskID: "t2", Kind: KindImage, ExpiresAt: exp},
		{ID: "3", Host: "c.example", TaskID: "t3", Kind: KindWidget},
	})
	if len(added) != 1 || added[0].ID != "3" {
		t.Fatalf("second sync added = %+v, want just id 3", added)
	}
	if len(changed) != 1 || changed[0].ID != "2" {
		t.Fatalf("second sync changed = %+v, want just id 2", changed)
	}
	if len(removed) != 0 {
		t.Fatalf("second sync removed = %+v, want none", removed)
	}

	// Third sync: id 1 and id 3 vanish, id 2 stays as-is.
	added, changed, removed = s.Sync([]Challenge{
		{ID: "2", Host: "b.example", TaskID: "t2", Kind: KindImage, ExpiresAt: exp},
	})
	if len(added) != 0 || len(changed) != 0 {
		t.Fatalf("third sync added/changed = %d/%d, want 0/0", len(added), len(changed))
	}
	if len(removed) != 2 {
		t.Fatalf("third sync removed = %+v, want 2 entries", removed)
	}
	gotIDs := map[string]bool{}
	for _, c := range removed {
		gotIDs[c.ID] = true
		// The removed snapshot must carry what it was, not a zero value - the
		// whole reason Sync returns the Challenge and not only the id.
		if c.Host == "" || c.TaskID == "" {
			t.Errorf("removed challenge %q lost its own fields: %+v", c.ID, c)
		}
	}
	if !gotIDs["1"] || !gotIDs["3"] {
		t.Fatalf("removed ids = %v, want 1 and 3", gotIDs)
	}
}

// TestStoreSyncIgnoresSubSecondExpiresAtJitter is the reason sameChallenge
// truncates to the second: JD recomputes ExpiresAt fresh on every list()
// call (jdsource.go), so without rounding, two polls milliseconds apart
// would read as "changed" purely from clock noise and a caller that
// broadcasts on "changed" would spam an update nobody asked for on every
// ordinary tick.
func TestStoreSyncIgnoresSubSecondExpiresAtJitter(t *testing.T) {
	s := NewStore()
	base := time.Now()
	s.Sync([]Challenge{{ID: "1", Host: "a.example", ExpiresAt: base}})

	_, changed, _ := s.Sync([]Challenge{{ID: "1", Host: "a.example", ExpiresAt: base.Add(400 * time.Millisecond)}})
	if len(changed) != 0 {
		t.Fatalf("sub-second ExpiresAt jitter reported as changed: %+v", changed)
	}

	_, changed, _ = s.Sync([]Challenge{{ID: "1", Host: "a.example", ExpiresAt: base.Add(2 * time.Second)}})
	if len(changed) != 1 {
		t.Fatalf("a real 2s ExpiresAt move was not reported as changed")
	}
}

func TestStoreRemoveIsIdempotentAndKeepsSyncHonest(t *testing.T) {
	s := NewStore()
	s.Sync([]Challenge{{ID: "1", Host: "a.example", TaskID: "t1"}})

	c, ok := s.Remove("1")
	if !ok || c.TaskID != "t1" {
		t.Fatalf("Remove(1) = %+v, %v; want the stored snapshot and true", c, ok)
	}
	if _, ok := s.Remove("1"); ok {
		t.Fatalf("Remove(1) a second time reported ok=true; removing an already-gone id must be quiet")
	}

	// A Sync after an out-of-band Remove must not report the same id as
	// removed a second time - it is already absent from what Sync diffs
	// against, which is the whole point of removing it early.
	_, _, removed := s.Sync(nil)
	if len(removed) != 0 {
		t.Fatalf("Sync after Remove reported %+v as removed a second time", removed)
	}
}

func TestStoreByTask(t *testing.T) {
	s := NewStore()
	if _, ok := s.ByTask("t1"); ok {
		t.Fatalf("ByTask on an empty store answered ok=true")
	}
	s.Sync([]Challenge{{ID: "1", Host: "a.example", TaskID: "t1"}})
	c, ok := s.ByTask("t1")
	if !ok || c.ID != "1" {
		t.Fatalf("ByTask(t1) = %+v, %v; want challenge 1", c, ok)
	}
	if _, ok := s.ByTask("t-unknown"); ok {
		t.Fatalf("ByTask answered true for a task nothing is waiting on")
	}

	// A task's challenge id changing (the old one resolved, a new one
	// appeared for the same task) must not leave the stale id reachable.
	s.Sync([]Challenge{{ID: "2", Host: "a.example", TaskID: "t1"}})
	c, ok = s.ByTask("t1")
	if !ok || c.ID != "2" {
		t.Fatalf("ByTask(t1) after replacement = %+v, %v; want challenge 2", c, ok)
	}

	// A challenge with no TaskID must never occupy the "" slot and shadow a
	// real lookup miss.
	s.Sync([]Challenge{{ID: "3", Host: "b.example", TaskID: ""}})
	if _, ok := s.ByTask(""); ok {
		t.Fatalf("ByTask(\"\") answered true; an unresolved TaskID must not be a usable key")
	}
}

func TestStoreListOrdersByExpiryThenID(t *testing.T) {
	s := NewStore()
	now := time.Now()
	s.Sync([]Challenge{
		{ID: "no-expiry-b", Host: "x"},
		{ID: "far", Host: "x", ExpiresAt: now.Add(time.Hour)},
		{ID: "no-expiry-a", Host: "x"},
		{ID: "near", Host: "x", ExpiresAt: now.Add(time.Minute)},
	})
	got := s.List()
	want := []string{"near", "far", "no-expiry-a", "no-expiry-b"}
	if len(got) != len(want) {
		t.Fatalf("List() returned %d challenges, want %d", len(got), len(want))
	}
	for i, id := range want {
		if got[i].ID != id {
			t.Errorf("List()[%d].ID = %q, want %q (full order: %v)", i, got[i].ID, id, idsOf(got))
		}
	}
}

func idsOf(cs []Challenge) []string {
	out := make([]string, len(cs))
	for i, c := range cs {
		out[i] = c.ID
	}
	return out
}
