package app

// Account health at the App level: tasks on a benched account are held for
// fallback rather than failed, the bench-expiry probe fires once and never
// loops, and an unrecognised failure stays HealthTempDisabled. The resolvers
// are fakes, so no test reaches a real debrid API.

import (
	"context"
	"errors"
	"net/url"
	"sync/atomic"
	"testing"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/accounts"
	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/resolver"
)

// fakeResolver claims exactly one host under a given resolver id.
type fakeResolver struct {
	id   string
	prio int
	host string
}

func (f fakeResolver) Info() resolver.Info { return resolver.Info{ID: f.id, Prio: f.prio} }

func (f fakeResolver) Match(raw string) bool {
	u, err := url.Parse(raw)
	return err == nil && u.Hostname() == f.host
}

func (f fakeResolver) Resolve(_ context.Context, req resolver.Request) (resolver.Result, error) {
	return resolver.Result{DirectURL: req.URL, Name: req.URL}, nil
}

// isolateResolvers replaces a's registry with one holding only the given
// resolvers. New registers Direct and HTTPFallback, which match nearly any URL
// and carry no account, so they would hide every health effect. It must run
// right after New, before anything dispatches.
func isolateResolvers(a *App, resolvers ...resolver.Resolver) {
	a.Registry = resolver.NewRegistry()
	for _, r := range resolvers {
		a.Registry.Register(r)
	}
}

func TestAccountForResolverMapping(t *testing.T) {
	a := newAccountsTestApp(t)

	for _, id := range []string{"alldebrid", "realdebrid", "torbox"} {
		if _, _, ok := a.accountForResolverLocked(id); !ok {
			t.Errorf("accountForResolverLocked(%q) ok = false, want true", id)
		}
	}
	for _, id := range []string{"jd", "ytdlp", "direct", "http", ""} {
		if _, _, ok := a.accountForResolverLocked(id); ok {
			t.Errorf("accountForResolverLocked(%q) ok = true, want false since this resolver has no tracked account", id)
		}
		if !a.accountRoutableLocked(id) {
			t.Errorf("accountRoutableLocked(%q) = false, want true: a resolver with no tracked account must never be blocked by health", id)
		}
	}
}

func TestBenchDelayGrowsAndCaps(t *testing.T) {
	if got := benchDelay(1); got != benchBase {
		t.Errorf("benchDelay(1) = %v, want %v", got, benchBase)
	}
	if got := benchDelay(2); got != 2*benchBase {
		t.Errorf("benchDelay(2) = %v, want %v", got, 2*benchBase)
	}
	if got := benchDelay(0); got != benchBase {
		t.Errorf("benchDelay(0) = %v, want the base delay (clamped), not zero or negative", got)
	}
	if got := benchDelay(1000); got != benchMax {
		t.Errorf("benchDelay(1000) = %v, want it capped at %v", got, benchMax)
	}
}

func TestRefineStateDefaultsSafely(t *testing.T) {
	cases := []struct{ service, text string }{
		{"alldebrid", "alldebrid /link/unlock: Server error (INTERNAL_ERROR)"},
		{"alldebrid", "alldebrid /link/unlock: HTTP 500"},
		{"realdebrid", "realdebrid /unrestrict/link: Slow down"},
		{"realdebrid", "realdebrid /unrestrict/link: HTTP 429"},
		{"torbox", "torbox /api/webdl/createwebdownload: map[] api key invalid"},
	}
	for _, c := range cases {
		if got := refineState(c.service, accounts.HealthTempDisabled, c.text); got != accounts.HealthTempDisabled {
			t.Errorf("refineState(%q, TempDisabled, %q) = %q, want %q (unrecognised text must never promote to something more specific)", c.service, c.text, got, accounts.HealthTempDisabled)
		}
	}
}

func TestRefineStatePromotesVerifiedCodes(t *testing.T) {
	cases := []struct {
		service, text string
		want          accounts.HealthState
	}{
		{"alldebrid", "alldebrid /link/unlock: The auth apikey is invalid (AUTH_BAD_APIKEY)", accounts.HealthInvalid},
		{"alldebrid", "alldebrid /link/unlock: You must be premium to process this link (MUST_BE_PREMIUM)", accounts.HealthExpired},
		{"alldebrid", "alldebrid /link/unlock: This apikey is geo-blocked or ip-blocked (AUTH_BLOCKED)", accounts.HealthError},
		{"realdebrid", "realdebrid /unrestrict/link: HTTP 401", accounts.HealthInvalid},
		{"realdebrid", "realdebrid /unrestrict/link: HTTP 403", accounts.HealthError},
	}
	for _, c := range cases {
		if got := refineState(c.service, accounts.HealthTempDisabled, c.text); got != c.want {
			t.Errorf("refineState(%q, TempDisabled, %q) = %q, want %q", c.service, c.text, got, c.want)
		}
	}
	// One service's needle must not fire on another's failure.
	if got := refineState("realdebrid", accounts.HealthTempDisabled, "auth_bad_apikey"); got != accounts.HealthTempDisabled {
		t.Errorf("refineState leaked an AllDebrid needle onto realdebrid: got %q, want %q", got, accounts.HealthTempDisabled)
	}
	// Only TempDisabled is ever refined.
	if got := refineState("alldebrid", "", "AUTH_BAD_APIKEY"); got != "" {
		t.Errorf("refineState promoted a non-TempDisabled base (%q) using a needle match; it must only ever refine TempDisabled", got)
	}
}

func TestQueuedTaskOnBenchedAccountIsHeldNotFailed(t *testing.T) {
	a := newAccountsTestApp(t)
	const testURL = "https://only-alldebrid.example/file.bin"
	isolateResolvers(a, fakeResolver{id: "alldebrid", prio: 34, host: "only-alldebrid.example"})

	if _, started := a.acctHealthTracker().ReportFailure("alldebrid", "", accounts.HealthTempDisabled, "seed", time.Hour); !started {
		t.Fatal("setup: expected the bench to start")
	}

	task := &core.Task{ID: "t1", URL: testURL, Status: core.StatusQueued, Enabled: true}
	a.mu.Lock()
	a.tasks[task.ID] = task
	a.queue = append(a.queue, task.ID)
	a.dispatchLocked()
	status, reason := task.Status, task.Reason
	stillQueued := false
	for _, id := range a.queue {
		if id == task.ID {
			stillQueued = true
		}
	}
	a.mu.Unlock()

	if status == core.StatusError {
		t.Fatalf("queued task on a benched, solely-matching account settled as StatusError (reason %q); it must be held, not failed", reason)
	}
	if !stillQueued {
		t.Fatal("the task left the queue without being dispatched or settled, so it was lost")
	}
	if reason == core.ReasonUnsupported {
		t.Error("task carries ReasonUnsupported, which is untrue for a link this resolver can normally fetch while only its account is unavailable")
	}
}

func TestQueuedTaskFallsBackWhenAnotherAccountCanTakeIt(t *testing.T) {
	a := newAccountsTestApp(t)
	const testURL = "https://two-debrids.example/file.bin"
	isolateResolvers(a,
		fakeResolver{id: "alldebrid", prio: 34, host: "two-debrids.example"},
		fakeResolver{id: "realdebrid", prio: 33, host: "two-debrids.example"},
	)

	if _, started := a.acctHealthTracker().ReportFailure("alldebrid", "", accounts.HealthTempDisabled, "seed", time.Hour); !started {
		t.Fatal("setup: expected the bench to start")
	}

	task := &core.Task{ID: "t1", URL: testURL, Status: core.StatusQueued, Enabled: true}
	a.mu.Lock()
	a.tasks[task.ID] = task
	a.queue = append(a.queue, task.ID)
	a.dispatchLocked()
	res := task.Resolver
	a.mu.Unlock()

	if res != "realdebrid" {
		t.Fatalf("task.Resolver = %q, want realdebrid since the healthy account should have been picked over the benched one", res)
	}
}

// TestInFlightTaskFallsBackWhenBenched: a task failing on a benched account
// moves to the next resolver in the chain, like an explicit u.Unsupported.
func TestInFlightTaskFallsBackWhenBenched(t *testing.T) {
	a := newAccountsTestApp(t)
	const testURL = "https://two-debrids-live.example/file.bin"
	isolateResolvers(a,
		fakeResolver{id: "alldebrid", prio: 34, host: "two-debrids-live.example"},
		fakeResolver{id: "realdebrid", prio: 33, host: "two-debrids-live.example"},
	)

	task := &core.Task{ID: "t1", URL: testURL, Resolver: "alldebrid", Status: core.StatusRunning}
	a.mu.Lock()
	a.tasks[task.ID] = task
	a.active[task.ID] = true
	a.started[task.ID] = true
	a.mu.Unlock()

	// ReasonUnknown, which ClassifyReason still treats as applicable.
	a.onUpdate(task.ID, core.Update{Status: core.StatusError, Err: "alldebrid: an unexpected failure"})

	a.mu.Lock()
	status, res := task.Status, task.Resolver
	a.mu.Unlock()

	if status == core.StatusError {
		t.Fatalf("in-flight task on a now-benched account settled as StatusError; it must be held for fallback")
	}
	if status != core.StatusQueued {
		t.Errorf("status = %q, want %q", status, core.StatusQueued)
	}
	if res != "realdebrid" {
		t.Errorf("task.Resolver = %q, want realdebrid (the fallback chain picking it up)", res)
	}
	if a.acctHealthTracker().Usable("alldebrid", "") {
		t.Error("alldebrid should now read benched after this failure")
	}
}

func TestInFlightTaskHeldWhenNoFallbackExists(t *testing.T) {
	a := newAccountsTestApp(t)
	const testURL = "https://only-alldebrid-live.example/file.bin"
	isolateResolvers(a, fakeResolver{id: "alldebrid", prio: 34, host: "only-alldebrid-live.example"})

	task := &core.Task{ID: "t1", URL: testURL, Resolver: "alldebrid", Status: core.StatusRunning}
	a.mu.Lock()
	a.tasks[task.ID] = task
	a.active[task.ID] = true
	a.started[task.ID] = true
	a.mu.Unlock()

	a.onUpdate(task.ID, core.Update{Status: core.StatusError, Err: "alldebrid: an unexpected failure"})

	a.mu.Lock()
	status := task.Status
	held := false
	for _, id := range a.queue {
		if id == task.ID {
			held = true
		}
	}
	a.mu.Unlock()

	if status == core.StatusError {
		t.Fatal("in-flight task with no fallback available settled as StatusError; it must be held for the account to recover")
	}
	if !held {
		t.Fatalf("task status = %q but it is not sitting in the queue, so it was lost rather than held", status)
	}
}

// TestUnsupportedFallbackUnaffectedByAccountHealth: u.Unsupported fallback keeps
// working for resolvers without a tracked account.
func TestUnsupportedFallbackUnaffectedByAccountHealth(t *testing.T) {
	a := newAccountsTestApp(t)
	const testURL = "https://media.example/watch/x"
	isolateResolvers(a,
		fakeResolver{id: "ytdlp", prio: 10, host: "media.example"},
		fakeResolver{id: "http", prio: -100, host: "media.example"},
	)

	task := &core.Task{ID: "t1", URL: testURL, Resolver: "ytdlp", Status: core.StatusRunning}
	a.mu.Lock()
	a.tasks[task.ID] = task
	a.active[task.ID] = true
	a.started[task.ID] = true
	a.mu.Unlock()

	a.onUpdate(task.ID, core.Update{Status: core.StatusError, Err: "yt-dlp: Unsupported URL", Unsupported: true})

	a.mu.Lock()
	res := task.Resolver
	a.mu.Unlock()
	if res != "http" {
		t.Errorf("task.Resolver = %q, want http since an unrelated resolver's own fallback must be untouched by account health", res)
	}
}

// TestProbeFiresExactlyOnceAtExpiry runs through the real timer and spawn
// wiring: one probe per expiry, even when that probe fails.
func TestProbeFiresExactlyOnceAtExpiry(t *testing.T) {
	a := newAccountsTestApp(t)
	if err := a.Accounts.SetCredential("alldebrid", "", accounts.Credential{APIKey: "fake-key"}); err != nil {
		t.Fatal(err)
	}

	var calls int32
	orig := probeCredential
	probeCredential = func(ctx context.Context, service string, cred accounts.Credential) (bool, int, error) {
		atomic.AddInt32(&calls, 1)
		return false, 0, errors.New("still down")
	}
	t.Cleanup(func() { probeCredential = orig })

	rec, started := a.acctHealthTracker().ReportFailure("alldebrid", "", accounts.HealthTempDisabled, "seed", 20*time.Millisecond)
	if !started {
		t.Fatal("setup: expected the bench to start")
	}
	a.scheduleProbe("alldebrid", "", rec.BenchedUntil)

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) && atomic.LoadInt32(&calls) == 0 {
		time.Sleep(10 * time.Millisecond)
	}
	// Room for a wrongly looping second probe to show up.
	time.Sleep(300 * time.Millisecond)

	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Fatalf("probeCredential called %d times for one bench expiry, want exactly 1 (never a loop)", got)
	}
}

func TestProbeRecoversAccount(t *testing.T) {
	a := newAccountsTestApp(t)
	if err := a.Accounts.SetCredential("alldebrid", "", accounts.Credential{APIKey: "fake-key"}); err != nil {
		t.Fatal(err)
	}

	var calls int32
	orig := probeCredential
	probeCredential = func(ctx context.Context, service string, cred accounts.Credential) (bool, int, error) {
		atomic.AddInt32(&calls, 1)
		return true, 3, nil
	}
	t.Cleanup(func() { probeCredential = orig })

	rec, started := a.acctHealthTracker().ReportFailure("alldebrid", "", accounts.HealthTempDisabled, "seed", 20*time.Millisecond)
	if !started {
		t.Fatal("setup: expected the bench to start")
	}
	a.scheduleProbe("alldebrid", "", rec.BenchedUntil)

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) && !a.acctHealthTracker().Usable("alldebrid", "") {
		time.Sleep(10 * time.Millisecond)
	}

	if !a.acctHealthTracker().Usable("alldebrid", "") {
		t.Fatal("account still unusable after a successful probe")
	}
	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Fatalf("probeCredential called %d times, want exactly 1", got)
	}
}

// TestReportAccountFailureFeedsSameTrackerTestAccountUses: TestAccount (the
// Refresh button) and failing downloads must write to the tracker routing
// reads.
func TestReportAccountFailureFeedsSameTrackerTestAccountUses(t *testing.T) {
	a := newAccountsTestApp(t)

	a.reportAccountFailure("realdebrid", "", core.ReasonAuth, "realdebrid /unrestrict/link: HTTP 401")
	if a.accountRoutableLocked("realdebrid") {
		t.Fatal("realdebrid still reads routable after reportAccountFailure recorded an auth rejection")
	}
	rec := a.acctHealthTracker().Get("realdebrid", "")
	if rec.State != accounts.HealthInvalid {
		t.Fatalf("state after a Real-Debrid HTTP 401 = %q, want %q", rec.State, accounts.HealthInvalid)
	}

	a.reportAccountSuccess("realdebrid", "")
	if !a.accountRoutableLocked("realdebrid") {
		t.Fatal("realdebrid still unroutable after reportAccountSuccess")
	}
}
