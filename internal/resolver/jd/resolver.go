package jd

import (
	"context"
	"net/url"
	"sync"

	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/hostalias"
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
	host = hostalias.Canonical(host)
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
	return activeHosts.set[hostalias.Canonical(host)]
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
		if n := hostalias.Canonical(h); n != "" {
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
	return knownHosts.set[hostalias.Canonical(host)]
}

// mediaHosts are the video sites yt-dlp serves while it runs. JD's plugin list
// includes YouTube, and without this the knownHostPrio boost would take such
// links away from yt-dlp. Every other host JD knows counts as a file hoster,
// whether or not a debrid list names it too.
var mediaHosts = struct {
	mu  sync.RWMutex
	set map[string]bool
}{set: map[string]bool{}}

// SetMediaHosts replaces the video sites JD leaves to yt-dlp. Nil, for a
// yt-dlp that is not running, leaves none.
func SetMediaHosts(hosts map[string]bool) {
	set := make(map[string]bool, len(hosts))
	for h := range hosts {
		if n := hostalias.Canonical(h); n != "" {
			set[n] = true
		}
	}
	mediaHosts.mu.Lock()
	defer mediaHosts.mu.Unlock()
	mediaHosts.set = set
}

func mediaSite(host string) bool {
	mediaHosts.mu.RLock()
	defer mediaHosts.mu.RUnlock()
	return mediaHosts.set[hostalias.Canonical(host)]
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
	if HostKnown(u.Hostname()) && !mediaSite(u.Hostname()) {
		return knownHostPrio
	}
	return basePrio
}

// FileHoster reports whether PriorityFor lifts host above resolver.Direct: a
// host with a confirmed login, or a file hoster JD has a plugin for. A plain
// GET there fetches the hoster's page rather than the file. Every host set in
// this file is kept by the hoster's main domain (internal/hostalias), so rg.to
// counts wherever rapidgator.net does.
func FileHoster(host string) bool {
	return HostActive(host) || (HostKnown(host) && !mediaSite(host))
}

// LoginHost returns the main domain of rawURL's host, as SetHostActive stores
// it, when that host has a confirmed-active native login, and "" otherwise.
func LoginHost(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil || u.Hostname() == "" || !HostActive(u.Hostname()) {
		return ""
	}
	return hostalias.Canonical(u.Hostname())
}
