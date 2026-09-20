package resolver

import (
	"context"
	"sync"
	"time"
)

// HostCache is a supported-host set that refreshes itself from a live source
// and never lets a failed refresh empty what it is already holding.
//
// Resolvers read an empty host set as "supports nothing" (see
// debrid.HostInSet), so a transient error from the host list must not replace
// a good set: a failed Refresh only updates LastError. Load and Save are
// optional; without them the cache lives in memory.
type HostCache struct {
	// Fetch asks the live source for a fresh host set. A nil Fetch makes
	// Refresh a no-op, so a cache with only Load set still answers from disk.
	Fetch func(ctx context.Context) (map[string]bool, error)
	// Load seeds the cache before first use, so a restart during an outage
	// still serves the last known list. ok is false when nothing was ever
	// persisted, which differs from an empty set.
	Load func() (hosts map[string]bool, fetchedAt time.Time, ok bool)
	// Save is called after every successful refresh and never after a failed
	// one.
	Save func(hosts map[string]bool, fetchedAt time.Time)

	mu        sync.Mutex
	hosts     map[string]bool
	fetchedAt time.Time
	lastErr   error
	seeded    bool
}

// seedLocked loads the persisted set on first use, so a zero HostCache needs
// no constructor and Load's file I/O happens only when the set is needed.
func (c *HostCache) seedLocked() {
	if c.seeded {
		return
	}
	c.seeded = true
	if c.Load == nil {
		return
	}
	if hosts, at, ok := c.Load(); ok {
		c.hosts, c.fetchedAt = hosts, at
	}
}

// Hosts is the set to match against right now: the last successful fetch or
// the persisted set. It is nil until either exists, which Match reads as
// "matches nothing".
func (c *HostCache) Hosts() map[string]bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.seedLocked()
	return c.hosts
}

// FetchedAt is when the current set was obtained, from a live fetch or from
// the persisted copy, or the zero time. A failed Refresh leaves it alone so it
// shows how stale the list really is.
func (c *HostCache) FetchedAt() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.seedLocked()
	return c.fetchedAt
}

// LastError is the most recent refresh failure, or nil once a refresh has
// succeeded since.
func (c *HostCache) LastError() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.lastErr
}

// Refresh asks Fetch for a fresh set. Success replaces Hosts, stamps
// FetchedAt and calls Save; failure only records LastError.
func (c *HostCache) Refresh(ctx context.Context) error {
	if c.Fetch == nil {
		return nil
	}
	hosts, err := c.Fetch(ctx)
	c.mu.Lock()
	defer c.mu.Unlock()
	c.seedLocked()
	if err != nil {
		c.lastErr = err
		return err
	}
	c.hosts, c.fetchedAt, c.lastErr = hosts, time.Now(), nil
	if c.Save != nil {
		c.Save(hosts, c.fetchedAt)
	}
	return nil
}
