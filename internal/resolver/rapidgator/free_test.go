package rapidgator

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log"
	"os"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/hoster"
	"github.com/junkerderprovinz/knightloader/internal/ownlogin"
	"github.com/junkerderprovinz/knightloader/internal/resolver/rapidgator/rgtest"
)

// clock is the time the stand-in and the backend go by, moved on by the
// backend's waits and by the user answering a captcha.
type clock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *clock) now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *clock) pass(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
}

type answers struct {
	mu       sync.Mutex
	token    string
	captchas []hoster.Captcha
	// took is how long the user takes over each captcha.
	took  time.Duration
	clock *clock
}

func (a *answers) solve(_ context.Context, _ string, c hoster.Captcha) (string, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.captchas = append(a.captchas, c)
	a.clock.pass(a.took)
	return a.token, nil
}

func newFreeBackend(srv *rgtest.Server) (*FreeBackend, *fakeEngine, *updates, *answers) {
	eng := &fakeEngine{got: make(chan handoff, 4)}
	ups := &updates{waiting: make(chan struct{}, 1)}
	clk := &clock{t: time.Date(2026, 11, 19, 20, 0, 0, 0, time.UTC)}
	srv.Clock(clk.now)
	a := &answers{token: rgtest.Token, clock: clk}
	b := NewFreeBackend(NewClient(srv.URL, srv.Client()), eng, ups.add)
	b.Sleep = func(ctx context.Context, d time.Duration) error {
		clk.pass(d)
		return ups.sleep(ctx, d)
	}
	b.Solve, b.now = a.solve, clk.now
	return b, eng, ups, a
}

// awaitOutcome waits for the link to reach the engine, or for the task to end
// without it.
func awaitOutcome(t *testing.T, eng *fakeEngine, ups *updates) (handoff, bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case h := <-eng.got:
			return h, true
		default:
		}
		if u := ups.last(); u.Status == core.StatusError {
			return handoff{}, false
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("the task never settled: %+v", ups.list())
	return handoff{}, false
}

func TestAFreeDownloadSitsOutTheCountdownAnswersTheTurnstileAndGoesOutOnOneConnection(t *testing.T) {
	srv := rgtest.New(t)
	srv.Countdown(30)
	body := bytes.Repeat([]byte("darjeeling "), 1024)
	link := srv.Add(fileID, rgtest.File{Name: "The Darjeeling Limited.mkv", Body: body})
	b, eng, ups, a := newFreeBackend(srv)

	b.Download("t1", link, nil, 8)
	got := awaitHandoff(t, eng)

	if got.conns != 1 {
		t.Errorf("handed on for %d connections, want one", got.conns)
	}
	resp, err := srv.Client().Get(got.url)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if fetched, _ := io.ReadAll(resp.Body); !bytes.Equal(fetched, body) {
		t.Errorf("the link handed on does not serve the file: %s", got.url)
	}
	if len(a.captchas) != 1 || a.captchas[0].Kind != hoster.Turnstile || a.captchas[0].SiteKey != rgtest.SiteKey ||
		a.captchas[0].PageURL != "https://rapidgator.net/download/captcha" {
		t.Errorf("captchas = %+v, want the captcha page's Turnstile", a.captchas)
	}
	if ups.waited() != 31*time.Second {
		t.Errorf("waited %v, want the countdown and a second", ups.waited())
	}
	named := slices.ContainsFunc(ups.list(), func(u core.Update) bool { return u.Name == "The Darjeeling Limited.mkv" })
	if !named {
		t.Error("the free page's name never reached the task")
	}
	if slices.ContainsFunc(srv.Requests(), func(r string) bool { return strings.Contains(r, "/api/") }) {
		t.Error("a free download used the API")
	}
}

// The Turnstile widget puts its answer into the form as cf-turnstile-response,
// and the page's callback copies it into the form's own field.
func TestATurnstileAnswerGoesInUnderTheFieldsABrowserSends(t *testing.T) {
	srv := rgtest.New(t)
	srv.Countdown(30)
	link := srv.Add(fileID, rgtest.File{Name: "a.bin", Body: make([]byte, 64)})
	b, eng, _, _ := newFreeBackend(srv)

	b.Download("t1", link, nil, 1)
	awaitHandoff(t, eng)
	forms := srv.Answers()
	if len(forms) != 1 {
		t.Fatalf("posted %d captcha forms, want one", len(forms))
	}
	for _, field := range []string{"DownloadCaptchaForm[verifyCode]", "cf-turnstile-response"} {
		if got := forms[0].Get(field); got != rgtest.Token {
			t.Errorf("%s = %q, want the answer", field, got)
		}
	}
}

// Rapidgator bars the address for an hour when the answer comes in more than
// a couple of minutes after the countdown.
func TestACaptchaTheUserTakesMinutesOverStillLeadsToTheDownload(t *testing.T) {
	srv := rgtest.New(t)
	link := srv.Add(fileID, rgtest.File{Name: "a.bin", Body: make([]byte, 64)})
	b, eng, ups, a := newFreeBackend(srv)
	a.took = 2*time.Minute + 30*time.Second

	b.Download("t1", link, nil, 1)
	if _, ok := awaitOutcome(t, eng, ups); !ok {
		t.Fatalf("the download ended as %+v after %d captchas, want it handed on", ups.last(), len(a.captchas))
	}
	if len(a.captchas) != 1 {
		t.Errorf("asked %d captchas, want one", len(a.captchas))
	}
	if ups.waited() >= time.Hour {
		t.Errorf("waited %v, want no pause between free downloads", ups.waited())
	}
}

func TestACaptchaPageWithAnotherKeyThanTheSignInFormIsAnsweredThere(t *testing.T) {
	srv := rgtest.New(t)
	srv.Countdown(30)
	srv.LoginKey("0x4AAAAAAAsignInOnly")
	link := srv.Add(fileID, rgtest.File{Name: "a.bin", Body: make([]byte, 64)})
	b, eng, ups, a := newFreeBackend(srv)

	b.Download("t1", link, nil, 1)
	if _, ok := awaitOutcome(t, eng, ups); !ok {
		t.Fatalf("the download ended as %+v, want it handed on", ups.last())
	}
	if len(a.captchas) != 2 || a.captchas[1].SiteKey != rgtest.SiteKey {
		t.Errorf("captchas = %+v, want the captcha page's asked after the sign-in form's", a.captchas)
	}
}

// logLines collects the log until the test ends.
func logLines(t *testing.T) func() []string {
	var out bytes.Buffer
	log.SetOutput(&out)
	t.Cleanup(func() { log.SetOutput(os.Stderr) })
	return func() []string { return strings.Split(strings.TrimSpace(out.String()), "\n") }
}

func TestARefusedAnswerIsLoggedWithItsTimingAndRapidgatorsWords(t *testing.T) {
	lines := logLines(t)
	srv := rgtest.New(t)
	srv.Countdown(30)
	link := srv.Add(fileID, rgtest.File{Name: "a.bin", Body: make([]byte, 64)})
	b, _, ups, a := newFreeBackend(srv)
	a.token = "wrong"

	b.Download("t1", link, nil, 1)
	awaitSettled(t, ups)
	got := lines()
	if len(got) != len(srv.Answers()) {
		t.Errorf("logged %d lines for %d answers, want one each: %q", len(got), len(srv.Answers()), got)
	}
	want := "rapidgator: Rapidgator refused the captcha answer for file " + fileID +
		`, answer sent 0s after the countdown (failed_on_captcha not set): "Please fix the following input errors: The verification code is incorrect."`
	if !strings.HasSuffix(got[0], want) {
		t.Errorf("logged %q, want %q", got[0], want)
	}
}

func TestAnAnswerThatLeadsNowhereIsLoggedWithItsTimingAndRapidgatorsWords(t *testing.T) {
	lines := logLines(t)
	srv := rgtest.New(t)
	srv.LoginKey("0x4AAAAAAAsignInOnly")
	link := srv.Add(fileID, rgtest.File{Name: "a.bin", Body: make([]byte, 64)})
	b, _, ups, a := newFreeBackend(srv)
	a.took = 2*time.Minute + 30*time.Second

	b.Download("t1", link, nil, 1)
	awaitSettled(t, ups)
	got := lines()
	if len(got) != len(srv.Answers()) {
		t.Errorf("logged %d lines for %d answers, want one each: %q", len(got), len(srv.Answers()), got)
	}
	want := "rapidgator: no download link after the captcha for file " + fileID +
		`, answer sent 150s after the countdown (failed_on_captcha set): "You have reached your hourly downloads limit."`
	if !strings.HasSuffix(got[0], want) {
		t.Errorf("logged %q, want %q", got[0], want)
	}
}

func TestADelayBetweenFreeDownloadsIsSatOutBeforeTheCountdown(t *testing.T) {
	srv := rgtest.New(t)
	srv.Countdown(0)
	srv.Pause(1)
	link := srv.Add(fileID, rgtest.File{Name: "a.bin", Body: make([]byte, 64)})
	b, eng, ups, _ := newFreeBackend(srv)

	b.Download("t1", link, nil, 1)
	awaitHandoff(t, eng)
	if ups.waited() != 15*time.Minute+time.Second {
		t.Errorf("waited %v, want the 15 minutes the page asks for and the countdown's second", ups.waited())
	}
}

func TestAWrongAnswerIsTriedAgainAndThenFailsAsACaptcha(t *testing.T) {
	srv := rgtest.New(t)
	srv.Countdown(0)
	link := srv.Add(fileID, rgtest.File{Name: "a.bin", Body: make([]byte, 64)})
	b, _, ups, a := newFreeBackend(srv)
	a.token = "wrong"

	b.Download("t1", link, nil, 1)
	if u := awaitSettled(t, ups); u.Reason != core.ReasonCaptcha || len(a.captchas) != 3 {
		t.Errorf("settled as %+v after %d captchas, want a captcha failure after three", u, len(a.captchas))
	}
}

func TestAMissingFileFailsAsGoneBeforeAnyCountdown(t *testing.T) {
	srv := rgtest.New(t)
	b, _, ups, a := newFreeBackend(srv)

	b.Download("t1", "https://rapidgator.net/file/"+strings.Repeat("0", 32), nil, 1)
	if u := awaitSettled(t, ups); u.Reason != core.ReasonGone || ups.waited() != 0 || len(a.captchas) != 0 {
		t.Errorf("settled as %+v, want gone without a wait or a captcha", u)
	}
}

func TestTheFreeRefusalsReadAsTheirCause(t *testing.T) {
	for _, page := range []string{"<div>You can download files up to 500 MB in free mode</div>", "<div>This file can be downloaded by premium only</div>"} {
		if err := freeRefusal(page); !errors.Is(err, ErrPremiumOnly) {
			t.Errorf("%q reads as %v, want premium only", page, err)
		}
	}
	var pause *ownlogin.PauseError
	if err := freeRefusal("<div>You have reached your daily downloads limit. Wait</div>"); !errors.As(err, &pause) || pause.Wait != time.Hour {
		t.Errorf("the daily limit reads as %v, want an hour's pause", err)
	}
	if err := freeRefusal("<div>Downloading: a.bin</div>"); err != nil {
		t.Errorf("an ordinary page reads as %v", err)
	}
}
