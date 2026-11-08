// Package workupload downloads Workupload files itself, so they need no
// JDownloader. Every page sits behind Workupload's robot check, a SHA-256
// puzzle the client solves on its own, and the check comes back after a few
// requests. The name and size come from the file page, the download link from
// the API the start page asks. An archive link stays with JDownloader.
//
// The link forms, the puzzle, the password form and the API follow the
// WorkuploadCom plugins of JDownloader and pyLoad (both GPLv3).
package workupload

import (
	"context"
	"errors"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/hoster"
	"github.com/junkerderprovinz/knightloader/internal/resolver"
)

// ResolverID is the routing id of the resolver and its backend.
const ResolverID = "workupload"

// prio puts the client below every debrid service (43) and above
// JDownloader's own free mode (41), as the other free clients of our own.
// Workupload has no accounts to rank a login by.
const prio = 42

// maxConns is the one connection JDownloader's plugin opens, since the
// download servers do not resume.
const maxConns = 1

const site = "workupload.com"

var idPattern = regexp.MustCompile(`^[A-Za-z0-9]+$`)

// fileID is the id of a Workupload file link, "" for anything else.
func fileID(raw string) string {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return ""
	}
	host := strings.ToLower(u.Hostname())
	if host != site && host != "www."+site && host != "en."+site {
		return ""
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(parts) != 2 || (parts[0] != "file" && parts[0] != "start" && parts[0] != "report") || !idPattern.MatchString(parts[1]) {
		return ""
	}
	return parts[1]
}

// Normalize writes a Workupload file link of any form as
// https://workupload.com/file/ID. It reports false for anything else.
func Normalize(raw string) (string, bool) {
	id := fileID(raw)
	if id == "" {
		return "", false
	}
	return "https://" + site + "/file/" + id, true
}

// Resolver claims Workupload file links and answers what is behind them.
type Resolver struct {
	Client *Client
}

var (
	_ resolver.Resolver  = Resolver{}
	_ resolver.Inspector = Resolver{}
)

func (Resolver) Info() resolver.Info { return resolver.Info{ID: ResolverID, Prio: prio} }

func (Resolver) Match(raw string) bool { return fileID(raw) != "" }

// Resolve says what to fetch without asking Workupload. It runs while the
// app holds its lock, so the name and size come from Inspect, and the backend
// reads the file's page when the download starts.
func (Resolver) Resolve(_ context.Context, req resolver.Request) (resolver.Result, error) {
	link, ok := Normalize(req.URL)
	if !ok {
		return resolver.Result{}, errors.New("workupload: not a link to a file")
	}
	return resolver.Result{DirectURL: link, Name: req.URL, Connections: maxConns}, nil
}

// InspectPause spaces the page reads of one Inspect, so a long paste does not
// look like a crawler to the site. Tests shorten it.
var InspectPause = 500 * time.Millisecond

// Inspect reads the file page of each link, one after the other in one visit,
// for its name, size and whether it is still there. A link that is no
// Workupload file and a page that could not be read get no opinion.
func (r Resolver) Inspect(ctx context.Context, urls []string) []resolver.LinkInfo {
	out := make([]resolver.LinkInfo, len(urls))
	v := r.Client.visit()
	asked := false
	for i, raw := range urls {
		id := fileID(raw)
		if id == "" {
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
		switch p, err := v.filePage(ctx, id); {
		case hoster.IsGone(err):
			out[i] = resolver.LinkInfo{Availability: core.AvailOffline}
		case err == nil:
			out[i] = resolver.LinkInfo{Availability: core.AvailOnline, Name: p.name, Size: p.size}
		}
	}
	return out
}
