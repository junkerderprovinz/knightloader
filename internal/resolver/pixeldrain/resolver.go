// Package pixeldrain downloads Pixeldrain files and lists through Pixeldrain's
// public API, so they need no JDownloader. A stored Pixeldrain login lends
// its API key, which lifts the free limits for a paid account. A debrid
// service that carries Pixeldrain and a Pixeldrain login in JDownloader stay
// ahead of it.
//
// The link forms and the answers to each refusal follow the PixeldrainCom
// plugins of JDownloader (GPLv3) and Pixeldrain's API documentation.
package pixeldrain

import (
	"context"
	"errors"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/hostalias"
	"github.com/junkerderprovinz/knightloader/internal/hoster"
	"github.com/junkerderprovinz/knightloader/internal/resolver"
)

// ResolverID is the routing id of the resolver and its backend.
const ResolverID = "pixeldrain"

// prio puts the client without a key below every debrid service (43 and up),
// which gets past the free daily limit, and above JDownloader's own free mode
// (41). With a key it ranks at the login's row, see Resolver.LoginHost.
const prio = 42

// site is the domain every link is written out with.
const site = "pixeldrain.com"

// infoBatch is how many files one info request may name.
const infoBatch = 100

var (
	idPattern   = regexp.MustCompile(`^[A-Za-z0-9]+$`)
	itemPattern = regexp.MustCompile(`^item=(\d+)$`)
)

// link is one parsed Pixeldrain link: a file, or a list with the item it
// points at, -1 for the whole list.
type link struct {
	id   string
	list bool
	item int
}

func parse(raw string) (link, bool) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || hostalias.Canonical(u.Hostname()) != site {
		return link{}, false
	}
	var l link
	switch parts := strings.Split(strings.Trim(u.Path, "/"), "/"); {
	case len(parts) == 2 && parts[0] == "u":
		l = link{id: parts[1]}
	case len(parts) == 3 && parts[0] == "api" && parts[1] == "file":
		l = link{id: parts[2]}
	case len(parts) == 2 && parts[0] == "l":
		l = link{id: parts[1], list: true, item: -1}
		if m := itemPattern.FindStringSubmatch(u.Fragment); m != nil {
			l.item, _ = strconv.Atoi(m[1])
		}
	default:
		return link{}, false
	}
	return l, idPattern.MatchString(l.id)
}

func (l link) String() string {
	if !l.list {
		return "https://" + site + "/u/" + l.id
	}
	s := "https://" + site + "/l/" + l.id
	if l.item >= 0 {
		s += "#item=" + strconv.Itoa(l.item)
	}
	return s
}

// Normalize writes a Pixeldrain file or list link of any domain as
// https://pixeldrain.com/u/ID or https://pixeldrain.com/l/ID. It reports
// false for anything else.
func Normalize(raw string) (string, bool) {
	l, ok := parse(raw)
	if !ok {
		return "", false
	}
	return l.String(), true
}

// Resolver claims Pixeldrain links and answers what is behind them.
type Resolver struct {
	Client *Client
}

var (
	_ resolver.Resolver     = Resolver{}
	_ resolver.Inspector    = Resolver{}
	_ resolver.FolderLister = Resolver{}
	_ resolver.LoginHoster  = Resolver{}
)

func (Resolver) Info() resolver.Info { return resolver.Info{ID: ResolverID, Prio: prio} }

// LoginHost is the host of the stored Pixeldrain login while there is a key
// to send, "" without one. Key reads only the stored logins, so the app may
// ask while holding its lock.
func (r Resolver) LoginHost() string {
	if r.Client.key() == "" {
		return ""
	}
	return site
}

func (Resolver) Match(raw string) bool {
	_, ok := parse(raw)
	return ok
}

// Resolve says what to fetch without asking Pixeldrain. It runs while the app
// holds its lock, so the name and size come from Inspect, and the backend
// asks for the file when the download starts. The connections are capped
// there, once it is known whether a paid account goes along.
func (Resolver) Resolve(_ context.Context, req resolver.Request) (resolver.Result, error) {
	l, ok := parse(req.URL)
	if !ok || l.list {
		return resolver.Result{}, errors.New("pixeldrain: a list link opens into its files when it is added, and cannot be downloaded as one")
	}
	return resolver.Result{DirectURL: l.String(), Name: req.URL}, nil
}

// Inspect asks for the name and size of every file link, a hundred to a
// request, in input order. A list link gets no opinion, since List is what
// opens one, and so does a batch that could not be asked.
func (r Resolver) Inspect(ctx context.Context, urls []string) []resolver.LinkInfo {
	out := make([]resolver.LinkInfo, len(urls))
	var ids []string
	var at []int
	flush := func() {
		files, err := r.Client.infos(ctx, ids)
		for j, id := range ids {
			switch f, ok := files[id]; {
			case err != nil:
			case !ok || f.AbuseType != "":
				out[at[j]] = resolver.LinkInfo{Availability: core.AvailOffline}
			default:
				out[at[j]] = resolver.LinkInfo{Availability: core.AvailOnline, Name: f.Name, Size: f.Size}
			}
		}
		ids, at = ids[:0], at[:0]
	}
	for i, raw := range urls {
		l, ok := parse(raw)
		if !ok || l.list {
			continue
		}
		ids, at = append(ids, l.id), append(at, i)
		if len(ids) == infoBatch {
			flush()
		}
	}
	if len(ids) > 0 {
		flush()
	}
	return out
}

// List opens a list link into its files, under the list's title. A link to
// one item of a list comes back as that file alone, and a file link yields
// nothing.
func (r Resolver) List(ctx context.Context, raw, _ string) ([]resolver.Listing, error) {
	l, ok := parse(raw)
	if !ok || !l.list {
		return nil, nil
	}
	list, err := r.Client.list(ctx, l.id)
	if err != nil {
		return nil, err
	}
	var out []resolver.Listing
	for i, f := range list.Files {
		if l.item >= 0 && i != l.item {
			continue
		}
		out = append(out, resolver.Listing{URL: link{id: f.ID}.String(), Name: f.Name, Size: f.Size, Folder: list.Title})
	}
	if len(out) == 0 && l.item >= 0 {
		return nil, hoster.Gone("pixeldrain")
	}
	return out, nil
}
