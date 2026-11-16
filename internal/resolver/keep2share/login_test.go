package keep2share

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/hoster"
	"github.com/junkerderprovinz/knightloader/internal/resolver/keep2share/k2stest"
)

func TestAStatus400AtSignInIsAPauseAndNotAWrongPassword(t *testing.T) {
	srv := newServer(t, premium())
	link := srv.Add(fileID, k2stest.File{Name: "a.bin", Body: make([]byte, 4096)})
	// One for the check and one for the download.
	srv.Fail("login", http.StatusBadRequest, 2, "Invalid request params", 0)
	srv.Fail("login", http.StatusBadRequest, 2, "Invalid request params", 0)
	b, eng, ups := newBackend(srv)
	b.SetLogin(email, password)

	if st := b.Check(context.Background()); st.Refused || !b.Ready() {
		t.Fatalf("after a 400 at sign-in the login reads as %+v, want it neither refused nor out of routing", st)
	}
	b.Download("t1", link, nil, 1)
	awaitHandoff(t, eng)

	if got := ups.waited(); got != loginWait {
		t.Errorf("waited %v, want the pause of %v before signing in again", got, loginWait)
	}
	if st := b.Status(); !st.Checked || st.Refused || !st.Premium {
		t.Errorf("after the pause the login reads as %+v, want it signed in", st)
	}
}

func TestAnImageCaptchaAtSignInIsAnsweredAndTheLoginGoesThrough(t *testing.T) {
	acct := premium()
	acct.ImageCaptcha = true
	srv := newServer(t, acct)
	b, _, _ := newBackend(srv)
	a := &answers{code: k2stest.Code}
	b.Solve = a.solve
	b.SetLogin(email, password)

	if st := b.Check(context.Background()); st.Refused || !st.Premium {
		t.Fatalf("the login reads as %+v, want it signed in on premium", st)
	}
	if len(a.captchas) != 1 || a.captchas[0].Kind != hoster.Image || !bytes.Equal(a.captchas[0].Image, k2stest.Picture) {
		t.Errorf("captchas = %+v, want the API's picture once", a.captchas)
	}
	if n := srv.Logins(); n != 2 {
		t.Errorf("signed in %d times, want twice: once refused, once with the answer", n)
	}
}

func TestAReCaptchaAtSignInIsSolvedOnTheAPIsPageAndTheLoginGoesThrough(t *testing.T) {
	acct := premium()
	acct.Captcha = true
	srv := newServer(t, acct)
	b, _, _ := newBackend(srv)
	a := &answers{token: k2stest.Token}
	b.Solve = a.solve
	b.SetLogin(email, password)

	if st := b.Check(context.Background()); st.Refused || !st.Premium {
		t.Fatalf("the login reads as %+v, want it signed in on premium", st)
	}
	if len(a.captchas) != 1 {
		t.Fatalf("captchas = %+v, want one reCAPTCHA", a.captchas)
	}
	c := a.captchas[0]
	if c.Kind != hoster.Recaptcha || c.SiteKey != k2stest.SiteKey || !strings.HasPrefix(c.PageURL, srv.URL+"/api/v2/reCaptcha.html?id=") {
		t.Errorf("captcha = %+v, want the page's reCAPTCHA on the API's own address", c)
	}
	if !slices.Contains(srv.Requests(), "GET /api/v2/reCaptcha.html") {
		t.Errorf("requests %v, want the reCAPTCHA page read for its key", srv.Requests())
	}
}

func TestAWrongAnswerAtSignInIsTriedOnceAndLeavesTheLoginStanding(t *testing.T) {
	acct := premium()
	acct.Captcha = true
	srv := newServer(t, acct)
	link := srv.Add(fileID, k2stest.File{Name: "a.bin", Body: make([]byte, 4096)})
	b, eng, ups := newBackend(srv)
	a := &answers{token: "wrong"}
	b.Solve = a.solve
	b.SetLogin(email, password)

	b.Download("t1", link, nil, 1)
	u := awaitSettled(t, ups)

	if u.Reason != core.ReasonCaptcha || !strings.HasPrefix(u.Err, "Keep2Share: ") {
		t.Errorf("settled as %+v, want a captcha failure named after the site", u)
	}
	if n := srv.Logins(); n != 2 || len(a.captchas) != 1 {
		t.Errorf("signed in %d times on %d captchas, want one answer and no second one", n, len(a.captchas))
	}
	if st := b.Status(); st.Refused || st.Handed || !b.Ready() || len(eng.handoffs()) > 0 {
		t.Errorf("the login reads as %+v, want it still standing for the next try", st)
	}
}

func TestACaptchaNobodyAnswersAtSignInFailsAsACaptcha(t *testing.T) {
	acct := premium()
	acct.ImageCaptcha = true
	srv := newServer(t, acct)
	b, _, _ := newBackend(srv)
	b.Solve = func(context.Context, string, hoster.Captcha) (string, error) {
		return "", errors.New("nobody was there")
	}
	b.SetLogin(email, password)

	st := b.Check(context.Background())
	if st.Refused || st.Handed || !b.Ready() || srv.Logins() != 1 {
		t.Errorf("the login reads as %+v after %d sign-ins, want it standing without a second one", st, srv.Logins())
	}
}

func TestACaptchaAtSignInWithoutASolverRefusesTheLogin(t *testing.T) {
	for _, c := range []struct {
		name string
		acct func(*k2stest.Account)
	}{
		{"image", func(a *k2stest.Account) { a.ImageCaptcha = true }},
		{"recaptcha", func(a *k2stest.Account) { a.Captcha = true }},
	} {
		t.Run(c.name, func(t *testing.T) {
			acct := premium()
			c.acct(&acct)
			srv := newServer(t, acct)
			b, _, _ := newBackend(srv)
			b.SetLogin(email, password)

			if st := b.Check(context.Background()); !st.Refused || !st.Captcha {
				t.Errorf("the login reads as %+v, want it refused for a captcha", st)
			}
			if n := srv.Logins(); n != 1 {
				t.Errorf("signed in %d times, want once", n)
			}
		})
	}
}
