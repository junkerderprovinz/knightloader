package app

// KnightLoader's own clients for the XFileSharing hosters: a backend per
// site, the login each signs in with, the session kept across restarts, and
// the checks that tell the accounts page and routing what the login is worth.
// A login is stored as a hoster login like any other, and hosterauth leaves
// the login of a site that signs in itself out of JD (see ownLogin).

import (
	"context"
	"encoding/base64"
	"log"
	"net/http"
	"net/url"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/accounts"
	"github.com/junkerderprovinz/knightloader/internal/captcha"
	"github.com/junkerderprovinz/knightloader/internal/hostalias"
	"github.com/junkerderprovinz/knightloader/internal/hoster"
	"github.com/junkerderprovinz/knightloader/internal/hosterauth"
	"github.com/junkerderprovinz/knightloader/internal/httpx"
	"github.com/junkerderprovinz/knightloader/internal/ownlogin"
	"github.com/junkerderprovinz/knightloader/internal/resolver/clicknupload"
	"github.com/junkerderprovinz/knightloader/internal/resolver/ddownload"
	"github.com/junkerderprovinz/knightloader/internal/resolver/filespace"
	"github.com/junkerderprovinz/knightloader/internal/resolver/katfile"
	"github.com/junkerderprovinz/knightloader/internal/resolver/sendnow"
	"github.com/junkerderprovinz/knightloader/internal/resolver/uploadrar"
	"github.com/junkerderprovinz/knightloader/internal/resolver/xfs"
	"github.com/junkerderprovinz/knightloader/internal/sitelogin"
)

// xfsSites are the XFileSharing hosters KnightLoader downloads from itself.
var xfsSites = []*xfs.Site{ddownload.Site, katfile.Site, clicknupload.Site, filespace.Site, uploadrar.Site, sendnow.Site}

// hosterSessionService is the accounts.Store service a hoster client's
// signed-in session is sealed under, with the host as account.
const hosterSessionService = "hostersession"

// sitePageMark is the Username of a sealed XFS session the user signed in to
// on the site's own page (see xfs.Kept).
const sitePageMark = "site-page"

// loginCheckInterval is how often a login is looked at again with the session
// it has. Those looks never sign in, since a sign-in can ask for a captcha
// nobody is waiting for.
const loginCheckInterval = time.Hour

// xfsSiteFor is the site host belongs to, nil for none.
func xfsSiteFor(host string) *xfs.Site {
	host = hostalias.Canonical(host)
	for _, s := range xfsSites {
		if s.Host == host {
			return s
		}
	}
	return nil
}

// xfsBackend returns the app's backend for site, made on first use, which
// also starts the first look at a stored login.
func (a *App) xfsBackend(site *xfs.Site, eng xfs.Downloader) *xfs.Backend {
	a.bmu.Lock()
	defer a.bmu.Unlock()
	if b := a.xfsBackends[site.ID]; b != nil {
		return b
	}
	c := xfs.NewClient(site, "https://"+site.Host, httpx.New(httpx.Options{Class: httpx.ClassHosters, RefuseBrowserChecks: true}))
	b := a.newXFSBackend(c, eng)
	if a.xfsBackends == nil {
		a.xfsBackends = map[string]*xfs.Backend{}
	}
	a.xfsBackends[site.ID] = b
	if site.OwnLogin {
		a.spawn(func() { a.checkXFSLogin(site, true) })
	}
	return b
}

func (a *App) newXFSBackend(c *xfs.Client, eng xfs.Downloader) *xfs.Backend {
	site := c.Site()
	b := xfs.NewBackend(c, eng, a.onUpdate)
	b.Password = a.downloadPassword
	b.Solve = func(ctx context.Context, taskID string, ch xfs.Captcha) (string, error) {
		c := hoster.Captcha{Kind: hoster.CaptchaKind(ch.Kind), SiteKey: ch.SiteKey, PageURL: ch.PageURL, Image: ch.Image}
		return a.askCaptcha(ctx, site.Host, taskID, c)
	}
	if !site.OwnLogin {
		return b
	}
	b.Login = func() (string, string, bool) { return a.xfsLogin(site) }
	b.LoadSession = func() xfs.Kept {
		cred, _ := a.Accounts.GetCredential(hosterSessionService, site.Host)
		return xfs.Kept{Cookies: cred.APIKey, OwnPage: cred.Username == sitePageMark}
	}
	b.SaveSession = func(k xfs.Kept) {
		cred := accounts.Credential{APIKey: k.Cookies}
		if k.OwnPage {
			cred.Username = sitePageMark
		}
		if err := a.Accounts.SetCredential(hosterSessionService, site.Host, cred); err != nil {
			log.Printf("%s: keeping the session failed: %v", site.ID, err)
		}
	}
	// What the client found out may open or close the way down a link held
	// for premium only.
	b.OnLogin = func(xfs.LoginState) { a.spawn(a.refreshPremiumHolds) }
	b.Moved = a.ownLoginMoved
	return b
}

// xfsLoginHost is the host site's login is stored under, any of its domains,
// and "" for none.
func (a *App) xfsLoginHost(site *xfs.Site) string {
	for _, h := range hosterauth.NewStore(a.Accounts).Hosts() {
		if hostalias.Canonical(h) == site.Host {
			return h
		}
	}
	return ""
}

// xfsLogin is the stored login of site while it is switched on.
func (a *App) xfsLogin(site *xfs.Site) (string, string, bool) {
	host := a.xfsLoginHost(site)
	if host == "" || !a.accountEnabled(hosterauth.Service, host) {
		return "", "", false
	}
	cred, err := hosterauth.NewStore(a.Accounts).Get(host)
	if err != nil || cred.Username == "" || cred.Password == "" {
		return "", "", false
	}
	return cred.Username, cred.Password, true
}

// checkXFSLogin looks at the stored login of site, signing in when signIn is
// set.
func (a *App) checkXFSLogin(site *xfs.Site, signIn bool) {
	a.bmu.RLock()
	b := a.xfsBackends[site.ID]
	a.bmu.RUnlock()
	if b == nil {
		return
	}
	b.CheckLogin(a.ctx, signIn)
}

// watchXFSLogins looks at every login again each loginCheckInterval until the
// app closes.
func (a *App) watchXFSLogins() {
	t := time.NewTicker(loginCheckInterval)
	defer t.Stop()
	for {
		select {
		case <-a.ctx.Done():
			return
		case <-t.C:
			for _, site := range xfsSites {
				if site.OwnLogin {
					a.checkXFSLogin(site, false)
				}
			}
		}
	}
}

// xfsLoginState is what site's client knows about the stored login, for the
// accounts page.
func (a *App) xfsLoginState(site *xfs.Site) hosterauth.OwnState {
	a.bmu.RLock()
	b := a.xfsBackends[site.ID]
	a.bmu.RUnlock()
	if b == nil {
		return hosterauth.OwnState{Waiting: true}
	}
	st, signingIn := b.LoginStatus()
	own := hosterauth.OwnState{SignInSite: site.ID}
	switch {
	case b.Handed():
		own.Handed = true
	case signingIn:
		own.SigningIn = true
	case st.SignInHere:
		own.Checked, own.Refused, own.SignInHere = true, true, true
		own.Detail = site.Name + " asks for a second factor at sign-in, so sign in on its own page"
	case st.Code == ownlogin.CodeWrong:
		own.Checked, own.Refused, own.Code = true, true, string(st.Code)
		own.Detail = site.Name + " did not take the two-factor code, so enter the current one"
	case st.Code != "":
		own.Checked, own.Refused, own.Code = true, true, string(st.Code)
		own.Detail = site.Name + " asks for a two-factor code at sign-in, so enter it with the login"
	case st.Activate:
		own.Checked, own.Refused, own.Activate = true, true, true
		own.Detail = site.Name + " says the account is not activated yet, so activate it with the link from its welcome mail"
	case st.Refused:
		own.Checked, own.Refused, own.Detail = true, true, site.Name+" turned this login down"
	case !st.Checked:
		own.Waiting = true
	default:
		own.Checked, own.Premium, own.Expiry = true, st.Premium, st.Expires
		own.TrafficLeft, own.TrafficMax = max(st.TrafficLeft, 0), st.TrafficMax
	}
	return own
}

// xfsOwnLoginSite is the site KnightLoader signs in to itself whose routing
// id is id, nil for none.
func xfsOwnLoginSite(id string) *xfs.Site {
	for _, s := range xfsSites {
		if s.ID == id && s.OwnLogin {
			return s
		}
	}
	return nil
}

// xfsSignInSite is site's sign-in page as the sign-in browser opens it, on
// all of the hoster's domains.
func xfsSignInSite(site *xfs.Site) sitelogin.Site {
	return sitelogin.Hoster(site.ID, site.Name, site.Host, site.SignInPage(), hostalias.Names(site.Host), xfs.SessionCookie)
}

// xfsSignedIn takes up the session the sign-in browser holds for site, which
// the user signed in to on the site's own page, second factor and all.
func (a *App) xfsSignedIn(site *xfs.Site, jar string) SiteLoginState {
	a.bmu.RLock()
	b := a.xfsBackends[site.ID]
	a.bmu.RUnlock()
	if b != nil && jar != "" {
		b.UseSession(a.ctx, sitelogin.CookieHeader(jar, time.Now()))
	}
	return a.xfsSignInState(site)
}

// xfsSignInState is how site's login stands, as the sign-in window says it.
func (a *App) xfsSignInState(site *xfs.Site) SiteLoginState {
	st := SiteLoginState{Site: xfsSignInSite(site), Status: sitelogin.Status{State: sitelogin.SignedOut}}
	a.bmu.RLock()
	b := a.xfsBackends[site.ID]
	a.bmu.RUnlock()
	if b == nil {
		return st
	}
	if ls, _ := b.LoginStatus(); ls.Checked && !ls.Refused && !ls.SignInHere {
		st.State, st.Source = sitelogin.SignedIn, SourceSignInBrowser
	}
	return st
}

// changedXFSLogin drops the session and what is known about the login of the
// site host belongs to after the login was changed, switched or removed, and
// signs in again with one that is still there and switched on.
func (a *App) changedXFSLogin(host string) {
	site := xfsSiteFor(host)
	if site == nil || !site.OwnLogin {
		return
	}
	a.bmu.RLock()
	b := a.xfsBackends[site.ID]
	a.bmu.RUnlock()
	if b != nil {
		b.Forget()
	}
	a.spawn(a.refreshPremiumHolds)
	if _, _, ok := a.xfsLogin(site); ok {
		a.spawn(func() { a.checkXFSLogin(site, true) })
	}
}

// xfsCodeTaker is the client of the site KnightLoader signs in to itself that
// host belongs to, nil for none or one not made yet.
func (a *App) xfsCodeTaker(host string) *xfs.Backend {
	site := xfsSiteFor(host)
	if site == nil || !site.OwnLogin {
		return nil
	}
	a.bmu.RLock()
	defer a.bmu.RUnlock()
	return a.xfsBackends[site.ID]
}

// saveXFSLoginWithCode stores the login under host and has b sign it in with
// the one-time code of its second factor, into the form the last sign-in
// brought up while the login stays the same, and returns its row.
func (a *App) saveXFSLoginWithCode(ctx context.Context, b *xfs.Backend, host, username, password, code string) (hosterauth.LoginState, error) {
	site := b.Client().Site()
	user, pass, _ := a.xfsLogin(site)
	r := a.hosterAuth()
	if err := r.SetLogin(host, username, password); err != nil {
		return hosterauth.LoginState{}, err
	}
	if u, p, _ := a.xfsLogin(site); u != user || p != pass {
		b.Forget()
	}
	b.SignInCode(ctx, code)
	a.refreshPremiumHolds()
	a.spawn(func() {
		if _, err := r.Reconcile(a.ctx); err != nil {
			log.Printf("hosterauth: reconcile after save failed: %v", err)
		}
	})
	return a.hosterLoginRow(host), nil
}

// solveFor answers the captchas one of KnightLoader's own hoster clients
// meets at host.
func (a *App) solveFor(host string) hoster.Solve {
	return func(ctx context.Context, taskID string, c hoster.Captcha) (string, error) {
		return a.askCaptcha(ctx, host, taskID, c)
	}
}

// askCaptcha puts a captcha one of KnightLoader's own hoster clients met at
// host in front of the captcha prompts and solvers, and waits for the answer.
// taskID is the download waiting on it, "" for none.
func (a *App) askCaptcha(ctx context.Context, host, taskID string, ch hoster.Captcha) (string, error) {
	c := captcha.Challenge{Host: host, TaskID: taskID}
	if ch.Kind == hoster.Image {
		c.Kind = captcha.KindImage
		c.Payload = &captcha.ImagePayload{
			DataURL: "data:" + http.DetectContentType(ch.Image) + ";base64," + base64.StdEncoding.EncodeToString(ch.Image),
		}
	} else {
		origin := ch.PageURL
		if u, err := url.Parse(ch.PageURL); err == nil {
			origin = u.Scheme + "://" + u.Host
		}
		c.Kind = captcha.KindWidget
		c.Payload = &captcha.WidgetPayload{
			Vendor:     string(ch.Kind),
			SiteKey:    ch.SiteKey,
			SiteURL:    ch.PageURL,
			ContextURL: origin,
		}
	}
	a.ensureCaptchaPoller()
	return a.captchaStateFor().asks.Ask(ctx, c)
}
