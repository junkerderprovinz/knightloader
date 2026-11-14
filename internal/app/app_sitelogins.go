package app

// Sites signed into through the user's browser (internal/sitelogin): the
// accounts page's tiles, the wait between its Sign in and the session the
// extension hands over, and who each stored session signs in as.

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/httpx"
	"github.com/junkerderprovinz/knightloader/internal/sitelogin"
)

// ErrNoSuchSite is a site id the catalogue does not have.
var ErrNoSuchSite = errors.New("there is no such site to sign in to")

// ErrNoSiteSession is a hand-over or paste that holds no cookie of the site,
// such as one made before signing in.
var ErrNoSiteSession = errors.New("these cookies hold no session for this site")

// siteLoginWait is how long a Sign in waits for its session: long enough to
// get through a captcha and a mailed code.
const siteLoginWait = 15 * time.Minute

// A check that named the account is trusted for a while, since the page
// polls; one the site did not answer is asked again sooner.
const (
	siteCheckFresh     = 10 * time.Minute
	siteUncheckedFresh = time.Minute
	siteCheckTimeout   = 15 * time.Second
)

var siteLoginClient = httpx.New(httpx.Options{Timeout: siteCheckTimeout, Class: httpx.ClassHosters})

// siteLoginCheck is sitelogin.Check against the network, a variable so tests
// can answer for the sites.
var siteLoginCheck = func(ctx context.Context, site sitelogin.Site, jar string) sitelogin.Status {
	return sitelogin.Check(ctx, siteLoginClient, site, jar, time.Now())
}

// SiteLoginState is one tile of the accounts page: the site and what is known
// of its session, never the session itself.
type SiteLoginState struct {
	sitelogin.Site
	sitelogin.Status
	// Waiting is true from Sign in until a session arrives or the wait ends.
	Waiting bool `json:"waiting"`
}

type siteCheck struct {
	status sitelogin.Status
	at     time.Time
}

type siteLoginState struct {
	mu      sync.Mutex
	waiting map[string]time.Time
	checked map[string]siteCheck
}

var (
	siteLoginMu    sync.Mutex
	siteLoginByApp = map[*App]*siteLoginState{}
)

func (a *App) siteLogins() *siteLoginState {
	siteLoginMu.Lock()
	defer siteLoginMu.Unlock()
	st, ok := siteLoginByApp[a]
	if !ok {
		st = &siteLoginState{waiting: map[string]time.Time{}, checked: map[string]siteCheck{}}
		siteLoginByApp[a] = st
	}
	return st
}

func (a *App) siteStore() *sitelogin.Store { return sitelogin.NewStore(a.Accounts) }

// SiteLogins lists every site on offer, in the catalogue's order.
func (a *App) SiteLogins() []SiteLoginState {
	out := make([]SiteLoginState, 0, len(sitelogin.Sites))
	for _, site := range sitelogin.Sites {
		out = append(out, a.siteLoginState(site, false))
	}
	return out
}

// CheckSiteLogin asks the site again, whatever the last check said.
func (a *App) CheckSiteLogin(id string) (SiteLoginState, error) {
	site, ok := sitelogin.Lookup(id)
	if !ok {
		return SiteLoginState{}, ErrNoSuchSite
	}
	return a.siteLoginState(site, true), nil
}

// BeginSiteLogin starts the wait for a site's session, which the extension
// hands over once the user has signed in in the browser.
func (a *App) BeginSiteLogin(id string) (SiteLoginState, error) {
	site, ok := sitelogin.Lookup(id)
	if !ok {
		return SiteLoginState{}, ErrNoSuchSite
	}
	st := a.siteLogins()
	st.mu.Lock()
	st.waiting[id] = time.Now().Add(siteLoginWait)
	st.mu.Unlock()
	return a.siteLoginState(site, false), nil
}

// HandOverSiteSession stores the session the extension read from the
// browser, keeping only the site's own live cookies.
func (a *App) HandOverSiteSession(id string, cookies []sitelogin.BrowserCookie) (SiteLoginState, error) {
	site, ok := sitelogin.Lookup(id)
	if !ok {
		return SiteLoginState{}, ErrNoSuchSite
	}
	return a.StoreSiteSession(id, sitelogin.JarFromCookies(site, cookies, time.Now()))
}

// StoreSiteSession stores a cookies.txt as a site's session and ends its
// wait, unless the site signs nobody in with it. The answer says who is
// signed in.
func (a *App) StoreSiteSession(id, jar string) (SiteLoginState, error) {
	site, ok := sitelogin.Lookup(id)
	if !ok {
		return SiteLoginState{}, ErrNoSuchSite
	}
	if jar == "" {
		return SiteLoginState{}, ErrNoSiteSession
	}
	// Asked before it is stored, so cookies from a browser that is not signed
	// in never replace a session that works.
	status := a.askSite(site, jar)
	if status.State != sitelogin.SignedIn && status.State != sitelogin.Unchecked {
		return SiteLoginState{}, ErrNoSiteSession
	}
	if err := a.siteStore().Set(id, jar); err != nil {
		return SiteLoginState{}, err
	}
	st := a.siteLogins()
	st.mu.Lock()
	delete(st.waiting, id)
	st.checked[id] = siteCheck{status: status, at: time.Now()}
	st.mu.Unlock()
	return a.siteLoginState(site, false), nil
}

// RemoveSiteLogin forgets a site's session.
func (a *App) RemoveSiteLogin(id string) error {
	if _, ok := sitelogin.Lookup(id); !ok {
		return ErrNoSuchSite
	}
	if err := a.siteStore().Set(id, ""); err != nil {
		return err
	}
	st := a.siteLogins()
	st.mu.Lock()
	delete(st.checked, id)
	delete(st.waiting, id)
	st.mu.Unlock()
	return nil
}

// siteLoginState puts together what the page shows of one site, checking the
// session when the last check is stale or fresh is set.
func (a *App) siteLoginState(site sitelogin.Site, fresh bool) SiteLoginState {
	st := a.siteLogins()
	now := time.Now()
	out := SiteLoginState{Site: site}

	st.mu.Lock()
	if until, ok := st.waiting[site.ID]; ok && now.After(until) {
		delete(st.waiting, site.ID)
	}
	_, out.Waiting = st.waiting[site.ID]
	last, known := st.checked[site.ID]
	st.mu.Unlock()

	jar := a.siteStore().Session(site.ID)
	if jar == "" {
		out.Status = sitelogin.Status{State: sitelogin.SignedOut}
		return out
	}
	keep := siteCheckFresh
	if last.status.State == sitelogin.Unchecked {
		keep = siteUncheckedFresh
	}
	end, err := time.Parse(time.RFC3339, last.status.Expires)
	ranOut := err == nil && !end.After(now)
	if known && !fresh && !ranOut && now.Sub(last.at) < keep {
		out.Status = last.status
		return out
	}
	out.Status = a.askSite(site, jar)
	st.mu.Lock()
	st.checked[site.ID] = siteCheck{status: out.Status, at: now}
	st.mu.Unlock()
	return out
}

// askSite is siteLoginCheck within the time a check may take.
func (a *App) askSite(site sitelogin.Site, jar string) sitelogin.Status {
	ctx, cancel := context.WithTimeout(a.ctx, siteCheckTimeout)
	defer cancel()
	return siteLoginCheck(ctx, site, jar)
}

// siteSession is the stored session of the site a link belongs to, for
// yt-dlp's Sessions hook.
func (a *App) siteSession(rawurl string) string {
	site, ok := sitelogin.ForURL(rawurl)
	if !ok {
		return ""
	}
	return a.siteStore().Session(site.ID)
}
