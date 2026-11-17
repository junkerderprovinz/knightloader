package app

// Sites signed into through the user's browser (internal/sitelogin): the
// sites added on the accounts page, the wait between its Sign in and the
// session that turns up in a browser here or is handed over by the extension,
// and who each session signs in as. A site has a row and its session in use
// once it is added, like a hoster account. app_browsersessions.go reads the
// browsers.

import (
	"context"
	"errors"
	"slices"
	"sync"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/httpx"
	"github.com/junkerderprovinz/knightloader/internal/sitelogin"
)

// ErrNoSuchSite is a site id the catalogue does not have.
var ErrNoSuchSite = errors.New("there is no such site to sign in to")

// ErrSiteNotAdded is a hand-over for a site that is not added on the
// accounts page, which a hand-over never adds by itself.
var ErrSiteNotAdded = errors.New("this site is not added on the accounts page")

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

// SiteLoginState is one site of the accounts page: the site and what is known
// of its session, never the session itself.
type SiteLoginState struct {
	sitelogin.Site
	sitelogin.Status
	// Added says the site has a row, and not only an entry in the picker.
	Added bool `json:"added"`
	// Waiting is true from Sign in until a session arrives or the wait ends.
	Waiting bool `json:"waiting"`
	// Source is where the session comes from: SourceBrowser, SourceExtension,
	// SourceSignInBrowser, or "" without one.
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
	// HandsOver says the extension in a browser of the group may read the
	// site's cookies, so a sign-in there reaches this instance.
	HandsOver bool `json:"handsOver"`
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

// SiteLogins lists every site on offer, in the catalogue's order: the added
// ones with their sessions, the others for the picker.
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

// BeginSiteLogin adds a site and starts the wait for its session, which
// turns up in a browser here or is handed over by the extension once the user
// has signed in.
func (a *App) BeginSiteLogin(id string) (SiteLoginState, error) {
	site, ok := sitelogin.Lookup(id)
	if !ok {
		return SiteLoginState{}, ErrNoSuchSite
	}
	if err := a.addSite(site); err != nil {
		return SiteLoginState{}, err
	}
	st := a.siteLogins()
	st.mu.Lock()
	st.waiting[id] = time.Now().Add(siteLoginWait)
	st.mu.Unlock()
	return a.siteLoginState(site, false), nil
}

// AddSiteLogin gives a site its row, taking up the sign-in a browser here
// already holds.
func (a *App) AddSiteLogin(id string) (SiteLoginState, error) {
	site, ok := sitelogin.Lookup(id)
	if !ok {
		return SiteLoginState{}, ErrNoSuchSite
	}
	if err := a.addSite(site); err != nil {
		return SiteLoginState{}, err
	}
	return a.siteLoginState(site, false), nil
}

// NoteSiteReaders records which sites the extension in browser app may read,
// as it says each time it lists them.
func (a *App) NoteSiteReaders(app string, reads []string) error {
	for _, site := range sitelogin.Sites {
		if err := a.siteStore().SetReader(site.ID, app, slices.Contains(reads, site.ID)); err != nil {
			return err
		}
	}
	return nil
}

// addSite adds a site and clears its mark not to read the browsers for it,
// along with the last read of them, which a site that was not added never
// made.
func (a *App) addSite(site sitelogin.Site) error {
	if err := a.siteStore().SetAdded(site.ID, true); err != nil {
		return err
	}
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
	if !a.siteStore().Added(id) {
		return SiteLoginState{}, ErrSiteNotAdded
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
	if err := a.addSite(site); err != nil {
		return SiteLoginState{}, err
	}
	st := a.siteLogins()
	st.mu.Lock()
	delete(st.waiting, id)
	st.checked[id] = siteCheck{status: status, at: time.Now()}
	st.mu.Unlock()
	return a.siteLoginState(site, false), nil
}

// RemoveSiteLogin takes a site's row away: it forgets the stored session and
// stops reading the browsers for the site, which stay signed in, until it is
// added again.
func (a *App) RemoveSiteLogin(id string) error {
	if err := a.forgetHandedOver(id); err != nil {
		return err
	}
	if err := a.siteStore().SetAdded(id, false); err != nil {
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
	out := SiteLoginState{Site: site, Unreadable: []string{}, Added: a.siteStore().Added(site.ID)}

	st.mu.Lock()
	if until, ok := st.waiting[site.ID]; ok && now.After(until) {
		delete(st.waiting, site.ID)
	}
	_, out.Waiting = st.waiting[site.ID]
	last, known := st.checked[site.ID]
	out.ReadsBrowsers = st.read != nil
	st.mu.Unlock()
	out.HandsOver = slices.ContainsFunc(a.siteStore().Readers(site.ID), a.Federation.StillMember)
	if !out.Added {
		out.Status = sitelogin.Status{State: sitelogin.SignedOut}
		return out
	}

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
	if a.fromSignInBrowser(site.ID, jar) {
		out.Source = SourceSignInBrowser
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
