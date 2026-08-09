package resolver

import (
	"context"
	"sync"
	"time"
)

// HostCache is a supported-host set that refreshes itself from a live source
// and never lets a failed refresh empty what it is already holding.
//
// THE FAILURE MODE THIS EXISTS FOR: a Resolver's Match reads a host set built
// from this cache, and an empty set reads as "this service supports nothing"
// - see debrid.HostInSet and the identical helper in torbox and ytdlp, all of
// which treat a nil or empty map as "matches nothing", on purpose, for a
// service that genuinely has no hosts configured. Before this existed,
// whatever asked a service for its host list and got a transient error (a
// timeout, a 500, a rate limit) handed that same empty result straight to
// Registry.Register, and the resolver kept its slot in the registry while
// silently matching nothing - not until the next successful refresh, but
// until the process restarted and asked again from a clean slate. So a
// failed Refresh leaves Hosts() exactly as it was, and only FetchedAt and
// LastError move.
//
// It is deliberately unopinionated about persistence: Load and Save are nil
// by default, which makes the type pure and trivially testable with nothing
// but a fake Fetch func, and a caller that wants a refresh to survive a
// restart wires real storage in once, at construction.
type HostCache struct {
	// Fetch asks the live source for a fresh host set. Required; a nil Fetch
	// makes Refresh a no-op rather than a panic, which is what lets a
	// zero-value HostCache with only Load set still answer Hosts() from disk.
	Fetch func(ctx context.Context) (map[string]bool, error)
	// Load seeds the cache before its first use, so a process that restarts
	// mid-outage still serves yesterday's list rather than an empty one for
	// however long the source stays down. ok is false for "nothing was ever
	// persisted", which is not the same as an empty set. Nil means "nothing
	// to seed from".
	Load func() (hosts map[string]bool, fetchedAt time.Time, ok bool)
	// Save is called after every SUCCESSFUL refresh, never after a failed
	// one - a failed fetch has nothing new worth writing down, and rewriting
	// the same bytes back on every failed retry would only wear the disk for
	// no reason. Nil means "keep it in memory only".
	Save func(hosts map[string]bool, fetchedAt time.Time)

	mu        sync.Mutex
	hosts     map[string]bool
	fetchedAt time.Time
	lastErr   error
	seeded    bool
}

// seedLocked loads the persisted set on first use rather than in a
// constructor - Load may do file I/O, and a type with no constructor at all
// is one fewer thing a caller has to get right: a zero HostCache with Fetch
// set is already usable, exactly as the tests for it rely on.
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

// Hosts is the set to match against right now: the last successful fetch (or
// the persisted set nothing has yet had a chance to replace). Before anything
// has ever succeeded and nothing was ever persisted, it is nil - which every
// Resolver.Match built on top of this already reads as "matches nothing",
// truthfully: no refresh has run yet, this is not a claim about the service.
func (c *HostCache) Hosts() map[string]bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.seedLocked()
	return c.hosts
}

// FetchedAt is when the current set was actually obtained - a live fetch if
// one has succeeded, else whenever the persisted set was last written, else
// the zero time. It is what "host list last refreshed" reads off this cache,
// and it is deliberately untouched by a failed Refresh: the point is to say
// how stale the list really is, not to reset the clock on every retry.
func (c *HostCache) FetchedAt() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.seedLocked()
	return c.fetchedAt
}

// LastError is the most recent refresh failure, or nil once a refresh has
// succeeded since - what explains a "last refreshed" stamp older than
// expected.
func (c *HostCache) LastError() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.lastErr
}

// Refresh asks Fetch for a fresh set. Success replaces Hosts, stamps
// FetchedAt to now and calls Save; failure records LastError and changes
// nothing else - see the type's own doc comment for why that half is the
// entire point of this existing.
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
