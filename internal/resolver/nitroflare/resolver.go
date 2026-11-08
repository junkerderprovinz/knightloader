// Package nitroflare downloads Nitroflare links on the user's own premium
// login through Nitroflare's API v2, so they need no JDownloader, and names
// the files in the link collector through the same API, which answers
// without a login. A link without a premium login stays with JDownloader's
// free mode, which answers Nitroflare's captcha.
//
// The API's calls and errors, the link forms and the domains follow
// JDownloader's NitroFlareCom plugin and pyLoad's NitroflareCom plugins
// (both GPLv3).
package nitroflare

import (
	"context"
	"errors"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/hostalias"
	"github.com/junkerderprovinz/knightloader/internal/resolver"
)

// ResolverID is the routing id of the resolver and its backend.
const ResolverID = "nitroflare"

// Site is the domain every link is written out with, and the host a
// Nitroflare login is stored under.
const Site = "nitroflare.com"

// prio is that of a premium login in JDownloader (jd.ActiveLoginPrio), which
// this login replaces, above every debrid service.
const prio = 60

// filePath is a file page. The ids are upper-case, as JDownloader's plugin
// takes them.
var filePath = regexp.MustCompile(`^/(?:view|watch)/([A-Z0-9]+)(?:/[^/]*)?$`)

// Normalize writes a Nitroflare file link of any domain as
// https://nitroflare.com/view/ID. It reports false for anything else, a
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
	return "https://" + Site + "/view/" + m[1], true
}

func fileIDOf(link string) string {
	return strings.TrimPrefix(link, "https://"+Site+"/view/")
}

// Resolver claims Nitroflare file links while its backend has a login that
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

// LoginHost is the host whose login the resolver downloads on. The
// dispatcher ranks it at that login's row.
func (Resolver) LoginHost() string { return Site }

// Resolve says what to fetch without asking Nitroflare. It runs while the app
// holds its lock, so the name and size come from Inspect, and the backend asks
// the API when the download starts. JDownloader's plugin sets no limit on the
// connections of a premium download, so neither does the resolver.
func (Resolver) Resolve(_ context.Context, req resolver.Request) (resolver.Result, error) {
	link, ok := Normalize(req.URL)
	if !ok {
		return resolver.Result{}, errors.New("nitroflare: not a link to a file")
	}
	return resolver.Result{DirectURL: link, Name: req.URL}, nil
}

// Inspect asks for the name, size and state of every link at once, as many
// as the API takes in one question, with or without a login. A link that is
// no Nitroflare file and an answer that could not be read get no opinion.
func (r Resolver) Inspect(ctx context.Context, urls []string) []resolver.LinkInfo {
	b := r.Backend
	out := make([]resolver.LinkInfo, len(urls))
	ids := make([]string, len(urls))
	var batch []string
	for i, raw := range urls {
		if link, ok := Normalize(raw); ok {
			ids[i] = fileIDOf(link)
			if !slices.Contains(batch, ids[i]) {
				batch = append(batch, ids[i])
			}
		}
	}
	for start := 0; start < len(batch); start += maxBatch {
		if start > 0 {
			select {
			case <-ctx.Done():
				return out
			case <-time.After(b.inspectPause):
			}
		}
		part := batch[start:min(start+maxBatch, len(batch))]
		files, err := b.client.Files(ctx, part)
		for i, id := range ids {
			switch f, ok := files[id]; {
			case err != nil || !slices.Contains(part, id):
			case ok && f.Online:
				out[i] = resolver.LinkInfo{Availability: core.AvailOnline, Name: f.Name, Size: f.Size}
			default:
				out[i] = resolver.LinkInfo{Availability: core.AvailOffline}
			}
		}
	}
	return out
}
