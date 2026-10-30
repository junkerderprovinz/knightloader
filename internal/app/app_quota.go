package app

// An account's download volume running out, named as that wherever it
// surfaces. A debrid service says so in its own refusal. JDownloader does not:
// with the login for a hoster spent, or with the debrid service ahead of it
// benched, it fetches the link in free mode, and a hoster that wants a captcha
// from free users turns the spent volume into "Skipped - Captcha is required".

import (
	"strings"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/accounts"
	"github.com/junkerderprovinz/knightloader/internal/core"
)

// spentQuotaLocked names the used-up volume behind a captcha JDownloader gave
// up on: its own login for the link's hoster with no traffic left, or a debrid
// service carrying the hoster whose volume is spent. ok is false for a captcha
// nothing explains, which stays a captcha. Caller holds a.mu.
func (a *App) spentQuotaLocked(t *core.Task) (params map[string]string, ok bool) {
	host := siteOf(t.URL)
	if a.hosterAuth().TrafficSpent(host) {
		return map[string]string{"host": host}, true
	}
	for _, res := range a.chainFor(t) {
		service, account, debrid := a.accountForResolverLocked(res.Info().ID)
		if debrid && a.volumeSpent(service, account) {
			return a.quotaParams(host, service, account), true
		}
	}
	return nil, false
}

// volumeSpent reports whether a debrid account has run out of volume: benched
// after refusing a download for it, or read as spent by the last account
// sweep.
func (a *App) volumeSpent(service, account string) bool {
	rec := a.acctHealthTracker().Get(service, account)
	if rec.State == accounts.HealthTempDisabled && containsAny(strings.ToLower(rec.Detail), quotaPhrases) {
		return true
	}
	tr := a.accountHealth(service, account).Traffic
	switch {
	case tr.Unlimited:
		return false
	case tr.Limit > 0:
		return tr.Used >= tr.Limit
	case tr.PercentKnown:
		return tr.UsedPercent >= 100
	}
	return false
}

// quotaParams are the values CodeQuotaUsed is worded with for a debrid
// account. The reset is left out once it has passed, since the last sweep's
// reading is then out of date.
func (a *App) quotaParams(host, service, account string) map[string]string {
	label := service
	if svc, ok := accounts.Lookup(service); ok {
		label = svc.Label
	}
	p := map[string]string{"host": host, "service": label}
	if at := a.quotaResetsAt(service, account); !at.IsZero() {
		p["resetsAt"] = at.Format(time.RFC3339)
	}
	return p
}

// quotaResetsAt is when the account's volume comes back, or zero when the
// service does not say or the time has passed.
func (a *App) quotaResetsAt(service, account string) time.Time {
	at, err := time.Parse(time.RFC3339, a.accountHealth(service, account).Traffic.ResetsAt)
	if err != nil || !at.After(time.Now()) {
		return time.Time{}
	}
	return at
}

// quotaFailureLocked rewords the failure t is settling with, sentence and the
// code it was given, as a spent volume where that is what it is. It also
// reports when the volume comes back, zero when nobody says. A debrid refusal
// is one when its sentence says so; a captcha from JDownloader is one when
// spentQuotaLocked finds the volume behind it. Caller holds a.mu.
func (a *App) quotaFailureLocked(t *core.Task, sentence string, code core.ErrorCode, params map[string]string) (core.ErrorCode, map[string]string, time.Time) {
	if t.Reason == core.ReasonCaptcha && t.Resolver == "jd" {
		p, ok := a.spentQuotaLocked(t)
		if !ok {
			return code, params, time.Time{}
		}
		t.Reason = core.ReasonLimit
		// JD's own sentence, kept as the note, would say captcha beside the
		// status.
		t.Note = ""
		return core.CodeQuotaUsed, p, resetOf(p)
	}
	service, account, debrid := a.accountForResolverLocked(t.Resolver)
	if !debrid || t.Reason != core.ReasonLimit || !containsAny(strings.ToLower(sentence), quotaPhrases) {
		return code, params, time.Time{}
	}
	p := a.quotaParams(siteOf(t.URL), service, account)
	return core.CodeQuotaUsed, p, resetOf(p)
}

func resetOf(p map[string]string) time.Time {
	at, _ := time.Parse(time.RFC3339, p["resetsAt"])
	return at
}
