// Package swisstransfer downloads SwissTransfer transfers itself, so they
// need no JDownloader. A transfer opens into one row per file from the page
// data of its download page, and each file comes from the signed link the
// site's API hands out for it. A debrid service that carries SwissTransfer
// stays ahead of it.
//
// The page data, the file call and the password post follow the
// SwisstransferCom plugins of JDownloader (GPLv3).
package swisstransfer

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
const ResolverID = "swisstransfer"

// prio puts the client below every debrid service (43 and up) and above
// JDownloader's own free mode (41), as every free client of KnightLoader's
// own.
const prio = 42

// A transfer link is /dl/ and a UUID, or /d/ on the older links.
var linkPattern = regexp.MustCompile(`^/dl?/([0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12})/?$`)

// link is one parsed transfer link. file is the id of one file of the
// transfer, which a listing writes into the fragment.
type link struct {
	id, file string
}

func parse(raw string) (link, bool) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || hostalias.Canonical(u.Hostname()) != "swisstransfer.com" {
		return link{}, false
	}
	m := linkPattern.FindStringSubmatch(strings.ToLower(u.Path))
	if m == nil {
		return link{}, false
	}
	return link{id: m[1], file: strings.TrimPrefix(u.Fragment, "file=")}, true
}

func (l link) String() string {
	s := "https://www.swisstransfer.com/dl/" + l.id
	if l.file != "" {
		s += "#file=" + l.file
	}
	return s
}

// Resolver claims SwissTransfer links and answers what is behind them.
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

// Resolve says what to fetch without asking SwissTransfer. It runs while the
// app holds its lock, so the name and size come from Inspect, and the
// backend asks for the file's link when the download starts.
func (Resolver) Resolve(_ context.Context, req resolver.Request) (resolver.Result, error) {
	l, _ := parse(req.URL)
	return resolver.Result{DirectURL: l.String(), Name: req.URL}, nil
}

// Inspect reads each transfer's page once, in input order of the links, and
// names every file link of it. A locked transfer is there but goes unnamed,
// and a transfer link stands for its only file
// or is there as a whole.
func (r Resolver) Inspect(ctx context.Context, urls []string) []resolver.LinkInfo {
	out := make([]resolver.LinkInfo, len(urls))
	type answer struct {
		t   transfer
		err error
	}
	read := map[string]answer{}
	for i, raw := range urls {
		l, ok := parse(raw)
		if !ok {
			continue
		}
		a, done := read[l.id]
		if !done {
			a.t, _, a.err = r.Client.transfer(ctx, l, "")
			read[l.id] = a
		}
		switch f, found := a.t.find(l.file); {
		case hoster.IsGone(a.err):
			out[i] = resolver.LinkInfo{Availability: core.AvailOffline}
		case a.err == errPassword:
			out[i] = resolver.LinkInfo{Availability: core.AvailOnline}
		case a.err != nil:
		case found:
			out[i] = resolver.LinkInfo{Availability: core.AvailOnline, Name: path.Base(f.Path), Size: f.Size}
		case l.file != "":
			out[i] = resolver.LinkInfo{Availability: core.AvailOffline}
		default:
			out[i] = resolver.LinkInfo{Availability: core.AvailOnline}
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
	t, _, err := r.Client.transfer(ctx, l, password)
	if err != nil {
		return nil, err
	}
	root := strings.TrimSpace(t.Title)
	if root == "" {
		root = l.id
	}
	out := make([]resolver.Listing, 0, len(t.Files))
	for _, f := range t.Files {
		dir := root
		if d := path.Dir(f.Path); d != "." {
			dir = path.Join(root, d)
		}
		out = append(out, resolver.Listing{URL: link{id: l.id, file: f.ID}.String(), Name: path.Base(f.Path), Size: f.Size, Folder: dir})
	}
	return out, nil
}
