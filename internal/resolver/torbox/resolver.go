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
	// Account is which stored TorBox account this entry routes through: ""
	// for the default one, the account id for a second key on the same
	// service. Two accounts unlock the same hosts, so this changes nothing
	// about what is claimed - it is what puts both keys in the routing table
	// at all, which is what lets a benched one fall through to the other. See
	// resolver.SlotID and debrid.Resolver.Account, its exact sibling.
	Account string
}

// 50, above resolver.Direct's 40 like every other debrid service since
// 2026-09-07 - see the block over `configured` in internal/app/app_accounts.go
// for the measurement that moved them all: a service that lists a host by name
// outranks one that claimed the link because its path looked file-shaped.
//
// Every account of the service carries the SAME number, so a second key sorts
// directly behind the first (the registry's tie-break is registration order)
// and still ahead of whichever service comes next.
func (r Resolver) Info() resolver.Info {
	return resolver.Info{ID: resolver.SlotID("torbox", r.Account), Prio: 50}
}

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
