// Package dropbox downloads files and folders shared from Dropbox itself, so
// they need no JDownloader. A file comes from where its link leads with
// dl=1, and a folder opens through the listing Dropbox's own folder page asks
// for, one row per file. A folder link that is downloaded as it is comes as
// the zip Dropbox makes of it. A debrid service that carries Dropbox and a
// Dropbox login in JDownloader stay ahead of it.
//
// The link forms, the password prompt and the folder listing follow the
// DropboxCom plugins of JDownloader (GPLv3).
package dropbox

import (
	"context"
	"net/http"
	"net/url"
	"path"
	"sort"
	"strings"

	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/hostalias"
	"github.com/junkerderprovinz/knightloader/internal/hoster"
	"github.com/junkerderprovinz/knightloader/internal/resolver"
)

// ResolverID is the routing id of the resolver and its backend.
const ResolverID = "dropbox"

// prio puts the client below every debrid service (43 and up) and a Dropbox
// login in JDownloader (jd.ActiveLoginPrio), and above JDownloader's own free
// mode (41), as every free client of KnightLoader's own.
const prio = 42

// link is one parsed shared link. A folder link names the folder by its link
// key and secure hash, and sub is the path of a subfolder or of a file inside
// it, "" for the shared folder itself.
type link struct {
	path   string
	query  url.Values
	folder bool
	// kind is the website's name for the form of a folder link: "c" for
	// /scl/fo/ and "s" for the older /sh/.
	kind, key, hash, sub string
}

func parse(raw string) (link, bool) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return link{}, false
	}
	// dl.dropbox.com is the older host of the same links with dl=1.
	if host := hostalias.Canonical(u.Hostname()); host != "dropbox.com" && host != "dl.dropbox.com" {
		return link{}, false
	}
	q := u.Query()
	q.Del("dl")
	q.Del("raw")
	l := link{path: u.EscapedPath(), query: q}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	switch {
	case len(parts) >= 3 && parts[0] == "scl" && parts[1] == "fi":
	case len(parts) >= 2 && parts[0] == "s":
	case len(parts) >= 4 && parts[0] == "scl" && parts[1] == "fo":
		l.folder, l.kind, l.key, l.hash = true, "c", parts[2], parts[3]
		parts = parts[4:]
	case len(parts) >= 3 && parts[0] == "sh":
		l.folder, l.kind, l.key, l.hash = true, "s", parts[1], parts[2]
		parts = parts[3:]
	default:
		return link{}, false
	}
	if l.folder && len(parts) > 0 {
		l.sub = "/" + strings.Join(parts, "/")
	}
	return l, true
}

func (l link) String() string {
	s := "https://www.dropbox.com" + l.path
	if len(l.query) > 0 {
		s += "?" + l.query.Encode()
	}
	return s
}

// Normalize writes a Dropbox link without the dl and raw switches, which
// only decide whether the website shows a page or the file. It reports false
// for anything that is not a shared file or folder.
func Normalize(raw string) (string, bool) {
	l, ok := parse(raw)
	if !ok {
		return "", false
	}
	return l.String(), true
}

// Resolver claims Dropbox links and answers what is behind them.
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

// Resolve says what to fetch without asking Dropbox. It runs while the app
// holds its lock, so the name and size come from Inspect, and the backend
// follows the link when the download starts. A folder link downloads as one
// zip.
func (Resolver) Resolve(_ context.Context, req resolver.Request) (resolver.Result, error) {
	l, _ := parse(req.URL)
	return resolver.Result{DirectURL: l.String(), Name: req.URL, Connections: maxConns}, nil
}

// Inspect asks for the first byte of every shared file, in input order, and
// reads its name and size from the answer. A file behind a password is there
// but goes unnamed, and a folder link gets no opinion, since List opens it.
func (r Resolver) Inspect(ctx context.Context, urls []string) []resolver.LinkInfo {
	out := make([]resolver.LinkInfo, len(urls))
	for i, raw := range urls {
		l, ok := parse(raw)
		if !ok || l.folder {
			continue
		}
		got, err := r.Client.unlock(ctx, l, "")
		switch {
		case err == nil:
			out[i] = resolver.LinkInfo{Availability: core.AvailOnline, Name: got.Name, Size: got.Size}
		case err == errPassword:
			out[i] = resolver.LinkInfo{Availability: core.AvailOnline}
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

// List opens a folder link into every file under it, subfolders included.
// A file link yields nothing.
func (r Resolver) List(ctx context.Context, raw, password string) ([]resolver.Listing, error) {
	l, ok := parse(raw)
	if !ok || !l.folder {
		return nil, nil
	}
	hc, err := r.Client.openFolder(ctx, l, password)
	if err != nil {
		return nil, err
	}
	var out []resolver.Listing
	if err := r.walk(ctx, hc, l, "", 0, &out); err != nil {
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

func (r Resolver) walk(ctx context.Context, hc *http.Client, folder link, dir string, depth int, out *[]resolver.Listing) error {
	name, entries, err := r.Client.entries(ctx, hc, folder)
	if err != nil {
		return err
	}
	if dir == "" {
		dir = name
	}
	var subs []entry
	for _, e := range entries {
		if e.Dir {
			subs = append(subs, e)
			continue
		}
		if len(*out) >= maxListing {
			return nil
		}
		u, ok := Normalize(e.Href)
		if !ok {
			continue
		}
		*out = append(*out, resolver.Listing{URL: u, Name: e.Name, Size: e.Bytes, Folder: dir})
	}
	if depth+1 >= maxDepth {
		return nil
	}
	for _, e := range subs {
		sub, ok := parse(e.Href)
		if !ok || !sub.folder {
			continue
		}
		if err := r.walk(ctx, hc, sub, path.Join(dir, e.Name), depth+1, out); err != nil {
			return err
		}
	}
	return nil
}
