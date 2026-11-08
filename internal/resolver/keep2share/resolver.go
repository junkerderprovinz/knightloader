// Package keep2share downloads the links of the Keep2Share family, which is
// Keep2Share itself, FileBoom and TezFiles, on the user's own premium login
// through their shared API v2, so they need no JDownloader. It names the
// files in the link collector through the same API, which answers without a
// login. A link without a premium login stays with JDownloader's free mode,
// which answers the captcha.
//
// The API's errors, the link forms and the domains follow JDownloader's
// K2SApi, Keep2ShareCc and FileBoomMe plugins (GPLv3).
package keep2share

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

// Host is one site of the family. Each has its own accounts and API, and its
// own resolver and backend.
type Host struct {
	// ID is the routing id of the host's resolver and backend.
	ID string
	// Name is how the site writes itself.
	Name string
	// Site is the domain every link is written out with, and the host a
	// login is stored under.
	Site string
	// API is where the site's API v2 answers.
	API string
}

var (
	Keep2Share = Host{ID: "keep2share", Name: "Keep2Share", Site: "keep2share.cc", API: "https://keep2share.cc"}
	FileBoom   = Host{ID: "fileboom", Name: "FileBoom", Site: "fileboom.me", API: "https://fboom.me"}
	TezFiles   = Host{ID: "tezfiles", Name: "TezFiles", Site: "tezfiles.com", API: "https://tezfiles.com"}

	// Hosts is the whole family.
	Hosts = []Host{Keep2Share, FileBoom, TezFiles}
)

// prio is that of a premium login in JDownloader (jd.ActiveLoginPrio), which
// this login replaces, above every debrid service.
const prio = 60

// maxConns is as many connections as JDownloader's plugin opens on a premium
// download.
const maxConns = 10

// filePath is a file page in any of the forms JDownloader's plugin accepts.
// Most ids are 13 hex digits; longer ones with dashes and underscores carry
// a tracking part and point at a file all the same.
var filePath = regexp.MustCompile(`(?i)^/(?:f|file|preview)/(?:info/)?([a-z0-9_\-]{13,})(?:/[^/]*)?$`)

// Normalize writes a file link of any of the host's domains as
// https://SITE/file/ID. It reports false for anything else, a folder link
// included.
func (h Host) Normalize(raw string) (string, bool) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || hostalias.Canonical(u.Hostname()) != h.Site {
		return "", false
	}
	m := filePath.FindStringSubmatch(u.Path)
	if m == nil {
		return "", false
	}
	return h.link(m[1]), true
}

func (h Host) link(id string) string { return "https://" + h.Site + "/file/" + id }

func (h Host) fileIDOf(link string) string { return strings.TrimPrefix(link, h.link("")) }

// Resolver claims the host's file links while its backend has a login that
// can download them, and answers what is behind any of them.
type Resolver struct {
	Backend *Backend
}

var (
	_ resolver.Resolver    = Resolver{}
	_ resolver.LoginHoster = Resolver{}
)

func (r Resolver) Info() resolver.Info { return resolver.Info{ID: r.Backend.host.ID, Prio: prio} }

func (r Resolver) Match(raw string) bool {
	_, ok := r.Backend.host.Normalize(raw)
	return ok && r.Backend.Ready()
}

// LoginHost is the host whose login the resolver downloads on. The
// dispatcher ranks it at that login's row.
func (r Resolver) LoginHost() string { return r.Backend.host.Site }

// Resolve says what to fetch without asking the host. It runs while the app
// holds its lock, so the name and size come from Inspect, and the backend asks
// the API when the download starts.
func (r Resolver) Resolve(_ context.Context, req resolver.Request) (resolver.Result, error) {
	link, ok := r.Backend.host.Normalize(req.URL)
	if !ok {
		return resolver.Result{}, errors.New(r.Backend.host.ID + ": not a link to a file")
	}
	return resolver.Result{DirectURL: link, Name: req.URL, Connections: maxConns}, nil
}

// Inspect asks for the name, size and state of every link at once, as many
// as the API takes in one question, with or without a login. A link that is
// none of the host's files and an answer that could not be read get no
// opinion.
func (r Resolver) Inspect(ctx context.Context, urls []string) []resolver.LinkInfo {
	b := r.Backend
	out := make([]resolver.LinkInfo, len(urls))
	ids := make([]string, len(urls))
	var batch []string
	for i, raw := range urls {
		if link, ok := b.host.Normalize(raw); ok {
			ids[i] = b.host.fileIDOf(link)
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
			case ok && f.Available && !f.Folder:
				out[i] = resolver.LinkInfo{Availability: core.AvailOnline, Name: f.Name, Size: f.Size}
			default:
				out[i] = resolver.LinkInfo{Availability: core.AvailOffline}
			}
		}
	}
	return out
}
