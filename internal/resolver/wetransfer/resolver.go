// Package wetransfer downloads WeTransfer transfers itself, so they need no
// JDownloader. A transfer opens into one row per file through the public API
// WeTransfer's own download page calls, and each file comes from the direct
// link the API hands out for it. A transfer link downloaded as it is comes as
// the zip WeTransfer makes of it. A debrid service that carries WeTransfer
// stays ahead of it.
//
// The link forms and both API calls follow the WeTransferCom plugins of
// JDownloader (GPLv3).
package wetransfer

import (
	"context"
	"net/url"
	"path"
	"regexp"
	"strings"

	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/hostalias"
	"github.com/junkerderprovinz/knightloader/internal/hoster"
	"github.com/junkerderprovinz/knightloader/internal/resolver"
)

// ResolverID is the routing id of the resolver and its backend.
const ResolverID = "wetransfer"

// prio puts the client below every debrid service (43 and up) and above
// JDownloader's own free mode (41), as every free client of KnightLoader's
// own.
const prio = 42

var (
	// A transfer is named by its id and security hash, with the
	// recipient's id between them in a link that was mailed.
	downloadPattern = regexp.MustCompile(`^/downloads/([a-f0-9]{46})/(?:([a-f0-9]{46})/)?([a-f0-9]{4,12})/?$`)
	shortPattern    = regexp.MustCompile(`^/[\w-]+$`)
)

// shortHosts are the hosts of WeTransfer's short links.
var shortHosts = map[string]bool{"we.tl": true, "shorturls.wetransfer.com": true, "go.wetransfer.com": true}

// link is one parsed transfer link, or a short link to one. file is the id
// of one file of the transfer, which a listing writes into the fragment.
type link struct {
	short               string
	id, recipient, hash string
	file                string
}

func parse(raw string) (link, bool) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return link{}, false
	}
	host := strings.ToLower(u.Hostname())
	if shortHosts[host] {
		if !shortPattern.MatchString(u.Path) {
			return link{}, false
		}
		return link{short: u.Scheme + "://" + u.Host + u.Path}, true
	}
	if hostalias.Canonical(host) != "wetransfer.com" && !strings.HasSuffix(host, ".wetransfer.com") {
		return link{}, false
	}
	return parsePath(u)
}

// parsePath reads a transfer off a link's path alone, which is all a short
// link's redirect is trusted for.
func parsePath(u *url.URL) (link, bool) {
	m := downloadPattern.FindStringSubmatch(u.Path)
	if m == nil {
		return link{}, false
	}
	return link{id: m[1], recipient: m[2], hash: m[3], file: strings.TrimPrefix(u.Fragment, "file=")}, true
}

func (l link) String() string {
	s := "https://wetransfer.com/downloads/" + l.id + "/"
	if l.recipient != "" {
		s += l.recipient + "/"
	}
	s += l.hash
	if l.file != "" {
		s += "#file=" + l.file
	}
	return s
}

// Resolver claims WeTransfer links and answers what is behind them.
type Resolver struct {
	Client *Client
}

var (
	_ resolver.Resolver     = Resolver{}
	_ resolver.Inspector    = Resolver{}
	_ resolver.FolderLister = Resolver{}
)

func (Resolver) Info() resolver.Info { return resolver.Info{ID: ResolverID, Prio: prio} }

func (Resolver) Match(raw string) bool {
	_, ok := parse(raw)
	return ok
}

// Resolve says what to fetch without asking WeTransfer. It runs while the
// app holds its lock, so the name and size come from Inspect, and the
// backend asks the API when the download starts.
func (Resolver) Resolve(_ context.Context, req resolver.Request) (resolver.Result, error) {
	return resolver.Result{DirectURL: strings.TrimSpace(req.URL), Name: req.URL, Connections: maxConns}, nil
}

// Inspect asks the API about each transfer once, in input order of the
// links, and names every file link of it. A transfer link stands for its zip.
func (r Resolver) Inspect(ctx context.Context, urls []string) []resolver.LinkInfo {
	out := make([]resolver.LinkInfo, len(urls))
	type answer struct {
		t   transfer
		err error
	}
	asked := map[string]answer{}
	for i, raw := range urls {
		l, ok := parse(raw)
		if !ok || l.short != "" {
			continue
		}
		a, done := asked[l.id]
		if !done {
			a.t, a.err = r.Client.prepare(ctx, l, "")
			asked[l.id] = a
		}
		switch {
		case hoster.IsGone(a.err):
			out[i] = resolver.LinkInfo{Availability: core.AvailOffline}
		case a.err != nil:
		case a.t.Locked && len(a.t.Items) == 0:
			out[i] = resolver.LinkInfo{Availability: core.AvailOnline}
		case l.file == "":
			out[i] = resolver.LinkInfo{Availability: core.AvailOnline, Name: a.t.Zip, Size: a.t.Size}
		default:
			out[i] = resolver.LinkInfo{Availability: core.AvailOffline}
			for _, it := range a.t.Items {
				if it.ID == l.file {
					out[i] = resolver.LinkInfo{Availability: core.AvailOnline, Name: path.Base(it.Name), Size: it.Size}
				}
			}
		}
	}
	return out
}

// List opens a transfer into one row per file, the folders it was sent in
// kept. A link to one file of a transfer yields nothing.
func (r Resolver) List(ctx context.Context, raw, password string) ([]resolver.Listing, error) {
	l, ok := parse(raw)
	if !ok || l.file != "" {
		return nil, nil
	}
	l, err := r.Client.expand(ctx, l)
	if err != nil {
		return nil, err
	}
	t, err := r.Client.prepare(ctx, l, password)
	if err != nil {
		return nil, err
	}
	if t.Locked && len(t.Items) == 0 {
		return nil, passwordRefusal(password)
	}
	root := strings.TrimSuffix(t.Zip, ".zip")
	if root == "" {
		root = l.id
	}
	out := make([]resolver.Listing, 0, len(t.Items))
	for _, it := range t.Items {
		row := l
		row.file = it.ID
		dir := root
		if d := path.Dir(it.Name); d != "." {
			dir = path.Join(root, d)
		}
		out = append(out, resolver.Listing{URL: row.String(), Name: path.Base(it.Name), Size: it.Size, Folder: dir})
	}
	return out, nil
}
