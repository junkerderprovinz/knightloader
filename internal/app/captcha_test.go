package app

// Tests for this file's own captcha wiring (app_captcha.go) and for
// dispatchLocked's captcha-waiting check (app_dispatch.go). None of these
// touch a real JD sidecar - KL_JD is unset in this process, so
// captcha.JDSource.List/Answer/Abort answer captcha.ErrJDNotConfigured
// immediately, without a network call (see JDSource.client). What is worth
// testing here does not need JD at all: it is what this file does with a
// challenge already in its own Store, which every test below seeds directly
// - a same-package test can reach captchaStateFor and its unexported store
// exactly the way the poll loop itself does, so nothing here needs a fake
// captcha.Source.

import (
	"context"
	"errors"
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

// TestDispatchLockedHoldsCaptchaWaitingTask is build-plan.md section 8's Wave
// 7 note, pinned: a task the captcha store says is waiting must not be
// re-dispatched, and must not settle as a hard failure either - it just
// stays exactly where it was, the same as Hold.
func TestDispatchLockedHoldsCaptchaWaitingTask(t *testing.T) {
	a := newCaptchaTestApp(t)

	a.mu.Lock()
	a.tasks["t1"] = &core.Task{ID: "t1", URL: "https://host.example/t1", Status: core.StatusQueued, Enabled: true, Resolver: "jd"}
	a.queue = append(a.queue, "t1")
	a.mu.Unlock()

	// Seed the store directly, bypassing Source entirely - see this file's
	// own package comment.
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

// TestDispatchLockedDispatchesOnceTheCaptchaIsGone is the other half: once
// Store no longer knows about a challenge for a task, dispatchLocked must
// treat it like any other queued task again - the hold is not permanent.
func TestDispatchLockedDispatchesOnceTheCaptchaIsGone(t *testing.T) {
	a := newCaptchaTestApp(t)

	a.mu.Lock()
	a.tasks["t1"] = &core.Task{ID: "t1", URL: "https://host.example/t1", Status: core.StatusQueued, Enabled: true}
	a.queue = append(a.queue, "t1")
	a.mu.Unlock()

	// Never seeded into the store at all - the ordinary case for a plain
	// queued task with nothing captcha-related about it.
	a.mu.Lock()
	a.dispatchLocked()
	dispatched := a.active["t1"]
	a.mu.Unlock()

	if !dispatched {
		t.Errorf("a task with no captcha pending was held anyway")
	}
}

// TestMarkCaptchaTasksStampsReasonOnce checks the one-way stamp: a brand-new
// challenge sets core.ReasonCaptcha, and a second call for a task already
// marked (Sync's "changed" case, which never reaches markCaptchaTasks - see
// pollCaptchasOnce - but the guard inside markCaptchaTasks is what would
// also protect a caller that did) does not re-touch it.
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

	// A task with no TaskID resolved (the honest "could not say" case) has
	// nothing to mark and must not panic on a missing map entry.
	a.markCaptchaTasks([]captcha.Challenge{{ID: "c2", Host: "other.example", TaskID: ""}})
}

// TestSettleCaptchaClearsReasonOnlyWhenStillCaptcha is settleCaptcha's own
// promise: a task still carrying core.ReasonCaptcha gets it cleared, but a
// task whose Reason has since moved on to something else (a real failure
// that arrived in the meantime) is left alone - see settleCaptcha's own doc
// comment for why clobbering the newer fact would be wrong.
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

// TestSettleCaptchaIsSafeWithNoMatchingTask covers a challenge whose task was
// removed from the list entirely (deleted by the user) while its captcha was
// still pending - settleCaptcha must not panic on the missing map entry.
func TestSettleCaptchaIsSafeWithNoMatchingTask(t *testing.T) {
	a := newCaptchaTestApp(t)
	a.settleCaptcha(captcha.Challenge{ID: "c1", Host: "h", TaskID: "gone"}, "resolved")
}

// TestCaptchaWaitingLockedReflectsTheStore is captchaWaitingLocked's own
// contract, isolated from dispatchLocked's larger loop.
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

// TestPollCaptchasOnceKeepsLastGoodListOnError is pollCaptchasOnce's own
// promise against a transient Source failure: KL_JD is unset in this test
// process, so Source.List answers captcha.ErrJDNotConfigured on every call,
// and a poll pass hitting that must leave whatever Store already held
// exactly as it was - never read as "everything just resolved".
func TestPollCaptchasOnceKeepsLastGoodListOnError(t *testing.T) {
	a := newCaptchaTestApp(t)
	st := a.captchaStateFor()
	st.store.Sync([]captcha.Challenge{{ID: "c1", Host: "h", TaskID: "t1"}})

	got := a.pollCaptchasOnce(st)
	if len(got) != 1 || got[0].ID != "c1" {
		t.Fatalf("pollCaptchasOnce against an unconfigured JD returned %+v, want the seeded challenge untouched", got)
	}
}

// TestCaptchaChallengesStartsThePollerWithoutBlocking is CaptchaChallenges'
// own contract: a cache read that never itself makes the live call - see the
// function's own doc comment. It must return promptly even though it also
// starts the (otherwise KL_JD-unconfigured, so harmless) poll loop as a side
// effect.
func TestCaptchaChallengesStartsThePollerWithoutBlocking(t *testing.T) {
	a := newCaptchaTestApp(t)
	if got := a.CaptchaChallenges(); len(got) != 0 {
		t.Fatalf("CaptchaChallenges on a fresh App = %+v, want empty", got)
	}
}

// TestAnswerAndAbortCaptchaReportJDNotConfigured is the honest answer this
// process can give without a real sidecar: both surface
// captcha.ErrJDNotConfigured rather than hanging or panicking, which is what
// routes_captcha.go and routes_captcha_skip.go's own error handling depends
// on (errors.Is(err, captcha.ErrJDNotConfigured) -> 503).
func TestAnswerAndAbortCaptchaReportJDNotConfigured(t *testing.T) {
	a := newCaptchaTestApp(t)

	if _, err := a.AnswerCaptcha(t.Context(), "c1", "text"); err == nil {
		t.Error("AnswerCaptcha with no JD configured returned no error")
	}
	if err := a.AbortCaptcha(t.Context(), "c1", captcha.AbortSkipOnce); err == nil {
		t.Error("AbortCaptcha with no JD configured returned no error")
	}
}

// fakeSolver is captcha.Solver's own test double - the seam solveCaptchaWith
// exists for (see its own doc comment), so these tests never build a real
// TwoCaptchaSolver/AntiCaptchaSolver or reach a real solving API.
type fakeSolver struct {
	calls int
	text  string
	err   error
	// waitForDone, when set, blocks Solve until ctx is done and returns
	// ctx.Err() - a real HTTP client's own behaviour against an
	// already-cancelled or already-expired context, and the only realistic
	// way a fake can exercise solveCaptchaWith's own
	// "ctx.Err() != nil -> stop trying" branch.
	waitForDone bool
}

func (f *fakeSolver) Solve(ctx context.Context, _ captcha.Kind, _, _ string) (string, error) {
	f.calls++
	if f.waitForDone {
		<-ctx.Done()
		return "", ctx.Err()
	}
	return f.text, f.err
}

func imageChallenge(id string) captcha.Challenge {
	return captcha.Challenge{ID: id, Host: "h", Kind: captcha.KindImage, Payload: &captcha.ImagePayload{DataURL: "data:image/png;base64,Zm9v"}}
}

// TestSolveCaptchaWithStopsAtFirstSuccess is solveCaptchaWith's central
// promise: the first solver to succeed wins, and nothing later in the order
// is ever tried - the literal meaning of "solver order".
func TestSolveCaptchaWithStopsAtFirstSuccess(t *testing.T) {
	a := newCaptchaTestApp(t)
	first := &fakeSolver{text: "ABCD"}
	second := &fakeSolver{text: "should never run"}

	a.solveCaptchaWith([]captcha.Solver{first, second}, imageChallenge("c1"))

	if first.calls != 1 {
		t.Errorf("first solver called %d times, want exactly 1", first.calls)
	}
	if second.calls != 0 {
		t.Errorf("second solver called %d times, want 0 - the first already succeeded", second.calls)
	}
}

// TestSolveCaptchaWithFallsThroughOnFailure is the other half: a solver that
// fails (a real transport error, a service declining the image) must not
// stop the attempt - the next configured solver still gets its own try.
func TestSolveCaptchaWithFallsThroughOnFailure(t *testing.T) {
	a := newCaptchaTestApp(t)
	failing := &fakeSolver{err: errors.New("captcha unsolvable")}
	succeeding := &fakeSolver{text: "EFGH"}

	a.solveCaptchaWith([]captcha.Solver{failing, succeeding}, imageChallenge("c1"))

	if failing.calls != 1 {
		t.Errorf("failing solver called %d times, want exactly 1", failing.calls)
	}
	if succeeding.calls != 1 {
		t.Errorf("succeeding solver called %d times, want exactly 1 - it should have been tried after the first failed", succeeding.calls)
	}
}

// TestSolveCaptchaWithSkipsNonImagePayload covers the two ways a challenge
// can carry nothing a solver can act on: a KindWidget/KindUnsupported
// payload type, and an ImagePayload with no actual image data. Neither may
// reach a solver at all - see solveCaptchaWith's own doc comment on why
// KindWidget is out of scope for both clients.
func TestSolveCaptchaWithSkipsNonImagePayload(t *testing.T) {
	a := newCaptchaTestApp(t)
	cases := []captcha.Challenge{
		{ID: "widget", Kind: captcha.KindWidget, Payload: &captcha.WidgetPayload{SiteKey: "x"}},
		{ID: "unsupported", Kind: captcha.KindUnsupported, Payload: &captcha.UnsupportedPayload{Vendor: "SomeVendor"}},
		{ID: "empty-image", Kind: captcha.KindImage, Payload: &captcha.ImagePayload{DataURL: ""}},
		{ID: "no-payload", Kind: captcha.KindImage, Payload: nil},
	}
	for _, c := range cases {
		f := &fakeSolver{text: "should never run"}
		a.solveCaptchaWith([]captcha.Solver{f}, c)
		if f.calls != 0 {
			t.Errorf("challenge %s: solver was called %d times, want 0", c.ID, f.calls)
		}
	}
}

// TestSolveCaptchaWithNoSolversIsANoop is the ordinary "nobody configured an
// automatic solver" install - must not panic on a nil/empty slice.
func TestSolveCaptchaWithNoSolversIsANoop(t *testing.T) {
	a := newCaptchaTestApp(t)
	a.solveCaptchaWith(nil, imageChallenge("c1"))
}

// TestSolveCaptchaWithStopsOnExpiredChallenge is the ctx-deadline half: a
// challenge whose own ExpiresAt has already passed must not let a stuck or
// slow first solver block a second one from being tried forever - the
// derived context is already Done before Solve is ever called, so the first
// solver's own ctx-respecting behaviour (see fakeSolver.waitForDone) is what
// solveCaptchaWith's own "ctx.Err() != nil -> stop" branch reacts to.
func TestSolveCaptchaWithStopsOnExpiredChallenge(t *testing.T) {
	a := newCaptchaTestApp(t)
	first := &fakeSolver{waitForDone: true}
	second := &fakeSolver{text: "should never run"}

	c := imageChallenge("c1")
	c.ExpiresAt = time.Now().Add(-time.Hour)

	a.solveCaptchaWith([]captcha.Solver{first, second}, c)

	if first.calls != 1 {
		t.Errorf("first solver called %d times, want exactly 1", first.calls)
	}
	if second.calls != 0 {
		t.Errorf("second solver called %d times, want 0 - the challenge's own window was already closed", second.calls)
	}
}

// TestCaptchaSolversReadsOrderAndCredentials is captchaSolvers' own contract:
// an id with no stored credential is skipped, order is preserved, and an
// empty order short-circuits before ever consulting accounts.Lookup.
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
	if _, ok := got[0].(*captcha.TwoCaptchaSolver); !ok {
		t.Fatalf("captchaSolvers()[0] = %T, want *captcha.TwoCaptchaSolver", got[0])
	}

	if err := a.Accounts.SetCredential("anticaptcha", "", accounts.Credential{APIKey: "fake-anticaptcha-key"}); err != nil {
		t.Fatal(err)
	}
	got = a.captchaSolvers()
	if len(got) != 2 {
		t.Fatalf("captchaSolvers with both configured = %d solvers, want 2", len(got))
	}
	// Order preserved from CaptchaSolverOrder (anticaptcha, 2captcha), not
	// catalogue order (2captcha, anticaptcha) - see catalogue.go.
	if _, ok := got[0].(*captcha.AntiCaptchaSolver); !ok {
		t.Fatalf("captchaSolvers()[0] = %T, want *captcha.AntiCaptchaSolver (the configured order's first entry)", got[0])
	}
	if _, ok := got[1].(*captcha.TwoCaptchaSolver); !ok {
		t.Fatalf("captchaSolvers()[1] = %T, want *captcha.TwoCaptchaSolver", got[1])
	}
}
