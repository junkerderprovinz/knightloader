package app

// JDownloader reports a hoster that wants a captcha from free users as
// "Skipped - Captcha is required", and it falls back to free mode when the
// account meant for the link has no volume left. These tests hold the row to
// naming the volume, and to keeping a captcha nothing explains a captcha.

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/accounts"
	"github.com/junkerderprovinz/knightloader/internal/core"
	jdresolver "github.com/junkerderprovinz/knightloader/internal/resolver/jd"
	"github.com/junkerderprovinz/knightloader/internal/settings"
)

const jdCaptchaSkipped = "jd: Skipped - Captcha is required"

// debridLinkSpent is the bench Debrid-Link's refusal leaves, in the words
// of its client.
const debridLinkSpent = "debridlink: debrid-link /downloader/add: the daily traffic limit for this account is used up"

// quotaApp routes rapidgator.net through Debrid-Link first and JDownloader
// after it, as a container with a Debrid-Link key does.
func quotaApp(t *testing.T) *App {
	t.Helper()
	a := retryApp(t, func(*settings.Settings) {})
	isolateResolvers(a,
		fakeResolver{id: "debridlink", prio: 10, host: "rapidgator.net"},
		fakeResolver{id: "jd", prio: 50, host: "rapidgator.net"},
	)
	return a
}

func failCaptchaOnJD(a *App, id string) core.Task {
	runningOn(a, id, "rapidgator.net", "jd")
	a.onUpdate(id, core.Update{Status: core.StatusError, Err: jdCaptchaSkipped, Note: "Skipped - Captcha is required"})
	return liveTask(a, id)
}

func TestACaptchaBehindASpentDebridVolumeNamesTheVolume(t *testing.T) {
	a := quotaApp(t)
	reset := time.Now().Add(9 * time.Hour).UTC().Truncate(time.Second)
	st := a.healthState()
	st.mu.Lock()
	st.rows[metaKey("debridlink", "")] = AccountHealth{Tier: "premium", Traffic: TrafficState{
		UsedPercent: 70, PercentKnown: true, ResetsAt: reset.Format(time.RFC3339),
	}}
	st.mu.Unlock()
	a.reportAccountFailure("debridlink", "", core.ReasonLimit, debridLinkSpent)

	got := failCaptchaOnJD(a, "q1")

	if got.Reason != core.ReasonLimit || got.ErrorCode != core.CodeQuotaUsed {
		t.Fatalf("Reason, ErrorCode = %q, %q; want %q, %q", got.Reason, got.ErrorCode, core.ReasonLimit, core.CodeQuotaUsed)
	}
	want := map[string]string{"host": "rapidgator.net", "service": "Debrid-Link", "resetsAt": reset.Format(time.RFC3339)}
	for k, v := range want {
		if got.ErrorParams[k] != v {
			t.Errorf("ErrorParams[%q] = %q, want %q", k, got.ErrorParams[k], v)
		}
	}
	if got.Note != "" {
		t.Errorf("Note = %q; the status column would still say captcha", got.Note)
	}
	if got.GaveUp {
		t.Error("gave up, while the volume comes back by itself")
	}
	if d := got.NextTry.Sub(reset); d < -time.Second || d > time.Second {
		t.Errorf("NextTry = %s, want the reset at %s; every attempt before it meets the same refusal", got.NextTry, reset)
	}
	if !strings.Contains(got.Error, "Captcha is required") {
		t.Errorf("Error = %q; JDownloader's own sentence is the evidence and stays", got.Error)
	}
}

func TestACaptchaWithNoSpentAccountBehindItStaysACaptcha(t *testing.T) {
	a := quotaApp(t)
	// Benched for something other than its volume.
	a.acctHealthTracker().ReportFailure("debridlink", "", accounts.HealthTempDisabled,
		"debridlink: too many requests: Debrid-Link rate-limited this account, try again in an hour", time.Hour)

	got := failCaptchaOnJD(a, "c1")

	if got.Reason != core.ReasonCaptcha || got.ErrorCode != core.CodeCaptcha {
		t.Fatalf("Reason, ErrorCode = %q, %q; want a captcha", got.Reason, got.ErrorCode)
	}
	if !got.GaveUp {
		t.Error("a captcha was set to retry, and time does not answer one")
	}
	if got.Note == "" {
		t.Error("JDownloader's note went missing from a real captcha")
	}
}

// fakeJDAccounts answers JDownloader's account list with one rapidgator.net
// login and the traffic given.
func fakeJDAccounts(t *testing.T, left, max int64) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/accounts/queryAccounts" {
			http.Error(w, "not here", http.StatusInternalServerError)
			return
		}
		_, _ = w.Write([]byte(`{"data":[{"uuid":1,"hostname":"rapidgator.net","infoMap":{"username":"u","enabled":true,` +
			`"valid":true,"validUntil":1893456000000,"trafficLeft":` + strconv.FormatInt(left, 10) + `,"trafficMax":` + strconv.FormatInt(max, 10) + `}}]}`))
	}))
	t.Cleanup(srv.Close)
	t.Setenv("KL_JD", srv.URL)
	t.Cleanup(func() { jdresolver.SetHostActive("rapidgator.net", false) })
}

func TestACaptchaBehindASpentHosterLoginNamesTheLogin(t *testing.T) {
	for _, c := range []struct {
		name      string
		left      int64
		wantQuota bool
	}{
		{name: "no traffic left", left: 0, wantQuota: true},
		{name: "traffic left", left: 20 << 30},
	} {
		t.Run(c.name, func(t *testing.T) {
			fakeJDAccounts(t, c.left, 50<<30)
			a := quotaApp(t)
			r := a.hosterAuth()
			if err := r.SetLogin("rapidgator.net", "u", "p"); err != nil {
				t.Fatal(err)
			}
			if _, err := r.Reconcile(context.Background()); err != nil {
				t.Fatal(err)
			}

			got := failCaptchaOnJD(a, "h1")

			if !c.wantQuota {
				if got.ErrorCode != core.CodeCaptcha {
					t.Errorf("ErrorCode = %q, want a captcha while the login has traffic", got.ErrorCode)
				}
				return
			}
			if got.ErrorCode != core.CodeQuotaUsed {
				t.Fatalf("ErrorCode = %q, want %q", got.ErrorCode, core.CodeQuotaUsed)
			}
			if got.ErrorParams["host"] != "rapidgator.net" || got.ErrorParams["service"] != "" {
				t.Errorf("ErrorParams = %v, want the hoster and no service, since the login is the hoster's own", got.ErrorParams)
			}
		})
	}
}

// A task pinned to a debrid service stays there, so its row is the one place
// the service's own refusal is shown, and it names the volume rather than an
// unusable account.
func TestAPinnedDebridTaskNamesTheSpentVolume(t *testing.T) {
	a := quotaApp(t)
	runningOn(a, "p1", "rapidgator.net", "debridlink")
	a.mu.Lock()
	a.tasks["p1"].ResolverPin = "debridlink"
	a.mu.Unlock()

	a.onUpdate("p1", core.Update{Status: core.StatusError, Err: debridLinkSpent, Code: core.CodeDebridRefused,
		Params: map[string]string{"service": "Debrid-Link"}})

	got := liveTask(a, "p1")
	if got.Reason != core.ReasonLimit || got.ErrorCode != core.CodeQuotaUsed {
		t.Fatalf("Reason, ErrorCode = %q, %q; want %q, %q", got.Reason, got.ErrorCode, core.ReasonLimit, core.CodeQuotaUsed)
	}
	if got.ErrorParams["service"] != "Debrid-Link" || got.ErrorParams["host"] != "rapidgator.net" {
		t.Errorf("ErrorParams = %v", got.ErrorParams)
	}
}

// The debrid clients' own sentences for a spent volume are read as a limit,
// and only those: a rate limit is a limit, but no volume.
func TestTheDebridVolumeSentencesReadAsSpentVolume(t *testing.T) {
	for _, s := range []string{
		"debridlink: debrid-link /downloader/add: the daily link limit for this account is used up",
		"bestdebrid: the daily limit for this hoster is used up",
		"neodebrid: the account has no traffic left",
		"multiup: this account has reached a download limit",
		"torbox /api/webdl/createwebdownload: MONTHLY_LIMIT You have reached your monthly download limit.",
	} {
		if got := classify(failure{text: s}); got != core.ReasonLimit {
			t.Errorf("classify(%q) = %q, want %q", s, got, core.ReasonLimit)
		}
		if !containsAny(strings.ToLower(s), quotaPhrases) {
			t.Errorf("%q is not read as a spent volume", s)
		}
	}
	if containsAny("debridlink: too many requests: debrid-link rate-limited this account", quotaPhrases) {
		t.Error("a rate limit is read as a spent volume")
	}
}
