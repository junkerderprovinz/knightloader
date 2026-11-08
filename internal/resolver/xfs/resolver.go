package xfs

import (
	"context"
	"errors"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/resolver"
)

// prio puts a free client below every debrid service (43 and up), which
// downloads without the captchas and waits, and above JDownloader's own free
// mode (41). A premium login ranks higher, see Resolver.LoginHost.
const prio = 42

// Resolver claims a site's file links and answers what is behind them.
type Resolver struct {
	Site *Site
	// Client reads the pages for Inspect.
	Client *Client
	// Premium reports whether a confirmed premium login is at hand. It is read
	// while the app holds its lock, so it must not ask the site.
	Premium func() bool
}

var _ resolver.Resolver = Resolver{}

func (r Resolver) Info() resolver.Info { return resolver.Info{ID: r.Site.ID, Prio: prio} }

func (r Resolver) Match(raw string) bool {
	_, ok := r.Site.Normalize(raw)
	return ok
}

func (r Resolver) premium() bool { return r.Premium != nil && r.Premium() }

// LoginHost is the host whose login the resolver downloads on, "" while it
// downloads for free. The dispatcher ranks it at that login's row.
func (r Resolver) LoginHost() string {
	if r.premium() {
		return r.Site.Host
	}
	return ""
}

// Resolve says what to fetch without asking the site. It runs while the app
// holds its lock, so the name and size come from Inspect, and the backend
// reads the page when the download starts.
func (r Resolver) Resolve(_ context.Context, req resolver.Request) (resolver.Result, error) {
	link, ok := r.Site.Normalize(req.URL)
	if !ok {
		return resolver.Result{}, errors.New(r.Site.Name + ": not a link to a file")
	}
	conns := r.Site.FreeConns
	if r.premium() {
		conns = r.Site.PremiumConns
	}
	return resolver.Result{DirectURL: link, Name: req.URL, Connections: conns}, nil
}

// InspectPause spaces the page reads of one Inspect, so a long paste does not
// look like a crawler to the site. Tests shorten it.
var InspectPause = 500 * time.Millisecond

// Inspect reads the file page of each link, one after the other and without
// signing in, for its name, size and whether it is still there. A link that
// is no file of the site and a page that could not be read get no opinion.
func (r Resolver) Inspect(ctx context.Context, urls []string) []resolver.LinkInfo {
	out := make([]resolver.LinkInfo, len(urls))
	asked := false
	for i, raw := range urls {
		link, ok := r.Site.Normalize(raw)
		if !ok {
			continue
		}
		if asked {
			select {
			case <-ctx.Done():
				return out
			case <-time.After(InspectPause):
			}
		}
		asked = true
		p, err := r.Client.Open(ctx, r.Client.NewSession(), link)
		switch {
		case errors.Is(err, ErrGone):
			out[i] = resolver.LinkInfo{Availability: core.AvailOffline}
		case p != nil && p.Name != "":
			out[i] = resolver.LinkInfo{Availability: core.AvailOnline, Name: p.Name, Size: p.Size}
		}
	}
	return out
}
