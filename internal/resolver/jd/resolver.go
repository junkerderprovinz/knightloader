package jd

import (
	"context"
	"net/url"
	"strings"
	"sync"

	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/resolver"
)

// basePrio is fixed because the registry sorts by Info().Prio once at Register
// time; the per-host boost comes from PriorityFor.
const basePrio = 10

// Resolver routes links to the JD backend. It is the lowest-priority catch-all
// for hoster links no other backend claims.
type Resolver struct {
	// Backend is the running JD sidecar that Check asks. Nil makes every link
	// uncheckable.
	Backend *Backend
}

func (Resolver) Info() resolver.Info { return resolver.Info{ID: "jd", Prio: basePrio} }

func (Resolver) Match(raw string) bool {
	u, err := url.Parse(raw)
	return err == nil && (u.Scheme == "http" || u.Scheme == "https") && u.Host != ""
}

func (Resolver) Resolve(_ context.Context, req resolver.Request) (resolver.Result, error) {
	// JD fetches the bytes; the backend hands DirectURL to addLinks, and the
	// real name and size arrive through its poll loop.
	return resolver.Result{DirectURL: req.URL, Name: req.URL}, nil
}

// Check asks JD's hoster plugins about a batch of links through
// Backend.CheckLinks. It writes to JD's linkgrabber and removes the entries
// again on a best-effort basis, but JD's plugins are the only thing that can
// tell a premium hoster's "file gone" from a login page without KnightLoader
// growing hoster-specific code.
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

// The priorities PriorityFor can answer with. A confirmed native login at the
// host is the user's own premium account there and beats everything. A host
// JD merely has a plugin for sits one above resolver.Direct (40) but below
// every debrid service, which the user pays for to unlock that host.
const (
	ActiveLoginPrio = 60
	knownHostPrio   = 41
)

// activeHosts holds the hosts with a confirmed-active native login. It is
// package state updated by hosterauth's reconciler, so the zero-value
// jd.Resolver{} the app registers needs no wiring.
var activeHosts = struct {
	mu  sync.RWMutex
	set map[string]bool
}{set: map[string]bool{}}

// SetHostActive records whether host has a confirmed-active native login.
// The reconciler clears it once JD's account list stops saying so, so a login
// that stopped working stops raising the host's priority.
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

// knownHosts is every host JD has a hoster plugin for (listPremiumHoster),
// whether or not a login exists.
var knownHosts = struct {
	mu  sync.RWMutex
	set map[string]bool
}{set: map[string]bool{}}

// SetKnownHosts replaces the list of hosts JD has a plugin for, so a host JD
// drops loses its boost on the next pass.
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

// fileHosts is the set of hosts classified as file hosters rather than media
// sites, from TorBox's host types and the debrid host lists (the app's
// ytdlpExclude). JD's plugin list includes YouTube, and without this the
// knownHostPrio boost would take such links away from yt-dlp. Empty means
// nothing has been classified, and the boost applies to every known host.
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

// PriorityFor is JD's priority for one link: ActiveLoginPrio for a host with a
// confirmed native login, knownHostPrio for a file hoster JD has a plugin for,
// and basePrio otherwise. The dispatcher's dynamicPrio consults it per URL,
// since Info takes no URL and the registry sorts only once.
func PriorityFor(rawURL string) int {
	u, err := url.Parse(rawURL)
	if err != nil || u.Hostname() == "" {
		return basePrio
	}
	if HostActive(u.Hostname()) {
		return ActiveLoginPrio
	}

	// A known hoster without a login is fetched in JD's free mode. Direct
	// would do a plain GET and often save the hoster's landing page under the
	// file name, reported as a success; only JD's plugin handles the wait,
	// countdown and captcha. Media sites stay with yt-dlp.
	if HostKnown(u.Hostname()) && !mediaSiteForYtdlp(u.Hostname()) {
		return knownHostPrio
	}
	return basePrio
}

// LoginHost returns rawURL's host, normalised as SetHostActive stores it, when
// that host has a confirmed-active native login, and "" otherwise.
func LoginHost(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil || u.Hostname() == "" || !HostActive(u.Hostname()) {
		return ""
	}
	return normalizeHost(u.Hostname())
}

// normalizeHost lower-cases a domain before stripping a leading "www.", so an
// upper-case "WWW." prefix goes too.
func normalizeHost(h string) string {
	h = strings.ToLower(strings.TrimSpace(h))
	return strings.TrimPrefix(h, "www.")
}
