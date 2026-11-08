// Package gofile downloads Gofile files and folders itself, so they need no
// JDownloader. It reads a folder the way Gofile's website does, with a guest
// account and the website's own signing script, or with the account token of
// a stored Gofile login. A debrid service that carries Gofile and a Gofile
// login in JDownloader stay ahead of it.
//
// The guest account, the password hash and the cookie the download servers
// want follow the GofileIo plugins of JDownloader (GPLv3) and Gofile's own
// website code.
package gofile

import (
	"context"
	"errors"
	"net/url"
	"path"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/hostalias"
	"github.com/junkerderprovinz/knightloader/internal/hoster"
	"github.com/junkerderprovinz/knightloader/internal/resolver"
)

// ResolverID is the routing id of the resolver and its backend.
const ResolverID = "gofile"

// prio puts the client below every debrid service (43 and up) and a Gofile
// login in JDownloader (jd.ActiveLoginPrio), and above JDownloader's own
// free mode (41).
const prio = 42

// site is the domain every link is written out with.
const site = "gofile.io"

// A folder is walked no deeper than Gofile itself nests a listing, and one
// link stages no more files than a MEGA folder does.
const (
	maxDepth   = 16
	maxListing = 2000
)

// A content id is a share code or a UUID.
var idPattern = regexp.MustCompile(`^[A-Za-z0-9-]+$`)

// parse reads the content id out of a link, https://gofile.io/d/ID or the
// older https://gofile.io/?c=ID.
func parse(raw string) (string, bool) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || hostalias.Canonical(u.Hostname()) != site {
		return "", false
	}
	id := u.Query().Get("c")
	if p := strings.Trim(u.Path, "/"); p != "" {
		d, rest, _ := strings.Cut(p, "/")
		if d != "d" || strings.Contains(rest, "/") {
			return "", false
		}
		id = rest
	}
	return id, idPattern.MatchString(id)
}

// Normalize writes a Gofile link of any domain and either form as
// https://gofile.io/d/ID. It reports false for anything else.
func Normalize(raw string) (string, bool) {
	id, ok := parse(raw)
	if !ok {
		return "", false
	}
	return "https://" + site + "/d/" + id, true
}

// Resolver claims Gofile links and answers what is behind them.
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

// Resolve says what to fetch without asking Gofile. It runs while the app
// holds its lock, so the name and size come from Inspect, and the backend
// asks for the download link when the download starts.
func (Resolver) Resolve(_ context.Context, req resolver.Request) (resolver.Result, error) {
	link, ok := Normalize(req.URL)
	if !ok {
		return resolver.Result{}, errors.New("gofile: not a Gofile link")
	}
	return resolver.Result{DirectURL: link, Name: req.URL, Connections: maxConns}, nil
}

// inspectPause spaces the requests of one Inspect, as JDownloader spaces its
// own, since Gofile turns bursts away. Tests shorten it.
var inspectPause = 350 * time.Millisecond

// Inspect asks for each link's content, one after the other, for the name
// and size of a file and whether it is still there. A folder gets no
// opinion, since List is what opens one, and so does a link that could not
// be asked about.
func (r Resolver) Inspect(ctx context.Context, urls []string) []resolver.LinkInfo {
	out := make([]resolver.LinkInfo, len(urls))
	asked := false
	for i, raw := range urls {
		id, ok := parse(raw)
		if !ok {
			continue
		}
		if asked && r.Client.sleep(ctx, inspectPause) != nil {
			return out
		}
		asked = true
		ct, _, err := r.Client.contents(ctx, id, "", 1)
		switch {
		case hoster.IsGone(err):
			out[i] = resolver.LinkInfo{Availability: core.AvailOffline}
		case err == nil && ct.Type == "file":
			out[i] = resolver.LinkInfo{Availability: core.AvailOnline, Name: ct.Name, Size: ct.Size}
		}
	}
	return out
}

// List opens a folder link into every file under it, subfolders included,
// each row a link of its own to the file. A file link yields nothing.
// password is the folder's download password.
func (r Resolver) List(ctx context.Context, raw, password string) ([]resolver.Listing, error) {
	id, ok := parse(raw)
	if !ok {
		return nil, nil
	}
	root, err := r.Client.open(ctx, id, password)
	if err != nil || root.Type != "folder" {
		return nil, err
	}
	var out []resolver.Listing
	if err := r.walk(ctx, root, root.Name, password, 0, &out); err != nil {
		return nil, err
	}
	// Gofile hands children out as a map, and the collector should not
	// reshuffle on every paste.
	sort.Slice(out, func(i, j int) bool {
		if out[i].Folder != out[j].Folder {
			return out[i].Folder < out[j].Folder
		}
		return out[i].Name < out[j].Name
	})
	return out, nil
}

func (r Resolver) walk(ctx context.Context, folder content, dir, password string, depth int, out *[]resolver.Listing) error {
	for _, ch := range folder.Children {
		if len(*out) >= maxListing {
			return nil
		}
		switch ch.Type {
		case "file":
			*out = append(*out, resolver.Listing{URL: "https://" + site + "/d/" + ch.ID, Name: ch.Name, Size: ch.Size, Folder: dir})
		case "folder":
			if depth+1 >= maxDepth {
				continue
			}
			sub, err := r.Client.open(ctx, ch.ID, password)
			if err != nil {
				return err
			}
			if err := r.walk(ctx, sub, path.Join(dir, ch.Name), password, depth+1, out); err != nil {
				return err
			}
		}
	}
	return nil
}
