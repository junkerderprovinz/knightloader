package jd

import (
	"context"
	"net/url"
	"strings"
	"sync"

	"github.com/junkerderprovinz/knightloader/internal/resolver"
)

// Resolver routes links to the JD backend. It is the lowest-priority catch-all:
// a final backup for hoster links that direct/torbox/yt-dlp don't claim, routed
// through JD's crawler and hoster plugins.
//
// basePrio is that catch-all's fixed position - unchanged by anything below,
// because resolver.Registry sorts its list once, at Register time, from
// Info().Prio, and never consults it again per URL (see resolver.Registry.For).
// A single scalar answered with no URL in hand cannot express "above Direct for
// rapidgator.net, still below it for everything else" - that needs a per-URL
// answer, which is what PriorityFor below is for, and why Info() itself is left
// alone here rather than made to lie about what one number can say.
const basePrio = 10

type Resolver struct{}

func (Resolver) Info() resolver.Info { return resolver.Info{ID: "jd", Prio: basePrio} }

func (Resolver) Match(raw string) bool {
	u, err := url.Parse(raw)
	return err == nil && (u.Scheme == "http" || u.Scheme == "https") && u.Host != ""
}

func (Resolver) Resolve(_ context.Context, req resolver.Request) (resolver.Result, error) {
	// JD fetches the bytes; we carry the original URL through DirectURL so the
	// backend can hand it to JD's addLinks. Real name/size arrive once JD has
	// crawled it (mirrored by the backend's poll loop).
	return resolver.Result{DirectURL: req.URL, Name: req.URL}, nil
}

// No resolver.Checker here. JD does know whether a link is online - its
// linkgrabber reports exactly that - but the only way to ask is to add the link
// to the linkgrabber, wait for the crawl, read the availability and remove it
// again. That is not a check, it is a write to somebody else's application: the
// JD instance may be shared, the crawl is asynchronous, and a check that fails
// halfway leaves packages behind in a list this app does not own.
//
// Links routed here therefore answer core.AvailUncheckable, which is the honest
// version of what they said before this seam existed - they sat at "not checked"
// forever, which reads as "nobody has looked" when somebody had.

// activeHostPrio is one above resolver.Direct's 40 (internal/resolver/direct.go)
// - the value PriorityFor answers for a host whose native login internal/hosterauth
// has confirmed active on the JD sidecar. Direct claims a link whose path merely
// looks like a file name, with no idea a hoster login exists for it; without a
// value that outranks it, a premium account the user just entered would sit
// unused every time that host's URLs happen to take that shape, and the link
// would go out anonymously anyway.
const activeHostPrio = 41

// activeHosts is this package's own small registry of hosts with a confirmed-
// active native login - the "lookup consulted at match/priority time" the
// per-host priority nudge is built on, kept here rather than as a field on
// Resolver so the zero-value literal every caller constructs
// (internal/app/app_accounts.go registers a bare jd.Resolver{}) keeps working
// unchanged: this is package state a reconciler updates from outside, not
// something wired into that construction this wave.
var activeHosts = struct {
	mu  sync.RWMutex
	set map[string]bool
}{set: map[string]bool{}}

// SetHostActive records whether host currently has a confirmed-active native
// login - called by internal/hosterauth's reconciler once JD's own account
// list says so, and cleared the moment JD stops saying so (removed, disabled,
// or gone back to unvalidated) so a login that stopped working does not go on
// silently outranking Direct for a host it no longer actually helps.
func SetHostActive(host string, active bool) {
	host = normalizeHost(host)
	if host == "" {
		return
	}
	activeHosts.mu.Lock()
	defer activeHosts.mu.Unlock()
	if active {
		activeHosts.set[host] = true
	} else {
		delete(activeHosts.set, host)
	}
}

// HostActive reports whether host currently has a confirmed-active native
// login, per the last call to SetHostActive for it.
func HostActive(host string) bool {
	activeHosts.mu.RLock()
	defer activeHosts.mu.RUnlock()
	return activeHosts.set[normalizeHost(host)]
}

// PriorityFor is the per-host priority nudge requirement 3 of the hoster-login
// design asks for: activeHostPrio, above resolver.Direct's 40, once rawURL's
// host has a confirmed-active native login; basePrio otherwise - the same
// answer Info().Prio gives today, so a host nothing has activated is routed
// exactly as before.
//
// WHY THIS FUNCTION EXISTS RATHER THAN A HIGHER Info().Prio: resolver.Registry
// (internal/resolver/resolver.go) sorts its resolver list once, from
// Info().Prio, at Register time - see resolver.Registry.For - and does not ask
// Info() again per URL afterwards. Info() itself takes no URL, so it cannot
// answer per-host at all. PriorityFor is consulted directly by
// internal/app/app_dispatch.go's dynamicPrio, which re-ranks the registry's
// frozen order per dispatch rather than trusting it outright - see
// dynamicPrio's own comment for why that lives there and not here.
func PriorityFor(rawURL string) int {
	u, err := url.Parse(rawURL)
	if err != nil || u.Hostname() == "" {
		return basePrio
	}
	if HostActive(u.Hostname()) {
		return activeHostPrio
	}
	return basePrio
}

// normalizeHost lower-cases a domain and strips a leading "www.", the same
// comparison debrid.NormalizeHost (internal/resolver/debrid/debrid.go) applies
// for the same reason - a browser-pasted URL and a curated host id need to
// compare equal regardless of case. Lower-cased BEFORE the prefix is stripped,
// unlike that sibling helper, so an all-caps "WWW." still matches - worth
// getting right here because SetHostActive/PriorityFor is exactly the kind of
// call a test or a future caller makes with whatever case it has on hand.
func normalizeHost(h string) string {
	h = strings.ToLower(strings.TrimSpace(h))
	return strings.TrimPrefix(h, "www.")
}
