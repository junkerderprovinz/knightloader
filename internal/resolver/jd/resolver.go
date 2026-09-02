package jd

import (
	"context"
	"net/url"
	"strings"
	"sync"

	"github.com/junkerderprovinz/knightloader/internal/core"
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

type Resolver struct {
	// Backend is the running JD sidecar this resolver's Check reaches for a
	// verdict - nil is allowed and means the same as a Checker with nothing to
	// say: every link comes back uncheckable. It is nil in every test that
	// only cares about which links this resolver claims (the pattern
	// debrid.Resolver.Svc already established for the identical reason).
	Backend *Backend
}

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

// Check asks JD's own hoster plugins about a batch of links via
// Backend.CheckLinks: add them to the linkgrabber, wait for the crawl, read
// the availability, remove them again. This was deliberately left unbuilt for
// a long time - it is a write to somebody else's application, not a read, and
// a check that fails halfway can leave packages behind in a list this app
// does not own (Backend.CheckLinks's own removal is best-effort for exactly
// that reason). It exists now because there is no lighter alternative that is
// still honest: a generic HTTP probe cannot tell a premium hoster's "the file
// is gone" from "here is a login page" (see app_tasks.go's analyze, never used
// for a JD-routed link), and JD's ~1000 hoster plugins are the one thing that
// already knows the difference, for every hoster JD covers, without
// KnightLoader growing hoster-specific code of its own.
func (r Resolver) Check(ctx context.Context, urls []string) ([]core.Availability, error) {
	if r.Backend == nil {
		return resolver.Answers(nil, len(urls)), nil
	}
	got, err := r.Backend.CheckLinks(ctx, urls)
	if err != nil {
		return nil, err
	}
	return resolver.Answers(got, len(urls)), nil
}

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

// knownHosts is every host JD has a hoster plugin for, whether or not anybody
// has a login for it - JD's own listPremiumHoster, pushed here by
// internal/hosterauth's reconciler on the same pass that pushes SetHostActive.
//
// It is a SEPARATE fact from activeHosts and answers a different question.
// activeHosts asks "does a login for this host work"; this asks "does JD know
// how to fetch from this host at all".
var knownHosts = struct {
	mu  sync.RWMutex
	set map[string]bool
}{set: map[string]bool{}}

// SetKnownHosts replaces the list of hosts JD has a plugin for. Replaced whole
// rather than added to, so a host JD stops supporting stops outranking Direct
// on the very next pass instead of lingering until a restart.
func SetKnownHosts(hosts []string) {
	set := make(map[string]bool, len(hosts))
	for _, h := range hosts {
		if n := normalizeHost(h); n != "" {
			set[n] = true
		}
	}
	knownHosts.mu.Lock()
	defer knownHosts.mu.Unlock()
	knownHosts.set = set
}

// HostKnown reports whether JD has a hoster plugin for host.
func HostKnown(host string) bool {
	knownHosts.mu.RLock()
	defer knownHosts.mu.RUnlock()
	return knownHosts.set[normalizeHost(host)]
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
	// A host JD has a PLUGIN for outranks Direct even with no login at all, and
	// that is the whole of "free mode, like JDownloader" (jdp, 2026-09-02: "Wenn
	// man links runterladen möchte für die kein premium account hinterlegt ist
	// muss das angezeigt werden un der link im free modus heruntergeladen
	// werden. wie in JD").
	//
	// Without it, such a link went to resolver.Direct, whose "fetch" is a plain
	// HTTP GET with no idea a hoster is on the other end: for most premium
	// hosters that saves the landing PAGE under the real file name and reports a
	// successful download. That is worse than a failure, because nothing on
	// screen is wrong. JD's own plugin for that host is the only thing in this
	// app that knows the free-mode dance - the wait, the countdown, the captcha,
	// the per-IP limit - so an anonymous fetch of a known hoster belongs there.
	//
	// Same number as the active case on purpose: both mean "JD beats a blind
	// GET", and only one resolver is being ranked, so a second value below it
	// would express a difference nothing can act on. What the two cases DO
	// differ in is what the user is told, which is core.Task.Mode's job, not
	// this one's.
	if HostActive(u.Hostname()) || HostKnown(u.Hostname()) {
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
