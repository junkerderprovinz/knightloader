package jd

import (
	"context"
	"net/url"

	"github.com/junkerderprovinz/knightloader/internal/resolver"
)

// Resolver routes links to the JD backend. It is the lowest-priority catch-all:
// a final backup for hoster links that direct/torbox/yt-dlp don't claim, routed
// through JD's crawler and hoster plugins.
type Resolver struct{}

func (Resolver) Info() resolver.Info { return resolver.Info{ID: "jd", Prio: 10} }

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
