package resolver

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"
)

// TestHostCacheKeepsLastGoodOnFailure is the fix itself: a transient error
// from the live source must not empty the set a resolver is matching against.
// Before HostCache existed, whatever called a service's Hosts and got this
// exact error handed the empty result straight to Registry.Register, and the
// service silently claimed nothing until the process restarted.
func TestHostCacheKeepsLastGoodOnFailure(t *testing.T) {
	boom := errors.New("boom: the service timed out")
	calls := 0
	c := &HostCache{Fetch: func(context.Context) (map[string]bool, error) {
		calls++
		if calls == 1 {
			return map[string]bool{"a.example": true, "b.example": true}, nil
		}
		return nil, boom
	}}

	if err := c.Refresh(context.Background()); err != nil {
		t.Fatalf("first refresh: %v", err)
	}
	good := c.Hosts()
	firstFetchedAt := c.FetchedAt()
	if len(good) != 2 {
		t.Fatalf("first refresh left %v, want the two hosts fetched", good)
	}

	// Construct the failure.
	err := c.Refresh(context.Background())
	if !errors.Is(err, boom) {
		t.Fatalf("second refresh error = %v, want boom", err)
	}

	// Assert the list is unchanged.
	if got := c.Hosts(); !reflect.DeepEqual(got, good) {
		t.Errorf("Hosts() after a failed refresh = %v, want the last good set %v unchanged", got, good)
	}
	if got := c.FetchedAt(); !got.Equal(firstFetchedAt) {
		t.Errorf("FetchedAt moved on a failed refresh: got %v, want the earlier %v unchanged", got, firstFetchedAt)
	}
	if got := c.LastError(); !errors.Is(got, boom) {
		t.Errorf("LastError = %v, want boom recorded so staleness is explainable", got)
	}

	// And a later success clears the memory of the failure and moves the set
	// again - the cache must not get stuck reporting an old error forever.
	calls = 0 // one more "success" call
	c.Fetch = func(context.Context) (map[string]bool, error) {
		return map[string]bool{"c.example": true}, nil
	}
	if err := c.Refresh(context.Background()); err != nil {
		t.Fatalf("recovery refresh: %v", err)
	}
	if got := c.Hosts(); len(got) != 1 || !got["c.example"] {
		t.Errorf("Hosts() after recovery = %v, want only c.example", got)
	}
	if err := c.LastError(); err != nil {
		t.Errorf("LastError after a successful refresh = %v, want nil", err)
	}
}

// TestHostCacheNeverSucceededIsNil pins the state before any refresh has ever
// run - and before any persisted set has been loaded: nil, which every Match
// built on HostInSet reads as "matches nothing", correctly, because nothing
// has looked yet.
func TestHostCacheNeverSucceededIsNil(t *testing.T) {
	c := &HostCache{Fetch: func(context.Context) (map[string]bool, error) {
		return nil, errors.New("still booting")
	}}
	if got := c.Hosts(); got != nil {
		t.Errorf("Hosts() before any refresh = %v, want nil", got)
	}
	_ = c.Refresh(context.Background())
	if got := c.Hosts(); got != nil {
		t.Errorf("Hosts() after only a failed refresh = %v, want nil (nothing has ever succeeded)", got)
	}
	if !c.FetchedAt().IsZero() {
		t.Errorf("FetchedAt = %v, want the zero time before any success", c.FetchedAt())
	}
}

// TestHostCacheSeedsFromLoadAndCallsSaveOnlyOnSuccess pins the persistence
// seam a fresh HostCache is reconstructed against every time
// rewireBackends-style code runs: Load supplies what a previous process
// already knew, and Save is asked to remember a fetch that actually worked -
// never one that didn't, which would just rewrite the same bytes for nothing.
func TestHostCacheSeedsFromLoadAndCallsSaveOnlyOnSuccess(t *testing.T) {
	persisted := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	var saved map[string]bool
	var saveCalls int

	c := &HostCache{
		Fetch: func(context.Context) (map[string]bool, error) {
			return nil, errors.New("source is down")
		},
		Load: func() (map[string]bool, time.Time, bool) {
			return map[string]bool{"seeded.example": true}, persisted, true
		},
		Save: func(hosts map[string]bool, _ time.Time) {
			saveCalls++
			saved = hosts
		},
	}

	// Seeded set is visible before any refresh, and a failed refresh must not
	// erase it - the same guarantee as the live-source test above, this time
	// against the persisted seed rather than an earlier live fetch.
	if got := c.Hosts(); !got["seeded.example"] {
		t.Fatalf("Hosts() before any refresh = %v, want the seeded set", got)
	}
	if err := c.Refresh(context.Background()); err == nil {
		t.Fatal("expected the fetch to fail")
	}
	if got := c.Hosts(); !got["seeded.example"] || len(got) != 1 {
		t.Errorf("Hosts() after a failed refresh = %v, want the seeded set unchanged", got)
	}
	if saveCalls != 0 {
		t.Errorf("Save was called %d times on a failed refresh, want 0", saveCalls)
	}

	c.Fetch = func(context.Context) (map[string]bool, error) {
		return map[string]bool{"fresh.example": true}, nil
	}
	if err := c.Refresh(context.Background()); err != nil {
		t.Fatalf("refresh: %v", err)
	}
	if saveCalls != 1 {
		t.Fatalf("Save was called %d times after a successful refresh, want 1", saveCalls)
	}
	if !saved["fresh.example"] {
		t.Errorf("Save got %v, want the freshly fetched set", saved)
	}
}
