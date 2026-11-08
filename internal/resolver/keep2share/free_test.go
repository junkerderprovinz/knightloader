package keep2share

import (
	"bytes"
	"context"
	"net/http"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/ownlogin"
	"github.com/junkerderprovinz/knightloader/internal/resolver/keep2share/k2stest"
)

type answers struct {
	mu       sync.Mutex
	code     string
	captchas []ownlogin.Captcha
}

func (a *answers) solve(_ context.Context, _ string, c ownlogin.Captcha) (string, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.captchas = append(a.captchas, c)
	return a.code, nil
}

func newFreeBackend(srv *k2stest.Server) (*FreeBackend, *fakeEngine, *updates, *answers) {
	eng := &fakeEngine{got: make(chan handoff, 4)}
	ups := &updates{}
	a := &answers{code: k2stest.Code}
	b := NewFreeBackend(Keep2Share, NewClient(srv.URL, srv.Client()), eng, ups.add)
	b.Sleep, b.Solve = ups.sleep, a.solve
	return b, eng, ups, a
}

func TestAFreeDownloadAnswersTheImageCaptchaSitsOutTheWaitAndGoesOutOnOneConnection(t *testing.T) {
	srv := k2stest.New(t, "k2s.cc")
	srv.FreeWait(30)
	link := srv.Add(fileID, k2stest.File{Name: "Holiday.part1.rar", Body: make([]byte, 4096)})
	b, eng, ups, a := newFreeBackend(srv)

	b.Download("t1", link, nil, 8)
	got := awaitHandoff(t, eng)

	if got.conns != 1 || !strings.Contains(got.url, "/download/"+fileID) {
		t.Errorf("handed on %s for %d connections, want the free link over one", got.url, got.conns)
	}
	if len(a.captchas) != 1 || a.captchas[0].Kind != ownlogin.Image || !bytes.Equal(a.captchas[0].Image, k2stest.Picture) {
		t.Errorf("captchas = %+v, want the API's picture", a.captchas)
	}
	if ups.waited() != 30*time.Second {
		t.Errorf("waited %v, want the 30 seconds the accepted captcha comes with", ups.waited())
	}
	if slices.Contains(srv.Requests(), "POST /api/v2/login") {
		t.Error("a free download signed in")
	}
}

func TestAWrongAnswerAsksForAFreshCaptcha(t *testing.T) {
	srv := k2stest.New(t, "k2s.cc")
	srv.FreeWait(0)
	link := srv.Add(fileID, k2stest.File{Name: "a.bin", Body: make([]byte, 64)})
	b, _, ups, a := newFreeBackend(srv)
	a.code = "00000"

	b.Download("t1", link, nil, 1)
	if u := awaitSettled(t, ups); u.Reason != core.ReasonCaptcha || len(a.captchas) != 3 {
		t.Errorf("settled as %+v after %d captchas, want a captcha failure after three", u, len(a.captchas))
	}
}

func TestTheFreeLimitsWaitOrFailAsTheAPIExplainsThem(t *testing.T) {
	for _, c := range []struct {
		name   string
		status int
		code   any
		sub    int
		want   core.Reason
		retry  time.Duration
	}{
		{name: "premium only", status: http.StatusNotAcceptable, code: 42, sub: 7, want: core.ReasonAuth},
		{name: "too large", status: http.StatusNotAcceptable, code: 21, sub: 3, want: core.ReasonAuth},
		{name: "banned for a day", status: http.StatusNotAcceptable, code: "captcha_need_wait_daily", want: core.ReasonLimit, retry: 24 * time.Hour},
	} {
		t.Run(c.name, func(t *testing.T) {
			srv := k2stest.New(t, "k2s.cc")
			link := srv.Add(fileID, k2stest.File{Name: "a.bin", Body: make([]byte, 64)})
			srv.Fail("getUrl", c.status, c.code, "Download is not available", c.sub)
			b, eng, ups, _ := newFreeBackend(srv)

			b.Download("t1", link, nil, 1)
			if u := awaitSettled(t, ups); u.Reason != c.want || u.Retry != c.retry || !strings.HasPrefix(u.Err, "Keep2Share: ") {
				t.Errorf("settled as %+v, want %q named after the site", u, c.want)
			}
			if len(eng.handoffs()) > 0 {
				t.Error("a refused download reached the engine")
			}
		})
	}
}

func TestAnHourlyLimitIsSatOutAndTheDownloadStartsAfterIt(t *testing.T) {
	srv := k2stest.New(t, "k2s.cc")
	srv.FreeWait(0)
	link := srv.Add(fileID, k2stest.File{Name: "a.bin", Body: make([]byte, 64)})
	srv.Fail("getUrl", http.StatusNotAcceptable, 21, "Download is not available", 1)
	b, eng, ups, a := newFreeBackend(srv)

	b.Download("t1", link, nil, 1)
	awaitHandoff(t, eng)
	if ups.waited() != time.Hour || len(a.captchas) != 2 {
		t.Errorf("waited %v after %d captchas, want the hour sat out before a fresh captcha", ups.waited(), len(a.captchas))
	}
}

func TestADeletedFileFailsAsGoneWithoutACaptcha(t *testing.T) {
	srv := k2stest.New(t, "k2s.cc")
	b, _, ups, a := newFreeBackend(srv)

	b.Download("t1", "https://k2s.cc/file/0000000000000", nil, 1)
	if u := awaitSettled(t, ups); u.Reason != core.ReasonGone || len(a.captchas) != 0 {
		t.Errorf("settled as %+v after %d captchas, want gone without a captcha", u, len(a.captchas))
	}
}

func TestEverySiteHasAFreeClientBelowTheDebridServices(t *testing.T) {
	for _, h := range Hosts {
		r := FreeResolver{Host: h}
		if r.Info().ID != h.ID+"-free" || r.Info().Prio != 42 {
			t.Errorf("%s: info %+v, want its own id at 42", h.Name, r.Info())
		}
	}
	if !(FreeResolver{Host: FileBoom}).Match("https://fboom.me/file/"+fileID) || (FreeResolver{Host: FileBoom}).Match("https://k2s.cc/file/"+fileID) {
		t.Error("FileBoom's free client claims the wrong links")
	}
}
