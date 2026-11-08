package app

// The hoster logins KnightLoader's own clients sign in with. Such a login
// belongs to KnightLoader: hosterauth keeps it from JD through its Own hook
// (ownLoginState), and the client ranks at the login's row of the priority
// card through its resolver's LoginHost (see dynamicPrio). Three kinds share
// that path: the hosters downloaded on a premium login only (ownHoster), the
// XFileSharing sites (app_xfs.go), and Pixeldrain and Gofile, whose login is
// an API key (keyLogins).

import (
	"context"

	"github.com/junkerderprovinz/knightloader/internal/accounts"
	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/hostalias"
	"github.com/junkerderprovinz/knightloader/internal/hosterauth"
	"github.com/junkerderprovinz/knightloader/internal/httpx"
	"github.com/junkerderprovinz/knightloader/internal/ownlogin"
	"github.com/junkerderprovinz/knightloader/internal/resolver"
	"github.com/junkerderprovinz/knightloader/internal/resolver/filer"
	"github.com/junkerderprovinz/knightloader/internal/resolver/keep2share"
	"github.com/junkerderprovinz/knightloader/internal/resolver/nitroflare"
	"github.com/junkerderprovinz/knightloader/internal/resolver/rapidgator"
	"github.com/junkerderprovinz/knightloader/internal/resolver/xfs"
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
// the Keep2Share family, Nitroflare or filer.net. It downloads its links on
// the stored login and names every link of its host in the collector. The
// login is kept from JD (see ownLogin), and its row on the priority card
// stands for the resolver (see dynamicPrio).
type ownHoster struct {
	// id is the routing id of its resolver and backend.
	id string
	// site is the host its login is stored under, by its main domain.
	site      string
	normalize func(string) (string, bool)
	be        ownBackend
	res       linkInspectingResolver
	// free downloads the hoster's links without a login, nil for a hoster
	// whose free downloads stay with JD.
	free *ownFree
}

// ownFree is the free client of a hoster KnightLoader signs in to itself. It
// ranks below the debrid services and above JD's free mode, and its row on
// the priority card is its own.
type ownFree struct {
	id  string
	be  backend
	res resolver.Resolver
}

// handedFree is a free client that claims nothing while its hoster's login
// is with JD, which downloads the links on it.
type handedFree struct {
	resolver.Resolver
	handed func() bool
}

func (r handedFree) Match(url string) bool { return !r.handed() && r.Resolver.Match(url) }

// newOwnFree makes the free client whose captchas are asked for site, and
// which stands aside while handed reports the hoster's login with JD.
func (a *App) newOwnFree(id, site string, res resolver.Resolver, f *ownlogin.Free, be backend, handed func() bool) *ownFree {
	f.Solve = func(ctx context.Context, taskID string, c ownlogin.Captcha) (string, error) {
		return a.askCaptcha(ctx, site, taskID, xfs.Captcha{Kind: xfs.CaptchaKind(c.Kind), SiteKey: c.SiteKey, PageURL: c.PageURL, Image: c.Image})
	}
	return &ownFree{id: id, be: be, res: handedFree{res, handed}}
}

// ownFreeID reports whether id is the resolver id of an own hoster's free
// client. It reads the ids alone, so the app may ask holding its lock.
func ownFreeID(id string) bool {
	if id == rapidgator.FreeResolverID || id == filer.FreeResolverID {
		return true
	}
	for _, h := range keep2share.Hosts {
		if id == h.ID+"-free" {
			return true
		}
	}
	return false
}

// ownSites maps the resolver of every hoster downloaded on a premium login
// only to the site its login is stored under.
var ownSites = func() map[string]string {
	m := map[string]string{rapidgator.ResolverID: rapidgator.Site, nitroflare.ResolverID: nitroflare.Site, filer.ResolverID: filer.Site}
	for _, h := range keep2share.Hosts {
		m[h.ID] = h.Site
	}
	return m
}()

// ownSite reports whether host belongs to a hoster downloaded on a premium
// login only.
func ownSite(host string) bool {
	host = hostalias.Canonical(host)
	for _, site := range ownSites {
		if site == host {
			return true
		}
	}
	return false
}

// keyLogins are the hosts whose stored login is an API key or token
// KnightLoader's own client sends along (see hosterSecret).
var keyLogins = map[string]bool{"pixeldrain.com": true, "gofile.io": true}

// ownLogin reports whether KnightLoader signs in to host with its own client
// rather than handing the login to JD.
func ownLogin(host string) bool {
	if s := xfsSiteFor(host); s != nil {
		return s.OwnLogin
	}
	return ownSite(host) || keyLogins[hostalias.Canonical(host)]
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
	rg.Checked, rg.Moved = a.refreshPremiumHolds, a.ownLoginMoved
	rgFree := rapidgator.NewFreeBackend(rapidgator.NewClient(rapidgator.DefaultSite, hc), eng, a.onUpdate)
	a.own = append(a.own, &ownHoster{id: rapidgator.ResolverID, site: rapidgator.Site,
		normalize: rapidgator.Normalize, be: rg, res: rapidgator.Resolver{Backend: rg},
		free: a.newOwnFree(rapidgator.FreeResolverID, rapidgator.Site, rapidgator.FreeResolver{}, rgFree.Free, rgFree, rg.Handed)})
	for _, h := range keep2share.Hosts {
		b := keep2share.NewBackend(h, keep2share.NewClient(h.API, hc), eng, a.onUpdate)
		b.Checked, b.Moved = a.refreshPremiumHolds, a.ownLoginMoved
		fb := keep2share.NewFreeBackend(h, keep2share.NewClient(h.API, hc), eng, a.onUpdate)
		a.own = append(a.own, &ownHoster{id: h.ID, site: h.Site, normalize: h.Normalize, be: b, res: keep2share.Resolver{Backend: b},
			free: a.newOwnFree(h.ID+"-free", h.Site, keep2share.FreeResolver{Host: h}, fb.Free, fb, b.Handed)})
	}
	nf := nitroflare.NewBackend(nitroflare.NewClient(nitroflare.DefaultSite, hc), eng, a.onUpdate)
	nf.Checked, nf.Moved = a.refreshPremiumHolds, a.ownLoginMoved
	a.own = append(a.own, &ownHoster{id: nitroflare.ResolverID, site: nitroflare.Site,
		normalize: nitroflare.Normalize, be: nf, res: nitroflare.Resolver{Backend: nf}})
	fn := filer.NewBackend(filer.NewClient(filer.DefaultSite, hc), eng, a.onUpdate)
	fn.Checked, fn.Moved = a.refreshPremiumHolds, a.ownLoginMoved
	fnFree := filer.NewFreeBackend(filer.NewClient(filer.DefaultSite, hc), eng, a.onUpdate)
	a.own = append(a.own, &ownHoster{id: filer.ResolverID, site: filer.Site,
		normalize: filer.Normalize, be: fn, res: filer.Resolver{Backend: fn},
		free: a.newOwnFree(filer.FreeResolverID, filer.Site, filer.FreeResolver{}, fnFree.Free, fnFree, fn.Handed)})
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
// in with it, after the login was saved, switched or removed. A login the
// client gave to JD is checked again, which takes it back once the client
// can use it.
func (a *App) ownLoginChanged(host string) {
	a.syncOwnLogins()
	a.bmu.RLock()
	own := a.own
	a.bmu.RUnlock()
	for _, h := range own {
		if h.site == hostalias.Canonical(host) && h.be.Status().Handed {
			a.spawn(func() { h.be.Check(a.ctx) })
		}
	}
	a.changedXFSLogin(host)
}

// ownLoginMoved follows a login one of KnightLoader's own clients gave to JD
// or took back: the reconciler hands it over or takes it off JD at once
// rather than on its next pass, and the links held for premium look again.
func (a *App) ownLoginMoved() {
	a.spawn(func() {
		a.hosterAuth().Pass(a.ctx)
		a.refreshPremiumHolds()
	})
}

// ownLoginState answers hosterauth for the hosts KnightLoader signs in to
// itself with what the hoster said about the login.
func (a *App) ownLoginState(host string) (hosterauth.OwnState, bool) {
	if site := xfsSiteFor(host); site != nil && site.OwnLogin {
		return a.xfsLoginState(site), true
	}
	if keyLogins[hostalias.Canonical(host)] {
		return hosterauth.OwnState{Key: true}, true
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
				Checked: st.Checked, Refused: st.Refused, Detail: st.Err, Captcha: st.Captcha, Handed: st.Handed, Premium: st.Premium,
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
