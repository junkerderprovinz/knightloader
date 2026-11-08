package app

import (
	"context"

	"github.com/junkerderprovinz/knightloader/internal/accounts"
	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/hostalias"
	"github.com/junkerderprovinz/knightloader/internal/hosterauth"
	"github.com/junkerderprovinz/knightloader/internal/httpx"
	"github.com/junkerderprovinz/knightloader/internal/ownlogin"
	"github.com/junkerderprovinz/knightloader/internal/resolver"
	"github.com/junkerderprovinz/knightloader/internal/resolver/keep2share"
	"github.com/junkerderprovinz/knightloader/internal/resolver/nitroflare"
	"github.com/junkerderprovinz/knightloader/internal/resolver/rapidgator"
)

// ownBackend is the backend of a hoster KnightLoader signs in to itself.
type ownBackend interface {
	backend
	SetLogin(user, password string) bool
	Check(ctx context.Context) ownlogin.Status
	Status() ownlogin.Status
}

// linkInspectingResolver is a resolver that also names the links it is
// given.
type linkInspectingResolver interface {
	resolver.Resolver
	Inspect(ctx context.Context, urls []string) []resolver.LinkInfo
}

// ownHoster is one hoster KnightLoader signs in to itself, on the user's own
// premium login, rather than handing the login to JD: Rapidgator, a site of
// the Keep2Share family or Nitroflare. It downloads its links on the stored
// login and names every link of its host in the collector. The login is kept
// from JD (see ownLogin), and its row on the priority card stands for the
// resolver (see dynamicPrio).
type ownHoster struct {
	// id is the routing id of its resolver and backend.
	id string
	// site is the host its login is stored under, by its main domain.
	site      string
	normalize func(string) (string, bool)
	be        ownBackend
	res       linkInspectingResolver
}

// ownSites maps the resolver of every hoster KnightLoader signs in to itself
// to the site its login is stored under.
var ownSites = func() map[string]string {
	m := map[string]string{rapidgator.ResolverID: rapidgator.Site, nitroflare.ResolverID: nitroflare.Site}
	for _, h := range keep2share.Hosts {
		m[h.ID] = h.Site
	}
	return m
}()

// ownSite reports whether host belongs to a hoster KnightLoader signs in to
// itself.
func ownSite(host string) bool {
	host = hostalias.Canonical(host)
	for _, site := range ownSites {
		if site == host {
			return true
		}
	}
	return false
}

// ownLogin reports whether KnightLoader signs in to host with its own client
// rather than handing the login to JD.
func ownLogin(host string) bool {
	if s := xfsSiteFor(host); s != nil {
		return s.OwnLogin
	}
	return ownSite(host)
}

// ownHosters returns the app's hosters KnightLoader signs in to itself, made
// on first use. They live across rewires, which hand them the logins.
func (a *App) ownHosters(eng ownlogin.Downloader) []*ownHoster {
	a.bmu.Lock()
	defer a.bmu.Unlock()
	if a.own != nil {
		return a.own
	}
	hc := httpx.New(httpx.Options{Class: httpx.ClassHosters})
	// A login a hoster has just answered for may open or close the way down a
	// link held for premium only was waiting for.
	rg := rapidgator.NewBackend(rapidgator.NewClient(rapidgator.DefaultSite, hc), eng, a.onUpdate)
	rg.Checked = a.refreshPremiumHolds
	a.own = append(a.own, &ownHoster{id: rapidgator.ResolverID, site: rapidgator.Site,
		normalize: rapidgator.Normalize, be: rg, res: rapidgator.Resolver{Backend: rg}})
	for _, h := range keep2share.Hosts {
		b := keep2share.NewBackend(h, keep2share.NewClient(h.API, hc), eng, a.onUpdate)
		b.Checked = a.refreshPremiumHolds
		a.own = append(a.own, &ownHoster{id: h.ID, site: h.Site, normalize: h.Normalize, be: b, res: keep2share.Resolver{Backend: b}})
	}
	nf := nitroflare.NewBackend(nitroflare.NewClient(nitroflare.DefaultSite, hc), eng, a.onUpdate)
	nf.Checked = a.refreshPremiumHolds
	a.own = append(a.own, &ownHoster{id: nitroflare.ResolverID, site: nitroflare.Site,
		normalize: nitroflare.Normalize, be: nf, res: nitroflare.Resolver{Backend: nf}})
	return a.own
}

// ownHosterFor is the hoster whose resolver is id, nil for none.
func (a *App) ownHosterFor(id string) *ownHoster {
	a.bmu.RLock()
	defer a.bmu.RUnlock()
	for _, h := range a.own {
		if h.id == id {
			return h
		}
	}
	return nil
}

// syncOwnLogins hands each hoster KnightLoader signs in to itself the
// switched-on login stored for it, or none, and has the hoster check one it
// has not seen yet.
func (a *App) syncOwnLogins() {
	for _, h := range a.ownHosters(engineHandoff{a.Engine, a}) {
		var cred accounts.Credential
		for _, host := range a.Accounts.AccountIDs(hosterauth.Service) {
			if hostalias.Canonical(host) == h.site && a.accountEnabled(hosterauth.Service, host) {
				cred, _ = a.Accounts.GetCredential(hosterauth.Service, host)
				break
			}
		}
		if !h.be.SetLogin(cred.Username, cred.Password) {
			continue
		}
		a.spawn(func() {
			if cred.Username != "" {
				h.be.Check(a.ctx)
			}
			a.refreshPremiumHolds()
		})
	}
}

// ownLoginChanged hands the login stored under host to the client that signs
// in with it, after the login was saved, switched or removed.
func (a *App) ownLoginChanged(host string) {
	a.syncOwnLogins()
	a.changedXFSLogin(host)
}

// ownLoginState answers hosterauth for the hosts KnightLoader signs in to
// itself with what the hoster said about the login.
func (a *App) ownLoginState(host string) (hosterauth.OwnState, bool) {
	if site := xfsSiteFor(host); site != nil && site.OwnLogin {
		return a.xfsLoginState(site), true
	}
	if !ownSite(host) {
		return hosterauth.OwnState{}, false
	}
	a.bmu.RLock()
	own := a.own
	a.bmu.RUnlock()
	for _, h := range own {
		if h.site == hostalias.Canonical(host) {
			st := h.be.Status()
			return hosterauth.OwnState{
				Checked: st.Checked, Refused: st.Refused, Detail: st.Err, Captcha: st.Captcha, Premium: st.Premium,
				Expiry: st.Expiry, TrafficLeft: st.TrafficLeft, TrafficMax: st.TrafficMax,
			}, true
		}
	}
	return hosterauth.OwnState{}, true
}

// inspectOwnHosterLinks fills in the name, size and state of every collected
// file link of a hoster KnightLoader signs in to itself, whichever backend is
// to fetch it, and returns the other ids. The hosters answer without a login
// too, so a link JD fetches in free mode is named without JD's plugin.
func (a *App) inspectOwnHosterLinks(ids []string) []string {
	a.bmu.RLock()
	own := a.own
	a.bmu.RUnlock()
	for _, h := range own {
		var rest, targets, urls []string
		a.mu.Lock()
		for _, id := range ids {
			t := a.tasks[id]
			if t == nil {
				continue
			}
			if _, ok := h.normalize(t.URL); !ok {
				rest = append(rest, id)
				continue
			}
			if t.Status == core.StatusCollected && !t.Skipped {
				targets = append(targets, id)
				urls = append(urls, t.URL)
			}
		}
		a.mu.Unlock()
		if len(targets) > 0 {
			a.inspectOwnLinks(h.id, targets, urls, h.res.Inspect)
		}
		ids = rest
	}
	return ids
}
