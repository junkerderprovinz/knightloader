package app

// Account HEALTH STATE MACHINE: turning a service call's failure into a
// signal at the ACCOUNT level instead of only the task it happened to be
// attached to, and the one probe that clears a bench once it expires - see
// internal/accounts/health.go for the state machine itself and
// docs/build-plan.md package 14 for the four rows this file answers to.
//
// This is deliberately NOT the same thing as app_accounts.go's AccountHealth
// (agent 6B's tier/traffic/expiry ticker, "account-health refresher" in the
// build plan's own words) - two different features that ended up sharing a
// name in the plan. To keep the two apart in code as well as in name, every
// symbol here reads acctHealth*/accountRoutable*/bench* rather than
// account_health* or accountHealth*, none of which this file ever spells.
//
// Two existing mechanisms already route around a bad account for a
// different reason, and this file deliberately does not touch either of
// them: rewireBackends (app_accounts.go) un/registers a resolver for a
// MISSING credential or one the user switched off by hand (Enabled), which
// is a decision this file's own tests must not fight - see
// accountRoutableLocked's doc comment. And resolverForTaskLocked /
// dispatchLocked (app_dispatch.go) get exactly the small, additive hooks
// this file exposes, not a rewrite: both are contended files this wave.
//
// The Tracker itself is kept off App's own struct, the same way
// app_hosterauth.go keeps its Reconciler off it and for the identical
// reason - app.go's struct belongs to another agent this wave. A
// package-level map keyed by *App gives the same one-tracker-per-instance
// guarantee without touching it.

import (
	"context"
	"log"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/accounts"
	"github.com/junkerderprovinz/knightloader/internal/core"
)

var (
	acctHealthMu  sync.Mutex
	acctHealthReg = map[*App]*accounts.Tracker{}
)

// acctHealthTracker returns this App's health-state-machine tracker,
// building it on first use. Not cleaned up on Close, the same reasoning
// app_hosterauth.go's hosterAuth() states: production runs exactly one App
// for the life of the process, and a test suite that builds many discards
// each one quickly enough that the accumulated entries cost nothing that
// matters.
func (a *App) acctHealthTracker() *accounts.Tracker {
	acctHealthMu.Lock()
	defer acctHealthMu.Unlock()
	if t, ok := acctHealthReg[a]; ok {
		return t
	}
	// Same data directory acctMetaPath (app_accounts.go) derives from dlDir -
	// account_health.json sits beside accounts.json and account_meta.json,
	// three small files for three different lifetimes of the same
	// (service, account) pair: the secret, the user's own switches, and now
	// what this app has itself observed.
	t := accounts.OpenTracker(filepath.Dir(a.dlDir))
	acctHealthReg[a] = t
	return t
}

// accountForResolverLocked maps a resolver id to the (service, account) its
// credential lives under, or ok=false when the resolver has no tracked
// account at all - JD's sidecar, yt-dlp and the engine's own direct/
// http-fallback resolvers never touch internal/accounts, so nothing about
// their failures belongs at this level.
//
// Only the three one-shot debrid services are listed, and only their
// default account: rewireBackends' own comment explains why - "only a
// service's default account is ever wired into routing" - so (resolver id,
// "") is the whole mapping until a later wave routes named accounts too.
func (a *App) accountForResolverLocked(resolverID string) (service, account string, ok bool) {
	switch resolverID {
	case "alldebrid", "realdebrid", "torbox":
		return resolverID, "", true
	default:
		return "", "", false
	}
}

// accountRoutableLocked reports whether resolverID should still be tried. A
// resolver with no tracked account (accountForResolverLocked's ok=false)
// always answers true - there is nothing here for health to have an opinion
// about, and this must never be the thing that stops jd/ytdlp/the engine
// from being picked.
//
// This is deliberately independent of accountEnabled (app_accounts.go) and
// of whether the resolver is even registered: Enabled is the user's own
// on/off switch and already gates registration in rewireBackends, a
// completely different axis from what THIS app has observed about calls
// actually succeeding or failing. A benched account stays registered (row 2)
// - this is the one place that turns "registered" back into "will actually
// be tried."
func (a *App) accountRoutableLocked(resolverID string) bool {
	svc, acct, ok := a.accountForResolverLocked(resolverID)
	if !ok {
		return true
	}
	return a.acctHealthTracker().Usable(svc, acct)
}

// hasUnroutableMatchLocked reports whether url matches at least one
// registered resolver but every match is currently unroutable. It is the
// condition dispatchLocked holds a task open FOR - queued, no error -
// instead of settling it as "no resolver matches": that message is a lie
// about a link every one of these backends can normally fetch, and the
// forty-hard-errors failure mode row 2 exists to prevent starts exactly
// here, at the one call site that used to treat "picked nothing" as
// "nothing exists."
func (a *App) hasUnroutableMatchLocked(url string) bool {
	chain := a.Registry.All(url)
	if len(chain) == 0 {
		return false
	}
	for _, res := range chain {
		if a.accountRoutableLocked(res.Info().ID) {
			return false
		}
	}
	return true
}

// benchBase/benchMax bound one bench episode. Doubling per consecutive
// episode (benchDelay) is the same shape as app_dispatch.go's retryDelay, at
// a scale that fits hitting a paid API instead of retrying a single
// download: fifteen minutes the first time, capped at six hours so a key
// that has been dead for three days is not still being probed every fifteen
// minutes on day three (row 3 - a retry loop here spends the user's account
// allowance for nothing).
const (
	benchBase = 15 * time.Minute
	benchMax  = 6 * time.Hour
)

// benchDelay grows with each consecutive bench episode - a service still
// down at the third probe is not about to recover at the fourth, and every
// probe is a real call against the user's account.
func benchDelay(episode int) time.Duration {
	if episode < 1 {
		episode = 1
	}
	if episode > 32 { // clamp before the shift below can overflow or go negative
		episode = 32
	}
	d := benchBase * time.Duration(uint64(1)<<uint(episode-1))
	if d <= 0 || d > benchMax {
		d = benchMax
	}
	return d
}

// providerCode upgrades the generic, reason-derived verdict
// (accounts.ClassifyReason) into something more specific, because that
// generic layer only ever gets as far as "this looks like an auth problem" -
// which SERVICE actually sent the failure, and in which words, is the only
// way to tell a wrong key from an unpaid subscription from a geo-block.
// Every needle below was read off the service's own documentation, not
// guessed:
//
//   - AllDebrid (docs.alldebrid.com/#errors): debrid/alldebrid.go's send()
//     puts the code verbatim in parens at the end of the message -
//     fmt.Errorf("alldebrid %s: %s (%s)", path, message, code).
//   - Real-Debrid (api.real-debrid.com): realdebrid.go's do() only ever
//     folds the free-text `error` field into the message, never the numeric
//     `error_code` - the one reliable signal left is the plain "HTTP %d"
//     fallback it emits when that field is empty. Real-Debrid's own docs:
//     401 = "Bad token (expired, invalid)", 403 = "Permission denied
//     (account locked)".
//   - TorBox has no such table here: torbox/client.go's do() never puts an
//     HTTP status OR a documented code string into its error text for a
//     well-formed API error (see that function), so nothing reaching this
//     file can be matched with real confidence - it is left to the generic
//     HealthTempDisabled default on purpose rather than guessed at.
type providerCode struct {
	service string
	needle  string // matched case-insensitively against the failure text
	state   accounts.HealthState
}

var providerCodes = []providerCode{
	{"alldebrid", "auth_bad_apikey", accounts.HealthInvalid},
	{"alldebrid", "auth_missing_apikey", accounts.HealthInvalid},
	// Banned needs a human to appeal it, same actionable outcome as a wrong
	// key - see accounts.HealthInvalid's own doc comment on why this folds
	// in here rather than becoming a fifth state.
	{"alldebrid", "auth_user_banned", accounts.HealthInvalid},
	{"alldebrid", "must_be_premium", accounts.HealthExpired},
	{"alldebrid", "free_trial_limit_reached", accounts.HealthExpired},
	// Geo/IP-blocked: the key itself is fine, this address is not - neither
	// a new key nor waiting fixes it, which is exactly accounts.HealthError.
	{"alldebrid", "auth_blocked", accounts.HealthError},

	{"realdebrid", "http 401", accounts.HealthInvalid},
	{"realdebrid", "http 403", accounts.HealthError},
}

// refineState looks for a provider-specific reason to be more precise than
// the generic verdict accounts.ClassifyReason already gave - see
// providerCodes. It only ever promotes: base must already be
// HealthTempDisabled (the generic, safe default) for a needle to have any
// effect at all, so nothing here can ever turn an unrelated failure into
// HealthInvalid - that guarantee lives structurally in this one guard clause,
// not in how carefully each needle was chosen.
func refineState(service string, base accounts.HealthState, text string) accounts.HealthState {
	if base != accounts.HealthTempDisabled {
		return base
	}
	low := strings.ToLower(text)
	for _, c := range providerCodes {
		if c.service == service && strings.Contains(low, c.needle) {
			return c.state
		}
	}
	return base
}

// reportAccountFailure tells the health tracker about one failed call and,
// the first time it tips an account into HealthTempDisabled, schedules the
// one probe that will ever fire for that bench (scheduleProbe). It answers
// whether the account is not currently routable - true even when THIS
// particular failure did not itself change anything, because a task that
// lands on an already-benched account deserves the same soft handling as
// the one that tripped the bench (row 2: in-flight and queued tasks are held
// for fallback, not failed).
//
// reason is the caller's own already-computed core.Reason for this same
// failure (classify() in app_errors.go) - reused rather than re-derived, so
// the account and the task it came from never disagree about what kind of
// failure this was.
func (a *App) reportAccountFailure(service, account string, reason core.Reason, errText string) (unroutable bool) {
	tr := a.acctHealthTracker()
	base, applicable := accounts.ClassifyReason(reason)
	if !applicable {
		// This failure said nothing about the account - report nothing, but
		// still answer honestly about whatever the account's standing
		// already was, so a task that happens to hit an unrelated dead link
		// on an account that is ALREADY benched still gets the soft
		// handling it needs.
		return !tr.Usable(service, account)
	}
	state := refineState(service, base, errText)
	prev := tr.Get(service, account)
	var benchFor time.Duration
	if state == accounts.HealthTempDisabled {
		benchFor = benchDelay(prev.BenchCount + 1)
	}
	rec, started := tr.ReportFailure(service, account, state, errText, benchFor)
	if started {
		log.Printf("account health: %s/%s -> %s until %s (%s)", service, account, rec.State, rec.BenchedUntil.Format(time.RFC3339), errText)
		a.scheduleProbe(service, account, rec.BenchedUntil)
	}
	return true
}

// reportAccountSuccess clears an account back to healthy.
func (a *App) reportAccountSuccess(service, account string) {
	a.acctHealthTracker().ReportSuccess(service, account)
}

// scheduleProbe fires exactly one health check when a bench expires - never
// a loop (row 3): this is a real call against a paid API. The timer itself
// is not tracked or cancelled by Close - a.spawn already refuses to start
// new work once the app is shutting down (see app.go's own doc comment on
// spawn), which is what makes an untracked timer safe to leave running: it
// either fires before Close and is waited on normally, or fires after and
// simply does nothing.
func (a *App) scheduleProbe(service, account string, until time.Time) {
	d := time.Until(until)
	if d < 0 {
		d = 0
	}
	time.AfterFunc(d, func() {
		a.spawn(func() { a.probeBenchExpiry(service, account, until) })
	})
}

// probeCredential is the seam probeBenchExpiry calls through rather than
// calling checkCredential directly, so a test can prove the probe fires
// exactly once (row 3) without spending a real call against a real debrid
// API - the identical reason app_accounts.go's accountInfoFetcher exists.
// Swapped only by a test, and restored before it returns.
var probeCredential = checkCredential

// probeBenchExpiry is the one probe row 3 requires, for the one bench episode
// identified by until. If the account has since moved on - a person retyped
// the credential, a manual test already cleared or replaced this verdict, or
// the account was switched off in the meantime - there is nothing left to
// prove and the call is not spent.
func (a *App) probeBenchExpiry(service, account string, until time.Time) {
	tr := a.acctHealthTracker()
	cur := tr.Get(service, account)
	if cur.State != accounts.HealthTempDisabled || !cur.BenchedUntil.Equal(until) {
		return // superseded
	}
	if !a.accountEnabled(service, account) {
		return // switched off since the bench started; not worth the call
	}
	svc, known := accounts.Lookup(service)
	if !known {
		return
	}
	cred := a.credentialFor(svc, account)
	if cred.IsZero() {
		return // the credential is gone; nothing left to probe
	}

	ctx, cancel := context.WithTimeout(a.ctx, 15*time.Second)
	defer cancel()
	ok, _, err := probeCredential(ctx, service, cred)
	if ok {
		tr.ReportSuccess(service, account)
		log.Printf("account health: %s/%s recovered", service, account)
		// A resolver that had been skipped by dispatchLocked may claim
		// links again right away - wake the queue rather than leaving
		// whatever is sitting there waiting for the next unrelated event.
		a.mu.Lock()
		a.dispatchLocked()
		a.mu.Unlock()
		return
	}
	errText := ""
	var reason core.Reason
	if err != nil {
		errText = err.Error()
		reason = classify(failure{err: err, text: errText})
	}
	a.reportAccountFailure(service, account, reason, errText)
}
