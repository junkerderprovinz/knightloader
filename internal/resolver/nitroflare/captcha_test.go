package nitroflare

import (
	"context"
	"errors"
	"net/http"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/hoster"
	"github.com/junkerderprovinz/knightloader/internal/resolver/nitroflare/nftest"
)

// solver answers every captcha with token and keeps what it was asked.
type solver struct {
	mu       sync.Mutex
	token    string
	err      error
	captchas []hoster.Captcha
	tasks    []string
}

func (s *solver) solve(_ context.Context, taskID string, c hoster.Captcha) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.captchas = append(s.captchas, c)
	s.tasks = append(s.tasks, taskID)
	return s.token, s.err
}

func captchaAccount() nftest.Account {
	a := premium()
	a.Captcha = true
	return a
}

func TestTheAPIsCaptchaIsSolvedAndTheSignInMadeAgain(t *testing.T) {
	srv := newServer(t, captchaAccount())
	b, _, _ := newBackend(srv)
	s := &solver{token: nftest.Token}
	b.Solve = s.solve
	b.SetLogin(email, password)

	if st := b.Check(context.Background()); st.Refused || !st.Premium {
		t.Fatalf("the login reads as %+v, want it signed in on premium", st)
	}
	if len(s.captchas) != 1 {
		t.Fatalf("captchas = %+v, want one", s.captchas)
	}
	c := s.captchas[0]
	if c.Kind != hoster.Recaptcha || c.SiteKey != siteKey || c.PageURL != srv.URL+"/api/v2/solveCaptcha?user=john%40example.com" {
		t.Errorf("captcha = %+v, want Nitroflare's reCAPTCHA on its solveCaptcha page", c)
	}
	want := []string{"GET /api/v2/getKeyInfo", "POST /api/v2/solveCaptcha", "GET /api/v2/solveCaptcha", "GET /api/v2/getKeyInfo"}
	if got := srv.Requests(); !slices.Equal(got, want) {
		t.Errorf("requests %v, want %v", got, want)
	}
}

func TestACaptchaAtTheDownloadLinkIsSolvedForTheTask(t *testing.T) {
	srv := newServer(t, premium())
	link := srv.Add(fileID, nftest.File{Name: "a.bin", Body: make([]byte, 4096)})
	srv.Fail("getDownloadLink", http.StatusOK, 12, "Captcha required")
	b, eng, ups := newBackend(srv)
	s := &solver{token: nftest.Token}
	b.Solve = s.solve
	b.SetLogin(email, password)

	b.Download("t1", link, nil, 1)
	awaitHandoff(t, eng)

	if !slices.Equal(s.tasks, []string{"t1"}) {
		t.Errorf("captchas were asked for %q, want the one task", s.tasks)
	}
	if !slices.ContainsFunc(ups.list(), func(u core.Update) bool { return strings.Contains(u.Note, "captcha Nitroflare") }) {
		t.Errorf("the task never said it waits for the captcha: %+v", ups.list())
	}
}

func TestAWrongAnswerIsTriedOnceAndLeavesTheLoginStanding(t *testing.T) {
	srv := newServer(t, captchaAccount())
	link := srv.Add(fileID, nftest.File{Name: "a.bin", Body: make([]byte, 4096)})
	b, eng, ups := newBackend(srv)
	s := &solver{token: "wrong"}
	b.Solve = s.solve
	b.SetLogin(email, password)

	b.Download("t1", link, nil, 1)
	u := awaitSettled(t, ups)

	if u.Reason != core.ReasonCaptcha {
		t.Errorf("settled as %+v, want a captcha failure", u)
	}
	if len(s.captchas) != 1 {
		t.Errorf("asked %d captchas, want one", len(s.captchas))
	}
	if st := b.Status(); st.Refused || st.Handed || !b.Ready() || len(eng.handoffs()) > 0 {
		t.Errorf("the login reads as %+v, want it still standing for the next try", st)
	}
}

func TestACaptchaNobodyAnswersFailsAsACaptcha(t *testing.T) {
	srv := newServer(t, captchaAccount())
	b, _, _ := newBackend(srv)
	b.Solve = (&solver{err: errors.New("nobody was there")}).solve
	b.SetLogin(email, password)

	st := b.Check(context.Background())
	if st.Refused || st.Handed || !b.Ready() {
		t.Errorf("the login reads as %+v, want it standing", st)
	}
	if got := srv.Requests(); slices.Contains(got, "POST /api/v2/solveCaptcha") {
		t.Errorf("requests %v, want no answer posted", got)
	}
}

func TestTheEmailAddressGoesOutInLowerCase(t *testing.T) {
	srv := newServer(t, premium())
	b, _, _ := newBackend(srv)
	b.SetLogin("John@Example.COM", password)

	if st := b.Check(context.Background()); st.Refused || !st.Premium {
		t.Errorf("the login reads as %+v, want the address taken in lower case", st)
	}
}
