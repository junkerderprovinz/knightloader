package app

import (
	"context"
	"testing"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/captcha"
	"github.com/junkerderprovinz/knightloader/internal/hoster"
	"github.com/junkerderprovinz/knightloader/internal/resolver/filer"
	"github.com/junkerderprovinz/knightloader/internal/resolver/filer/filertest"
)

// handedLinks is an engine that only records the links handed to it.
type handedLinks chan string

func (h handedLinks) Handover(_, url string, _ int, _ func(context.Context) (string, error)) {
	h <- url
}
func (handedLinks) Pause(string)        {}
func (handedLinks) Resume(string)       {}
func (handedLinks) Remove(string, bool) {}

// askedFor waits until a captcha an own client asked for at host is listed.
func askedFor(t *testing.T, a *App, host string) captcha.Challenge {
	t.Helper()
	var c captcha.Challenge
	waitFor(t, "the captcha reaching the prompts", func() bool {
		for _, ch := range a.CaptchaChallenges() {
			if ch.Source == captcha.SourceAsk && ch.Host == host {
				c = ch
				return true
			}
		}
		return false
	})
	return c
}

// The widget is loaded under the hoster's page on the phone and handed to a
// paid solver with that page, so every widget names it, and an image travels
// as a picture the prompts can show.
func TestEveryCaptchaAnOwnClientAsksForReachesThePromptsReadyToAnswer(t *testing.T) {
	const page = "https://hoster.example/file/1"
	for _, tc := range []struct {
		kind   hoster.CaptchaKind
		vendor string
	}{
		{hoster.Turnstile, captcha.VendorTurnstile},
		{hoster.Recaptcha, captcha.VendorRecaptcha},
		{hoster.HCaptcha, captcha.VendorHCaptcha},
		{hoster.Image, ""},
	} {
		t.Run(string(tc.kind), func(t *testing.T) {
			a := newCaptchaTestApp(t)
			answered := make(chan string, 1)
			go func() {
				c := hoster.Captcha{Kind: tc.kind, SiteKey: "site-key", PageURL: page, Image: []byte("\x89PNG\r\n\x1a\n")}
				token, err := a.solveFor("hoster.example")(context.Background(), "t1", c)
				if err != nil {
					t.Error(err)
				}
				answered <- token
			}()
			c := askedFor(t, a, "hoster.example")

			switch p := c.Payload.(type) {
			case *captcha.WidgetPayload:
				if c.Kind != captcha.KindWidget || p.Vendor != tc.vendor || p.SiteKey != "site-key" || p.SiteURL != page || p.ContextURL != "https://hoster.example" {
					t.Errorf("listed %+v with %+v, want the %s widget on the hoster's page", c, p, tc.vendor)
				}
			case *captcha.ImagePayload:
				if tc.kind != hoster.Image || c.Kind != captcha.KindImage || p.DataURL != "data:image/png;base64,iVBORw0KGgo=" {
					t.Errorf("listed %+v with %+v, want the picture", c, p)
				}
			default:
				t.Fatalf("listed %+v, want a widget or a picture", c)
			}
			if c.TaskID != "t1" || c.ExpiresAt.IsZero() {
				t.Errorf("listed %+v, want the task and a deadline", c)
			}

			if res, err := a.AnswerCaptcha(context.Background(), c.ID, "the-token"); err != nil || !res.StillValid {
				t.Fatalf("AnswerCaptcha = %+v, %v", res, err)
			}
			select {
			case got := <-answered:
				if got != "the-token" {
					t.Errorf("the client got %q, want the answer", got)
				}
			case <-time.After(10 * time.Second):
				t.Fatal("the answer never reached the client")
			}
		})
	}
}

// A free download on a hoster's own client asks through the same prompts as
// JDownloader's captchas, a paid solver's answer reaches the hoster's form,
// and the hoster hands out the link for it.
func TestAPaidSolverAnswersTheCaptchaOfAFreeDownload(t *testing.T) {
	a := newCaptchaTestApp(t)
	srv := filertest.New(t)
	srv.FreeWait(0)
	link := srv.Add("dv6qg8tfckhk7mja", filertest.File{Name: "Isle of Dogs.mkv", Body: make([]byte, 1024)})
	got := make(handedLinks, 1)
	fb := filer.NewFreeBackend(filer.NewClient(srv.URL, srv.Client()), got, a.onUpdate)
	fb.Sleep = func(ctx context.Context, _ time.Duration) error { return ctx.Err() }
	a.newOwnFree(filer.FreeResolverID, filer.Site, filer.FreeResolver{}, fb.Free, fb, func() bool { return false })

	fb.Download("t1", link, nil, 1)
	c := askedFor(t, a, filer.Site)
	if w, ok := c.Payload.(*captcha.WidgetPayload); !ok || w.Vendor != captcha.VendorHCaptcha || w.SiteURL != "https://filer.net/get/dv6qg8tfckhk7mja" {
		t.Fatalf("listed %+v with %+v, want filer.net's hCaptcha on the download page", c, c.Payload)
	}
	solver := &fakeSolver{text: filertest.Token}
	a.solveCaptchaWith(paid(solver), c)

	select {
	case url := <-got:
		if url == "" {
			t.Error("an empty link was handed on")
		}
	case <-time.After(10 * time.Second):
		t.Fatal("filer.net handed out no link for the solver's answer")
	}
	if solver.calls.Load() != 1 {
		t.Errorf("the solver was asked %d times, want once", solver.calls.Load())
	}
}
