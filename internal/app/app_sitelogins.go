package app

// Sites signed into through the user's browser (internal/sitelogin): the
// accounts page's tiles, the wait between its Sign in and the session that
// turns up in a browser here or is handed over by the extension, and who each
// session signs in as. app_browsersessions.go reads the browsers.

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

// ErrNoSiteSession is a hand-over that holds no cookie of the site, such as
// one made before signing in.
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
	// Source is where the session comes from: SourceBrowser, SourceExtension,
	// or "" without one.
	Source string `json:"source"`
	// Browser names the browser and profile a session read from a browser
	// came from, "Firefox (default-release)".
	Browser string `json:"browser"`
	// Unreadable names the browsers here whose cookies of the site could not
	// be read, such as Chrome on Windows with its app-bound key.
	Unreadable []string `json:"unreadable"`
	// ReadsBrowsers says this instance reads the browsers on its computer,
	// which one in a container never does.
	ReadsBrowsers bool `json:"readsBrowsers"`
	// Disconnected says the site was removed here and the browsers' sign-in
	// to it is not used until it is signed in through KnightLoader again.
	Disconnected bool `json:"disconnected"`
}

type siteCheck struct {
	status sitelogin.Status
	at     time.Time
}

type siteLoginState struct {
	mu      sync.Mutex
	waiting map[string]time.Time
	// checked is the last check of each stored session.
	checked map[string]siteCheck

	// read is nil until StartBrowserSessions. scans are the last read of the
	// browsers per site, verdicts the site's answers about the sessions
	// found, by a hash of site and cookies, and logged the unreadable
	// browsers already logged.
	read     browserReader
	scanning map[string]*sync.Mutex
	scans    map[string]browserScan
	verdicts map[[32]byte]siteCheck
	logged   map[string]bool
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
		st = &siteLoginState{
			waiting:  map[string]time.Time{},
			checked:  map[string]siteCheck{},
			scanning: scanLocks(),
			scans:    map[string]browserScan{},
			verdicts: map[[32]byte]siteCheck{},
			logged:   map[string]bool{},
		}
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

// CheckSiteLogin reads the browsers and asks the site again, whatever the
// last check said.
func (a *App) CheckSiteLogin(id string) (SiteLoginState, error) {
	site, ok := sitelogin.Lookup(id)
	if !ok {
		return SiteLoginState{}, ErrNoSuchSite
	}
	return a.siteLoginState(site, true), nil
}

// BeginSiteLogin starts the wait for a site's session, which turns up in a
// browser here or is handed over by the extension once the user has signed in.
func (a *App) BeginSiteLogin(id string) (SiteLoginState, error) {
	site, ok := sitelogin.Lookup(id)
	if !ok {
		return SiteLoginState{}, ErrNoSuchSite
	}
	if err := a.connect(site); err != nil {
		return SiteLoginState{}, err
	}
	st := a.siteLogins()
	st.mu.Lock()
	st.waiting[id] = time.Now().Add(siteLoginWait)
	st.mu.Unlock()
	return a.siteLoginState(site, false), nil
}

// ReconnectSiteLogin uses the browsers' sign-in to a site that was removed
// here again, without waiting for a new one.
func (a *App) ReconnectSiteLogin(id string) (SiteLoginState, error) {
	site, ok := sitelogin.Lookup(id)
	if !ok {
		return SiteLoginState{}, ErrNoSuchSite
	}
	if err := a.connect(site); err != nil {
		return SiteLoginState{}, err
	}
	return a.siteLoginState(site, false), nil
}

// connect clears a site's mark not to use the browsers' sign-in, and the last
// read of them, which a disconnected site never made.
func (a *App) connect(site sitelogin.Site) error {
	if err := a.siteStore().SetDisconnected(site.ID, false); err != nil {
		return err
	}
	st := a.siteLogins()
	st.mu.Lock()
	delete(st.scans, site.ID)
	st.mu.Unlock()
	return nil
}

// HandOverSiteSession stores the session the extension read from the
// browser, keeping only the site's own live cookies. No cookies at all is the
// browser signed out, which forgets the session handed over before.
func (a *App) HandOverSiteSession(id string, cookies []sitelogin.BrowserCookie) (SiteLoginState, error) {
	site, ok := sitelogin.Lookup(id)
	if !ok {
		return SiteLoginState{}, ErrNoSuchSite
	}
	if len(cookies) == 0 {
		if err := a.forgetHandedOver(id); err != nil {
			return SiteLoginState{}, err
		}
		return a.siteLoginState(site, false), nil
	}
	return a.storeSiteSession(id, sitelogin.JarFromCookies(site, cookies, time.Now()))
}

// storeSiteSession stores a cookies.txt as a site's session and ends its
// wait, unless the site signs nobody in with it. The answer says who is
// signed in.
func (a *App) storeSiteSession(id, jar string) (SiteLoginState, error) {
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
	if err := a.connect(site); err != nil {
		return SiteLoginState{}, err
	}
	st := a.siteLogins()
	st.mu.Lock()
	delete(st.waiting, id)
	st.checked[id] = siteCheck{status: status, at: time.Now()}
	st.mu.Unlock()
	return a.siteLoginState(site, false), nil
}

// RemoveSiteLogin signs a site out here: it forgets the session the
// extension handed over and stops using the browsers' sign-in, which the
// browser keeps, until the site is signed in through KnightLoader again.
func (a *App) RemoveSiteLogin(id string) error {
	if err := a.forgetHandedOver(id); err != nil {
		return err
	}
	if err := a.siteStore().SetDisconnected(id, true); err != nil {
		return err
	}
	st := a.siteLogins()
	st.mu.Lock()
	delete(st.scans, id)
	st.mu.Unlock()
	return nil
}

// forgetHandedOver forgets the session the extension handed over.
func (a *App) forgetHandedOver(id string) error {
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

// siteLoginState puts together what the page shows of one site: a browser's
// session first, the stored one otherwise, checking it when the last check is
// stale or fresh is set.
func (a *App) siteLoginState(site sitelogin.Site, fresh bool) SiteLoginState {
	st := a.siteLogins()
	now := time.Now()
	out := SiteLoginState{Site: site, Unreadable: []string{}}

	st.mu.Lock()
	if until, ok := st.waiting[site.ID]; ok && now.After(until) {
		delete(st.waiting, site.ID)
	}
	_, out.Waiting = st.waiting[site.ID]
	last, known := st.checked[site.ID]
	out.ReadsBrowsers = st.read != nil
	st.mu.Unlock()
	out.Disconnected = a.siteStore().Disconnected(site.ID)

	age := browserScanFresh
	switch {
	case fresh:
		age = 0
	case out.Waiting:
		age = browserWaitFresh
	}
	scan := a.browserSession(site, age)
	if scan.unreadable != nil {
		out.Unreadable = scan.unreadable
	}
	if scan.jar != "" {
		out.Status, out.Source, out.Browser = scan.status, SourceBrowser, scan.from
		if out.Waiting {
			st.mu.Lock()
			delete(st.waiting, site.ID)
			st.mu.Unlock()
			out.Waiting = false
		}
		return out
	}

	jar := a.siteStore().Session(site.ID)
	if jar == "" {
		out.Status = sitelogin.Status{State: sitelogin.SignedOut}
		if scan.from != "" {
			out.Status, out.Source, out.Browser = scan.status, SourceBrowser, scan.from
		}
		return out
	}
	out.Source = SourceExtension
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
