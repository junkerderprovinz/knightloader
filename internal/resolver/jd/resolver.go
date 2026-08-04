package jd

import (
	"context"
	"net/url"

	"github.com/junkerderprovinz/knightloader/internal/resolver"
)

// Resolver routes links to the JD backend. It is a catch-all for http(s) that
// outranks the Direct resolver, so when a JD instance is configured every link
// goes through JD's crawler and hoster plugins.
type Resolver struct{}

func (Resolver) Info() resolver.Info { return resolver.Info{ID: "jd", Prio: 100} }

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
