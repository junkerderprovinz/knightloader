package ownlogin

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/hoster"
)

var errRefused = errors.New("hoster: the answer to the captcha was not accepted")

// freeHarness runs free downloads whose unlock goes through steps, one per
// try, and records what the runner did.
type freeHarness struct {
	mu       sync.Mutex
	ups      []core.Update
	slept    time.Duration
	captchas []hoster.Captcha
	answer   string
	solveErr error
}

func (h *freeHarness) add(_ string, u core.Update) {
	h.mu.Lock()
	h.ups = append(h.ups, u)
	h.mu.Unlock()
}

func (h *freeHarness) sleep(_ context.Context, d time.Duration) error {
	h.mu.Lock()
	h.slept += d
	h.mu.Unlock()
	return nil
}

func (h *freeHarness) solve(_ context.Context, _ string, c hoster.Captcha) (string, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.captchas = append(h.captchas, c)
	return h.answer, h.solveErr
}

func (h *freeHarness) last() core.Update {
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.ups) == 0 {
		return core.Update{}
	}
	return h.ups[len(h.ups)-1]
}

func (h *freeHarness) notes() []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	var out []string
	for _, u := range h.ups {
		if u.Note != "" {
			out = append(out, u.Note)
		}
	}
	return out
}

func newFree(h *freeHarness, eng Downloader, steps ...FreeUnlock) *Free {
	var mu sync.Mutex
	f := NewFree(eng, h.add, FreeConfig{
		Name: "Hoster",
		Unlock: func(ctx context.Context, taskID, link string, ask func(hoster.Captcha) (string, error), delay func(time.Duration) error) (string, error) {
			mu.Lock()
			step := steps[0]
			if len(steps) > 1 {
				steps = steps[1:]
			}
			mu.Unlock()
			return step(ctx, taskID, link, ask, delay)
		},
		Reason:  reasonOf,
		Refused: errRefused,
	})
	f.Sleep, f.Solve = h.sleep, h.solve
	return f
}

func awaitEnd(t *testing.T, h *freeHarness, eng handEngine) (core.Update, bool) {
	t.Helper()
	deadline := time.After(5 * time.Second)
	for {
		select {
		case <-eng.got:
			return core.Update{}, true
		case <-deadline:
			t.Fatal("the free download neither failed nor was handed on")
			return core.Update{}, false
		default:
		}
		if u := h.last(); u.Status == core.StatusError {
			return u, false
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// captchaThenCountdown asks for a Turnstile captcha, sits out a minute and
// answers with a link that names the answer.
func captchaThenCountdown(_ context.Context, _, _ string, ask func(hoster.Captcha) (string, error), delay func(time.Duration) error) (string, error) {
	answer, err := ask(hoster.Captcha{Kind: hoster.Turnstile, SiteKey: "0x4AAA", PageURL: "https://hoster.example/file/1"})
	if err != nil {
		return "", err
	}
	if err := delay(time.Minute); err != nil {
		return "", err
	}
	return "https://dl.hoster.example/" + answer, nil
}

func TestAFreeDownloadAsksTheCaptchaSitsOutTheCountdownAndGoesOutOnOneConnection(t *testing.T) {
	h := &freeHarness{answer: "token"}
	eng := handEngine{got: make(chan string, 1)}
	f := newFree(h, &countingEngine{handEngine: eng}, captchaThenCountdown)
	t.Cleanup(func() { f.Remove("t1", false) })

	f.Download("t1", "https://hoster.example/file/1", nil, 8)
	if _, handed := awaitEnd(t, h, eng); !handed {
		t.Fatalf("the download failed: %+v", h.last())
	}
	if len(h.captchas) != 1 || h.captchas[0].Kind != hoster.Turnstile || h.captchas[0].SiteKey != "0x4AAA" {
		t.Errorf("captchas = %+v, want the hoster's Turnstile", h.captchas)
	}
	if h.slept != time.Minute {
		t.Errorf("slept %v, want the minute the countdown asks", h.slept)
	}
	notes := strings.Join(h.notes(), "\n")
	if !strings.Contains(notes, "Waiting for the captcha Hoster asks for") || !strings.Contains(notes, "Waiting for Hoster: 60 s left") {
		t.Errorf("notes %q, want the captcha and the countdown shown", notes)
	}
}

func TestAPauseBetweenFreeDownloadsIsSatOutVisiblyAndCountsAsWaiting(t *testing.T) {
	h := &freeHarness{answer: "token"}
	eng := handEngine{got: make(chan string, 1)}
	pause := func(context.Context, string, string, func(hoster.Captcha) (string, error), func(time.Duration) error) (string, error) {
		return "", &PauseError{Wait: 20 * time.Minute}
	}
	f := newFree(h, &countingEngine{handEngine: eng}, pause, captchaThenCountdown)
	t.Cleanup(func() { f.Remove("t1", false) })

	f.Download("t1", "https://hoster.example/file/1", nil, 1)
	if _, handed := awaitEnd(t, h, eng); !handed {
		t.Fatalf("the download failed: %+v", h.last())
	}
	if h.slept != 21*time.Minute {
		t.Errorf("slept %v, want the pause and then the countdown", h.slept)
	}
	if notes := strings.Join(h.notes(), "\n"); !strings.Contains(notes, "Hoster makes this address wait between free downloads, 20 min left") {
		t.Errorf("notes %q, want the pause shown", notes)
	}
}

func TestAPauseLongerThanARunSitsOutFailsWithItsWaitForTheRetry(t *testing.T) {
	h := &freeHarness{}
	eng := handEngine{got: make(chan string, 1)}
	f := newFree(h, &countingEngine{handEngine: eng}, func(context.Context, string, string, func(hoster.Captcha) (string, error), func(time.Duration) error) (string, error) {
		return "", &PauseError{Wait: 30 * time.Hour}
	})
	t.Cleanup(func() { f.Remove("t1", false) })

	f.Download("t1", "https://hoster.example/file/1", nil, 1)
	u, _ := awaitEnd(t, h, eng)
	if u.Retry != 30*time.Hour || u.Reason != core.ReasonLimit {
		t.Errorf("failed with %+v, want a limit retried after the hoster's wait", u)
	}
}

func TestARefusedAnswerAsksAgainAndThenGivesUpAsACaptchaFailure(t *testing.T) {
	h := &freeHarness{answer: "wrong"}
	eng := handEngine{got: make(chan string, 1)}
	refused := func(_ context.Context, _, _ string, ask func(hoster.Captcha) (string, error), _ func(time.Duration) error) (string, error) {
		if _, err := ask(hoster.Captcha{Kind: hoster.Image, Image: []byte("png")}); err != nil {
			return "", err
		}
		return "", errRefused
	}
	f := newFree(h, &countingEngine{handEngine: eng}, refused)
	t.Cleanup(func() { f.Remove("t1", false) })

	f.Download("t1", "https://hoster.example/file/1", nil, 1)
	u, _ := awaitEnd(t, h, eng)
	if u.Reason != core.ReasonCaptcha || len(h.captchas) != 3 {
		t.Errorf("failed with %+v after %d captchas, want a captcha failure after three", u, len(h.captchas))
	}
}

func TestNobodyAnsweringFailsAsACaptcha(t *testing.T) {
	h := &freeHarness{solveErr: errors.New("timed out")}
	eng := handEngine{got: make(chan string, 1)}
	f := newFree(h, &countingEngine{handEngine: eng}, captchaThenCountdown)
	t.Cleanup(func() { f.Remove("t1", false) })

	f.Download("t1", "https://hoster.example/file/1", nil, 1)
	if u, _ := awaitEnd(t, h, eng); u.Reason != core.ReasonCaptcha || !strings.Contains(u.Err, "nobody answered") {
		t.Errorf("failed with %+v, want nobody answering the captcha", u)
	}
}

// countingEngine checks the connections a free download goes out on.
type countingEngine struct {
	handEngine
	mu    sync.Mutex
	conns int
}

func (e *countingEngine) Handover(taskID, url string, conns int, relink func(context.Context) (string, error)) {
	e.mu.Lock()
	e.conns = conns
	e.mu.Unlock()
	e.handEngine.Handover(taskID, url, conns, relink)
}

func TestAFreeDownloadGoesOutOnOneConnectionWhateverTheTaskAsks(t *testing.T) {
	h := &freeHarness{answer: "token"}
	eng := &countingEngine{handEngine: handEngine{got: make(chan string, 1)}}
	f := newFree(h, eng, captchaThenCountdown)
	t.Cleanup(func() { f.Remove("t1", false) })

	f.Download("t1", "https://hoster.example/file/1", nil, 8)
	if _, handed := awaitEnd(t, h, eng.handEngine); !handed {
		t.Fatalf("the download failed: %+v", h.last())
	}
	if eng.conns != 1 {
		t.Errorf("handed on for %d connections, want one", eng.conns)
	}
}
