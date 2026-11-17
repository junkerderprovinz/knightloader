package signinbrowser

import (
	"context"
	"net/netip"
	"net/url"
	"slices"
	"strings"
	"sync"

	"github.com/junkerderprovinz/knightloader/internal/httpx"
	"github.com/junkerderprovinz/knightloader/internal/sitelogin"
)

// providers are the hosts a site sends people to for signing in with another
// account, by site id. Each is one host, not its domain: all of google.com
// would let the window open search results and Google's page translator.
// Facebook and Twitch sign in on their own domains alone.
var providers = map[string][]string{
	"instagram": {"www.facebook.com", "m.facebook.com"},
	"patreon":   {"accounts.google.com", "appleid.apple.com", "www.facebook.com", "m.facebook.com"},
	"reddit":    {"accounts.google.com", "appleid.apple.com"},
	"tiktok":    {"accounts.google.com", "appleid.apple.com", "www.facebook.com", "m.facebook.com"},
	"vimeo":     {"accounts.google.com", "appleid.apple.com"},
	"x":         {"accounts.google.com", "appleid.apple.com"},
	// Google's sign-in ends on its consent and recovery pages before it hands
	// over to YouTube.
	"youtube": {"accounts.google.com", "consent.google.com", "gds.google.com", "myaccount.google.com"},
}

// allowList is what a site's window may show in its top frame: the site's
// own domains with everything below them, and its providers' hosts.
type allowList struct {
	domains []string
	hosts   []string
}

func allowListFor(site sitelogin.Site) allowList {
	return allowList{domains: site.Domains, hosts: providers[site.ID]}
}

// topLevel reports whether rawurl may be shown at the top. Only https is, so
// a sign-in never goes over the wire unencrypted.
func (l allowList) topLevel(rawurl string) bool {
	u, err := url.Parse(rawurl)
	if err != nil || u.Scheme != "https" {
		return false
	}
	host := strings.ToLower(u.Hostname())
	if slices.Contains(l.hosts, host) {
		return true
	}
	return slices.ContainsFunc(l.domains, func(d string) bool { return host == d || strings.HasSuffix(host, "."+d) })
}

// guard decides each request the browser makes. The top frame stays on the
// allow list. Frames and resources the pages load come from anywhere on the
// internet, since captchas and CDNs are not the site's own, but never from
// this machine or the LAN.
type guard struct {
	list   allowList
	lookup func(context.Context, string) httpx.Scope

	mu     sync.Mutex
	scopes map[string]httpx.Scope
}

func newGuard(site sitelogin.Site, lookup func(context.Context, string) httpx.Scope) *guard {
	return &guard{list: allowListFor(site), lookup: lookup, scopes: map[string]httpx.Scope{}}
}

// allows reports whether a request may go out. kind is the resource type
// Chromium reports, and top is set for the document of a page's own frame.
func (g *guard) allows(ctx context.Context, rawurl, kind string, top bool) bool {
	if top && kind == "Document" && !g.list.topLevel(rawurl) {
		return false
	}
	u, err := url.Parse(rawurl)
	if err != nil {
		return false
	}
	switch u.Scheme {
	case "http", "https", "ws", "wss":
	default:
		return false
	}
	return g.scope(ctx, strings.ToLower(u.Hostname())) == httpx.ScopePublic
}

// scope places a host, asking DNS once per name for as long as the window is
// open. localhost never reaches DNS in Chromium, so it does not here either.
func (g *guard) scope(ctx context.Context, host string) httpx.Scope {
	if host == "" || host == "localhost" || strings.HasSuffix(host, ".localhost") {
		return httpx.ScopeLocal
	}
	if a, err := netip.ParseAddr(host); err == nil {
		return httpx.ScopeOf(a)
	}
	g.mu.Lock()
	s, ok := g.scopes[host]
	g.mu.Unlock()
	if ok {
		return s
	}
	s = g.lookup(ctx, host)
	g.mu.Lock()
	g.scopes[host] = s
	g.mu.Unlock()
	return s
}
