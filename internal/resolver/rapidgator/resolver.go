// Package rapidgator downloads Rapidgator links on the user's own premium
// login through Rapidgator's API, so they need no JDownloader, and names the
// files in the link collector, through the API with the login and from the
// public file page without one. A link without a premium login stays with
// JDownloader's free mode, which answers Rapidgator's captcha.
//
// The API's errors and the page patterns follow JDownloader's RapidGatorNet
// plugin (GPLv3).
package rapidgator

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
const ResolverID = "rapidgator"

// Site is the domain every link is written out with, and the host a
// Rapidgator login is stored under.
const Site = "rapidgator.net"

// prio is that of a premium login in JDownloader (jd.ActiveLoginPrio), which
// this login replaces, above every debrid service.
const prio = 60

// maxConns is as many connections as JDownloader's plugin opens on a premium
// download, the most Rapidgator's servers take without trouble.
const maxConns = 5

// filePath is a file page: a 32-digit hex id or an older numeric one,
// optionally followed by the file's name.
var filePath = regexp.MustCompile(`^/file/([0-9a-fA-F]{32}|\d+)(?:/[^/]*)?$`)

// Normalize writes a Rapidgator file link of any domain as
// https://rapidgator.net/file/ID. It reports false for anything else, a
// folder link included.
func Normalize(raw string) (string, bool) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || hostalias.Canonical(u.Hostname()) != Site {
		return "", false
	}
	m := filePath.FindStringSubmatch(u.Path)
	if m == nil {
		return "", false
	}
	return "https://" + Site + "/file/" + strings.ToLower(m[1]), true
}

func fileIDOf(link string) string {
	return strings.TrimPrefix(link, "https://"+Site+"/file/")
}

// Resolver claims Rapidgator file links while its backend has a login that
// can download them, and answers what is behind any of them.
type Resolver struct {
	Backend *Backend
}

var _ resolver.Resolver = Resolver{}

func (Resolver) Info() resolver.Info { return resolver.Info{ID: ResolverID, Prio: prio} }

func (r Resolver) Match(raw string) bool {
	_, ok := Normalize(raw)
	return ok && r.Backend.Ready()
}

// Resolve says what to fetch without asking Rapidgator. It runs while the app
// holds its lock, so the name and size come from Inspect, and the backend asks
// the API when the download starts.
func (Resolver) Resolve(_ context.Context, req resolver.Request) (resolver.Result, error) {
	link, ok := Normalize(req.URL)
	if !ok {
		return resolver.Result{}, errors.New("rapidgator: not a link to a file")
	}
	return resolver.Result{DirectURL: link, Name: req.URL, Connections: maxConns}, nil
}

// Inspect asks for the name, size and state of each link, one after the
// other: through the API while the login works, which gives the size to the
// byte, and from the public file page otherwise. A link that is no Rapidgator
// file and an answer that could not be read get no opinion.
func (r Resolver) Inspect(ctx context.Context, urls []string) []resolver.LinkInfo {
	b := r.Backend
	out := make([]resolver.LinkInfo, len(urls))
	asked := false
	token := ""
	for i, raw := range urls {
		link, ok := Normalize(raw)
		if !ok {
			continue
		}
		if asked {
			select {
			case <-ctx.Done():
				return out
			case <-time.After(b.inspectPause):
			}
		}
		asked = true
		if token == "" && b.Ready() {
			if s, err := b.session(ctx, false); err == nil {
				token = s.token
			}
		}
		if token != "" {
			f, err := b.client.File(ctx, token, fileIDOf(link))
			switch {
			case errors.Is(err, ErrGone):
				out[i] = resolver.LinkInfo{Availability: core.AvailOffline}
				continue
			case err == nil:
				out[i] = resolver.LinkInfo{Availability: core.AvailOnline, Name: f.Name, Size: f.Size}
				continue
			}
		}
		p, err := b.client.Page(ctx, link)
		switch {
		case errors.Is(err, ErrGone):
			out[i] = resolver.LinkInfo{Availability: core.AvailOffline}
		case err == nil && p.name != "":
			out[i] = resolver.LinkInfo{Availability: core.AvailOnline, Name: p.name, Size: p.size}
		}
	}
	return out
}
