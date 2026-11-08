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
	"github.com/junkerderprovinz/knightloader/internal/hosterauth"
	"github.com/junkerderprovinz/knightloader/internal/httpx"
	"github.com/junkerderprovinz/knightloader/internal/resolver/clicknupload"
	"github.com/junkerderprovinz/knightloader/internal/resolver/ddownload"
	"github.com/junkerderprovinz/knightloader/internal/resolver/filespace"
	"github.com/junkerderprovinz/knightloader/internal/resolver/katfile"
	"github.com/junkerderprovinz/knightloader/internal/resolver/uploadrar"
	"github.com/junkerderprovinz/knightloader/internal/resolver/xfs"
)

// xfsSites are the XFileSharing hosters KnightLoader downloads from itself.
var xfsSites = []*xfs.Site{ddownload.Site, katfile.Site, clicknupload.Site, filespace.Site, uploadrar.Site}

// hosterSessionService is the accounts.Store service a hoster client's
// signed-in session is sealed under, with the host as account.
const hosterSessionService = "hostersession"

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

// ownLogin reports whether KnightLoader signs in to host with its own client
// rather than handing the login to JD.
func ownLogin(host string) bool {
	s := xfsSiteFor(host)
	return s != nil && s.OwnLogin
}

// xfsBackend returns the app's backend for site, made on first use, which
// also starts the first look at a stored login.
func (a *App) xfsBackend(site *xfs.Site, eng xfs.Downloader) *xfs.Backend {
	a.bmu.Lock()
	defer a.bmu.Unlock()
	if b := a.xfsBackends[site.ID]; b != nil {
		return b
	}
	c := xfs.NewClient(site, "https://"+site.Host, httpx.New(httpx.Options{Class: httpx.ClassHosters}))
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
		return a.askCaptcha(ctx, site.Host, taskID, ch)
	}
	if !site.OwnLogin {
		return b
	}
	b.Login = func() (string, string, bool) { return a.xfsLogin(site) }
	b.LoadSession = func() string {
		cred, _ := a.Accounts.GetCredential(hosterSessionService, site.Host)
		return cred.APIKey
	}
	b.SaveSession = func(cookies string) {
		if err := a.Accounts.SetCredential(hosterSessionService, site.Host, accounts.Credential{APIKey: cookies}); err != nil {
			log.Printf("%s: keeping the session failed: %v", site.ID, err)
		}
	}
	b.OnLogin = func(st xfs.LoginState) { a.xfsLoginChanged(site, st) }
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
	if host := a.xfsLoginHost(site); host != "" && signIn && a.accountEnabled(hosterauth.Service, host) {
		a.hosterAuth().SetOwnState(host, hosterauth.LoginState{Status: hosterauth.StatusQueued,
			Detail: "signing in with this login", Code: hosterauth.CodeSigningIn})
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

// xfsLoginChanged puts what the client found out about the login on the
// accounts page, and lets the queue see whether it has premium.
func (a *App) xfsLoginChanged(site *xfs.Site, st xfs.LoginState) {
	host := a.xfsLoginHost(site)
	if host == "" {
		return
	}
	var ls hosterauth.LoginState
	switch {
	case st.Refused:
		ls = hosterauth.LoginState{Status: hosterauth.StatusRejected,
			Detail: site.Name + " turned this login down", Code: hosterauth.CodeRefused}
	case !st.Checked:
		ls = hosterauth.LoginState{Status: hosterauth.StatusQueued,
			Detail: "waiting for the next check", Code: hosterauth.CodeWaiting}
	default:
		ls = hosterauth.LoginState{Status: hosterauth.StatusActive, Tier: "free",
			TrafficLeft: max(st.TrafficLeft, 0), TrafficMax: st.TrafficMax}
		if st.Premium {
			ls.Tier = "premium"
		}
		if !st.Expires.IsZero() {
			ls.Expiry = st.Expires.UTC().Format(time.RFC3339)
		}
	}
	a.hosterAuth().SetOwnState(host, ls)
	a.spawn(a.refreshPremiumHolds)
}

// changedXFSLogin drops the session and what is known about the login of the
// site host belongs to after the login was changed, switched off or removed,
// and looks at it again when check is set. It reports false for a host whose
// login goes to JD.
func (a *App) changedXFSLogin(host string, check bool) bool {
	site := xfsSiteFor(host)
	if site == nil || !site.OwnLogin {
		return false
	}
	a.bmu.RLock()
	b := a.xfsBackends[site.ID]
	a.bmu.RUnlock()
	if b != nil {
		b.Forget()
	}
	a.spawn(a.refreshPremiumHolds)
	if check {
		a.spawn(func() { a.checkXFSLogin(site, true) })
	}
	return true
}

// askCaptcha puts a captcha one of KnightLoader's own hoster clients met in
// front of the captcha prompts and solvers, and waits for the answer. taskID
// is the download waiting on it, "" for none.
func (a *App) askCaptcha(ctx context.Context, host, taskID string, ch xfs.Captcha) (string, error) {
	if a.ModuleOff("captcha") {
		return "", ErrCaptchaOff
	}
	c := captcha.Challenge{Host: host, TaskID: taskID}
	if ch.Kind == xfs.Image {
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
