package app

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/junkerderprovinz/knightloader/internal/buildinfo"
	"github.com/junkerderprovinz/knightloader/internal/signinbrowser"
	"github.com/junkerderprovinz/knightloader/internal/sitelogin"
)

func patreon(t *testing.T) sitelogin.Site {
	t.Helper()
	s, _ := sitelogin.Lookup("patreon")
	return s
}

func TestASignInFromTheSignInBrowserIsStoredAndSaysWhereItCameFrom(t *testing.T) {
	stubSiteCheck(t, sitelogin.Status{State: sitelogin.SignedIn, Account: "Jane Doe"})
	a := siteApp(t)
	got, ok := a.signInBrowserSession(patreon(t), patreonJar).(SiteLoginState)
	if !ok || got.State != sitelogin.SignedIn || got.Account != "Jane Doe" {
		t.Fatalf("the window was told %+v, want signed in as Jane Doe", got)
	}
	if listed := stateOf(t, a, "patreon"); listed.Source != "kl-browser" {
		t.Errorf("source = %q, want kl-browser", listed.Source)
	}
	if a.siteStore().Session("patreon") != patreonJar {
		t.Error("the session was not stored")
	}
}

func TestASessionStoredSinceSaysNothingOfTheSignInBrowser(t *testing.T) {
	stubSiteCheck(t, sitelogin.Status{State: sitelogin.SignedIn})
	a := siteApp(t)
	a.signInBrowserSession(patreon(t), patreonJar)
	other := "# Netscape HTTP Cookie File\n.patreon.com\tTRUE\t/\tTRUE\t1900000000\tsession_id\tFROM-THE-EXTENSION\n"
	if _, err := a.storeSiteSession("patreon", other); err != nil {
		t.Fatal(err)
	}
	if listed := stateOf(t, a, "patreon"); listed.Source != SourceExtension {
		t.Errorf("source = %q, want the extension's for a session the sign-in browser did not store", listed.Source)
	}
}

func TestSigningOutInTheSignInBrowserForgetsItsSession(t *testing.T) {
	stubSiteCheck(t, sitelogin.Status{State: sitelogin.SignedIn})
	a := siteApp(t)
	a.signInBrowserSession(patreon(t), patreonJar)
	got, _ := a.signInBrowserSession(patreon(t), "").(SiteLoginState)
	if got.State != sitelogin.SignedOut || a.siteStore().Session("patreon") != "" {
		t.Errorf("after signing out in the window: %+v, stored %q", got, a.siteStore().Session("patreon"))
	}
}

func TestSigningOutInTheSignInBrowserKeepsASessionFromElsewhere(t *testing.T) {
	stubSiteCheck(t, sitelogin.Status{State: sitelogin.SignedIn})
	a := siteApp(t)
	if _, err := a.storeSiteSession("patreon", patreonJar); err != nil {
		t.Fatal(err)
	}
	a.signInBrowserSession(patreon(t), "")
	if a.siteStore().Session("patreon") != patreonJar {
		t.Error("a sign-out in the window forgot the session the extension handed over")
	}
}

func TestWhereASessionCameFromOutlivesARestart(t *testing.T) {
	stubSiteCheck(t, sitelogin.Status{State: sitelogin.SignedIn})
	dir := t.TempDir()
	a, err := newApp(dir)
	if err != nil {
		t.Fatal(err)
	}
	a.signInBrowserSession(patreon(t), patreonJar)
	a.Close()
	b, err := newApp(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	if listed := stateOf(t, b, "patreon"); listed.Source != "kl-browser" {
		t.Errorf("after a restart source = %q, want kl-browser", listed.Source)
	}
}

func TestTheSignInBrowserIsOfferedWhereverABrowserIsThere(t *testing.T) {
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	prev := buildinfo.Deployment
	t.Cleanup(func() { buildinfo.Deployment = prev })
	for _, c := range []struct {
		name, deployment, chromium string
		want                       bool
	}{
		{"the container with Chromium", "container", self, true},
		{"the desktop app with a browser", "desktop", self, true},
		{"a Chromium named that is not there", "container", filepath.Join(t.TempDir(), "no-chromium"), false},
	} {
		buildinfo.Deployment = c.deployment
		t.Setenv("KL_CHROMIUM", c.chromium)
		if got := siteApp(t).SignInBrowserAvailable(); got != c.want {
			t.Errorf("%s: available %v, want %v", c.name, got, c.want)
		}
	}
}

func TestEachBrowserSignsInWithAProfileOfItsOwn(t *testing.T) {
	a := siteApp(t)
	edge := a.signInBrowserProfile(signinbrowser.Browser{Name: "Microsoft Edge"})
	chrome := a.signInBrowserProfile(signinbrowser.Browser{Name: "Google Chrome"})
	if edge == chrome || filepath.Dir(edge) != a.signInBrowserDir() {
		t.Errorf("profiles %q and %q, want two folders under %q", edge, chrome, a.signInBrowserDir())
	}
}

func TestASignInFromTheSignInBrowserTakesBackARemove(t *testing.T) {
	stubSiteCheck(t, sitelogin.Status{State: sitelogin.SignedIn})
	a := siteApp(t)
	if err := a.RemoveSiteLogin("patreon"); err != nil {
		t.Fatal(err)
	}
	if !stateOf(t, a, "patreon").Disconnected {
		t.Fatal("Remove did not disconnect the site")
	}
	a.signInBrowserSession(patreon(t), patreonJar)
	if got := stateOf(t, a, "patreon"); got.Disconnected || got.Source != SourceSignInBrowser {
		t.Errorf("after signing in in the window: %+v, want connected with the sign-in browser's session", got)
	}
}
