package rapidgator

import (
	"bytes"
	"context"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/hoster"
	"github.com/junkerderprovinz/knightloader/internal/ownlogin"
	"github.com/junkerderprovinz/knightloader/internal/resolver/rapidgator/rgtest"
)

const notActivatedAnswer = "Error: Please activate your account! Before you can login, you must activate your account with the link sent to your email address."

// refusedByTheAPI is an account the API calls not activated while the
// website signs it in.
func refusedByTheAPI(a rgtest.Account) rgtest.Account {
	a.APIRefuses = notActivatedAnswer
	return a
}

func TestALoginTheAPICallsNotActivatedSignsInOnTheWebsite(t *testing.T) {
	srv := newServer(t, refusedByTheAPI(premium()))
	b, _, _ := newBackend(srv)
	b.SetLogin(email, password)

	st := b.Check(context.Background())

	if st.Refused || !st.Premium {
		t.Fatalf("the login reads as %+v, want it premium through the website", st)
	}
	if want := time.Date(2027, 3, 1, 23, 59, 59, 0, time.UTC); !st.Expiry.Equal(want) {
		t.Errorf("expiry = %v, want the end of %v", st.Expiry, want.Format("2006-01-02"))
	}
	if st.TrafficMax != 1<<40 || st.TrafficLeft != 1<<40 {
		t.Errorf("traffic = %d of %d, want the website's figures", st.TrafficLeft, st.TrafficMax)
	}
	if !slices.Contains(srv.Requests(), "POST /auth/login") {
		t.Errorf("the website was never signed in to: %v", srv.Requests())
	}
}

func TestTheWebsiteAsksForTheCodeAndTakesItOnce(t *testing.T) {
	srv := newServer(t, refusedByTheAPI(twoFactor()))
	link := srv.Add(fileID, rgtest.File{Name: "a.bin", Body: make([]byte, 4096)})
	b, eng, _ := newBackend(srv)
	var kept []string
	b.Saved = func(_, _, token string) { kept = append(kept, token) }
	b.SetLogin(email, password)

	st := b.Check(context.Background())
	if !st.Refused || st.Code != ownlogin.CodeNeeded {
		t.Fatalf("a login without its code reads as %+v, want it waiting for the code", st)
	}
	st = b.SignIn(context.Background(), "482913")
	if st.Refused || !st.Premium {
		t.Fatalf("signing in with the code reads as %+v, want a premium login", st)
	}
	if got := srv.SiteCodes(); !slices.Equal(got, []string{"", "482913"}) {
		t.Errorf("the website was sent the codes %q, want none and then the one entered", got)
	}
	if n := srv.Logins(); n != 1 {
		t.Errorf("the API was asked %d times, want once before the website took over", n)
	}

	b.Download("t1", link, nil, 1)
	if h := awaitHandoff(t, eng); !strings.HasPrefix(h.url, srv.URL+"/download/"+fileID) {
		t.Errorf("handed %q, want the premium download link of the file page", h.url)
	}
	if len(kept) == 0 || !strings.HasPrefix(kept[len(kept)-1], sitePrefix) {
		t.Fatalf("kept %q, want the website session", kept)
	}
	for _, secret := range []string{password, "482913"} {
		if strings.Contains(kept[len(kept)-1], secret) {
			t.Errorf("the kept session carries %q", secret)
		}
	}
}

func TestAWrongCodeOnTheWebsiteIsToldApartFromAMissingOne(t *testing.T) {
	srv := newServer(t, refusedByTheAPI(twoFactor()))
	b, _, _ := newBackend(srv)
	b.SetLogin(email, password)
	b.Check(context.Background())

	st := b.SignIn(context.Background(), "000000")

	if !st.Refused || st.Code != ownlogin.CodeWrong {
		t.Errorf("a wrong code reads as %+v, want it turned down as a wrong code", st)
	}
}

func TestAWrongPasswordOnTheWebsiteIsTurnedDownAsSuch(t *testing.T) {
	srv := newServer(t, refusedByTheAPI(premium()))
	b, _, _ := newBackend(srv)
	b.SetLogin(email, "letmein")

	st := b.Check(context.Background())

	if !st.Refused || st.Code != "" || st.Activate || !strings.Contains(st.Err, "password") {
		t.Errorf("a wrong password reads as %+v, want it turned down for the password", st)
	}
}

func TestAWebsiteAnswerItCannotReadKeepsTheActivationRefusal(t *testing.T) {
	srv := newServer(t, refusedByTheAPI(premium()))
	b, _, _ := newBackend(srv)
	b.SetLogin(email, password)
	srv.BreakSite()

	st := b.Check(context.Background())

	if !st.Refused || !st.Activate || !st.CodeMayHelp || st.Handed {
		t.Errorf("an unreadable website answer reads as %+v, want the activation refusal, kept from JDownloader", st)
	}
}

func TestACaptchaOnTheWebsiteIsReportedWhenNobodyCanAnswerIt(t *testing.T) {
	srv := newServer(t, refusedByTheAPI(premium()))
	srv.SiteCaptcha()
	b, _, _ := newBackend(srv)
	b.SetLogin(email, password)

	st := b.Check(context.Background())

	if !st.Refused || !st.Captcha {
		t.Errorf("a sign-in page with a captcha reads as %+v, want the captcha refusal", st)
	}
	if got := srv.SiteCodes(); len(got) != 0 {
		t.Errorf("the form was posted %d times past a captcha nobody answered", len(got))
	}
}

func TestACaptchaOnTheWebsiteGoesToTheCaptchaSolvers(t *testing.T) {
	for _, tc := range []struct {
		answer string
		ok     bool
	}{{rgtest.CaptchaAnswer, true}, {"wrong", false}} {
		srv := newServer(t, refusedByTheAPI(premium()))
		srv.SiteCaptcha()
		b, _, _ := newBackend(srv)
		var asked []hoster.Captcha
		b.Solve = func(_ context.Context, _ string, c hoster.Captcha) (string, error) {
			asked = append(asked, c)
			return tc.answer, nil
		}
		b.SetLogin(email, password)

		st := b.Check(context.Background())

		if len(asked) != 1 || asked[0].Kind != hoster.Image || !bytes.Equal(asked[0].Image, rgtest.CaptchaImage) {
			t.Fatalf("the solvers were asked %+v, want the sign-in page's captcha picture once", asked)
		}
		if tc.ok && (st.Refused || !st.Premium) {
			t.Errorf("with the captcha answered the login reads as %+v, want it premium", st)
		}
		if !tc.ok && (!st.Refused || !st.Captcha) {
			t.Errorf("with a wrong answer the login reads as %+v, want the captcha refusal", st)
		}
	}
}

func TestALoginTheAPIWantsConfirmedFromANewAddressSignsInOnTheWebsite(t *testing.T) {
	a := premium()
	a.APIRefuses = "Error: Login from unusual IP. Please confirm it with the link sent to your email address."
	srv := newServer(t, a)
	b, _, _ := newBackend(srv)
	b.SetLogin(email, password)

	if st := b.Check(context.Background()); st.Refused || !st.Premium {
		t.Errorf("the login reads as %+v, want it premium through the website", st)
	}
}

func TestATwoFactorLoginTakesItsCodeOnTheWebsiteOnly(t *testing.T) {
	srv := newServer(t, twoFactor())
	b, _, _ := newBackend(srv)
	b.SetLogin(email, password)

	st := b.Check(context.Background())
	if !st.Refused || st.Code != ownlogin.CodeNeeded {
		t.Fatalf("a login without its code reads as %+v, want it waiting for the code", st)
	}
	st = b.SignIn(context.Background(), "482913")
	if st.Refused || !st.Premium {
		t.Fatalf("signing in with the code reads as %+v, want a premium login", st)
	}
	if n := srv.Logins(); n != 1 {
		t.Errorf("the API was asked %d times, want only the sign-in without a code", n)
	}
	if got := srv.SiteCodes(); !slices.Equal(got, []string{"", "482913"}) {
		t.Errorf("the website was sent the codes %q, want none and then the one entered", got)
	}
}

func TestACodeEnteredFirstGoesStraightToTheWebsite(t *testing.T) {
	srv := newServer(t, twoFactor())
	b, _, _ := newBackend(srv)
	b.SetLogin(email, password)

	if st := b.SignIn(context.Background(), "482913"); st.Refused || !st.Premium {
		t.Fatalf("signing in with the code reads as %+v, want a premium login", st)
	}
	if n := srv.Logins(); n != 0 {
		t.Errorf("the API was asked %d times with the code, which it does not take", n)
	}
}

func TestAWebsiteSessionIsCheckedAgainOnlyAfterTwoHours(t *testing.T) {
	srv := newServer(t, refusedByTheAPI(premium()))
	b, _, _ := newBackend(srv)
	b.SetLogin(email, password)
	for range 3 {
		b.Check(context.Background())
	}
	profiles := func() int {
		n := 0
		for _, r := range srv.Requests() {
			if r == "GET /profile/index" {
				n++
			}
		}
		return n
	}
	// The sign-in lands on the profile and reads it once more.
	if n := profiles(); n != 2 {
		t.Errorf("the profile was read %d times, want only for the sign-in", n)
	}

	b.now = func() time.Time { return time.Now().Add(siteCheck + time.Minute) }
	b.Check(context.Background())
	if n := profiles(); n != 3 {
		t.Errorf("the profile was read %d times, want once more after %v", n, siteCheck)
	}
}

func TestAKeptWebsiteSessionIsTakenUpAfterARestart(t *testing.T) {
	srv := newServer(t, refusedByTheAPI(twoFactor()))
	link := srv.Add(fileID, rgtest.File{Name: "a.bin", Body: make([]byte, 4096)})
	b, _, _ := newBackend(srv)
	var kept string
	b.Saved = func(_, _, token string) { kept = token }
	b.SetLogin(email, password)
	b.Check(context.Background())
	b.SignIn(context.Background(), "482913")

	again, _, ups := newBackend(srv)
	again.SetLogin(email, password)
	again.RestoreSession(kept)
	st := again.Check(context.Background())

	if st.Refused || !st.Premium {
		t.Errorf("the kept session reads as %+v, want it premium without signing in", st)
	}
	if got := srv.SiteCodes(); len(got) != 2 {
		t.Errorf("the website was signed in to %d times, want the two before the restart only", len(got))
	}

	// A session the website ended shows on the next download, which then
	// waits for a new code rather than trying the password.
	srv.Expire()
	again.Download("t1", link, nil, 1)
	awaitSettled(t, ups)
	if st := again.Status(); st.Code != ownlogin.CodeEnded {
		t.Errorf("an ended website session reads as %+v, want it asking for a new code", st)
	}
	if got := srv.SiteCodes(); len(got) != 2 {
		t.Errorf("the website was signed in to %d times, want no sign-in without a new code", len(got))
	}
}
