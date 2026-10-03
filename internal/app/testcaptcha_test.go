package app

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/accounts"
	"github.com/junkerderprovinz/knightloader/internal/captcha"
	"github.com/junkerderprovinz/knightloader/internal/settings"
)

// broadcastOf reports whether a message of typ about id reached f.
func broadcastOf(f *activityFakeConn, typ, id string) bool {
	for _, raw := range f.snapshot() {
		var m struct {
			Type string `json:"type"`
			Data struct {
				ID string `json:"id"`
			} `json:"data"`
		}
		if json.Unmarshal(raw, &m) == nil && m.Type == typ && m.Data.ID == id {
			return true
		}
	}
	return false
}

// testResults lists the test results every captchaResolved for id reached f
// with.
func testResults(f *activityFakeConn, id string) []*TestCaptchaResult {
	var out []*TestCaptchaResult
	for _, raw := range f.snapshot() {
		var m struct {
			Type string            `json:"type"`
			Data CaptchaResolution `json:"data"`
		}
		if json.Unmarshal(raw, &m) == nil && m.Type == "captchaResolved" && m.Data.ID == id {
			out = append(out, m.Data.Test)
		}
	}
	return out
}

func TestATestCaptchaArrivesAndEndsLikeOneFromJD(t *testing.T) {
	a := newCaptchaTestApp(t)
	viewer := addViewer(t, a)
	jd := answeringTo(a)

	c, err := a.CreateTestCaptcha(false)
	if err != nil {
		t.Fatal(err)
	}
	if !a.captchaPending(c.ID) {
		t.Fatal("the test captcha is not in the list once created")
	}
	waitFor(t, "the test captcha's arrival", func() bool { return broadcastOf(viewer, "captcha", c.ID) })

	res, err := a.AnswerCaptcha(context.Background(), c.ID, "wrong")
	if err != nil {
		t.Fatal(err)
	}
	if !res.StillValid || res.Test == nil || res.Test.Correct || len(res.Test.Want) != 5 || res.Test.Given != "wrong" {
		t.Fatalf("the answer came back %+v with %+v, want a wrong answer and the five drawn characters", res, res.Test)
	}
	if got := jd.got(); len(got) != 0 {
		t.Errorf("JD was sent %q for a test captcha", got)
	}
	if a.captchaPending(c.ID) {
		t.Error("the answered test captcha is still pending")
	}
	waitFor(t, "the test captcha's end", func() bool { return len(testResults(viewer, c.ID)) > 0 })
	if got := testResults(viewer, c.ID); got[0] == nil || got[0].Correct || got[0].Want != res.Test.Want {
		t.Errorf("the end was broadcast with %+v, want the same wrong result", got[0])
	}
}

func TestATestCaptchaStaysOutOfJDsWayWhenSkipped(t *testing.T) {
	a := newCaptchaTestApp(t)

	c, err := a.CreateTestCaptcha(false)
	if err != nil {
		t.Fatal(err)
	}
	if err := a.AbortCaptcha(t.Context(), c.ID, captcha.AbortSkipOnce); err != nil {
		t.Fatalf("skipping a test captcha without JD failed: %v", err)
	}
	if a.captchaPending(c.ID) {
		t.Error("the skipped test captcha is still pending")
	}
	if got := a.RefreshCaptchas(t.Context()); len(got) != 0 {
		t.Errorf("the skipped test captcha came back with the next poll: %+v", got)
	}
}

func TestATestCaptchaShowsWhileJDCannotBeAsked(t *testing.T) {
	a := newCaptchaTestApp(t)
	pending(a, imageChallenge("c1"))

	c, err := a.CreateTestCaptcha(false)
	if err != nil {
		t.Fatal(err)
	}
	if !a.captchaPending(c.ID) {
		t.Error("the test captcha did not arrive while JD could not be asked")
	}
	if !a.captchaPending("c1") {
		t.Error("the JD captcha was dropped when the test captcha arrived")
	}
}

func TestNoTestCaptchaWhileCaptchasAreSwitchedOff(t *testing.T) {
	a := newCaptchaTestApp(t)
	if _, err := a.ApplySettings(settings.Settings{
		MaxConcurrent: 4, MaxPerHost: 4, DownloadDir: t.TempDir(), ModulesOff: []string{"captcha"},
	}); err != nil {
		t.Fatal(err)
	}

	if _, err := a.CreateTestCaptcha(false); !errors.Is(err, ErrCaptchaOff) {
		t.Errorf("CreateTestCaptcha with captchas off = %v, want ErrCaptchaOff", err)
	}
	if got := a.CaptchaChallenges(); len(got) != 0 {
		t.Errorf("a test captcha was put up while captchas are off: %+v", got)
	}
}

// A captcha account bills a test captcha like any other, so it only gets one
// that was sent to it. Somebody is watching, so the one it gets waits for them
// and never reaches the provider.
func TestATestCaptchaGoesToTheCaptchaAccountsOnlyWhenSentToThem(t *testing.T) {
	a := newCaptchaTestApp(t)
	if _, err := a.ApplySettings(settings.Settings{
		MaxConcurrent: 4, MaxPerHost: 4, DownloadDir: t.TempDir(),
		CaptchaSolverOrder:         []string{"2captcha"},
		CaptchaSolverOnlyUnwatched: true, CaptchaSolverWait: settings.MaxCaptchaSolverWait,
	}); err != nil {
		t.Fatal(err)
	}
	if err := a.Accounts.SetCredential("2captcha", "", accounts.Credential{APIKey: "fake-2captcha-key"}); err != nil {
		t.Fatal(err)
	}
	addViewer(t, a)

	mine, err := a.CreateTestCaptcha(false)
	if err != nil {
		t.Fatal(err)
	}
	theirs, err := a.CreateTestCaptcha(true)
	if err != nil {
		t.Fatal(err)
	}

	waitFor(t, "the captcha account to take the test captcha sent to it", func() bool {
		c, _ := a.captchaStateFor().store.Get(theirs.ID)
		return c.Solver != nil && c.Solver.State == captcha.SolverWaiting
	})
	if c, _ := a.captchaStateFor().store.Get(mine.ID); c.Solver != nil {
		t.Errorf("the test captcha not sent to the captcha accounts has a solver report: %+v", c.Solver)
	}
	if !a.captchaStateFor().paid.claim(mine.ID, time.Now()) {
		t.Error("the test captcha not sent to the captcha accounts was claimed for them")
	}
}

// The Modules page lets the captcha module back on only once JD is, so the
// refusal names JD.
func TestATestCaptchaRefusedForJDBeingOffSaysSo(t *testing.T) {
	a := newCaptchaTestApp(t)
	if _, err := a.ApplySettings(settings.Settings{
		MaxConcurrent: 4, MaxPerHost: 4, DownloadDir: t.TempDir(), ModulesOff: []string{"jd"},
	}); err != nil {
		t.Fatal(err)
	}

	if _, err := a.CreateTestCaptcha(false); !errors.Is(err, ErrCaptchaJDOff) {
		t.Errorf("CreateTestCaptcha with JD off = %v, want ErrCaptchaJDOff", err)
	}
}

func TestNoTestCaptchaForTheCaptchaAccountsWhileNoneHasAKey(t *testing.T) {
	a := newCaptchaTestApp(t)
	if _, err := a.ApplySettings(settings.Settings{
		MaxConcurrent: 4, MaxPerHost: 4, DownloadDir: t.TempDir(), CaptchaSolverOrder: []string{"2captcha"},
	}); err != nil {
		t.Fatal(err)
	}

	if _, err := a.CreateTestCaptcha(true); !errors.Is(err, ErrNoCaptchaAccount) {
		t.Errorf("CreateTestCaptcha for the accounts without a key = %v, want ErrNoCaptchaAccount", err)
	}
	if got := a.CaptchaChallenges(); len(got) != 0 {
		t.Errorf("a test captcha nobody could take was put up: %+v", got)
	}
	if _, err := a.CreateTestCaptcha(false); err != nil {
		t.Errorf("a test captcha for people only was refused: %v", err)
	}
}

func TestAWaitingTestCaptchaLeavesTheCaptchaHealthAlone(t *testing.T) {
	a := newCaptchaTestApp(t)
	t.Setenv("KL_JD", "http://127.0.0.1:1")

	if _, err := a.CreateTestCaptcha(false); err != nil {
		t.Fatal(err)
	}
	if row := a.captchaSubsystem(); row.State != StateOK || row.Remedy != "" {
		t.Errorf("with only a test captcha waiting the captcha row reads %q with remedy %q, want %q", row.State, row.Remedy, StateOK)
	}
}

// A download waits on a captcha that times out, but nothing waits on a test
// one, so its end says what it was.
func TestTheEndOfATestCaptchaSaysItWasATest(t *testing.T) {
	a := newCaptchaTestApp(t)
	viewer := addViewer(t, a)

	c, err := a.CreateTestCaptcha(false)
	if err != nil {
		t.Fatal(err)
	}
	a.settleCaptcha(c, "timedOut")

	waitFor(t, "the test captcha's end", func() bool { return broadcastOf(viewer, "captchaResolved", c.ID) })
	for _, raw := range viewer.snapshot() {
		var m struct {
			Type string            `json:"type"`
			Data CaptchaResolution `json:"data"`
		}
		if json.Unmarshal(raw, &m) == nil && m.Type == "captchaResolved" && m.Data.ID == c.ID && !m.Data.TestCaptcha {
			t.Errorf("the test captcha ended as %+v, not marked as a test", m.Data)
		}
	}
}
