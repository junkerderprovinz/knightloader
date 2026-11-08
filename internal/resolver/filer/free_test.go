package filer

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/ownlogin"
	"github.com/junkerderprovinz/knightloader/internal/resolver/filer/filertest"
)

type freeHarness struct {
	mu       sync.Mutex
	answer   string
	captchas []ownlogin.Captcha
	slept    time.Duration
}

func (h *freeHarness) solve(_ context.Context, _ string, c ownlogin.Captcha) (string, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.captchas = append(h.captchas, c)
	return h.answer, nil
}

func (h *freeHarness) sleep(_ context.Context, d time.Duration) error {
	h.mu.Lock()
	h.slept += d
	h.mu.Unlock()
	return nil
}

func newFreeBackend(srv *filertest.Server) (*FreeBackend, *fakeEngine, *updates, *freeHarness) {
	eng := &fakeEngine{got: make(chan handoff, 4)}
	ups := &updates{}
	h := &freeHarness{answer: filertest.Token}
	b := NewFreeBackend(NewClient(srv.URL, srv.Client()), eng, ups.add)
	b.Solve, b.Sleep = h.solve, h.sleep
	return b, eng, ups, h
}

func TestAFreeDownloadAnswersTheHCaptchaSitsOutTheTicketsWaitAndGoesOutOnOneConnection(t *testing.T) {
	srv := filertest.New(t)
	srv.FreeWait(30)
	link := srv.Add(fileID, filertest.File{Name: "Rushmore.mkv", Body: make([]byte, 2048)})
	b, eng, ups, h := newFreeBackend(srv)

	b.Download("t1", link, nil, 8)
	got := awaitHandoff(t, eng)

	if got.conns != 1 || !strings.HasSuffix(got.url, "/download/"+fileID) {
		t.Errorf("handed on %s for %d connections, want the ticket's link over one", got.url, got.conns)
	}
	if len(h.captchas) != 1 || h.captchas[0].Kind != ownlogin.HCaptcha || h.captchas[0].SiteKey != hCaptchaSite ||
		h.captchas[0].PageURL != "https://filer.net/get/"+fileID {
		t.Errorf("captchas = %+v, want the download page's hCaptcha", h.captchas)
	}
	if h.slept != 40*time.Second {
		t.Errorf("slept %v, want the ticket's 30 seconds and the slack", h.slept)
	}
	shown := false
	for _, u := range ups.list() {
		shown = shown || strings.Contains(u.Note, "Waiting for filer.net:")
	}
	if !shown {
		t.Error("the ticket's wait never showed on the task")
	}
}

func TestTheHourlyLimitIsSatOutBeforeTheNextCaptcha(t *testing.T) {
	srv := filertest.New(t)
	srv.FreeWait(0)
	srv.HourlyLimit(1)
	link := srv.Add(fileID, filertest.File{Name: "a.bin", Body: make([]byte, 64)})
	b, eng, _, h := newFreeBackend(srv)

	b.Download("t1", link, nil, 1)
	awaitHandoff(t, eng)
	if h.slept < time.Hour || len(h.captchas) != 2 {
		t.Errorf("slept %v after %d captchas, want the hour sat out and a fresh captcha", h.slept, len(h.captchas))
	}
}

func TestAFreeDownloadThatCannotGoAheadFailsWithItsCause(t *testing.T) {
	for _, c := range []struct {
		name   string
		file   *filertest.File
		answer string
		want   core.Reason
	}{
		{name: "gone", want: core.ReasonGone},
		{name: "premium only", file: &filertest.File{Name: "a.bin", PremiumOnly: true}, answer: filertest.Token, want: core.ReasonAuth},
		{name: "wrong answer", file: &filertest.File{Name: "a.bin"}, answer: "nope", want: core.ReasonCaptcha},
	} {
		t.Run(c.name, func(t *testing.T) {
			srv := filertest.New(t)
			link := "https://filer.net/get/" + fileID
			if c.file != nil {
				link = srv.Add(fileID, *c.file)
			}
			b, eng, ups, h := newFreeBackend(srv)
			h.answer = c.answer

			b.Download("t1", link, nil, 1)
			if u := awaitSettled(t, ups); u.Reason != c.want {
				t.Errorf("settled as %+v, want %q", u, c.want)
			}
			if len(eng.handoffs()) > 0 {
				t.Error("a failed download reached the engine")
			}
		})
	}
}

func TestTheFreeClientClaimsEveryFileLink(t *testing.T) {
	r := FreeResolver{}
	if !r.Match("https://filer.net/get/"+fileID) || r.Match("https://filer.net/folder/"+fileID) {
		t.Error("the free client claims the wrong links")
	}
	if r.Info().Prio != 42 {
		t.Errorf("prio = %d, want 42, below the debrid services and above JDownloader's free mode", r.Info().Prio)
	}
}
