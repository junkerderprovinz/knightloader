package app

// Request headers a browser hands over with a download it gave up to this
// instance: its cookies for the link's site, the Referer and the User-Agent,
// so a link behind a login downloads here as it would have there.
//
// They belong to one task and live in memory only. The task record, the store
// and the API never carry them, hostheaders.Set prints header names alone, so
// no log line holds a value, and they are dropped when the download finishes
// or the task is removed. A restart loses them, and the download then runs
// without.

import (
	"fmt"

	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/resolver"
	"github.com/junkerderprovinz/knightloader/internal/resolver/hostheaders"
)

// browserHeaderNames are the headers a browser may hand over. Anything else,
// Authorization or Host for a start, is refused rather than dropped.
var browserHeaderNames = map[string]bool{"Cookie": true, "Referer": true, "User-Agent": true}

// BrowserHeaders checks the headers handed over with link and scopes them to
// its origin, so they never reach another host. Errors name a header, never
// its value.
func BrowserHeaders(link string, headers map[string]string) (hostheaders.Set, error) {
	set := hostheaders.Set{Origin: link}
	for name, value := range headers {
		set.Headers = append(set.Headers, hostheaders.Header{Name: name, Value: value})
	}
	set, err := hostheaders.Normalize(set)
	if err != nil {
		return hostheaders.Set{}, err
	}
	for _, h := range set.Headers {
		if !browserHeaderNames[h.Name] {
			return hostheaders.Set{}, fmt.Errorf("the header %s is not accepted with a link", h.Name)
		}
	}
	return set, nil
}

// keepBrowserHeaders gives set to every task in ids whose link is on its
// origin, which covers the variant rows of a yt-dlp link.
func (a *App) keepBrowserHeaders(set hostheaders.Set, ids []string) {
	if len(set.Headers) == 0 {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.browserHeaders == nil {
		a.browserHeaders = map[string]hostheaders.Set{}
	}
	for _, id := range ids {
		if t := a.tasks[id]; t != nil && hostheaders.OriginOf(t.URL) == set.Origin {
			a.browserHeaders[id] = set
		}
	}
}

// browserHeadersFor returns the headers handed over with task id that may go
// to rawurl, or nil.
func (a *App) browserHeadersFor(id, rawurl string) map[string]string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.browserHeaders[id].Attach(rawurl)
}

// resolveLocked asks res for t's download target. A plain http link that came
// with a browser's headers goes through the preflight a stored header profile
// gets instead, which follows the redirects itself and keeps the headers on
// the link's origin. Caller holds a.mu.
func (a *App) resolveLocked(res resolver.Resolver, t *core.Task) (resolver.Result, error) {
	if set, ok := a.browserHeaders[t.ID]; ok && (res.Info().ID == "direct" || res.Info().ID == "http") {
		res = hostheaders.Resolver{Profiles: onlyProfile{set}}
	}
	return res.Resolve(a.ctx, resolver.Request{URL: t.URL})
}

// onlyProfile serves one header set to hostheaders.Resolver as if it were the
// only stored profile.
type onlyProfile struct{ set hostheaders.Set }

func (p onlyProfile) Covers(rawurl string) bool               { return hostheaders.OriginOf(rawurl) == p.set.Origin }
func (p onlyProfile) ForURL(string) (string, hostheaders.Set) { return "", p.set }
func (p onlyProfile) Get(string) hostheaders.Set              { return p.set }
