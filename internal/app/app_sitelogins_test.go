package app

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/sitelogin"
)

// stubSiteCheck answers every check with st and counts the checks.
func stubSiteCheck(t *testing.T, st sitelogin.Status) *atomic.Int32 {
	t.Helper()
	var n atomic.Int32
	prev := siteLoginCheck
	siteLoginCheck = func(context.Context, sitelogin.Site, string) sitelogin.Status {
		n.Add(1)
		return st
	}
	t.Cleanup(func() { siteLoginCheck = prev })
	return &n
}

func siteApp(t *testing.T) *App {
	t.Helper()
	a, err := newApp(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { a.Close() })
	return a
}

func stateOf(t *testing.T, a *App, id string) SiteLoginState {
	t.Helper()
	for _, s := range a.SiteLogins() {
		if s.ID == id {
			return s
		}
	}
	t.Fatalf("SiteLogins lists no %s", id)
	return SiteLoginState{}
}

const patreonJar = "# Netscape HTTP Cookie File\n.patreon.com\tTRUE\t/\tTRUE\t1900000000\tsession_id\tPATREON-SECRET\n"

func TestEverySiteStartsSignedOut(t *testing.T) {
	checks := stubSiteCheck(t, sitelogin.Status{State: sitelogin.SignedIn})
	a := siteApp(t)
	for _, s := range a.SiteLogins() {
		if s.State != sitelogin.SignedOut || s.Waiting {
			t.Errorf("%s = %+v, want signed out and not waiting", s.ID, s)
		}
	}
	if checks.Load() != 0 {
		t.Errorf("a site without a session was checked %d times", checks.Load())
	}
}

func TestSigningInWaitsForTheSessionAndAStoredOneEndsTheWait(t *testing.T) {
	stubSiteCheck(t, sitelogin.Status{State: sitelogin.SignedIn, Account: "Jane Doe"})
	a := siteApp(t)
	begun, err := a.BeginSiteLogin("patreon")
	if err != nil || !begun.Waiting || begun.LoginURL == "" {
		t.Fatalf("BeginSiteLogin = %+v, %v; want it waiting, with the login page", begun, err)
	}
	got, err := a.StoreSiteSession("patreon", patreonJar)
	if err != nil {
		t.Fatal(err)
	}
	if got.Waiting || got.State != sitelogin.SignedIn || got.Account != "Jane Doe" {
		t.Errorf("after the session arrived = %+v, want signed in as Jane Doe and no longer waiting", got)
	}
	if listed := stateOf(t, a, "patreon"); listed.Account != "Jane Doe" {
		t.Errorf("the listing says %+v", listed)
	}
}

func TestTheWaitForASessionEnds(t *testing.T) {
	stubSiteCheck(t, sitelogin.Status{State: sitelogin.SignedIn})
	a := siteApp(t)
	if _, err := a.BeginSiteLogin("youtube"); err != nil {
		t.Fatal(err)
	}
	a.siteLogins().mu.Lock()
	a.siteLogins().waiting["youtube"] = time.Now().Add(-time.Second)
	a.siteLogins().mu.Unlock()
	if s := stateOf(t, a, "youtube"); s.Waiting {
		t.Errorf("youtube still waits after its wait ran out: %+v", s)
	}
}

func TestAHandedOverSessionKeepsOnlyTheSitesCookies(t *testing.T) {
	stubSiteCheck(t, sitelogin.Status{State: sitelogin.SignedIn})
	a := siteApp(t)
	_, err := a.HandOverSiteSession("patreon", []sitelogin.BrowserCookie{
		{Domain: ".patreon.com", Path: "/", Secure: true, Expires: 1.9e9, Name: "session_id", Value: "PATREON-SECRET"},
		{Domain: ".google.com", Path: "/", Secure: true, Expires: 1.9e9, Name: "SID", Value: "GOOGLE-SECRET"},
	})
	if err != nil {
		t.Fatal(err)
	}
	session := sitelogin.NewStore(a.Accounts).Session("patreon")
	if !strings.Contains(session, "PATREON-SECRET") || strings.Contains(session, "GOOGLE-SECRET") {
		t.Errorf("the stored session is %q, want Patreon's cookie alone", session)
	}
}

func TestAHandOverWithoutTheSitesCookiesStoresNothing(t *testing.T) {
	stubSiteCheck(t, sitelogin.Status{State: sitelogin.SignedIn})
	a := siteApp(t)
	_, err := a.HandOverSiteSession("patreon", []sitelogin.BrowserCookie{{Domain: ".google.com", Name: "SID", Value: "x", Expires: 1.9e9}})
	if !errors.Is(err, ErrNoSiteSession) {
		t.Errorf("err = %v, want ErrNoSiteSession", err)
	}
	if s := stateOf(t, a, "patreon"); s.State != sitelogin.SignedOut {
		t.Errorf("patreon = %+v, want still signed out", s)
	}
}

func TestAnUnknownSiteIsRefused(t *testing.T) {
	a := siteApp(t)
	if _, err := a.BeginSiteLogin("example"); !errors.Is(err, ErrNoSuchSite) {
		t.Errorf("BeginSiteLogin(example) err = %v, want ErrNoSuchSite", err)
	}
	if _, err := a.StoreSiteSession("example", patreonJar); !errors.Is(err, ErrNoSuchSite) {
		t.Errorf("StoreSiteSession(example) err = %v, want ErrNoSuchSite", err)
	}
}

func TestTheListingReusesARecentCheck(t *testing.T) {
	checks := stubSiteCheck(t, sitelogin.Status{State: sitelogin.SignedIn, Account: "Jane"})
	a := siteApp(t)
	if _, err := a.StoreSiteSession("patreon", patreonJar); err != nil {
		t.Fatal(err)
	}
	before := checks.Load()
	for range 3 {
		a.SiteLogins()
	}
	if checks.Load() != before {
		t.Errorf("listing three times checked %d more times, want the stored answer", checks.Load()-before)
	}
	if _, err := a.CheckSiteLogin("patreon"); err != nil {
		t.Fatal(err)
	}
	if checks.Load() != before+1 {
		t.Errorf("CheckSiteLogin did not ask again")
	}
}

func TestSigningOutClearsTheSession(t *testing.T) {
	stubSiteCheck(t, sitelogin.Status{State: sitelogin.SignedIn, Account: "Jane"})
	a := siteApp(t)
	if _, err := a.StoreSiteSession("patreon", patreonJar); err != nil {
		t.Fatal(err)
	}
	if err := a.RemoveSiteLogin("patreon"); err != nil {
		t.Fatal(err)
	}
	if s := stateOf(t, a, "patreon"); s.State != sitelogin.SignedOut || s.Account != "" {
		t.Errorf("after signing out = %+v", s)
	}
	if got := a.siteSession("https://www.patreon.com/posts/1"); got != "" {
		t.Errorf("yt-dlp still gets %q", got)
	}
}

func TestYtdlpGetsTheSessionOfTheLinksSite(t *testing.T) {
	stubSiteCheck(t, sitelogin.Status{State: sitelogin.SignedIn})
	a := siteApp(t)
	if _, err := a.StoreSiteSession("patreon", patreonJar); err != nil {
		t.Fatal(err)
	}
	if got := a.siteSession("https://www.patreon.com/posts/a-post-1"); got != patreonJar {
		t.Errorf("siteSession(patreon link) = %q, want the stored session", got)
	}
	if got := a.siteSession("https://vimeo.com/1"); got != "" {
		t.Errorf("siteSession(vimeo link) = %q, want nothing", got)
	}
	if a.ytdlpBackend("").Sessions == nil {
		t.Error("the yt-dlp backend has no Sessions hook, so a signed-in site's links load anonymously")
	}
}

// The listing reaches the accounts page and the extension, so it says who is
// signed in and never what the session is.
func TestTheListingNeverCarriesTheSession(t *testing.T) {
	stubSiteCheck(t, sitelogin.Status{State: sitelogin.SignedIn, Account: "Jane"})
	a := siteApp(t)
	if _, err := a.StoreSiteSession("patreon", patreonJar); err != nil {
		t.Fatal(err)
	}
	for _, s := range a.SiteLogins() {
		if strings.Contains(s.Account+s.Expires+s.LoginURL, "PATREON-SECRET") {
			t.Fatalf("the listing carries the session: %+v", s)
		}
	}
}
