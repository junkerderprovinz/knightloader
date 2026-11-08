// Package ddownload downloads ddownload links itself, so they need no
// JDownloader: on the user's premium login when there is one, and for free
// otherwise, with the Turnstile captchas ddownload shows going to the app's
// captcha prompts and solvers and its waits showing on the task. A debrid
// service that carries ddownload stays ahead of a free download, and a
// premium login ranks where a ddownload login in JDownloader would.
//
// The page patterns, the sign-in and the form handling follow the DdownloadCom
// and XFileSharingProBasic plugins of JDownloader and the DdownloadCom plugins
// of pyLoad (all GPLv3).
package ddownload

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
const ResolverID = "ddownload"

// prio puts the free client below every debrid service (43 and up), which
// downloads without the captchas and waits, and above JDownloader's own free
// mode (41). A premium login ranks higher, see Resolver.LoginHost.
const prio = 42

// Site is the domain every link is written out with, and the host a login is
// filed under.
const Site = "ddownload.com"

var filePath = regexp.MustCompile(`^/([A-Za-z0-9]{12})(?:/[^/]*)?$`)

// Normalize writes a ddownload file link of either domain, with or without the
// file name after the code, as https://ddownload.com/CODE. It reports false
// for anything else.
func Normalize(raw string) (string, bool) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return "", false
	}
	host := strings.TrimPrefix(strings.ToLower(u.Hostname()), "www.")
	if hostalias.Canonical(host) != Site {
		return "", false
	}
	m := filePath.FindStringSubmatch(u.Path)
	if m == nil {
		return "", false
	}
	return "https://" + Site + "/" + strings.ToLower(m[1]), true
}

// Resolver claims ddownload file links and answers what is behind them.
type Resolver struct {
	Client *Client
	// Premium reports whether a confirmed premium login is at hand. It is read
	// while the app holds its lock, so it must not ask ddownload.
	Premium func() bool
}

var _ resolver.Resolver = Resolver{}

func (Resolver) Info() resolver.Info { return resolver.Info{ID: ResolverID, Prio: prio} }

func (Resolver) Match(raw string) bool {
	_, ok := Normalize(raw)
	return ok
}

// LoginHost is the host whose login the resolver downloads on, "" while it
// downloads for free. The dispatcher ranks it at that login's row.
func (r Resolver) LoginHost() string {
	if r.Premium != nil && r.Premium() {
		return Site
	}
	return ""
}

// Resolve says what to fetch without asking ddownload. It runs while the app
// holds its lock, so the name and size come from Inspect, and the backend
// reads the page when the download starts. ddownload serves every download
// over one connection.
func (Resolver) Resolve(_ context.Context, req resolver.Request) (resolver.Result, error) {
	link, ok := Normalize(req.URL)
	if !ok {
		return resolver.Result{}, errors.New("ddownload: not a link to a file")
	}
	return resolver.Result{DirectURL: link, Name: req.URL, Connections: 1}, nil
}

// inspectPause spaces the page reads of one Inspect, so a long paste does not
// look like a crawler to ddownload. Tests shorten it.
var inspectPause = 500 * time.Millisecond

// Inspect reads the file page of each link, one after the other and without
// signing in, for its name, size and whether it is still there. A link that
// is no ddownload file and a page that could not be read get no opinion.
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
