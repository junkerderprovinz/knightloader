package app

// ddownload's own client: the backend, the login it signs in with, the
// session it keeps across restarts, and the checks that tell the accounts
// page and routing what the login is worth. The login is stored as a hoster
// login like any other, and hosterauth leaves it out of JD (see ownLogin).

import (
	"context"
	"log"
	"net/url"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/accounts"
	"github.com/junkerderprovinz/knightloader/internal/captcha"
	"github.com/junkerderprovinz/knightloader/internal/hostalias"
	"github.com/junkerderprovinz/knightloader/internal/hosterauth"
	"github.com/junkerderprovinz/knightloader/internal/httpx"
	"github.com/junkerderprovinz/knightloader/internal/resolver/ddownload"
)

// hosterSessionService is the accounts.Store service a hoster client's
// signed-in session is sealed under, with the host as account.
const hosterSessionService = "hostersession"

// loginCheckInterval is how often the ddownload login is looked at again with
// the session it has. Those looks never sign in, since a sign-in asks for a
// captcha nobody is waiting for.
const loginCheckInterval = time.Hour

// ownLogin reports whether KnightLoader signs in to host with its own client
// rather than handing the login to JD.
func ownLogin(host string) bool {
	return hostalias.Canonical(host) == ddownload.Site
}

// ddownloadBackend returns the app's one ddownload backend, made on first use,
// which also starts the first look at a stored login.
func (a *App) ddownloadBackend(eng ddownload.Downloader) *ddownload.Backend {
	a.bmu.Lock()
	defer a.bmu.Unlock()
	if a.ddl == nil {
		c := ddownload.NewClient(ddownload.DefaultSite, httpx.New(httpx.Options{Class: httpx.ClassHosters}))
		a.ddl = a.newDdownloadBackend(c, eng)
		a.spawn(func() { a.checkDdownloadLogin(true) })
	}
	return a.ddl
}

func (a *App) newDdownloadBackend(c *ddownload.Client, eng ddownload.Downloader) *ddownload.Backend {
	b := ddownload.NewBackend(c, eng, a.onUpdate)
	b.Password = a.downloadPassword
	b.Login = a.ddownloadLogin
	b.Solve = func(ctx context.Context, taskID, pageURL, siteKey string) (string, error) {
		return a.askCaptcha(ctx, ddownload.Site, taskID, pageURL, siteKey)
	}
	b.LoadSession = func() string {
		cred, _ := a.Accounts.GetCredential(hosterSessionService, ddownload.Site)
		return cred.APIKey
	}
	b.SaveSession = func(cookies string) {
		if err := a.Accounts.SetCredential(hosterSessionService, ddownload.Site, accounts.Credential{APIKey: cookies}); err != nil {
			log.Printf("ddownload: keeping the session failed: %v", err)
		}
	}
	b.OnLogin = a.ddownloadLoginChanged
	return b
}

// ddownloadLoginHost is the host the ddownload login is stored under, either
// of its domains, and "" for none.
func (a *App) ddownloadLoginHost() string {
	for _, h := range hosterauth.NewStore(a.Accounts).Hosts() {
		if ownLogin(h) {
			return h
		}
	}
	return ""
}

// ddownloadLogin is the stored ddownload login while it is switched on.
func (a *App) ddownloadLogin() (string, string, bool) {
	host := a.ddownloadLoginHost()
	if host == "" || !a.accountEnabled(hosterauth.Service, host) {
		return "", "", false
	}
	cred, err := hosterauth.NewStore(a.Accounts).Get(host)
	if err != nil || cred.Username == "" || cred.Password == "" {
		return "", "", false
	}
	return cred.Username, cred.Password, true
}

// checkDdownloadLogin looks at the stored login, signing in when signIn is
// set.
func (a *App) checkDdownloadLogin(signIn bool) {
	a.bmu.RLock()
	b := a.ddl
	a.bmu.RUnlock()
	if b == nil {
		return
	}
	if host := a.ddownloadLoginHost(); host != "" && signIn && a.accountEnabled(hosterauth.Service, host) {
		a.hosterAuth().SetOwnState(host, hosterauth.LoginState{Status: hosterauth.StatusQueued,
			Detail: "signing in with this login", Code: hosterauth.CodeSigningIn})
	}
	b.CheckLogin(a.ctx, signIn)
}

// watchDdownloadLogin looks at the login again every loginCheckInterval until
// the app closes.
func (a *App) watchDdownloadLogin() {
	t := time.NewTicker(loginCheckInterval)
	defer t.Stop()
	for {
		select {
		case <-a.ctx.Done():
			return
		case <-t.C:
			a.checkDdownloadLogin(false)
		}
	}
}

// ddownloadLoginChanged puts what the client found out about the login on the
// accounts page, and lets the queue see whether it has premium.
func (a *App) ddownloadLoginChanged(st ddownload.LoginState) {
	host := a.ddownloadLoginHost()
	if host == "" {
		return
	}
	var ls hosterauth.LoginState
	switch {
	case st.Refused:
		ls = hosterauth.LoginState{Status: hosterauth.StatusRejected,
			Detail: "ddownload turned this login down", Code: hosterauth.CodeRefused}
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

// forgetDdownloadLogin drops the session and what is known about the login
// after it was changed, switched off or removed.
func (a *App) forgetDdownloadLogin() {
	a.bmu.RLock()
	b := a.ddl
	a.bmu.RUnlock()
	if b != nil {
		b.Forget()
	}
	a.spawn(a.refreshPremiumHolds)
}

// askCaptcha puts a Turnstile check one of KnightLoader's own hoster clients
// met on pageURL in front of the captcha prompts and solvers, and waits for
// the answer. taskID is the download waiting on it, "" for none.
func (a *App) askCaptcha(ctx context.Context, host, taskID, pageURL, siteKey string) (string, error) {
	if a.ModuleOff("captcha") {
		return "", ErrCaptchaOff
	}
	origin := pageURL
	if u, err := url.Parse(pageURL); err == nil {
		origin = u.Scheme + "://" + u.Host
	}
	a.ensureCaptchaPoller()
	return a.captchaStateFor().asks.Ask(ctx, captcha.Challenge{
		Host:   host,
		TaskID: taskID,
		Kind:   captcha.KindWidget,
		Payload: &captcha.WidgetPayload{
			Vendor:     captcha.VendorTurnstile,
			SiteKey:    siteKey,
			SiteURL:    pageURL,
			ContextURL: origin,
		},
	})
}
