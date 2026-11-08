package rapidgator

import (
	"bytes"
	"context"
	"errors"
	"io"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/ownlogin"
	"github.com/junkerderprovinz/knightloader/internal/resolver/rapidgator/rgtest"
)

type answers struct {
	mu       sync.Mutex
	token    string
	captchas []ownlogin.Captcha
}

func (a *answers) solve(_ context.Context, _ string, c ownlogin.Captcha) (string, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.captchas = append(a.captchas, c)
	return a.token, nil
}

func newFreeBackend(srv *rgtest.Server) (*FreeBackend, *fakeEngine, *updates, *answers) {
	eng := &fakeEngine{got: make(chan handoff, 4)}
	ups := &updates{waiting: make(chan struct{}, 1)}
	a := &answers{token: rgtest.Token}
	b := NewFreeBackend(NewClient(srv.URL, srv.Client()), eng, ups.add)
	b.Sleep, b.Solve = ups.sleep, a.solve
	return b, eng, ups, a
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
	if len(a.captchas) != 1 || a.captchas[0].Kind != ownlogin.Turnstile || a.captchas[0].SiteKey != rgtest.SiteKey ||
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
