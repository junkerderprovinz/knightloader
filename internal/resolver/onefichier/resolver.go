// Package onefichier downloads free 1fichier links itself, so they need no
// JDownloader, and the waits 1fichier puts between free downloads show on the
// task while they are waited out. A debrid service that carries 1fichier and a
// 1fichier login in JDownloader stay ahead of it, since neither has to wait.
//
// The page patterns and the form handling follow pyLoad's OneFichierCom
// plugin (GPLv3).
package onefichier

import (
	"context"
	"errors"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/hostalias"
	"github.com/junkerderprovinz/knightloader/internal/resolver"
)

// ResolverID is the routing id of the resolver and its backend.
const ResolverID = "1fichier"

// prio puts the free client below every debrid service (43 and up) and a
// 1fichier login in JDownloader (jd.ActiveLoginPrio), which download without
// the waits, and above JDownloader's own free mode (41).
const prio = 42

// site is the domain every link is written out with.
const site = "1fichier.com"

var fileID = regexp.MustCompile(`^\w+`)

// Normalize writes a 1fichier file link of any domain and either form,
// https://1fichier.com/?ID or https://ID.1fichier.com/, as
// https://1fichier.com/?ID. It reports false for anything else, a folder
// link included.
func Normalize(raw string) (string, bool) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return "", false
	}
	host := strings.TrimPrefix(strings.ToLower(u.Hostname()), "www.")
	var id string
	if hostalias.Canonical(host) == site {
		if u.Path != "" && u.Path != "/" {
			return "", false
		}
		id = fileID.FindString(u.RawQuery)
	} else if sub, parent, ok := strings.Cut(host, "."); ok && hostalias.Canonical(parent) == site && fileID.FindString(sub) == sub {
		id = sub
	}
	if id == "" {
		return "", false
	}
	return "https://" + site + "/?" + id, true
}

// Resolver claims 1fichier file links and answers what is behind them.
type Resolver struct {
	Client *Client
}

var _ resolver.Resolver = Resolver{}

func (Resolver) Info() resolver.Info { return resolver.Info{ID: ResolverID, Prio: prio} }

func (Resolver) Match(raw string) bool {
	_, ok := Normalize(raw)
	return ok
}

// Resolve says what to fetch without asking 1fichier. It runs while the app
// holds its lock, so the name and size come from Inspect, and the backend
// reads the page when the download starts. A free download gets one
// connection.
func (Resolver) Resolve(_ context.Context, req resolver.Request) (resolver.Result, error) {
	link, ok := Normalize(req.URL)
	if !ok {
		return resolver.Result{}, errors.New("1fichier: not a link to a file")
	}
	return resolver.Result{DirectURL: link, Name: req.URL, Connections: 1}, nil
}

// inspectPause spaces the page reads of one Inspect, so a long paste does not
// look like a crawler to 1fichier. Tests shorten it.
var inspectPause = 500 * time.Millisecond

// Inspect reads the file page of each link, one after the other, for its name,
// size and whether it is still there. A link that is no 1fichier file and a
// page that could not be read get no opinion.
func (r Resolver) Inspect(ctx context.Context, urls []string) []resolver.LinkInfo {
	out := make([]resolver.LinkInfo, len(urls))
	asked := false
	for i, raw := range urls {
		link, ok := Normalize(raw)
		if !ok {
			continue
		}
		if asked {
			select {
			case <-ctx.Done():
				return out
			case <-time.After(inspectPause):
			}
		}
		asked = true
		p, err := r.Client.Open(ctx, link)
		switch {
		case errors.Is(err, ErrGone):
			out[i] = resolver.LinkInfo{Availability: core.AvailOffline}
		case p != nil && p.Name != "":
			out[i] = resolver.LinkInfo{Availability: core.AvailOnline, Name: p.Name, Size: p.Size}
		}
	}
	return out
}
