package app

// Captcha wiring (app_captcha.go), the paid solvers (app_captcha_solvers.go)
// and dispatchLocked's captcha check. KL_JD is unset, so captcha.JDSource
// answers ErrJDNotConfigured without a network call; the tests seed challenges
// straight into the store instead of faking a Source.

import (
	"context"
	"fmt"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/accounts"
	"github.com/junkerderprovinz/knightloader/internal/captcha"
	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/settings"
)

func newCaptchaTestApp(t *testing.T) *App {
	t.Helper()
	a, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { a.Close() })
	if _, err := a.ApplySettings(settings.Settings{
		MaxConcurrent: 4, MaxPerHost: 4, DownloadDir: t.TempDir(),
	}); err != nil {
		t.Fatal(err)
	}
	return a
}

// A task waiting on a captcha stays in the queue like a held one: not
// dispatched and not failed.
func TestDispatchLockedHoldsCaptchaWaitingTask(t *testing.T) {
	a := newCaptchaTestApp(t)

	a.mu.Lock()
	a.tasks["t1"] = &core.Task{ID: "t1", URL: "https://host.example/t1", Status: core.StatusQueued, Enabled: true, Resolver: "jd"}
	a.queue = append(a.queue, "t1")
	a.mu.Unlock()

	a.captchaStateFor().store.Sync([]captcha.Challenge{
		{ID: "c1", Host: "host.example", TaskID: "t1", Kind: captcha.KindImage},
	})

	a.mu.Lock()
	a.dispatchLocked()
	inQueue := len(a.queue) == 1 && a.queue[0] == "t1"
	dispatched := a.active["t1"]
	status := a.tasks["t1"].Status
	a.mu.Unlock()

	if !inQueue {
		t.Errorf("task t1 left the queue while its captcha was still pending")
	}
	if dispatched {
		t.Errorf("task t1 was dispatched while its captcha was still pending")
	}
	if status == core.StatusError {
		t.Errorf("task t1 settled as a hard failure while merely waiting on a captcha")
	}
}

// Without a challenge in the store, a queued task dispatches normally.
func TestDispatchLockedDispatchesOnceTheCaptchaIsGone(t *testing.T) {
	a := newCaptchaTestApp(t)

	a.mu.Lock()
	a.tasks["t1"] = &core.Task{ID: "t1", URL: "https://host.example/t1", Status: core.StatusQueued, Enabled: true}
	a.queue = append(a.queue, "t1")
	a.mu.Unlock()

	a.mu.Lock()
	a.dispatchLocked()
	dispatched := a.active["t1"]
	a.mu.Unlock()

	if !dispatched {
		t.Errorf("a task with no captcha pending was held anyway")
	}
}

// A new challenge stamps core.ReasonCaptcha on its task.
func TestMarkCaptchaTasksStampsReasonOnce(t *testing.T) {
	a := newCaptchaTestApp(t)
	a.mu.Lock()
	a.tasks["t1"] = &core.Task{ID: "t1", URL: "https://host.example/t1", Status: core.StatusRunning, Enabled: true}
	a.mu.Unlock()

	a.markCaptchaTasks([]captcha.Challenge{{ID: "c1", Host: "host.example", TaskID: "t1"}})

	a.mu.Lock()
	reason := a.tasks["t1"].Reason
	a.mu.Unlock()
	if reason != core.ReasonCaptcha {
		t.Fatalf("Reason = %q after markCaptchaTasks, want %q", reason, core.ReasonCaptcha)
	}

	// A challenge without a TaskID has nothing to mark and must not panic.
	a.markCaptchaTasks([]captcha.Challenge{{ID: "c2", Host: "other.example", TaskID: ""}})
}

// Settling clears ReasonCaptcha but leaves a newer reason, such as a real
// failure, alone.
func TestSettleCaptchaClearsReasonOnlyWhenStillCaptcha(t *testing.T) {
	a := newCaptchaTestApp(t)
	a.mu.Lock()
	a.tasks["still-waiting"] = &core.Task{ID: "still-waiting", Status: core.StatusRunning, Reason: core.ReasonCaptcha}
	a.tasks["moved-on"] = &core.Task{ID: "moved-on", Status: core.StatusError, Reason: core.ReasonNetwork}
	a.mu.Unlock()

	a.settleCaptcha(captcha.Challenge{ID: "c1", Host: "h", TaskID: "still-waiting"}, "solved")
	a.settleCaptcha(captcha.Challenge{ID: "c2", Host: "h", TaskID: "moved-on"}, "timedOut")

	a.mu.Lock()
	r1 := a.tasks["still-waiting"].Reason
	r2 := a.tasks["moved-on"].Reason
	a.mu.Unlock()

	if r1 != core.ReasonUnknown {
		t.Errorf("still-waiting.Reason = %q after settling, want cleared to %q", r1, core.ReasonUnknown)
	}
	if r2 != core.ReasonNetwork {
		t.Errorf("moved-on.Reason = %q after settling, want the real failure reason left alone", r2)
	}
}

// A challenge whose task was deleted meanwhile must not panic.
func TestSettleCaptchaIsSafeWithNoMatchingTask(t *testing.T) {
	a := newCaptchaTestApp(t)
	a.settleCaptcha(captcha.Challenge{ID: "c1", Host: "h", TaskID: "gone"}, "resolved")
}

func TestCaptchaWaitingLockedReflectsTheStore(t *testing.T) {
	a := newCaptchaTestApp(t)
	a.mu.Lock()
	waiting := a.captchaWaitingLocked("t1")
	a.mu.Unlock()
	if waiting {
		t.Fatalf("captchaWaitingLocked(t1) = true before anything was ever seeded")
	}

	a.captchaStateFor().store.Sync([]captcha.Challenge{{ID: "c1", Host: "h", TaskID: "t1"}})
	a.mu.Lock()
	waiting = a.captchaWaitingLocked("t1")
	a.mu.Unlock()
	if !waiting {
		t.Fatalf("captchaWaitingLocked(t1) = false right after Sync seeded a challenge for it")
	}

	a.captchaStateFor().store.Remove("c1")
	a.mu.Lock()
	waiting = a.captchaWaitingLocked("t1")
	a.mu.Unlock()
	if waiting {
		t.Fatalf("captchaWaitingLocked(t1) = true after its challenge was removed")
	}
}

// A failing Source (here ErrJDNotConfigured) keeps the last good list rather
// than reading as "everything resolved".
func TestPollCaptchasOnceKeepsLastGoodListOnError(t *testing.T) {
	a := newCaptchaTestApp(t)
	st := a.captchaStateFor()
	st.store.Sync([]captcha.Challenge{{ID: "c1", Host: "h", TaskID: "t1"}})

	got := a.pollCaptchasOnce(st)
	if len(got) != 1 || got[0].ID != "c1" {
		t.Fatalf("pollCaptchasOnce against an unconfigured JD returned %+v, want the seeded challenge untouched", got)
	}
}

// CaptchaChallenges reads the cache and returns at once, although it also
// starts the poll loop.
func TestCaptchaChallengesStartsThePollerWithoutBlocking(t *testing.T) {
	a := newCaptchaTestApp(t)
	if got := a.CaptchaChallenges(); len(got) != 0 {
		t.Fatalf("CaptchaChallenges on a fresh App = %+v, want empty", got)
	}
}

// Without a sidecar both return an error rather than hanging; the routes map
// ErrJDNotConfigured to 503.
func TestAnswerAndAbortCaptchaReportJDNotConfigured(t *testing.T) {
	a := newCaptchaTestApp(t)

	if _, err := a.AnswerCaptcha(t.Context(), "c1", "text"); err == nil {
		t.Error("AnswerCaptcha with no JD configured returned no error")
	}
	if err := a.AbortCaptcha(t.Context(), "c1", captcha.AbortSkipOnce); err == nil {
		t.Error("AbortCaptcha with no JD configured returned no error")
	}
}

// fakeSolver is a captcha.Solver for solveCaptchaWith, so no real solving API
// is reached.
type fakeSolver struct {
	calls atomic.Int32
	text  string
	err   error
	// refuse is what Takes answers.
	refuse error
	// waitForDone blocks Solve until ctx is done and returns ctx.Err(), as a
	// real HTTP client does with an expired context.
	waitForDone bool
	// during runs inside Solve, where the request would be on its way.
	during func()
}

func (f *fakeSolver) Takes(captcha.Challenge) error { return f.refuse }

func (f *fakeSolver) Solve(ctx context.Context, _ captcha.Challenge) (string, error) {
	f.calls.Add(1)
	if f.during != nil {
		f.during()
	}
	if f.waitForDone {
		<-ctx.Done()
		return "", ctx.Err()
	}
	return f.text, f.err
}

func paid(fs ...*fakeSolver) []paidSolver {
	out := make([]paidSolver, len(fs))
	for i, f := range fs {
		out[i] = paidSolver{label: fmt.Sprintf("solver %d", i+1), Solver: f}
	}
	return out
}

func imageChallenge(id string) captcha.Challenge {
	return captcha.Challenge{ID: id, Host: "h", Kind: captcha.KindImage, Payload: &captcha.ImagePayload{DataURL: "data:image/png;base64,Zm9v"}}
}

// pending puts c in the store the way a poll does before the solvers are
// started.
func pending(a *App, c captcha.Challenge) captcha.Challenge {
	a.captchaStateFor().store.Sync([]captcha.Challenge{c})
	return c
}

func solverReport(t *testing.T, a *App, id string) *captcha.SolverReport {
	t.Helper()
	c, ok := a.captchaStateFor().store.Get(id)
	if !ok {
		t.Fatalf("challenge %s has left the store", id)
	}
	return c.Solver
}

// The first solver to succeed wins and later ones are not tried.
func TestSolveCaptchaWithStopsAtFirstSuccess(t *testing.T) {
	a := newCaptchaTestApp(t)
	first := &fakeSolver{text: "ABCD"}
	second := &fakeSolver{text: "should never run"}

	a.solveCaptchaWith(paid(first, second), pending(a, imageChallenge("c1")))

	if first.calls.Load() != 1 {
		t.Errorf("first solver called %d times, want exactly 1", first.calls.Load())
	}
	if second.calls.Load() != 0 {
		t.Errorf("second solver called %d times, want 0; the first already succeeded", second.calls.Load())
	}
}

// A refusal costs nothing, so the next solver gets the captcha, and the prompt
// learns why the first one declined.
func TestARefusalPassesTheCaptchaToTheNextSolver(t *testing.T) {
	a := newCaptchaTestApp(t)
	broke := &fakeSolver{err: &captcha.Refusal{Code: "ERROR_ZERO_BALANCE", Detail: "no funds"}}
	next := &fakeSolver{text: "EFGH"}

	a.solveCaptchaWith(paid(broke, next), pending(a, imageChallenge("c1")))

	if broke.calls.Load() != 1 || next.calls.Load() != 1 {
		t.Fatalf("solvers called %d and %d times, want once each", broke.calls.Load(), next.calls.Load())
	}
	r := solverReport(t, a, "c1")
	if r == nil || len(r.Refusals) != 1 {
		t.Fatalf("report = %+v, want the one refusal on it", r)
	}
	if got := r.Refusals[0]; got.Solver != "solver 1" || got.Code != "ERROR_ZERO_BALANCE" || got.Detail != "no funds" {
		t.Errorf("refusal = %+v, want the provider's reason under the first solver's name", got)
	}
}

// A provider that took the task may bill it whether or not an answer came
// back, so the captcha is not paid for a second time elsewhere.
func TestATakenTaskIsNotSentToAnotherSolver(t *testing.T) {
	a := newCaptchaTestApp(t)
	silent := &fakeSolver{err: fmt.Errorf("%w: connection reset", captcha.ErrTaskTaken)}
	next := &fakeSolver{text: "should never run"}

	a.solveCaptchaWith(paid(silent, next), pending(a, imageChallenge("c1")))

	if next.calls.Load() != 0 {
		t.Errorf("second solver called %d times after the first took the task", next.calls.Load())
	}
	r := solverReport(t, a, "c1")
	if r == nil || r.State != captcha.SolverStopped || len(r.Refusals) != 1 || r.Refusals[0].Code != captcha.RefusalNoAnswer || !r.Refusals[0].Taken {
		t.Errorf("report = %+v, want stopped with a taken noAnswer line", r)
	}
}

// Anti-Captcha bills a task its workers gave up on, so that verdict ends the
// walk like any other failure of a task the provider took, and the prompt
// still shows the provider's reason.
func TestAProviderGivingUpOnATakenTaskEndsTheWalk(t *testing.T) {
	a := newCaptchaTestApp(t)
	gaveUp := &fakeSolver{err: fmt.Errorf("%w: %w", captcha.ErrTaskTaken, &captcha.Refusal{Code: "ERROR_CAPTCHA_UNSOLVABLE", Detail: "workers could not solve it"})}
	next := &fakeSolver{text: "should never run"}

	a.solveCaptchaWith(paid(gaveUp, next), pending(a, imageChallenge("c1")))

	if next.calls.Load() != 0 {
		t.Errorf("second solver called %d times after the first gave up on a task it had taken", next.calls.Load())
	}
	r := solverReport(t, a, "c1")
	if r == nil || len(r.Refusals) != 1 || r.Refusals[0].Code != "ERROR_CAPTCHA_UNSOLVABLE" || !r.Refusals[0].Taken {
		t.Errorf("report = %+v, want the provider's code on a taken line", r)
	}
}

// JD keeps its captchas across a restart of this app, so a captcha is written
// down before a solver is asked, and the next process does not send it again.
func TestACaptchaSentToASolverIsNotSentAgainAfterARestart(t *testing.T) {
	a := newCaptchaTestApp(t)
	restart := func() *paidLedger { return &paidLedger{path: filepath.Join(a.DataDir, paidLedgerFile)} }
	sentAgain := true
	s := &fakeSolver{text: "ABCD", during: func() { sentAgain = restart().claim("c1", time.Now()) }}

	a.solveCaptchaWith(paid(s), pending(a, imageChallenge("c1")))

	if s.calls.Load() != 1 {
		t.Fatalf("solver called %d times, want 1", s.calls.Load())
	}
	if sentAgain {
		t.Error("a process started while the solver worked would send the captcha again")
	}
}

// A captcha still held back for a watcher went to nobody, so after a restart
// the solvers may take it.
func TestACaptchaHeldBackAtARestartIsStillSolved(t *testing.T) {
	path := filepath.Join(t.TempDir(), paidLedgerFile)
	now := time.Now()
	before := &paidLedger{path: path}
	before.claim("held", now)
	before.claim("sent", now)
	before.markSent("sent", now)

	after := &paidLedger{path: path}
	if !after.claim("held", now) {
		t.Error("a captcha nobody was sent was lost with the restart")
	}
	if after.claim("sent", now) {
		t.Error("a captcha a solver was sent came back after the restart")
	}
}

// JD can list a challenge again after it dropped out of one poll; the solvers
// must not be paid for it twice.
func TestTheSameCaptchaIsNeverSentToTheSolversTwice(t *testing.T) {
	a := newCaptchaTestApp(t)
	s := &fakeSolver{err: &captcha.Refusal{Code: "ERROR_CAPTCHA_UNSOLVABLE"}}
	c := pending(a, imageChallenge("c1"))

	a.solveCaptchaWith(paid(s), c)
	a.solveCaptchaWith(paid(s), c)

	if s.calls.Load() != 1 {
		t.Errorf("solver called %d times for one captcha, want 1", s.calls.Load())
	}
}

// Challenges with nothing a solver could work from never reach one.
func TestUnsolvableChallengesNeverReachASolver(t *testing.T) {
	a := newCaptchaTestApp(t)
	cases := []captcha.Challenge{
		{ID: "unsupported", Kind: captcha.KindUnsupported, Payload: &captcha.UnsupportedPayload{Vendor: "SomeVendor"}},
		{ID: "empty-image", Kind: captcha.KindImage, Payload: &captcha.ImagePayload{DataURL: ""}},
		{ID: "unreadable-image", Kind: captcha.KindImage, Payload: &captcha.ImagePayload{DataURL: "data:image/png;base64,@@@"}},
		{ID: "no-payload", Kind: captcha.KindImage, Payload: nil},
		{ID: "no-site-key", Kind: captcha.KindWidget, Payload: &captcha.WidgetPayload{Vendor: captcha.VendorRecaptcha}},
	}
	for _, c := range cases {
		f := &fakeSolver{text: "should never run"}
		a.solveCaptchaWith(paid(f), pending(a, c))
		if f.calls.Load() != 0 {
			t.Errorf("challenge %s: solver was called %d times, want 0", c.ID, f.calls.Load())
		}
	}
}

func TestATokenCaptchaGoesToTheSolvers(t *testing.T) {
	a := newCaptchaTestApp(t)
	f := &fakeSolver{text: "03AGdBq-token"}
	c := captcha.Challenge{ID: "w1", Host: "h", Kind: captcha.KindWidget, Payload: &captcha.WidgetPayload{
		Vendor: captcha.VendorRecaptcha, SiteKey: "6Lc-key", SiteURL: "https://h/dl",
	}}

	a.solveCaptchaWith(paid(f), pending(a, c))

	if f.calls.Load() != 1 {
		t.Errorf("solver called %d times for a reCAPTCHA, want 1", f.calls.Load())
	}
}

// A captcha no configured solver takes is reported straight away and waits
// for nobody, even with the watching switch on and a viewer present.
func TestACaptchaNoSolverTakesIsReportedAtOnce(t *testing.T) {
	a := newCaptchaTestApp(t)
	onlyUnwatched(t, a)
	addViewer(t, a)
	refusal := &captcha.Refusal{Code: captcha.RefusalUnsupported}
	one, two := &fakeSolver{refuse: refusal}, &fakeSolver{refuse: refusal}
	c := captcha.Challenge{ID: "h1", Host: "h", Kind: captcha.KindWidget, Payload: &captcha.WidgetPayload{
		Vendor: captcha.VendorHCaptcha, SiteKey: "10000000-ffff-ffff-ffff-000000000001",
	}}

	a.solveCaptchaWith(paid(one, two), pending(a, c))

	if one.calls.Load()+two.calls.Load() != 0 {
		t.Error("a solver that does not take hCaptcha was asked to solve one")
	}
	r := solverReport(t, a, "h1")
	if r == nil || r.State != captcha.SolverStopped || len(r.Refusals) != 2 {
		t.Fatalf("report = %+v, want stopped with both refusals", r)
	}
	for _, line := range r.Refusals {
		if line.Code != captcha.RefusalUnsupported {
			t.Errorf("refusal = %+v, want unsupported", line)
		}
	}
}

// No configured solver is the ordinary case and must not panic.
func TestSolveCaptchaWithNoSolversIsANoop(t *testing.T) {
	a := newCaptchaTestApp(t)
	a.solveCaptchaWith(nil, pending(a, imageChallenge("c1")))
}

// Once a challenge has expired, no further solver is tried.
func TestSolveCaptchaWithStopsOnExpiredChallenge(t *testing.T) {
	a := newCaptchaTestApp(t)
	first := &fakeSolver{waitForDone: true}
	second := &fakeSolver{text: "should never run"}

	c := imageChallenge("c1")
	c.ExpiresAt = time.Now().Add(-time.Hour)

	a.solveCaptchaWith(paid(first, second), pending(a, c))

	if first.calls.Load() != 1 {
		t.Errorf("first solver called %d times, want exactly 1", first.calls.Load())
	}
	if second.calls.Load() != 0 {
		t.Errorf("second solver called %d times, want 0; the challenge had already expired", second.calls.Load())
	}
}

// onlyUnwatched switches the watching rule on with the longest wait there is,
// and shortens how often a held-back solver looks again.
func onlyUnwatched(t *testing.T, a *App) {
	t.Helper()
	if _, err := a.ApplySettings(settings.Settings{
		MaxConcurrent: 4, MaxPerHost: 4, DownloadDir: t.TempDir(),
		CaptchaSolverOnlyUnwatched: true, CaptchaSolverWait: settings.MaxCaptchaSolverWait,
	}); err != nil {
		t.Fatal(err)
	}
	prev := captchaWatchInterval
	captchaWatchInterval = 5 * time.Millisecond
	t.Cleanup(func() { captchaWatchInterval = prev })
}

// addViewer connects a viewer whose prompt listens for captchas and is on
// screen, the way the web interface's captcha window does.
func addViewer(t *testing.T, a *App) *activityFakeConn {
	t.Helper()
	fc := &activityFakeConn{}
	a.Hub.Add(fc)
	t.Cleanup(func() { a.Hub.Remove(fc) })
	a.Hub.Subscribe(fc, []string{"captcha", "captchaResolved"})
	a.Hub.SetVisible(fc, true)
	return fc
}

// shortGrace shortens how long a viewer that went away without saying so
// still counts as watching.
func shortGrace(t *testing.T, d time.Duration) {
	t.Helper()
	prev := captchaWatchGrace
	captchaWatchGrace = d
	t.Cleanup(func() { captchaWatchGrace = prev })
}

// A socket that drops may be the prompt reconnecting, behind a proxy that
// closes idle connections, so the solvers wait out the grace period before
// they take over.
func TestADroppedViewerHoldsTheSolversForTheGracePeriod(t *testing.T) {
	a := newCaptchaTestApp(t)
	onlyUnwatched(t, a)
	shortGrace(t, 500*time.Millisecond)
	viewer := addViewer(t, a)
	s := &fakeSolver{text: "ABCD"}

	solveInBackground(t, a, paid(s), pending(a, imageChallenge("c1")))
	waitFor(t, "the waiting report", func() bool {
		c, _ := a.captchaStateFor().store.Get("c1")
		return c.Solver != nil && c.Solver.State == captcha.SolverWaiting
	})

	a.Hub.Remove(viewer)
	time.Sleep(100 * time.Millisecond)
	if s.calls.Load() != 0 {
		t.Fatal("the solver started the moment the prompt's socket dropped")
	}
	waitFor(t, "the solver taking over after the grace period", func() bool { return s.calls.Load() == 1 })
}

// An app that polls the list holds no socket, and while it polls, somebody is
// watching.
func TestAPollingAppHoldsTheSolversBack(t *testing.T) {
	a := newCaptchaTestApp(t)
	onlyUnwatched(t, a)
	shortGrace(t, 500*time.Millisecond)
	s := &fakeSolver{text: "ABCD"}

	a.Hub.Seen("captcha")
	solveInBackground(t, a, paid(s), pending(a, imageChallenge("c1")))
	for range 3 {
		time.Sleep(100 * time.Millisecond)
		a.Hub.Seen("captcha")
	}
	if s.calls.Load() != 0 {
		t.Fatal("the solver started while the app was polling")
	}
	waitFor(t, "the solver taking over once the app stopped polling", func() bool { return s.calls.Load() == 1 })
}

// solveInBackground runs solveCaptchaWith the way a poll does, and waits for it
// before the test's App is closed.
func solveInBackground(t *testing.T, a *App, solvers []paidSolver, c captcha.Challenge) {
	t.Helper()
	done := make(chan struct{})
	go func() {
		defer close(done)
		a.solveCaptchaWith(solvers, c)
	}()
	t.Cleanup(func() { <-done })
}

func TestSolversWaitWhileSomebodyWatches(t *testing.T) {
	a := newCaptchaTestApp(t)
	onlyUnwatched(t, a)
	viewer := addViewer(t, a)
	s := &fakeSolver{text: "ABCD"}

	solveInBackground(t, a, paid(s), pending(a, imageChallenge("c1")))

	waitFor(t, "the waiting report", func() bool {
		c, _ := a.captchaStateFor().store.Get("c1")
		return c.Solver != nil && c.Solver.State == captcha.SolverWaiting
	})
	if r := solverReport(t, a, "c1"); r.Until.IsZero() {
		t.Error("the waiting report says nothing about when the solver takes over")
	}
	time.Sleep(50 * time.Millisecond)
	if s.calls.Load() != 0 {
		t.Fatal("the solver started while somebody was watching")
	}

	// The tab goes to the background: nobody is watching any more.
	a.Hub.SetVisible(viewer, false)
	waitFor(t, "the solver taking over", func() bool { return s.calls.Load() == 1 })
}

// Nobody answering in time hands the captcha to the solver anyway, at the
// latest halfway to its expiry.
func TestASolverTakesOverWhenNobodyAnswersInTime(t *testing.T) {
	a := newCaptchaTestApp(t)
	onlyUnwatched(t, a)
	addViewer(t, a)
	s := &fakeSolver{text: "ABCD"}
	c := imageChallenge("c1")
	c.ExpiresAt = time.Now().Add(2 * time.Second)

	solveInBackground(t, a, paid(s), pending(a, c))

	waitFor(t, "the solver taking over", func() bool { return s.calls.Load() == 1 })
	if time.Now().After(c.ExpiresAt) {
		t.Error("the solver took over only after the captcha had expired")
	}
}

// An answer at the prompt while the solvers wait means nobody is paid.
func TestAnAnswerWhileTheSolversWaitCostsNothing(t *testing.T) {
	a := newCaptchaTestApp(t)
	onlyUnwatched(t, a)
	addViewer(t, a)
	s := &fakeSolver{text: "ABCD"}
	c := pending(a, imageChallenge("c1"))

	done := make(chan struct{})
	go func() {
		defer close(done)
		a.solveCaptchaWith(paid(s), c)
	}()
	waitFor(t, "the waiting report", func() bool {
		got, _ := a.captchaStateFor().store.Get("c1")
		return got.Solver != nil && got.Solver.State == captcha.SolverWaiting
	})
	a.settleCaptcha(c, "solved")
	<-done

	if s.calls.Load() != 0 {
		t.Errorf("solver called %d times for a captcha somebody answered", s.calls.Load())
	}
}

func TestSolversStartAtOnceWhenNobodyWatches(t *testing.T) {
	a := newCaptchaTestApp(t)
	onlyUnwatched(t, a)
	hidden := addViewer(t, a)
	a.Hub.SetVisible(hidden, false)
	s := &fakeSolver{text: "ABCD"}

	a.solveCaptchaWith(paid(s), pending(a, imageChallenge("c1")))

	if s.calls.Load() != 1 {
		t.Errorf("solver called %d times with nobody watching, want 1 straight away", s.calls.Load())
	}
}

// With the switch off, the default, a viewer changes nothing: the solvers
// work alongside the prompt.
func TestWithoutTheSwitchSolversStartDespiteAViewer(t *testing.T) {
	a := newCaptchaTestApp(t)
	addViewer(t, a)
	s := &fakeSolver{text: "ABCD"}

	a.solveCaptchaWith(paid(s), pending(a, imageChallenge("c1")))

	if s.calls.Load() != 1 {
		t.Errorf("solver called %d times, want 1 straight away", s.calls.Load())
	}
}

func TestSolverTakeoverIsNeverPastHalfTheCaptchasTime(t *testing.T) {
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	cases := []struct {
		name    string
		expires time.Time
		want    time.Time
	}{
		{"no deadline", time.Time{}, now.Add(time.Minute)},
		{"a long deadline", now.Add(10 * time.Minute), now.Add(time.Minute)},
		{"a short deadline", now.Add(time.Minute), now.Add(30 * time.Second)},
	}
	for _, c := range cases {
		if got := solverTakeoverAt(now, time.Minute, c.expires); !got.Equal(c.want) {
			t.Errorf("%s: takeover at %v, want %v", c.name, got, c.want)
		}
	}
}

// Solvers without a stored credential are skipped and the configured order is
// kept.
func TestCaptchaSolversReadsOrderAndCredentials(t *testing.T) {
	a := newCaptchaTestApp(t)

	if got := a.captchaSolvers(); len(got) != 0 {
		t.Fatalf("captchaSolvers with no configured order = %d solvers, want 0", len(got))
	}

	if _, err := a.ApplySettings(settings.Settings{
		MaxConcurrent: 4, MaxPerHost: 4, DownloadDir: t.TempDir(),
		CaptchaSolverOrder: []string{"anticaptcha", "2captcha"},
	}); err != nil {
		t.Fatal(err)
	}
	if got := a.captchaSolvers(); len(got) != 0 {
		t.Fatalf("captchaSolvers with an order but no stored keys = %d solvers, want 0", len(got))
	}

	if err := a.Accounts.SetCredential("2captcha", "", accounts.Credential{APIKey: "fake-2captcha-key"}); err != nil {
		t.Fatal(err)
	}
	got := a.captchaSolvers()
	if len(got) != 1 {
		t.Fatalf("captchaSolvers with only 2captcha configured = %d solvers, want 1", len(got))
	}
	if _, ok := got[0].Solver.(*captcha.TwoCaptchaSolver); !ok || got[0].label != "2Captcha" {
		t.Fatalf("captchaSolvers()[0] = %s %T, want 2Captcha's solver", got[0].label, got[0].Solver)
	}

	if err := a.Accounts.SetCredential("anticaptcha", "", accounts.Credential{APIKey: "fake-anticaptcha-key"}); err != nil {
		t.Fatal(err)
	}
	got = a.captchaSolvers()
	if len(got) != 2 {
		t.Fatalf("captchaSolvers with both configured = %d solvers, want 2", len(got))
	}
	// CaptchaSolverOrder's order, not the catalogue's.
	if _, ok := got[0].Solver.(*captcha.AntiCaptchaSolver); !ok {
		t.Fatalf("captchaSolvers()[0] = %T, want *captcha.AntiCaptchaSolver (the configured order's first entry)", got[0].Solver)
	}
	if _, ok := got[1].Solver.(*captcha.TwoCaptchaSolver); !ok {
		t.Fatalf("captchaSolvers()[1] = %T, want *captcha.TwoCaptchaSolver", got[1].Solver)
	}
}
