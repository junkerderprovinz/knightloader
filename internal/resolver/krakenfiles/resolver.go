// Package krakenfiles downloads Krakenfiles files itself, so they need no
// JDownloader. The name, size and state of a file come from the JSON the
// site's embed player reads, and the download link is the answer to the file
// page's download form, which wants a Cloudflare Turnstile check solved
// through the app's captcha prompts. A debrid service that carries
// Krakenfiles and a Krakenfiles login in JDownloader stay ahead of it, and a
// profile's file list stays with JDownloader.
//
// The link forms, the form and its answer follow the KrakenfilesCom plugins
// of JDownloader and pyLoad (both GPLv3).
package krakenfiles

import (
	"context"
	"errors"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/resolver"
)

// ResolverID is the routing id of the resolver and its backend.
const ResolverID = "krakenfiles"

// prio puts the client below every debrid service (43 and up) and a
// Krakenfiles login in JDownloader (jd.ActiveLoginPrio), and above
// JDownloader's own free mode (41), as the other free clients of our own.
const prio = 42

// maxConns is the one connection JDownloader's plugin opens for a free
// download.
const maxConns = 1

const site = "krakenfiles.com"

var idPattern = regexp.MustCompile(`^[A-Za-z0-9]+$`)

// fileID is the id of a Krakenfiles file link, "" for anything else.
func fileID(raw string) string {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return ""
	}
	if strings.TrimPrefix(strings.ToLower(u.Hostname()), "www.") != site {
		return ""
	}
	var id string
	switch parts := strings.Split(strings.Trim(u.Path, "/"), "/"); {
	case len(parts) == 3 && parts[0] == "view" && parts[2] == "file.html":
		id = parts[1]
	case len(parts) == 2 && parts[0] == "embed-video":
		id = parts[1]
	}
	if !idPattern.MatchString(id) {
		return ""
	}
	return id
}

func pageURL(id string) string { return "https://" + site + "/view/" + id + "/file.html" }

// Normalize writes a Krakenfiles file link of any form as
// https://krakenfiles.com/view/ID/file.html. It reports false for anything
// else.
func Normalize(raw string) (string, bool) {
	id := fileID(raw)
	if id == "" {
		return "", false
	}
	return pageURL(id), true
}

// Resolver claims Krakenfiles file links and answers what is behind them.
type Resolver struct {
	Client *Client
}

var (
	_ resolver.Resolver  = Resolver{}
	_ resolver.Inspector = Resolver{}
)

func (Resolver) Info() resolver.Info { return resolver.Info{ID: ResolverID, Prio: prio} }

func (Resolver) Match(raw string) bool { return fileID(raw) != "" }

// Resolve says what to fetch without asking Krakenfiles. It runs while the
// app holds its lock, so the name and size come from Inspect, and the backend
// reads the file's page when the download starts.
func (Resolver) Resolve(_ context.Context, req resolver.Request) (resolver.Result, error) {
	link, ok := Normalize(req.URL)
	if !ok {
		return resolver.Result{}, errors.New("krakenfiles: not a link to a file")
	}
	return resolver.Result{DirectURL: link, Name: req.URL, Connections: maxConns}, nil
}

// InspectPause spaces the requests of one Inspect, so a long paste does not
// look like a crawler to the site. Tests shorten it.
var InspectPause = 500 * time.Millisecond

// Inspect asks for the name and size of every file link, one after the
// other. A link that is no Krakenfiles file and an answer that could not be
// read get no opinion.
func (r Resolver) Inspect(ctx context.Context, urls []string) []resolver.LinkInfo {
	out := make([]resolver.LinkInfo, len(urls))
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
		switch f, err := r.Client.info(ctx, id); {
		case err != nil:
		case f == nil:
			out[i] = resolver.LinkInfo{Availability: core.AvailOffline}
		default:
			out[i] = resolver.LinkInfo{Availability: core.AvailOnline, Name: f.Title, Size: parseSize(f.Size)}
		}
	}
	return out
}
