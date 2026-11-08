// Package mediafire downloads Mediafire files and folders itself, so they
// need no JDownloader. The name, size and state of a file come from
// Mediafire's public API, a folder opens through the same API, and the
// download link is the button on the file's page. A debrid service that
// carries Mediafire and a Mediafire login in JDownloader stay ahead of it.
//
// The link forms, the page patterns and the error numbers follow the
// MediafireCom plugins of JDownloader (GPLv3).
package mediafire

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
	"github.com/junkerderprovinz/knightloader/internal/resolver"
)

// ResolverID is the routing id of the resolver and its backend.
const ResolverID = "mediafire"

// prio puts the client below every debrid service (43 and up) and a
// Mediafire login in JDownloader (jd.ActiveLoginPrio), and above
// JDownloader's own free mode (41).
const prio = 42

// site is the domain every link is written out with.
const site = "www.mediafire.com"

// infoBatch is how many files one info request names, as JDownloader asks.
const infoBatch = 100

// A folder is walked no deeper than this, and one link stages no more files
// than a MEGA folder does.
const (
	maxDepth   = 16
	maxListing = 2000
)

var (
	keyPattern = regexp.MustCompile(`^[a-z0-9]+$`)
	// A folder key has thirteen characters, which tells a bare /?KEY link
	// to a folder from one to a file.
	folderKeyPattern = regexp.MustCompile(`^[a-z0-9]{13}$`)
)

// link is one parsed Mediafire link to a file or a folder.
type link struct {
	key    string
	folder bool
}

func parse(raw string) (link, bool) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return link{}, false
	}
	host := strings.TrimPrefix(strings.ToLower(u.Hostname()), "app.")
	if hostalias.Canonical(host) != "mediafire.com" {
		return link{}, false
	}
	var l link
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	switch {
	case len(parts) >= 2 && parts[0] == "folder":
		l = link{key: parts[1], folder: true}
	case len(parts) >= 2 && (parts[0] == "file" || parts[0] == "file_premium" || parts[0] == "view" || parts[0] == "download"):
		l = link{key: parts[1]}
	case parts[0] == "" && u.RawQuery != "":
		key, _, _ := strings.Cut(u.RawQuery, "&")
		l = link{key: key, folder: folderKeyPattern.MatchString(key)}
	default:
		return link{}, false
	}
	return l, keyPattern.MatchString(l.key)
}

func (l link) String() string {
	if l.folder {
		return "https://" + site + "/folder/" + l.key
	}
	return "https://" + site + "/file/" + l.key
}

// Normalize writes a Mediafire file or folder link of any domain and form as
// https://www.mediafire.com/file/KEY or https://www.mediafire.com/folder/KEY.
// It reports false for anything else.
func Normalize(raw string) (string, bool) {
	l, ok := parse(raw)
	if !ok {
		return "", false
	}
	return l.String(), true
}

// Resolver claims Mediafire links and answers what is behind them.
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

// Resolve says what to fetch without asking Mediafire. It runs while the app
// holds its lock, so the name and size come from Inspect, and the backend
// reads the file's page when the download starts.
func (Resolver) Resolve(_ context.Context, req resolver.Request) (resolver.Result, error) {
	l, ok := parse(req.URL)
	if !ok || l.folder {
		return resolver.Result{}, errors.New("mediafire: a folder link opens into its files when it is added, and cannot be downloaded as one")
	}
	return resolver.Result{DirectURL: l.String(), Name: req.URL, Connections: maxConns}, nil
}

// Inspect asks for the name and size of every file link, a hundred to a
// request, in input order. A folder link gets no opinion, since List is what
// opens one, and so does a batch that could not be asked.
func (r Resolver) Inspect(ctx context.Context, urls []string) []resolver.LinkInfo {
	out := make([]resolver.LinkInfo, len(urls))
	var keys []string
	var at []int
	flush := func() {
		files, err := r.Client.infos(ctx, keys)
		for j, key := range keys {
			switch f, ok := files[key]; {
			case err != nil:
			case !ok || f.DeleteDate != "":
				out[at[j]] = resolver.LinkInfo{Availability: core.AvailOffline}
			default:
				out[at[j]] = resolver.LinkInfo{Availability: core.AvailOnline, Name: f.Name, Size: f.size()}
			}
		}
		keys, at = keys[:0], at[:0]
	}
	for i, raw := range urls {
		l, ok := parse(raw)
		if !ok || l.folder {
			continue
		}
		keys, at = append(keys, l.key), append(at, i)
		if len(keys) == infoBatch {
			flush()
		}
	}
	if len(keys) > 0 {
		flush()
	}
	return out
}

// List opens a folder link into every file under it, subfolders included.
// A file link yields nothing.
func (r Resolver) List(ctx context.Context, raw, _ string) ([]resolver.Listing, error) {
	l, ok := parse(raw)
	if !ok || !l.folder {
		return nil, nil
	}
	name, err := r.Client.folderName(ctx, l.key)
	if err != nil {
		return nil, err
	}
	var out []resolver.Listing
	if err := r.walk(ctx, l.key, name, 0, &out); err != nil {
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

func (r Resolver) walk(ctx context.Context, key, dir string, depth int, out *[]resolver.Listing) error {
	files, _, err := r.Client.content(ctx, key, "files")
	if err != nil {
		return err
	}
	for _, f := range files {
		if len(*out) >= maxListing {
			return nil
		}
		if f.DeleteDate != "" {
			continue
		}
		u := f.Links.NormalDownload
		if _, ok := parse(u); !ok {
			u = link{key: f.QuickKey}.String()
		}
		*out = append(*out, resolver.Listing{URL: u, Name: f.Name, Size: f.size(), Folder: dir})
	}
	if depth+1 >= maxDepth {
		return nil
	}
	_, folders, err := r.Client.content(ctx, key, "folders")
	if err != nil {
		return err
	}
	for _, sub := range folders {
		if err := r.walk(ctx, sub.FolderKey, path.Join(dir, sub.Name), depth+1, out); err != nil {
			return err
		}
	}
	return nil
}
