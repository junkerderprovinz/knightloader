package app

// Premium only. JD has a plugin for premiumHost and nobody holds a login for
// it, so JD would fetch a link there in free mode, and nothing else claims it
// until a test gives it an account. The tests set JD's package-level host
// lists and KL_JD, so none of them runs in parallel.

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/accounts"
	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/hosterauth"
	"github.com/junkerderprovinz/knightloader/internal/resolver/debrid"
	jdresolver "github.com/junkerderprovinz/knightloader/internal/resolver/jd"
	"github.com/junkerderprovinz/knightloader/internal/settings"
)

const premiumHost = "premium-only.example"

// premiumApp is pinApp with JD wired in beside the two debrid services, which
// claim only pinHost, after a first pass over the hoster logins has read JD's
// hoster list. mutate adjusts the settings before they are applied.
func premiumApp(t *testing.T, mutate func(*settings.Settings)) (*App, *pinBackend, map[string]*pinBackend) {
	t.Helper()
	a, jd, bes := premiumAppAtStart(t, mutate)
	jdSidecar(t)
	if _, err := a.hosterAuth().Reconcile(context.Background()); err != nil {
		t.Fatal(err)
	}
	return a, jd, bes
}

// premiumAppAtStart is premiumApp as a process has it right after a restart:
// no pass has asked JD yet, so JD knows no host at all.
func premiumAppAtStart(t *testing.T, mutate func(*settings.Settings)) (*App, *pinBackend, map[string]*pinBackend) {
	t.Helper()
	jdresolver.SetKnownHosts(nil)
	t.Cleanup(func() {
		jdresolver.SetKnownHosts(nil)
		jdresolver.SetHostActive(premiumHost, false)
	})
	a, bes := pinApp(t)
	jd := &pinBackend{got: make(chan string, 4)}
	a.bmu.Lock()
	a.jd = jd
	a.bmu.Unlock()
	a.Registry.Register(jdresolver.Resolver{})
	s := a.Settings.Get()
	mutate(&s)
	if _, err := a.ApplySettings(s); err != nil {
		t.Fatal(err)
	}
	return a, jd, bes
}

// jdSidecar stands in for a JD sidecar with a plugin for premiumHost and a
// valid account at each host in logins, as far as the hoster login pass asks.
// KL_JD is set only once the app is built: at build time it would have
// rewireBackends ask TorBox for its host list over the network.
func jdSidecar(t *testing.T, logins ...string) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var data any
		switch r.URL.Path {
		case "/accounts/listPremiumHoster":
			data = []string{premiumHost}
		case "/accounts/queryAccounts":
			accts := []map[string]any{}
			for i, host := range logins {
				accts = append(accts, map[string]any{"uuid": i + 1, "hostname": host, "infoMap": map[string]any{"valid": true}})
			}
			data = accts
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"data": data})
	}))
	t.Cleanup(srv.Close)
	t.Setenv("KL_JD", srv.URL)
}

func premiumOnly(s *settings.Settings) { s.PremiumOnly = true }

// queueOnPremiumHost queues a link on premiumHost the way the collector
// leaves it: recorded on JD, in free mode.
func queueOnPremiumHost(a *App, id, category, pin string) {
	a.mu.Lock()
	a.tasks[id] = &core.Task{
		ID: id, URL: "https://" + premiumHost + "/file/" + id, Name: id,
		Status: core.StatusQueued, Enabled: true, Resolver: "jd", Mode: core.ModeFree,
		Category: category, ResolverPin: pin,
	}
	a.queue = append(a.queue, id)
	a.mu.Unlock()
}

func taskState(a *App, id string) core.Task {
	a.mu.Lock()
	defer a.mu.Unlock()
	return *a.tasks[id]
}

func isActive(a *App, id string) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.active[id]
}

func wantHeldForPremium(t *testing.T, a *App, id string) {
	t.Helper()
	got := taskState(a, id)
	if got.Status != core.StatusQueued || got.Waiting != core.WaitingPremium {
		t.Errorf("status %q, waiting %q; want it queued and waiting %q", got.Status, got.Waiting, core.WaitingPremium)
	}
}

func TestPremiumOnlyHoldsALinkOnlyAFreeDownloadCouldFetch(t *testing.T) {
	a, jd, _ := premiumApp(t, premiumOnly)
	queueOnPremiumHost(a, "f1", "", "")

	dispatchOnce(a)

	wantNothingHandled(t, jd, "a link went out in free mode with premium only on")
	wantHeldForPremium(t, a, "f1")
	if got := taskState(a, "f1").Error; got != "" {
		t.Errorf("error = %q, want none: nothing has failed", got)
	}
}

// The control: without the switch the same link goes out in free mode.
func TestWithoutPremiumOnlyAFreeDownloadStarts(t *testing.T) {
	a, jd, _ := premiumApp(t, func(*settings.Settings) {})
	queueOnPremiumHost(a, "f1", "", "")

	dispatchOnce(a)

	wantHandled(t, jd, "f1")
}

// After a restart JD's host lists stay empty until the first pass over the
// hoster logins has read them. A held link waits for that pass instead of
// taking JD's silence for a host without a free mode, and the pass then starts
// it on the login it confirms.
func TestAHeldLinkWaitsForTheFirstPassAfterARestart(t *testing.T) {
	a, jd, _ := premiumAppAtStart(t, premiumOnly)
	queueOnPremiumHost(a, "f1", "", "")

	dispatchOnce(a)

	wantNothingHandled(t, jd, "a link went out before JD had said whether it fetches the host for free")
	wantHeldForPremium(t, a, "f1")

	if err := hosterauth.NewStore(a.Accounts).Set(premiumHost, accounts.Credential{Username: "user", Password: "secret"}); err != nil {
		t.Fatal(err)
	}
	jdSidecar(t, premiumHost)
	if _, err := a.hosterAuth().Reconcile(context.Background()); err != nil {
		t.Fatal(err)
	}

	wantHandled(t, jd, "f1")
	if got := taskState(a, "f1").Mode; got != core.ModePremium {
		t.Errorf("mode %q, want it started on the login", got)
	}
}

// Saving a login runs a pass in the background, and the account JD confirms
// there is what the held link was waiting for.
func TestAHeldLinkStartsOnceJDConfirmsALoginForItsHoster(t *testing.T) {
	a, jd, _ := premiumApp(t, premiumOnly)
	queueOnPremiumHost(a, "f1", "", "")
	dispatchOnce(a)
	wantNothingHandled(t, jd, "a link went out in free mode with premium only on")

	jdSidecar(t, premiumHost)
	if err := a.SetHosterLogin(premiumHost, "user", "secret"); err != nil {
		t.Fatal(err)
	}

	wantHandled(t, jd, "f1")
	if got := taskState(a, "f1"); got.Mode != core.ModePremium || got.Waiting != core.WaitingNone {
		t.Errorf("mode %q, waiting %q; want it started in premium mode", got.Mode, got.Waiting)
	}
}

// unlockRecorder is a debrid service that carries premiumHost and reports
// each link it is asked to unlock instead of unlocking it.
type unlockRecorder struct{ got chan string }

func (unlockRecorder) ID() string    { return "alldebrid" }
func (unlockRecorder) Label() string { return "AllDebrid" }
func (unlockRecorder) Hosts(context.Context) (map[string]bool, error) {
	return map[string]bool{premiumHost: true}, nil
}
func (u unlockRecorder) Unlock(_ context.Context, link string) (debrid.Direct, error) {
	u.got <- link
	return debrid.Direct{}, errors.New("a test unlocks nothing")
}

// Saving a debrid key rewires the backends, and the rewired table has the way
// down the held link was waiting for.
func TestAHeldLinkGoesToADebridAccountSavedForItsHoster(t *testing.T) {
	a, _, _ := premiumApp(t, premiumOnly)
	queueOnPremiumHost(a, "f1", "", "")
	dispatchOnce(a)
	wantHeldForPremium(t, a, "f1")

	rec := unlockRecorder{got: make(chan string, 4)}
	orig := debridServices
	t.Cleanup(func() { debridServices = orig })
	debridServices = slices.Clone(orig)
	for i := range debridServices {
		if debridServices[i].id == "alldebrid" {
			debridServices[i].build = func(accounts.Credential) debrid.Service { return rec }
		}
	}
	// With KL_JD set the rewire asks TorBox for its hosters over the network,
	// and a key in the environment would wire accounts this test did not add.
	t.Setenv("KL_JD", "")
	for _, svc := range accounts.Catalogue {
		if svc.Env != "" {
			t.Setenv(svc.Env, "")
		}
	}
	if err := a.SetAccountCredential("alldebrid", "", accounts.Credential{APIKey: "key"}); err != nil {
		t.Fatal(err)
	}

	select {
	case link := <-rec.got:
		if !strings.HasSuffix(link, "/file/f1") {
			t.Errorf("the new account was asked to unlock %q, want the held link", link)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("the held link never went to the account saved for its hoster")
	}
}

// A link a debrid service declined falls to JD, where premium only holds it.
// A debrid service added for the host afterwards has never had the link, so
// the cut above JD does not keep it out, while the one that declined is not
// handed it again when the new one declines too.
func TestAHeldLinkThatFellToJDGoesToADebridServiceAddedLater(t *testing.T) {
	a, jd, bes := premiumApp(t, premiumOnly)
	a.Registry.Register(pinResolver{id: "alldebrid", host: premiumHost, prio: 90})
	queueOnPremiumHost(a, "f1", "", "")
	dispatchOnce(a)
	wantHandled(t, bes["alldebrid"], "f1")

	a.onUpdate("f1", core.Update{Status: core.StatusError, Err: "not my business", Unsupported: true})

	wantNothingHandled(t, jd, "a link went out in free mode with premium only on")
	wantHeldForPremium(t, a, "f1")

	a.Registry.Register(pinResolver{id: "torbox", host: premiumHost, prio: 95})
	a.refreshPremiumHolds()

	wantHandled(t, bes["torbox"], "f1")

	a.onUpdate("f1", core.Update{Status: core.StatusError, Err: "not mine either", Unsupported: true})

	wantNothingHandled(t, jd, "a link went out in free mode with premium only on")
	if got := taskState(a, "f1"); got.Resolver != "jd" || got.Waiting != core.WaitingPremium {
		t.Errorf("resolver %q, waiting %q; want it back on JD, held", got.Resolver, got.Waiting)
	}
}

// An account that is only benched for now is still a way down, and the row
// says so rather than asking for an account that is already there.
func TestABenchedAccountIsNotAMissingOne(t *testing.T) {
	a, jd, bes := premiumApp(t, premiumOnly)
	a.Registry.Register(pinResolver{id: "alldebrid", host: premiumHost, prio: 90})
	a.acctHealthTracker().ReportFailure("alldebrid", "", accounts.HealthInvalid, "test", 0)
	queueOnPremiumHost(a, "f1", "", "")

	dispatchOnce(a)

	wantNothingHandled(t, jd, "a link went out in free mode while its account was benched")
	wantNothingHandled(t, bes["alldebrid"], "a benched account was used")
	if got := taskState(a, "f1").Waiting; got != core.WaitingAccount {
		t.Errorf("waiting = %q, want %q", got, core.WaitingAccount)
	}
}

func TestADrawerMayAllowFreeDownloadsWherePremiumOnlyIsOn(t *testing.T) {
	allow := false
	a, jd, _ := premiumApp(t, func(s *settings.Settings) {
		s.PremiumOnly = true
		s.Categories = []settings.Category{{ID: "anything", PremiumOnly: &allow}}
	})
	queueOnPremiumHost(a, "f1", "anything", "")

	dispatchOnce(a)

	wantHandled(t, jd, "f1")
}

func TestADrawerMayHoldFreeDownloadsWherePremiumOnlyIsOff(t *testing.T) {
	hold := true
	a, jd, _ := premiumApp(t, func(s *settings.Settings) {
		s.Categories = []settings.Category{{ID: "paid", PremiumOnly: &hold}}
	})
	queueOnPremiumHost(a, "f1", "paid", "")

	dispatchOnce(a)

	wantNothingHandled(t, jd, "a link in a drawer that holds free downloads went out in free mode")
	if got := taskState(a, "f1").Waiting; got != core.WaitingPremium {
		t.Errorf("waiting = %q, want %q", got, core.WaitingPremium)
	}
}

// A pin chooses the service, not the mode: pinned to JD, the link still waits
// for a login instead of failing or going out free.
func TestAPinnedLinkWaitsForPremiumToo(t *testing.T) {
	a, jd, _ := premiumApp(t, premiumOnly)
	queueOnPremiumHost(a, "f1", "", "jd")

	dispatchOnce(a)

	wantNothingHandled(t, jd, "a pinned link went out in free mode with premium only on")
	wantHeldForPremium(t, a, "f1")
}

// Switched on while a free download is paused, premium only keeps it from
// going on in free mode. It waits on JD rather than starting over, so the
// login that lets it go resumes it there.
func TestAPausedFreeDownloadWaitsOncePremiumOnlyIsOn(t *testing.T) {
	a, jd, _ := premiumApp(t, func(*settings.Settings) {})
	queueOnPremiumHost(a, "f1", "", "")
	dispatchOnce(a)
	wantHandled(t, jd, "f1")
	a.Pause("f1")
	s := a.Settings.Get()
	s.PremiumOnly = true
	if _, err := a.ApplySettings(s); err != nil {
		t.Fatal(err)
	}

	a.Resume("f1")

	if isActive(a, "f1") {
		t.Error("a paused free download went on in free mode with premium only on")
	}
	wantHeldForPremium(t, a, "f1")

	jdSidecar(t, premiumHost)
	if err := a.SetHosterLogin(premiumHost, "user", "secret"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the download resuming on the login", func() bool { return isActive(a, "f1") })
	if got := taskState(a, "f1").Resolver; got != "jd" {
		t.Errorf("resolver = %q, want it resumed where it started", got)
	}
}

// The collector says what starting a link would do, and stops saying it once
// that has changed.
func TestTheCollectorMarksALinkPremiumOnlyWouldHold(t *testing.T) {
	a, _, _ := premiumApp(t, premiumOnly)

	staged := a.stage("https://"+premiumHost+"/file/c1", "", 0, intake{})
	if staged == nil {
		t.Fatal("the link was not collected")
	}
	if got := taskState(a, staged.ID); got.Status != core.StatusCollected || got.Waiting != core.WaitingPremium {
		t.Fatalf("status %q, waiting %q; want it collected and marked %q", got.Status, got.Waiting, core.WaitingPremium)
	}

	jdSidecar(t, premiumHost)
	if err := a.SetHosterLogin(premiumHost, "user", "secret"); err != nil {
		t.Fatal(err)
	}

	waitFor(t, "the mark going once the hoster has a login", func() bool {
		return taskState(a, staged.ID).Waiting == core.WaitingNone
	})
}

// Switching premium only off takes the mark off the collected links it put
// there, although no link needs its chain walked any more.
func TestSwitchingPremiumOnlyOffClearsTheCollectorsMarks(t *testing.T) {
	a, _, _ := premiumApp(t, premiumOnly)
	staged := a.stage("https://"+premiumHost+"/file/c1", "", 0, intake{})
	if staged == nil {
		t.Fatal("the link was not collected")
	}

	s := a.Settings.Get()
	s.PremiumOnly = false
	if _, err := a.ApplySettings(s); err != nil {
		t.Fatal(err)
	}

	if got := taskState(a, staged.ID).Waiting; got != core.WaitingNone {
		t.Errorf("waiting = %q with premium only off, want the mark gone", got)
	}
}
