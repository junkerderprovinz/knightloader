package app

// Covers the four rows of account health (docs/build-plan.md package 14) at
// the App level: a benched service's in-flight and queued tasks are held for
// fallback rather than failed, the bench-expiry probe fires exactly once and
// never loops, and an ambiguous failure defaults to HealthTempDisabled, never
// HealthInvalid, all the way through app_health.go's own service-specific
// refineState layer (internal/accounts' own package covers the same
// guarantee at the generic ClassifyReason layer).
//
// Every dispatch-level test here builds a real App (New(t.TempDir())) and a
// small fakeResolver registered directly into a.Registry, the same
// resolver-substitution shape fallback_test.go and routing_test.go already
// use - a unit test that depends on a real debrid API being reachable is not
// a unit test (see accounts_test.go's own package comment for the same rule
// applied to credentials).

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

// fakeResolver is a minimal resolver.Resolver claiming exactly one host, so a
// test can register it under a real account-bearing id ("alldebrid" etc.)
// without wiring a real debrid backend or spending a real API call.
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

// isolateResolvers replaces a's whole registry with one holding only the
// given resolvers. New() always registers resolver.Direct and
// resolver.HTTPFallback, and both match almost any http(s) URL by design -
// Direct claims anything whose path looks like a file, HTTPFallback claims
// literally everything left over. Neither carries a tracked account, so
// either one left in place is an unconditional, health-blind fallback that
// silently answers "no resolver is unroutable" and "the chain still has
// somewhere to go" regardless of what this file is testing. Every dispatch-
// and onUpdate-level test below calls this first so the fake resolvers it
// registers are the only ones in play - safe to reassign here because it
// runs synchronously right after New(), before any dispatch has touched
// a.Registry from another goroutine.
func isolateResolvers(a *App, resolvers ...resolver.Resolver) {
	a.Registry = resolver.NewRegistry()
	for _, r := range resolvers {
		a.Registry.Register(r)
	}
}

// TestAccountForResolverMapping pins the mapping every other test here leans
// on: only the three one-shot debrid ids carry a tracked account, and every
// other resolver id - jd, ytdlp, the engine's own direct/http-fallback -
// must never be affected by account health at all.
func TestAccountForResolverMapping(t *testing.T) {
	a := newAccountsTestApp(t)

	for _, id := range []string{"alldebrid", "realdebrid", "torbox"} {
		if _, _, ok := a.accountForResolverLocked(id); !ok {
			t.Errorf("accountForResolverLocked(%q) ok = false, want true", id)
		}
	}
	for _, id := range []string{"jd", "ytdlp", "direct", "http", ""} {
		if _, _, ok := a.accountForResolverLocked(id); ok {
			t.Errorf("accountForResolverLocked(%q) ok = true, want false - this resolver has no tracked account", id)
		}
		if !a.accountRoutableLocked(id) {
			t.Errorf("accountRoutableLocked(%q) = false, want true - a resolver with no tracked account must never be blocked by health", id)
		}
	}
}

// TestBenchDelayGrowsAndCaps is the backoff shape (row 3's cost concern,
// spelled out as a pure function): each consecutive episode waits longer,
// and it stops growing once it reaches benchMax.
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

// TestRefineStateDefaultsSafely is row 4, at the layer that actually reads
// each service's own error vocabulary: text nothing in providerCodes
// recognises must never promote past the generic HealthTempDisabled default,
// whichever service it claims to be from.
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

// TestRefineStatePromotesVerifiedCodes checks the small, documented needle
// table itself: each verified provider code lands on the state its own doc
// comment claims, and refineState never fires on the wrong service.
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
	// A needle for one service must never fire against another - AllDebrid's
	// AUTH_BAD_APIKEY text appearing (implausibly) in a Real-Debrid failure
	// must not borrow AllDebrid's verdict.
	if got := refineState("realdebrid", accounts.HealthTempDisabled, "auth_bad_apikey"); got != accounts.HealthTempDisabled {
		t.Errorf("refineState leaked an AllDebrid needle onto realdebrid: got %q, want %q", got, accounts.HealthTempDisabled)
	}
	// refineState never promotes a base that already settled as something
	// other than TempDisabled - only the generic layer decides "not
	// applicable at all" (accounts.ClassifyReason), and this layer must not
	// override that by matching text alone.
	if got := refineState("alldebrid", "", "AUTH_BAD_APIKEY"); got != "" {
		t.Errorf("refineState promoted a non-TempDisabled base (%q) using a needle match; it must only ever refine TempDisabled", got)
	}
}

// TestQueuedTaskOnBenchedAccountIsHeldNotFailed is row 2's queued half:
// dispatchLocked must hold a queued task whose only possible resolver is
// benched, not settle it as "no resolver matches" - see
// hasUnroutableMatchLocked.
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
		t.Fatalf("queued task on a benched, solely-matching account settled as StatusError (reason %q); row 2 requires it held, not failed", reason)
	}
	if !stillQueued {
		t.Fatal("the task left the queue without being dispatched or settled - it was lost")
	}
	if reason == core.ReasonUnsupported {
		t.Error("task carries ReasonUnsupported - that is a lie about a link this resolver can normally fetch, only its account is unavailable")
	}
}

// TestQueuedTaskFallsBackWhenAnotherAccountCanTakeIt is the companion case:
// when a SECOND, healthy resolver also matches, the benched one must not
// block dispatch at all - the task should actually start on the other one.
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
		t.Fatalf("task.Resolver = %q, want realdebrid - the healthy account should have been picked over the benched one", res)
	}
}

// TestInFlightTaskFallsBackWhenBenched is row 2's in-flight half: a task
// that fails while its resolver's account is (or just became) benched must
// be requeued onto the next resolver in the chain, exactly like an explicit
// u.Unsupported, rather than settling as StatusError.
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

	// A failure classify() cannot place at all (ReasonUnknown), which
	// ClassifyReason still treats as applicable - see row 4 and
	// TestRefineStateDefaultsSafely above.
	a.onUpdate(task.ID, core.Update{Status: core.StatusError, Err: "alldebrid: an unexpected failure"})

	a.mu.Lock()
	status, res := task.Status, task.Resolver
	a.mu.Unlock()

	if status == core.StatusError {
		t.Fatalf("in-flight task on a now-benched account settled as StatusError; row 2 requires it held for fallback")
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

// TestInFlightTaskHeldWhenNoFallbackExists is the same failure with only one
// resolver ever able to claim the link: still no hard error, held instead.
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
		t.Fatal("in-flight task with no fallback available settled as StatusError; row 2 requires it held for the account to recover")
	}
	if !held {
		t.Fatalf("task status = %q but it is not sitting in the queue - it was lost rather than held", status)
	}
}

// TestUnsupportedFallbackUnaffectedByAccountHealth is the guard the other
// way round: u.Unsupported must keep working exactly as before for a
// resolver with no tracked account at all (see fallback_test.go's own
// TestFallbackOnlyOnUnsupported for the same invariant against the
// pre-existing mechanism this file adds a sibling branch beside, not on top
// of).
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
		t.Errorf("task.Resolver = %q, want http - an unrelated resolver's own fallback must be untouched by account health", res)
	}
}

// TestProbeFiresExactlyOnceAtExpiry is row 3, exercised through the real
// timer/spawn wiring rather than only the state machine's own started flag
// (accounts/health_test.go covers that half): a bench that expires fires
// probeCredential exactly once, never a loop, even after that one probe
// itself fails.
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
	// A further window for a (wrongly) looping second probe to show up -
	// this is the actual point of the test, not the wait above.
	time.Sleep(300 * time.Millisecond)

	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Fatalf("probeCredential called %d times for one bench expiry, want exactly 1 (row 3: never a loop)", got)
	}
}

// TestProbeRecoversAccount is the success half of the same wiring: a probe
// that succeeds clears the bench and the account is usable again with no
// further calls.
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

// TestReportAccountFailureFeedsSameTrackerTestAccountUses is the "Refresh"
// hook's own contract (app_accounts.go's TestAccount calls exactly these two
// functions on its live checkCredential result): calling them directly, the
// same way TestAccount does, must land on the same tracker
// accountRoutableLocked reads for routing - proving the two paths (a real
// download failing, and a person pressing Refresh) are not two different
// stores that could disagree.
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
