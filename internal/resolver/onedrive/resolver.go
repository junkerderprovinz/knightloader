// Package onedrive downloads files and folders shared from OneDrive and
// SharePoint itself, so they need no JDownloader. A share link is followed
// the way a browser follows it, which opens a guest session on the site that
// holds the item and says where the item sits there. A file then comes with
// that session's cookie, and a folder opens through SharePoint's REST API,
// which answers the guest too. Personal OneDrive shares live on SharePoint as
// well, so 1drv.ms, onedrive.live.com and every tenant's sharepoint.com take
// the same way. A debrid service that carries OneDrive stays ahead of it.
//
// The link forms follow the OneDriveLiveCom plugins of JDownloader (GPLv3).
// Their API calls want a login, so the redirects and the REST calls follow
// what the live services answer a guest.
package onedrive

import (
	"context"
	"errors"
	"net/url"
	"path"
	"regexp"
	"sort"
	"strings"

	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/hostalias"
	"github.com/junkerderprovinz/knightloader/internal/hoster"
	"github.com/junkerderprovinz/knightloader/internal/resolver"
)

// ResolverID is the routing id of the resolver and its backend.
const ResolverID = "onedrive"

// prio puts the client below every debrid service (43 and up) and above
// JDownloader's own free mode (41), as every free client of KnightLoader's
// own.
const prio = 42

// A share link names the kind of item in its path: /f/ on 1drv.ms or /:f:/
// on the sites for a folder, another letter for a kind of file.
var (
	shortPattern = regexp.MustCompile(`^/([a-z])/.+`)
	sitePattern  = regexp.MustCompile(`^/:([a-z]):/.+`)
)

// link is one parsed share link. rel is the path of a file inside a shared
// folder, from the folder down, which a listing writes into the fragment.
type link struct {
	share  string
	folder bool
	rel    string
}

func parse(raw string) (link, bool) {
	raw = strings.TrimSpace(raw)
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return link{}, false
	}
	share, _, _ := strings.Cut(raw, "#")
	l := link{share: share, rel: strings.TrimPrefix(u.Fragment, "path=")}
	host := hostalias.Canonical(u.Hostname())
	var m []string
	switch {
	case strings.EqualFold(u.Hostname(), "1drv.ms"):
		m = shortPattern.FindStringSubmatch(u.Path)
	case host == "onedrive.live.com" && strings.HasPrefix(u.Path, "/:"):
		m = sitePattern.FindStringSubmatch(u.Path)
	case host == "onedrive.live.com":
		// The older links name the item by its resid and say a folder is one.
		q := u.Query()
		if q.Get("resid") == "" && q.Get("redeem") == "" {
			return link{}, false
		}
		l.folder = q.Get("ithint") == "folder"
		return l, true
	case strings.HasSuffix(host, ".sharepoint.com"):
		m = sitePattern.FindStringSubmatch(u.Path)
	}
	if m == nil {
		return link{}, false
	}
	l.folder = m[1] == "f"
	return l, true
}

// row writes the link of a file inside a shared folder.
func row(share, rel string) string {
	parts := strings.Split(rel, "/")
	for i, p := range parts {
		parts[i] = url.PathEscape(p)
	}
	return share + "#path=" + strings.Join(parts, "/")
}

// Resolver claims OneDrive and SharePoint share links and answers what is
// behind them.
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

// Resolve says what to fetch without asking the site. It runs while the app
// holds its lock, so the name and size come from Inspect, and the backend
// opens the share when the download starts.
func (Resolver) Resolve(_ context.Context, req resolver.Request) (resolver.Result, error) {
	l, ok := parse(req.URL)
	if !ok || (l.folder && l.rel == "") {
		return resolver.Result{}, errors.New("onedrive: a folder link opens into its files when it is added, and cannot be downloaded as one")
	}
	return resolver.Result{DirectURL: strings.TrimSpace(req.URL), Name: req.URL}, nil
}

// Inspect asks for the first byte of every shared file, in input order, and
// reads its name and size from the answer. A folder link gets no opinion,
// since List opens it.
func (r Resolver) Inspect(ctx context.Context, urls []string) []resolver.LinkInfo {
	out := make([]resolver.LinkInfo, len(urls))
	for i, raw := range urls {
		l, ok := parse(raw)
		if !ok || (l.folder && l.rel == "") {
			continue
		}
		got, err := r.Client.unlock(ctx, l)
		switch {
		case err == nil:
			out[i] = resolver.LinkInfo{Availability: core.AvailOnline, Name: got.Name, Size: got.Size}
		case hoster.IsGone(err):
			out[i] = resolver.LinkInfo{Availability: core.AvailOffline}
		}
	}
	return out
}

// A folder is walked no deeper than this, and one link stages no more files
// than a MEGA folder does.
const (
	maxDepth   = 16
	maxListing = 2000
)

// List opens a folder share into every file under it, subfolders included.
// A file share yields nothing.
func (r Resolver) List(ctx context.Context, raw, _ string) ([]resolver.Listing, error) {
	l, ok := parse(raw)
	if !ok || !l.folder || l.rel != "" {
		return nil, nil
	}
	g, err := r.Client.redeem(ctx, l)
	if err != nil {
		return nil, err
	}
	var out []resolver.Listing
	if err := r.walk(ctx, g, l.share, g.path, path.Base(g.path), 0, &out); err != nil {
		return nil, err
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Folder != out[j].Folder {
			return out[i].Folder < out[j].Folder
		}
		return out[i].Name < out[j].Name
	})
	return out, nil
}

func (r Resolver) walk(ctx context.Context, g *guest, share, dir, name string, depth int, out *[]resolver.Listing) error {
	f, err := r.Client.folder(ctx, g, dir)
	if err != nil {
		return err
	}
	for _, file := range f.Files {
		if len(*out) >= maxListing {
			return nil
		}
		rel := strings.TrimPrefix(file.Path, g.path+"/")
		*out = append(*out, resolver.Listing{URL: row(share, rel), Name: file.Name, Size: file.size(), Folder: name})
	}
	if depth+1 >= maxDepth {
		return nil
	}
	for _, sub := range f.Folders {
		if err := r.walk(ctx, g, share, sub.Path, path.Join(name, sub.Name), depth+1, out); err != nil {
			return err
		}
	}
	return nil
}
