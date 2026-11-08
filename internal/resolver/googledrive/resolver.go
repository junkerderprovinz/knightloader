// Package googledrive downloads files, folders and documents shared from
// Google Drive itself, so they need no JDownloader. A file comes from Drive's
// download server, past the warning Drive shows for a file too large to scan
// for viruses, and a Google document as its Office export. A folder opens
// through the files API Drive's own folder page calls, with the key that page
// carries, so no API key of the user's is needed. A debrid service that
// carries Google Drive and a Google login in JDownloader stay ahead of it.
//
// The link forms, the quota page and the folder listing follow the
// GoogleDrive plugins of JDownloader (GPLv3) and gdown.
package googledrive

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
const ResolverID = "googledrive"

// prio puts the client below every debrid service (43 and up) and a Google
// login in JDownloader (jd.ActiveLoginPrio), and above JDownloader's own free
// mode (41), as every free client of KnightLoader's own.
const prio = 42

// site is the domain every link is known by.
const site = "drive.google.com"

// What a link points at. A Google document is named by the path its editor
// uses, which is also where it is exported from.
const (
	kindFile         = "file"
	kindFolder       = "folder"
	kindDocument     = "document"
	kindSpreadsheet  = "spreadsheets"
	kindPresentation = "presentation"
)

// exports maps each kind of Google document to the Office format it is
// downloaded as, and the MIME type Drive lists it under to its kind.
var (
	exports = map[string]string{kindDocument: "docx", kindSpreadsheet: "xlsx", kindPresentation: "pptx"}
	docMime = map[string]string{
		"application/vnd.google-apps.document":     kindDocument,
		"application/vnd.google-apps.spreadsheet":  kindSpreadsheet,
		"application/vnd.google-apps.presentation": kindPresentation,
	}
)

const folderMime = "application/vnd.google-apps.folder"

var idPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{10,}$`)

// link is one parsed Drive link. key is the resource key an older shared
// link has to name.
type link struct {
	id, key, kind string
}

func parse(raw string) (link, bool) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || hostalias.Canonical(u.Hostname()) != site {
		return link{}, false
	}
	q := u.Query()
	l := link{id: q.Get("id"), key: q.Get("resourcekey"), kind: kindFile}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	switch {
	case len(parts) >= 3 && parts[0] == "file" && parts[1] == "d":
		l.id = parts[2]
	case len(parts) >= 3 && parts[1] == "d" && exports[parts[0]] != "":
		l.id, l.kind = parts[2], parts[0]
	case len(parts) >= 3 && parts[0] == "drive" && parts[len(parts)-2] == "folders":
		l.id, l.kind = parts[len(parts)-1], kindFolder
	case len(parts) == 1 && (parts[0] == "folderview" || parts[0] == "embeddedfolderview"):
		l.kind = kindFolder
	case len(parts) == 1 && (parts[0] == "uc" || parts[0] == "open" || parts[0] == "download"):
	default:
		return link{}, false
	}
	return l, idPattern.MatchString(l.id)
}

func (l link) String() string {
	var s string
	switch l.kind {
	case kindFile:
		s = "https://" + site + "/file/d/" + l.id
	case kindFolder:
		s = "https://" + site + "/drive/folders/" + l.id
	default:
		s = "https://docs.google.com/" + l.kind + "/d/" + l.id
	}
	if l.key != "" {
		s += "?resourcekey=" + url.QueryEscape(l.key)
	}
	return s
}

// Normalize writes a Drive link of any form as the link Drive shares the
// file, folder or document under. It reports false for anything else.
func Normalize(raw string) (string, bool) {
	l, ok := parse(raw)
	if !ok {
		return "", false
	}
	return l.String(), true
}

// Resolver claims Drive links and answers what is behind them.
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

// Resolve says what to fetch without asking Drive. It runs while the app
// holds its lock, so the name and size come from Inspect, and the backend
// asks Drive when the download starts.
func (Resolver) Resolve(_ context.Context, req resolver.Request) (resolver.Result, error) {
	l, ok := parse(req.URL)
	if !ok || l.kind == kindFolder {
		return resolver.Result{}, errors.New("googledrive: a folder link opens into its files when it is added, and cannot be downloaded as one")
	}
	conns := maxConns
	if l.kind != kindFile {
		conns = 1
	}
	return resolver.Result{DirectURL: l.String(), Name: req.URL, Connections: conns}, nil
}

// Inspect asks Drive's download server for the name and size of every file
// and document link, in input order. A file whose quota is used up is there
// but goes unnamed, and a folder link gets no opinion, since List opens it.
func (r Resolver) Inspect(ctx context.Context, urls []string) []resolver.LinkInfo {
	out := make([]resolver.LinkInfo, len(urls))
	for i, raw := range urls {
		l, ok := parse(raw)
		if !ok || l.kind == kindFolder {
			continue
		}
		got, err := r.Client.peek(ctx, l)
		switch {
		case err == nil:
			out[i] = resolver.LinkInfo{Availability: core.AvailOnline, Name: got.Name, Size: got.Size}
		case err == errQuota:
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

// List opens a folder link into every file and document under it,
// subfolders included. A file link yields nothing. Google's forms, maps and
// sites have no file to download and are left out.
func (r Resolver) List(ctx context.Context, raw, _ string) ([]resolver.Listing, error) {
	l, ok := parse(raw)
	if !ok || l.kind != kindFolder {
		return nil, nil
	}
	name, key, err := r.Client.folderPage(ctx, l)
	if err != nil {
		return nil, err
	}
	var out []resolver.Listing
	seen := map[string]bool{}
	if err := r.walk(ctx, key, l, name, 0, seen, &out); err != nil {
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

func (r Resolver) walk(ctx context.Context, key string, folder link, dir string, depth int, seen map[string]bool, out *[]resolver.Listing) error {
	items, err := r.Client.children(ctx, key, folder)
	if err != nil {
		return err
	}
	var subs []item
	for _, it := range items {
		id, mime := it.ID, it.Mime
		// A shortcut is listed in place of what it points at.
		if it.Shortcut.Target != "" {
			id, mime = it.Shortcut.Target, it.Shortcut.Mime
		}
		if seen[id] || len(*out) >= maxListing {
			continue
		}
		seen[id] = true
		switch kind := docMime[mime]; {
		case mime == folderMime:
			subs = append(subs, item{ID: id, Title: it.Title, Key: it.Key})
		case kind != "":
			*out = append(*out, resolver.Listing{URL: link{id: id, kind: kind}.String(), Name: it.Title + "." + exports[kind], Folder: dir})
		case !strings.HasPrefix(mime, "application/vnd.google-apps."):
			*out = append(*out, resolver.Listing{URL: link{id: id, key: it.Key, kind: kindFile}.String(), Name: it.Title, Size: it.size(), Folder: dir})
		}
	}
	if depth+1 >= maxDepth {
		return nil
	}
	for _, sub := range subs {
		if err := r.walk(ctx, key, link{id: sub.ID, key: sub.Key, kind: kindFolder}, path.Join(dir, sub.Title), depth+1, seen, out); err != nil {
			return err
		}
	}
	return nil
}
