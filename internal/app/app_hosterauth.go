package app

// Native hoster logins, backed by internal/hosterauth's reconciler. Each App's
// Reconciler lives in a package-level map; entries are never removed, which is
// harmless because production runs a single App.

import (
	"context"
	"errors"
	"log"
	"os"
	"strings"
	"sync"

	"github.com/junkerderprovinz/knightloader/internal/accounts"
	"github.com/junkerderprovinz/knightloader/internal/hosterauth"
	jdresolver "github.com/junkerderprovinz/knightloader/internal/resolver/jd"
)

var (
	hostAuthMu  sync.Mutex
	hostAuthReg = map[*App]*hosterauth.Reconciler{}
)

// hosterAuth returns this App's Reconciler, building it on first use.
func (a *App) hosterAuth() *hosterauth.Reconciler {
	hostAuthMu.Lock()
	defer hostAuthMu.Unlock()
	if r, ok := hostAuthReg[a]; ok {
		return r
	}
	// KL_JD is read on every pass, so a changed variable or a JD that comes up
	// later is picked up without a restart.
	r := hosterauth.NewReconciler(hosterauth.NewStore(a.Accounts), func() string { return os.Getenv("KL_JD") })
	r.Off = func() bool { return a.ModuleOff("jd") }
	// hosterauth files credentials under the "hosterauth" service with the host
	// as account, the same key accountEnabled uses, so this is the same switch
	// as every other account's.
	r.Enabled = func(host string) bool { return a.accountEnabled(hosterauth.Service, host) }
	// A login JD has just confirmed may be the way down a link held for
	// premium only was waiting for, and until JD's hoster list is first read
	// every link JD would fetch is held.
	r.Reconciled = a.refreshPremiumHolds
	hostAuthReg[a] = r
	return r
}

// StartHosterAuth starts the loop that keeps the JD sidecar's account list in
// step with the stored logins (see hosterauth.Reconciler.Run). It runs through
// a.spawn so Close waits for it.
func (a *App) StartHosterAuth() {
	a.spawn(func() { a.hosterAuth().Run(a.ctx) })
}

// HosterHosts lists the hosts the "add a login" picker offers. Debrid services
// are left out because they have their own card, and offering them in both
// places invites configuring one service twice.
func (a *App) HosterHosts(ctx context.Context) []hosterauth.Host {
	skip := debridServiceDomains()
	all := a.hosterAuth().Hosts(ctx)
	out := make([]hosterauth.Host, 0, len(all))
	for _, h := range all {
		if skip[serviceKey(h.ID)] || closedMultihosters[serviceKey(h.ID)] {
			continue
		}
		// Marked rather than removed; see app_multihoster.go.
		h.Multihoster = IsMultihoster(h.ID)
		out = append(out, h)
	}
	return out
}

// serviceKey normalises a hostname for comparison and drops a leading "www.",
// so the catalogue's www.premiumize.me matches JD's premiumize.me. It is kept
// apart from normaliseIconHost because www and the bare domain can serve
// different icons.
func serviceKey(s string) string {
	return strings.TrimPrefix(normaliseIconHost(s), "www.")
}

// debridServiceDomains returns each catalogue debrid service's domain, taken
// from its WhereURL and Domain so there is no second list to keep in sync.
func debridServiceDomains() map[string]bool {
	out := map[string]bool{}
	for _, svc := range accounts.Catalogue {
		if svc.Group != accounts.GroupDebrid {
			continue
		}
		for _, u := range []string{svc.WhereURL, svc.Domain} {
			if h := serviceKey(u); h != "" {
				out[h] = true
			}
		}
	}
	return out
}

// HosterLogins lists every stored hoster login with its sync status against
// JD, never the password.
func (a *App) HosterLogins() []hosterauth.LoginState {
	states := a.hosterAuth().States()
	for i := range states {
		states[i].Multihoster = IsMultihoster(states[i].Host)
	}
	return states
}

// SetHosterLogin stores one host's login and reconciles in the background, so
// the row's status reflects the save right away.
func (a *App) SetHosterLogin(host, username, password string) error {
	r := a.hosterAuth()
	if err := r.SetLogin(host, username, password); err != nil {
		return err
	}
	a.spawn(func() {
		if _, err := r.Reconcile(a.ctx); err != nil {
			log.Printf("hosterauth: reconcile after save failed: %v", err)
		}
	})
	return nil
}

// SetHosterLoginEnabled switches one host's login on or off and reconciles in
// the background. Off removes the account from JD but keeps the credential
// here, so switching it back on needs no password.
func (a *App) SetHosterLoginEnabled(host string, enabled bool) error {
	r := a.hosterAuth()
	if host = strings.ToLower(strings.TrimSpace(host)); host == "" {
		return errors.New("hosterauth: host is required")
	}
	host = strings.TrimPrefix(host, "www.")
	a.SetAccountEnabled(hosterauth.Service, host, enabled)
	if !enabled {
		// Reconcile only updates hosts it still has state for, and a disabled
		// host has none, so it is taken out of routing here.
		jdresolver.SetHostActive(host, false)
	}
	a.spawn(func() {
		if _, err := r.Reconcile(a.ctx); err != nil {
			log.Printf("hosterauth: reconcile after enable/disable failed: %v", err)
		}
	})
	return nil
}

// RemoveHosterLogin deletes one host's login and reconciles in the background
// so JD drops its copy too.
func (a *App) RemoveHosterLogin(host string) error {
	r := a.hosterAuth()
	if err := r.RemoveLogin(host); err != nil {
		return err
	}
	a.spawn(func() {
		if _, err := r.Reconcile(a.ctx); err != nil {
			log.Printf("hosterauth: reconcile after remove failed: %v", err)
		}
	})
	return nil
}
