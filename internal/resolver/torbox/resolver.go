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
