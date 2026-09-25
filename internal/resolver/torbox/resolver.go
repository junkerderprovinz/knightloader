package torbox

import (
	"context"
	"net/url"
	"strings"

	"github.com/junkerderprovinz/knightloader/internal/resolver"
	"github.com/junkerderprovinz/knightloader/internal/resolver/debrid"
	"github.com/junkerderprovinz/knightloader/internal/resolver/torrent"
)

// Resolver matches links whose host is a TorBox-supported file host; the
// backend unlocks them into a direct CDN URL for the engine.
type Resolver struct {
	Hosts map[string]bool // set of supported hoster domains
	// Account is the stored TorBox account this entry routes through, "" for
	// the default one. It does not change what is claimed; it gives each key
	// its own routing slot (see resolver.SlotID).
	Account string
	// Torrents also claims magnet links and uploaded .torrent files, which
	// TorBox fetches through its torrent API (see Torrents).
	Torrents bool
}

// Info places TorBox at 50, above resolver.Direct (40) like the other debrid
// services, since it claims hosts by name. All accounts share the number, so
// a second key sorts right behind the first.
func (r Resolver) Info() resolver.Info {
	return resolver.Info{ID: resolver.SlotID("torbox", r.Account), Prio: 50}
}

// Match claims a hoster link TorBox supports, a torrent when asked to, and a
// download imported from this very account.
func (r Resolver) Match(raw string) bool {
	if slot, _, ok := debrid.ParseJobLink(raw); ok {
		return slot == r.Info().ID
	}
	if r.Torrents && (torrent.Resolver{}).Match(raw) {
		return true
	}
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" {
		return false
	}
	return hostInSet(u.Hostname(), r.Hosts)
}

// Resolve checks a torrent the way the built-in client does and passes a
// hoster link on as it is. An imported download keeps the name it was staged
// with.
func (r Resolver) Resolve(ctx context.Context, req resolver.Request) (resolver.Result, error) {
	if _, _, ok := debrid.ParseJobLink(req.URL); ok {
		return resolver.Result{DirectURL: req.URL}, nil
	}
	if r.Torrents && torrent.IsURI(req.URL) {
		return (torrent.Resolver{}).Resolve(ctx, req)
	}
	return resolver.Result{DirectURL: req.URL, Name: req.URL}, nil
}

// Resolver implements no resolver.Checker. TorBox's cheap
// /api/webdl/checkcached only says whether TorBox already holds a file, so a
// live but uncached link would read as offline, and createwebdownload checks
// the hoster only by starting a job on the user's plan.

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
