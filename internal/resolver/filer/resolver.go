// Package filer downloads filer.net links on the user's own premium login
// through filer.net's API, so they need no JDownloader, and names the files
// in the link collector through the same API, which answers without a login.
// A link without a premium login stays with JDownloader's free mode, which
// answers filer.net's captcha.
//
// The calls and their answers follow filer.net's API documentation at
// https://filer.net/api. The link forms, the connection limit and the code
// for spent traffic follow JDownloader's FilerNet plugin (GPLv3).
package filer

import (
	"context"
	"errors"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/resolver"
)

// ResolverID is the routing id of the resolver and its backend.
const ResolverID = "filer"

// Site is the domain every link is written out with, and the host a filer.net
// login is stored under.
const Site = "filer.net"

// prio is that of a premium login in JDownloader (jd.ActiveLoginPrio), which
// this login replaces, above every debrid service.
const prio = 60

// conns is the one connection JDownloader's plugin opens on a download,
// premium too.
const conns = 1

// filePath is a file page. The API wants ids of at least 16 characters.
var filePath = regexp.MustCompile(`^/(?:app\.php/)?(?:get|dl)/([A-Za-z0-9]{16,})(?:/[^/]*)?$`)

// Normalize writes a filer.net file link as https://filer.net/get/ID. It
// reports false for anything else, a folder link included.
func Normalize(raw string) (string, bool) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || strings.TrimPrefix(strings.ToLower(u.Hostname()), "www.") != Site {
		return "", false
	}
	m := filePath.FindStringSubmatch(u.Path)
	if m == nil {
		return "", false
	}
	return "https://" + Site + "/get/" + m[1], true
}

func fileIDOf(link string) string {
	return strings.TrimPrefix(link, "https://"+Site+"/get/")
}

// Resolver claims filer.net file links while its backend has a login that can
// download them, and answers what is behind any of them.
type Resolver struct {
	Backend *Backend
}

var (
	_ resolver.Resolver    = Resolver{}
	_ resolver.LoginHoster = Resolver{}
)

func (Resolver) Info() resolver.Info { return resolver.Info{ID: ResolverID, Prio: prio} }

func (r Resolver) Match(raw string) bool {
	_, ok := Normalize(raw)
	return ok && r.Backend.Ready()
}

// LoginHost is the host whose login the resolver downloads on. The
// dispatcher ranks it at that login's row.
func (Resolver) LoginHost() string { return Site }

// Resolve says what to fetch without asking filer.net. It runs while the app
// holds its lock, so the name and size come from Inspect, and the backend asks
// the API when the download starts.
func (Resolver) Resolve(_ context.Context, req resolver.Request) (resolver.Result, error) {
	link, ok := Normalize(req.URL)
	if !ok {
		return resolver.Result{}, errors.New("filer.net: not a link to a file")
	}
	return resolver.Result{DirectURL: link, Name: req.URL, Connections: conns}, nil
}

// Inspect asks for the name, size and state of every link at once, as many
// as the API takes in one question, with or without a login. A link that is
// no filer.net file and an answer that could not be read get no opinion.
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
			case ok:
				out[i] = resolver.LinkInfo{Availability: core.AvailOnline, Name: f.Name, Size: f.Size}
			default:
				out[i] = resolver.LinkInfo{Availability: core.AvailOffline}
			}
		}
	}
	return out
}
