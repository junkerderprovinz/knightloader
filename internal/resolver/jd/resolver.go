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

// The two boosts PriorityFor can answer with, and the gap between them is the
// whole point (jdp, 2026-09-07: "Es lädt rapidgator und youtube dateien nach
// wie vor nicht herunter obwohl ich premium accounts habe die rapidgator
// abdecken").
//
// There used to be ONE value, 41, for both cases, chosen only to sit above
// resolver.Direct's 40. Measured on jdp's own instance: 54 rapidgator links,
// 23 of them routed to JD in FREE mode and none to the Debrid-Link account he
// had added precisely for rapidgator. The reason is that 41 outranks every
// debrid service (they sat at 28 to 35), and JD has a plugin for practically
// every hoster there is - so "JD knows this host" quietly beat "the user pays
// for an account that unlocks this host", on every hoster, always. The free-mode
// wait and captcha was the ONLY path a hoster link could ever take.
//
//   - activeLoginPrio: a native login at THIS host, confirmed active by JD's own
//     account list. That is the user's own premium account at that hoster, which
//     beats a multihoster unlock of the same file, so it sits above everything.
//   - knownHostPrio: JD merely has a plugin. One above Direct, which claims a
//     link because its path looks like a file name and knows nothing about the
//     host - but BELOW every debrid service, which claims the host by name.
const (
	activeLoginPrio = 60
	knownHostPrio   = 41
)

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

// fileHosts is which of the hosts JD knows are FILE hosters rather than media
// sites, as classified by TorBox's own per-host type and by every debrid
// service's host list (internal/app/app_accounts.go builds it as
// `ytdlpExclude`, the set yt-dlp is told to stay out of).
//
// It exists because JD's plugin list covers YouTube too - measured on jdp's
// instance, listPremiumHoster has 714 entries and youtube.com is one of them -
// so the knownHostPrio boost would put a YouTube link in JD's hands and past
// yt-dlp, which is the one backend that turns such a link into the five
// keepable rows with a quality to pick. Exactly the shape of the TorBox problem
// fixed on 2026-09-06, arriving a second time through a different door.
//
// Empty means "nothing has classified anything yet", and then the boost applies
// as it always did: an install with no debrid account and no TorBox key has no
// classification to consult, and JD is the only thing that can fetch from a
// hoster at all there.
var fileHosts = struct {
	mu  sync.RWMutex
	set map[string]bool
}{set: map[string]bool{}}

// SetFileHosts replaces the set of hosts known to be file hosters.
func SetFileHosts(hosts map[string]bool) {
	set := make(map[string]bool, len(hosts))
	for h := range hosts {
		if n := normalizeHost(h); n != "" {
			set[n] = true
		}
	}
	fileHosts.mu.Lock()
	defer fileHosts.mu.Unlock()
	fileHosts.set = set
}

// mediaSiteForYtdlp reports whether host is one the media backend should own:
// something has classified hosts, and this one is not among the file hosters.
func mediaSiteForYtdlp(host string) bool {
	fileHosts.mu.RLock()
	defer fileHosts.mu.RUnlock()
	return len(fileHosts.set) > 0 && !fileHosts.set[normalizeHost(host)]
}

// PriorityFor is the per-host priority nudge requirement 3 of the hoster-login
// design asks for, in three steps rather than the two it started with: a
// confirmed native login at this host outranks everything, a host JD merely has
// a plugin for outranks Direct but not a debrid account, and everything else is
// basePrio - the same answer Info().Prio gives, so a host nothing knows about
// is routed exactly as before.
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
	// The user's own premium account at this hoster. Nothing outranks it: a
	// direct premium download from the host itself is what every multihoster
	// below is an approximation of.
	if HostActive(u.Hostname()) {
		return activeLoginPrio
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
	// But NOT for a media site. JD's plugin list covers YouTube as well, and a
	// YouTube link in JD's hands is one file with no variants and no quality to
	// pick - see fileHosts above for the measurement.
	if HostKnown(u.Hostname()) && !mediaSiteForYtdlp(u.Hostname()) {
		return knownHostPrio
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
