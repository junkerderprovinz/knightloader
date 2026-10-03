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
	"net/http"

	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/engine"
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
		// Checked before Normalize, which drops a name that is no token
		// rather than refusing it.
		if !browserHeaderNames[http.CanonicalHeaderKey(name)] {
			return hostheaders.Set{}, fmt.Errorf("the header %q is not accepted with a link", name)
		}
		set.Headers = append(set.Headers, hostheaders.Header{Name: name, Value: value})
	}
	return hostheaders.Normalize(set)
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
	return a.browserHeaderSet(id).Attach(rawurl)
}

// browserHeadersForLink returns the headers handed over with any task for
// rawurl, or nil. The rows of one yt-dlp link share a probe and so share these.
func (a *App) browserHeadersForLink(rawurl string) map[string]string {
	a.mu.Lock()
	defer a.mu.Unlock()
	for id, set := range a.browserHeaders {
		if t := a.tasks[id]; t != nil && t.URL == rawurl {
			return set.Attach(rawurl)
		}
	}
	return nil
}

// browserHeaderSet returns the headers handed over with task id, or a zero
// Set.
func (a *App) browserHeaderSet(id string) hostheaders.Set {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.browserHeaders[id]
}

// resolveLocked asks res for t's download target. A plain http link that came
// with a browser's headers goes through the preflight a stored header profile
// gets instead, which follows the redirects itself and keeps the headers on
// the link's origin.
//
// That preflight, for a stored profile too, is a request to the link's server
// that may run to its timeout, and under a.mu it would hold up every other
// caller. So its resolver comes back as later, for preflight to ask off the
// lock. Caller holds a.mu.
func (a *App) resolveLocked(res resolver.Resolver, t *core.Task) (result resolver.Result, later resolver.Resolver, err error) {
	if set, ok := a.browserHeaders[t.ID]; ok && (res.Info().ID == "direct" || res.Info().ID == "http") {
		res = hostheaders.Resolver{Profiles: onlyProfile{set}}
	}
	if res.Info().ID == hostheaders.ResolverID {
		return resolver.Result{DirectURL: t.URL}, res, nil
	}
	result, err = res.Resolve(a.ctx, resolver.Request{URL: t.URL})
	return result, nil, err
}

// beginPreflightLocked records that the start of task id now waits for its
// preflight and returns the number preflight checks it by. Caller holds a.mu.
func (a *App) beginPreflightLocked(id string) uint64 {
	if a.preflights == nil {
		a.preflights = map[string]uint64{}
	}
	a.preflightSeq++
	a.preflights[id] = a.preflightSeq
	return a.preflightSeq
}

// preflight asks later, the resolver resolveLocked deferred, where job goes
// and with which headers. It reports false when the start is off: the task
// was paused or removed meanwhile, or a newer start took its place.
func (a *App) preflight(job *engine.Job, later resolver.Resolver, seq uint64) bool {
	target, err := later.Resolve(a.ctx, resolver.Request{URL: job.URL})
	a.mu.Lock()
	current := a.preflights[job.TaskID] == seq
	if current {
		delete(a.preflights, job.TaskID)
		if !a.active[job.TaskID] {
			// The engine never got the job, so a resume has nothing there to
			// resume and has to start the task afresh.
			delete(a.started, job.TaskID)
		}
	}
	current = current && a.active[job.TaskID]
	a.mu.Unlock()
	if !current {
		return false
	}
	if err != nil {
		a.onUpdate(job.TaskID, core.Update{Status: core.StatusError, Err: err.Error()})
		return false
	}
	job.URL, job.Headers = target.DirectURL, target.Headers
	return true
}

// onlyProfile serves one header set to hostheaders.Resolver as if it were the
// only stored profile.
type onlyProfile struct{ set hostheaders.Set }

func (p onlyProfile) Covers(rawurl string) bool               { return hostheaders.OriginOf(rawurl) == p.set.Origin }
func (p onlyProfile) ForURL(string) (string, hostheaders.Set) { return "", p.set }
func (p onlyProfile) Get(string) hostheaders.Set              { return p.set }
