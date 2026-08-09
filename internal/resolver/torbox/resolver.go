package torbox

import (
	"context"
	"net/url"
	"strings"

	"github.com/junkerderprovinz/knightloader/internal/resolver"
)

// Resolver matches links whose host is a TorBox-supported file host; the
// backend unlocks them into a direct CDN URL for the engine.
type Resolver struct {
	Hosts map[string]bool // set of supported hoster domains
}

func (Resolver) Info() resolver.Info { return resolver.Info{ID: "torbox", Prio: 35} }

func (r Resolver) Match(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" {
		return false
	}
	return hostInSet(u.Hostname(), r.Hosts)
}

func (Resolver) Resolve(_ context.Context, req resolver.Request) (resolver.Result, error) {
	return resolver.Result{DirectURL: req.URL, Name: req.URL}, nil
}

// No resolver.Checker here, and it is a decision rather than an omission.
//
// TorBox's only cheap read is /api/webdl/checkcached, which takes md5(link) and
// answers whether TorBox already holds the file on its own servers. That is a
// different question from the one an availability check asks, and answering it
// in the wrong column is worse than saying nothing: every live link TorBox has
// simply never fetched before comes back "not cached", which would be drawn as
// offline and deleted.
//
// The call that does ask the hoster is /api/webdl/createwebdownload, and it asks
// by starting a fetch job on the user's account. Spending somebody's plan to
// find out whether a link they have not started yet is still there is exactly
// the trade this seam refuses to make. If TorBox ever documents a read-only link
// check, it belongs here.

// hostInSet reports whether host or any parent domain is in set.
func hostInSet(host string, set map[string]bool) bool {
	if len(set) == 0 {
		return false
	}
	host = strings.ToLower(strings.TrimPrefix(host, "www."))
	for host != "" {
		if set[host] {
			return true
		}
		i := strings.IndexByte(host, '.')
		if i < 0 {
			break
		}
		host = host[i+1:]
	}
	return false
}
