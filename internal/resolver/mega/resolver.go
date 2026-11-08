// Package mega downloads public MEGA file and folder links itself, decrypting
// as the bytes arrive, so a MEGA link needs neither JDownloader nor a debrid
// service. Both stay behind it in the chain for when it cannot help.
package mega

import (
	"context"
	"errors"
	"path"
	"slices"
	"sort"
	"strings"

	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/resolver"
)

// ResolverID is the routing id of the resolver and its backend.
const ResolverID = "mega"

// prio puts MEGA's own client above every debrid service (up to 50) and below
// a MEGA login in JDownloader (jd.ActiveLoginPrio), which downloads without
// the free transfer quota.
const prio = 55

// maxListing caps the files one folder link stages, as the crawler caps a
// page.
const maxListing = 2000

// Resolver claims MEGA links and answers what is behind them.
type Resolver struct {
	Client *Client
}

var _ resolver.Resolver = Resolver{}

func (Resolver) Info() resolver.Info { return resolver.Info{ID: ResolverID, Prio: prio} }

// Match claims every link Parse reads, and also one whose key is cut short,
// so that it fails with that said rather than somewhere down the chain.
func (Resolver) Match(raw string) bool {
	_, err := Parse(raw)
	return !errors.Is(err, errNotMega)
}

// Resolve says what to fetch without asking MEGA. It runs while the app holds
// its lock, so the name and size come from Inspect and List instead, and the
// backend asks for the download address when it starts.
func (Resolver) Resolve(_ context.Context, req resolver.Request) (resolver.Result, error) {
	l, err := Parse(req.URL)
	if err != nil {
		return resolver.Result{}, err
	}
	if l.IsFolder() {
		return resolver.Result{}, errFolder
	}
	// MEGA lets a free download open few connections, and the decryption
	// runs front to back.
	return resolver.Result{DirectURL: l.String(), Name: req.URL, Connections: 1}, nil
}

// Info is what Inspect learned about one link.
type Info = resolver.LinkInfo

// Inspect asks MEGA for the name and size of each file link, in input order.
// A folder link gets no opinion, since List is what opens one.
func (r Resolver) Inspect(ctx context.Context, urls []string) []Info {
	out := make([]Info, len(urls))
	for i, raw := range urls {
		l, err := Parse(raw)
		if err != nil || l.IsFolder() {
			continue
		}
		f, err := r.Client.file(ctx, l, false)
		switch {
		case err == nil:
			out[i] = Info{Availability: core.AvailOnline, Name: f.name, Size: f.size}
		case Gone(err):
			out[i] = Info{Availability: core.AvailOffline}
		}
	}
	return out
}

// Listing is one file a folder link opens into.
type Listing = resolver.Listing

var _ resolver.FolderLister = Resolver{}

// List opens a folder link into the files under it, subfolders included. A
// file link yields nothing, meaning "stage it as itself", and so does a link
// to one file inside a folder, which comes back as that file's own row. A
// MEGA folder carries its key in the link and takes no password.
func (r Resolver) List(ctx context.Context, raw, _ string) ([]Listing, error) {
	l, err := Parse(raw)
	if err != nil || !l.Folder {
		return nil, err
	}
	t, err := r.Client.folder(ctx, l)
	if err != nil {
		return nil, err
	}
	top := t.root
	if l.Node != "" {
		top = l.Node
	}
	start := t.nodes[top]
	if start == nil {
		return nil, errNotFound
	}
	if !start.folder {
		in := ""
		if p := t.nodes[start.parent]; p != nil {
			in = p.name
		}
		return []Listing{{URL: l.fileIn(top).String(), Name: start.name, Size: start.size, Folder: in}}, nil
	}

	var out []Listing
	for h, n := range t.nodes {
		if n.folder {
			continue
		}
		dir, ok := t.pathTo(n.parent, top)
		if !ok {
			continue
		}
		out = append(out, Listing{URL: l.fileIn(h).String(), Name: n.name, Size: n.size, Folder: path.Join(start.name, dir)})
	}
	// MEGA lists in no particular order, and the collector should not
	// reshuffle on every paste.
	sort.Slice(out, func(i, j int) bool {
		if out[i].Folder != out[j].Folder {
			return out[i].Folder < out[j].Folder
		}
		return out[i].Name < out[j].Name
	})
	if len(out) > maxListing {
		out = out[:maxListing]
	}
	return out, nil
}

// pathTo is the folder path from top down to the folder dir, "" for top
// itself. It reports false when dir is not under top. The walk is bounded, as
// the parents come from the server and could go round in a circle.
func (t *tree) pathTo(dir, top string) (string, bool) {
	var names []string
	for range 64 {
		if dir == top {
			slices.Reverse(names)
			return strings.Join(names, "/"), true
		}
		n := t.nodes[dir]
		if n == nil {
			return "", false
		}
		names = append(names, n.name)
		dir = n.parent
	}
	return "", false
}
