package app

// Captcha wiring (app_captcha.go) and dispatchLocked's captcha check. KL_JD is
// unset, so captcha.JDSource answers ErrJDNotConfigured without a network call;
// the tests seed challenges straight into the store instead of faking a Source.

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
	calls int
	text  string
	err   error
	// waitForDone blocks Solve until ctx is done and returns ctx.Err(), as a
	// real HTTP client does with an expired context.
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

// The first solver to succeed wins and later ones are not tried.
func TestSolveCaptchaWithStopsAtFirstSuccess(t *testing.T) {
	a := newCaptchaTestApp(t)
	first := &fakeSolver{text: "ABCD"}
	second := &fakeSolver{text: "should never run"}

	a.solveCaptchaWith([]captcha.Solver{first, second}, imageChallenge("c1"))

	if first.calls != 1 {
		t.Errorf("first solver called %d times, want exactly 1", first.calls)
	}
	if second.calls != 0 {
		t.Errorf("second solver called %d times, want 0; the first already succeeded", second.calls)
	}
}

// A failing solver passes the challenge on to the next one.
func TestSolveCaptchaWithFallsThroughOnFailure(t *testing.T) {
	a := newCaptchaTestApp(t)
	failing := &fakeSolver{err: errors.New("captcha unsolvable")}
	succeeding := &fakeSolver{text: "EFGH"}

	a.solveCaptchaWith([]captcha.Solver{failing, succeeding}, imageChallenge("c1"))

	if failing.calls != 1 {
		t.Errorf("failing solver called %d times, want exactly 1", failing.calls)
	}
	if succeeding.calls != 1 {
		t.Errorf("succeeding solver called %d times, want exactly 1 after the first failed", succeeding.calls)
	}
}

// Widget and unsupported challenges, and images without data, never reach a
// solver.
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

// No configured solver is the ordinary case and must not panic.
func TestSolveCaptchaWithNoSolversIsANoop(t *testing.T) {
	a := newCaptchaTestApp(t)
	a.solveCaptchaWith(nil, imageChallenge("c1"))
}

// Once a challenge has expired, no further solver is tried.
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
		t.Errorf("second solver called %d times, want 0; the challenge had already expired", second.calls)
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
	// CaptchaSolverOrder's order, not the catalogue's.
	if _, ok := got[0].(*captcha.AntiCaptchaSolver); !ok {
		t.Fatalf("captchaSolvers()[0] = %T, want *captcha.AntiCaptchaSolver (the configured order's first entry)", got[0])
	}
	if _, ok := got[1].(*captcha.TwoCaptchaSolver); !ok {
		t.Fatalf("captchaSolvers()[1] = %T, want *captcha.TwoCaptchaSolver", got[1])
	}
}
