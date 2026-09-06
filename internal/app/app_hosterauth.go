package app

// Native hoster logins: KL's own host list, username/password form and
// per-row sync status, backed by internal/hosterauth's reconciler - see that
// package's doc comment for the full design. This file is the thin seam
// between it and the rest of the app: which App instance owns which
// Reconciler, and the handful of methods internal/api's routes call.
//
// Kept at package level rather than as a field on App (app.go), the same
// reason acctMetaMu is (app_accounts.go): app.go's struct is another agent's
// file this wave, and a package-level map gives the same per-instance
// guarantee without touching it. Keyed by *App rather than reference-counted
// or cleaned up on Close: production runs exactly one App for the life of
// the process, and a test suite that constructs many discards each one
// quickly enough that the accumulated entries cost nothing that matters.

import (
	"context"
	"errors"
	"log"
	"os"
	"strings"
	"sync"

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
	// os.Getenv("KL_JD") read live on every reconcile pass, not captured once
	// here - the same reason rewireBackends (app_accounts.go) re-reads it on
	// every call rather than trusting a value from App construction: a
	// container that changes KL_JD, or a headless JD that comes up after this
	// App already started, must be picked up without a restart.
	r := hosterauth.NewReconciler(hosterauth.NewStore(a.Accounts), func() string { return os.Getenv("KL_JD") })
	// The on/off switch, read live from the same account_meta.json every other
	// account's Enabled lives in (jdp, 2026-09-06: "bei den Hoster logins fehlt
	// der aktiviert toggle"). hosterauth files its credentials under the
	// pseudo-service "hosterauth" with the host as the account component, which
	// is exactly the (service, account) pair accountEnabled is keyed by - so
	// this is the same switch, not a parallel one that could disagree.
	r.Enabled = func(host string) bool { return a.accountEnabled(hosterauth.Service, host) }
	hostAuthReg[a] = r
	return r
}

// StartHosterAuth begins the reconcile loop that keeps the headless-JD
// sidecar's own account list in step with what KnightLoader has stored -
// see hosterauth.Reconciler.Run for why this has to be a loop, run again on
// every JD reconnect, and not a one-shot push at boot.
//
// Wired from main.go rather than from App.New (app.go), the same way
// Click'n'Load is: an optional subsystem with its own start, kept out of
// app.go's own lifecycle rather than added to a constructor this wave does
// not own.
//
// Run through a.spawn, like every other long-lived goroutine this package
// starts (see app.go's own doc comment on spawn) - so Close waits for it
// instead of leaving it running against a store that just closed.
func (a *App) StartHosterAuth() {
	a.spawn(func() { a.hosterAuth().Run(a.ctx) })
}

// HosterHosts lists the hosts the "add a login" picker offers.
func (a *App) HosterHosts(ctx context.Context) []hosterauth.Host {
	return a.hosterAuth().Hosts(ctx)
}

// HosterLogins lists every stored native hoster login and its current
// three-way sync status against JD - never the password (see
// hosterauth.LoginState).
func (a *App) HosterLogins() []hosterauth.LoginState {
	return a.hosterAuth().States()
}

// SetHosterLogin stores (or updates) one host's native login and reconciles
// right away, off this goroutine, so the row's status reflects the save
// instead of sitting at "queued" until the next periodic pass - the same
// "changing a credential re-wires immediately" contract
// SetAccountCredential (app_accounts.go) already holds for every other
// credential in this app.
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

// SetHosterLoginEnabled switches one host's login on or off and reconciles
// right away, so JD gains or loses the account within a second rather than at
// the next periodic pass (jdp, 2026-09-06: "bei den Hoster logins fehlt der
// aktiviert toggle").
//
// Off does NOT delete the credential: it is removed from JD's own account list
// and stays sealed in this app's store, so switching it back on needs no
// password retyped. That is the difference between this and the bin next to
// it, and the reason both exist.
func (a *App) SetHosterLoginEnabled(host string, enabled bool) error {
	r := a.hosterAuth()
	if host = strings.ToLower(strings.TrimSpace(host)); host == "" {
		return errors.New("hosterauth: host is required")
	}
	host = strings.TrimPrefix(host, "www.")
	// The same writer every other account's switch goes through, so one file
	// holds one answer per (service, account) - see hosterAuth() above for why
	// the key is the same one hosterauth.Store files the credential under.
	a.SetAccountEnabled(hosterauth.Service, host, enabled)
	if !enabled {
		// Ahead of the reconcile, not instead of it: a switched-off host must
		// stop outranking Direct in the routing table immediately, and
		// Reconcile only pushes SetHostActive for hosts it still has a state
		// for - which a disabled one, absent from `desired`, no longer is.
		jdresolver.SetHostActive(host, false)
	}
	a.spawn(func() {
		if _, err := r.Reconcile(a.ctx); err != nil {
			log.Printf("hosterauth: reconcile after enable/disable failed: %v", err)
		}
	})
	return nil
}

// RemoveHosterLogin clears one host's stored login, and reconciles right
// away so JD's own copy of the account is asked to go too - a credential the
// user deleted here must not keep sitting in JD's config indefinitely.
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
