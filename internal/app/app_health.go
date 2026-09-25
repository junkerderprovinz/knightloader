package app

// Account health: a failed service call benches the account, not just the task
// it happened on, and a single probe clears the bench when it expires. A
// service that has only switched off one site is benched for that site alone.
// The state machine is internal/accounts/health.go. This is unrelated to
// AccountHealth in app_accounts.go (tier, traffic and expiry), which is why
// everything here is named acctHealth*, accountRoutable* or bench*.
//
// It does not touch the user's own Enabled switch, which rewireBackends
// applies. Trackers live in a package-level map keyed by *App.

import (
	"context"
	"log"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/accounts"
	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/hostalias"
	"github.com/junkerderprovinz/knightloader/internal/resolver"
)

var (
	acctHealthMu  sync.Mutex
	acctHealthReg = map[*App]*accounts.Tracker{}
)

// acctHealthTracker returns this App's tracker, building it on first use.
func (a *App) acctHealthTracker() *accounts.Tracker {
	acctHealthMu.Lock()
	defer acctHealthMu.Unlock()
	if t, ok := acctHealthReg[a]; ok {
		return t
	}
	// account_health.json sits beside accounts.json and account_meta.json.
	t := accounts.OpenTracker(filepath.Dir(a.dlDir))
	acctHealthReg[a] = t
	return t
}

// accountForResolverLocked maps a resolver id to the (service, account) its
// credential lives under, or ok=false for resolvers without a tracked account
// (JD, yt-dlp, the engine's own). The account is part of the id (see
// resolver.SlotID), so one of two keys for a service can be benched alone.
//
// "remotefs" is left out: it holds one account per host, and with no host to
// name here, a single server going down would bench every remote server.
func (a *App) accountForResolverLocked(resolverID string) (service, account string, ok bool) {
	service, account = resolver.SplitSlot(resolverID)
	if !isDebridService(service) {
		return "", "", false
	}
	return service, account, true
}

// accountRoutableLocked reports whether resolverID should still be tried. A
// resolver without a tracked account always answers true. A benched account
// stays registered; this is where it is skipped.
func (a *App) accountRoutableLocked(resolverID string) bool {
	svc, acct, ok := a.accountForResolverLocked(resolverID)
	if !ok {
		return true
	}
	return a.acctHealthTracker().Usable(svc, acct)
}

// routableForLocked is accountRoutableLocked for one link: the account must be
// usable, and the service must not have switched off the link's site. Caller
// holds a.mu.
func (a *App) routableForLocked(resolverID, url string) bool {
	return a.accountRoutableLocked(resolverID) && !a.siteBenchedLocked(resolverID, url)
}

// hasUnroutableMatchLocked reports whether the task's link matches at least
// one resolver still open to it (see chainFromLocked) but every such match is
// currently unroutable. dispatchLocked then keeps the task queued instead of
// failing it with "no resolver matches".
func (a *App) hasUnroutableMatchLocked(t *core.Task) bool {
	chain := a.chainFromLocked(t, rankedChain(a.Registry.All(t.URL), t.URL, a.Settings.Get().ResolverOrder))
	if len(chain) == 0 {
		return false
	}
	for _, res := range chain {
		if a.routableForLocked(res.Info().ID, t.URL) {
			return false
		}
	}
	return true
}

// serviceSite is one service at one site. A service switches a site off for
// every account, so the bench covers all of its slots.
type serviceSite struct{ service, site string }

func serviceSiteOf(resolverID, url string) serviceSite {
	service, _ := resolver.SplitSlot(resolverID)
	return serviceSite{service, siteOf(url)}
}

// siteBenchLength is how long a service is passed over for a site it said it
// has switched off. The site comes back on the service's own schedule, and the
// next link after the bench finds out with one call. A var so tests need not
// wait it out.
var siteBenchLength = benchBase

// siteOf names a link's site by its main domain, so rg.to and rapidgator.net
// are benched together.
func siteOf(url string) string { return hostalias.Canonical(hostOf(url)) }

// benchSiteLocked passes resolverID's service over for url's site for
// siteBenchLength, leaving its accounts and every other site alone, and
// dispatches once the bench is over so a task held for it goes back. A bench
// already running keeps its end. Caller holds a.mu.
func (a *App) benchSiteLocked(resolverID, url string) {
	if a.siteBenchedLocked(resolverID, url) {
		return
	}
	key := serviceSiteOf(resolverID, url)
	a.siteBench[key] = time.Now().Add(siteBenchLength)
	log.Printf("%s has switched off %s; other backends take its links for %s", key.service, key.site, siteBenchLength)
	// As with scheduleProbe, a.spawn refuses the work once shutdown has begun.
	time.AfterFunc(siteBenchLength, func() {
		a.spawn(func() {
			a.mu.Lock()
			a.dispatchLocked()
			a.mu.Unlock()
		})
	})
}

// siteBenchedLocked reports whether resolverID's service is passed over for
// url's site. Caller holds a.mu.
func (a *App) siteBenchedLocked(resolverID, url string) bool {
	return time.Now().Before(a.siteBench[serviceSiteOf(resolverID, url)])
}

// benchBase and benchMax bound one bench episode. Each probe is a real call
// against a paid API, so the delay doubles per consecutive episode up to six
// hours.
const (
	benchBase = 15 * time.Minute
	benchMax  = 6 * time.Hour
)

// benchDelay returns the bench length for the given consecutive episode.
func benchDelay(episode int) time.Duration {
	if episode < 1 {
		episode = 1
	}
	if episode > 32 { // keeps the shift below from overflowing
		episode = 32
	}
	d := benchBase * time.Duration(uint64(1)<<uint(episode-1))
	if d <= 0 || d > benchMax {
		d = benchMax
	}
	return d
}

// providerCode refines the generic verdict from accounts.ClassifyReason with a
// service's own error text, which is the only way to tell a wrong key from an
// unpaid subscription. The needles come from the services' documentation:
//
//   - AllDebrid (docs.alldebrid.com/#errors): send() puts the error code in
//     parentheses at the end of the message.
//   - Real-Debrid: do() only includes the free-text error, so the "HTTP %d"
//     fallback is the reliable signal (401 bad token, 403 account locked).
//   - TorBox: its errors carry neither a status nor a documented code, so it
//     keeps the generic HealthTempDisabled.
type providerCode struct {
	service string
	needle  string // matched case-insensitively against the failure text
	state   accounts.HealthState
}

var providerCodes = []providerCode{
	{"alldebrid", "auth_bad_apikey", accounts.HealthInvalid},
	{"alldebrid", "auth_missing_apikey", accounts.HealthInvalid},
	// A ban needs a human to appeal, the same outcome as a wrong key.
	{"alldebrid", "auth_user_banned", accounts.HealthInvalid},
	{"alldebrid", "must_be_premium", accounts.HealthExpired},
	{"alldebrid", "free_trial_limit_reached", accounts.HealthExpired},
	// Geo or IP block: neither a new key nor waiting helps.
	{"alldebrid", "auth_blocked", accounts.HealthError},

	{"realdebrid", "http 401", accounts.HealthInvalid},
	{"realdebrid", "http 403", accounts.HealthError},
}

// refineState applies providerCodes. It only refines HealthTempDisabled, so no
// needle can turn an unrelated verdict into HealthInvalid.
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

// reportAccountFailure records one failed call and, when it starts a bench,
// schedules the probe that ends it. It reports whether the account is
// currently unroutable, even if this failure changed nothing, so every task on
// a benched account is held for fallback rather than failed. reason is the
// caller's classify() result, so account and task agree on the cause.
func (a *App) reportAccountFailure(service, account string, reason core.Reason, errText string) (unroutable bool) {
	tr := a.acctHealthTracker()
	if a.ctx != nil && a.ctx.Err() != nil {
		// A download abandoned at Close can still report in. The tracker
		// writes straight into the data directory, which after shutdown may
		// be mid-removal, so nothing is written.
		return !tr.Usable(service, account)
	}
	base, applicable := accounts.ClassifyReason(reason)
	if !applicable {
		// Nothing about the account, but an already benched account still
		// needs the soft handling.
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

// reportAccountSuccess clears an account back to healthy. It writes nothing
// after shutdown, for the same reason as reportAccountFailure.
func (a *App) reportAccountSuccess(service, account string) {
	if a.ctx != nil && a.ctx.Err() != nil {
		return
	}
	a.acctHealthTracker().ReportSuccess(service, account)
}

// scheduleProbe fires one health check when a bench expires, never a loop. The
// timer is not cancelled by Close; a.spawn refuses new work once shutdown has
// begun, so a late timer does nothing.
func (a *App) scheduleProbe(service, account string, until time.Time) {
	d := time.Until(until)
	if d < 0 {
		d = 0
	}
	time.AfterFunc(d, func() {
		a.spawn(func() { a.probeBenchExpiry(service, account, until) })
	})
}

// probeCredential is replaced in tests so they spend no real API call.
var probeCredential = checkCredential

// probeBenchExpiry probes the account for the bench that ends at until. When
// the verdict has changed since, the account was switched off or its
// credential is gone, the call is not made.
func (a *App) probeBenchExpiry(service, account string, until time.Time) {
	tr := a.acctHealthTracker()
	cur := tr.Get(service, account)
	if cur.State != accounts.HealthTempDisabled || !cur.BenchedUntil.Equal(until) {
		return
	}
	if !a.accountEnabled(service, account) {
		return
	}
	svc, known := accounts.Lookup(service)
	if !known {
		return
	}
	cred := a.credentialFor(svc, account)
	if cred.IsZero() {
		return
	}

	ctx, cancel := context.WithTimeout(a.ctx, 15*time.Second)
	defer cancel()
	ok, _, err := probeCredential(ctx, service, cred)
	if ok {
		tr.ReportSuccess(service, account)
		log.Printf("account health: %s/%s recovered", service, account)
		// Wake the queue so waiting tasks can use the account right away.
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
