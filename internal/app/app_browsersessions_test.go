package app

import (
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/accounts"
	"github.com/junkerderprovinz/knightloader/internal/browsercookies"
	"github.com/junkerderprovinz/knightloader/internal/buildinfo"
	"github.com/junkerderprovinz/knightloader/internal/sitelogin"
)

// fakeBrowsers stands in for the browsers on this machine.
type fakeBrowsers struct {
	mu         sync.Mutex
	found      map[string][]browsercookies.Found
	unreadable []browsercookies.Unreadable
	asked      [][]string
}

func (f *fakeBrowsers) read(domains []string) ([]browsercookies.Found, []browsercookies.Unreadable) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.asked = append(f.asked, slices.Clone(domains))
	return f.found[domains[0]], f.unreadable
}

func (f *fakeBrowsers) set(domain string, found ...browsercookies.Found) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.found[domain] = found
}

func (f *fakeBrowsers) reads() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.asked)
}

func withBrowsers(t *testing.T, a *App) *fakeBrowsers {
	t.Helper()
	f := &fakeBrowsers{found: map[string][]browsercookies.Found{}}
	st := a.siteLogins()
	st.mu.Lock()
	st.read = f.read
	st.mu.Unlock()
	return f
}

var (
	firefoxProfile = browsercookies.Profile{Browser: "Firefox", Name: "default-release"}
	edgeProfile    = browsercookies.Profile{Browser: "Edge", Name: "Personal"}
	lastWeek       = time.Now().Add(-7 * 24 * time.Hour)
	anHourAgo      = time.Now().Add(-time.Hour)
)

func patreonIn(p browsercookies.Profile, value string, used time.Time) browsercookies.Found {
	return browsercookies.Found{Profile: p, Cookies: []browsercookies.Cookie{
		{Host: ".patreon.com", Name: "session_id", Value: value, Path: "/", Secure: true, Expires: time.Now().Add(30 * 24 * time.Hour), LastUsed: used},
		{Host: "www.patreon.com", Name: "patreon_locale_code", Value: "en-US", Path: "/", LastUsed: used},
	}}
}

func youtubeIn(p browsercookies.Profile, value string) browsercookies.Found {
	return browsercookies.Found{Profile: p, Cookies: []browsercookies.Cookie{
		{Host: ".youtube.com", Name: "SID", Value: value, Path: "/", Secure: true, Expires: time.Now().Add(365 * 24 * time.Hour), LastUsed: anHourAgo},
	}}
}

func TestASessionSignedInInTheBrowserIsTakenWithoutDoingAnything(t *testing.T) {
	stubSiteCheck(t, sitelogin.Status{State: sitelogin.SignedIn, Account: "Jane Doe"})
	a := siteApp(t)
	withBrowsers(t, a).set("patreon.com", patreonIn(firefoxProfile, "FROM-FIREFOX", anHourAgo))

	s := stateOf(t, a, "patreon")
	if s.State != sitelogin.SignedIn || s.Account != "Jane Doe" || s.Source != SourceBrowser || s.Browser != "Firefox (default-release)" {
		t.Errorf("patreon = %+v, want signed in as Jane Doe from Firefox (default-release)", s)
	}
	if got := sitelogin.NewStore(a.Accounts).Session("patreon"); got != "" {
		t.Errorf("the browser's session was copied into the store: %q", got)
	}
}

func TestTheFreshestSessionAmongTheBrowsersWins(t *testing.T) {
	stubSiteCheck(t, sitelogin.Status{State: sitelogin.SignedIn})
	a := siteApp(t)
	withBrowsers(t, a).set("patreon.com", patreonIn(edgeProfile, "OLDER", lastWeek), patreonIn(firefoxProfile, "NEWER", anHourAgo))

	if s := stateOf(t, a, "patreon"); s.Browser != "Firefox (default-release)" {
		t.Errorf("the session came from %q, want the one used last", s.Browser)
	}
	if got := a.siteSession("https://www.patreon.com/posts/1"); !strings.Contains(got, "NEWER") {
		t.Errorf("yt-dlp gets %q, want the freshest session", got)
	}
}

func TestASessionTheSiteTurnsDownGivesWayToTheNextBrowser(t *testing.T) {
	stubSiteCheckByJar(t, "SIGNED-OUT")
	a := siteApp(t)
	withBrowsers(t, a).set("patreon.com", patreonIn(firefoxProfile, "SIGNED-OUT", anHourAgo), patreonIn(edgeProfile, "WORKS", lastWeek))

	if s := stateOf(t, a, "patreon"); s.State != sitelogin.SignedIn || s.Browser != "Edge (Personal)" {
		t.Errorf("patreon = %+v, want the Edge session the site signs in with", s)
	}
}

func TestOnlyASessionTheSiteTurnsDownReadsAsExpired(t *testing.T) {
	stubSiteCheckByJar(t, "SIGNED-OUT")
	a := siteApp(t)
	withBrowsers(t, a).set("patreon.com", patreonIn(firefoxProfile, "SIGNED-OUT", anHourAgo))

	if s := stateOf(t, a, "patreon"); s.State != sitelogin.Expired || s.Source != SourceBrowser {
		t.Errorf("patreon = %+v, want an expired session from the browser", s)
	}
	if got := a.siteSession("https://www.patreon.com/posts/1"); got != "" {
		t.Errorf("yt-dlp gets a session the site turned down: %q", got)
	}
}

func TestCookiesWithoutASignInAreNoSession(t *testing.T) {
	checks := stubSiteCheck(t, sitelogin.Status{State: sitelogin.SignedIn})
	a := siteApp(t)
	withBrowsers(t, a).set("patreon.com", browsercookies.Found{Profile: firefoxProfile, Cookies: []browsercookies.Cookie{
		{Host: "www.patreon.com", Name: "patreon_locale_code", Value: "en-US", Path: "/"},
	}})
	if s := stateOf(t, a, "patreon"); s.State != sitelogin.SignedOut || s.Source != "" {
		t.Errorf("patreon = %+v, want signed out", s)
	}
	if checks.Load() != 0 {
		t.Errorf("a browser without a sign-in was checked with the site")
	}
}

func TestSigningOutInTheBrowserTakesEffect(t *testing.T) {
	stubSiteCheck(t, sitelogin.Status{State: sitelogin.SignedIn})
	a := siteApp(t)
	b := withBrowsers(t, a)
	b.set("patreon.com", patreonIn(firefoxProfile, "FROM-FIREFOX", anHourAgo))
	if s := stateOf(t, a, "patreon"); s.State != sitelogin.SignedIn {
		t.Fatalf("patreon = %+v", s)
	}
	b.set("patreon.com")
	s, err := a.CheckSiteLogin("patreon")
	if err != nil {
		t.Fatal(err)
	}
	if s.State != sitelogin.SignedOut || s.Source != "" || s.Browser != "" {
		t.Errorf("after signing out in the browser = %+v, want signed out", s)
	}
}

func TestTheBrowsersAreNotReadOnEveryListing(t *testing.T) {
	stubSiteCheck(t, sitelogin.Status{State: sitelogin.SignedIn})
	a := siteApp(t)
	b := withBrowsers(t, a)
	b.set("patreon.com", patreonIn(firefoxProfile, "FROM-FIREFOX", anHourAgo))
	for range 3 {
		a.SiteLogins()
		a.siteSession("https://www.patreon.com/posts/1")
	}
	if n := b.reads(); n != len(sitelogin.Sites) {
		t.Errorf("the browsers were read %d times, want once per site", n)
	}
}

func TestAHandedOverSessionStandsInWhenNoBrowserHasOne(t *testing.T) {
	stubSiteCheck(t, sitelogin.Status{State: sitelogin.SignedIn, Account: "Jane"})
	a := siteApp(t)
	withBrowsers(t, a)
	if _, err := a.storeSiteSession("patreon", patreonJar); err != nil {
		t.Fatal(err)
	}
	if s := stateOf(t, a, "patreon"); s.Source != SourceExtension || s.Browser != "" {
		t.Errorf("patreon = %+v, want the extension's session", s)
	}
	if got := a.siteSession("https://www.patreon.com/posts/1"); got != patreonJar {
		t.Errorf("yt-dlp gets %q, want the handed-over session", got)
	}
}

func TestTheBrowsersSessionComesBeforeTheHandedOverOne(t *testing.T) {
	stubSiteCheck(t, sitelogin.Status{State: sitelogin.SignedIn})
	a := siteApp(t)
	if _, err := a.storeSiteSession("patreon", patreonJar); err != nil {
		t.Fatal(err)
	}
	withBrowsers(t, a).set("patreon.com", patreonIn(firefoxProfile, "FROM-FIREFOX", anHourAgo))
	if got := a.siteSession("https://www.patreon.com/posts/1"); !strings.Contains(got, "FROM-FIREFOX") {
		t.Errorf("yt-dlp gets %q, want the browser's session", got)
	}
	if s := stateOf(t, a, "patreon"); s.Source != SourceBrowser {
		t.Errorf("patreon = %+v, want the browser as the source", s)
	}
}

func TestABrowserThatCannotBeReadIsNamed(t *testing.T) {
	stubSiteCheck(t, sitelogin.Status{State: sitelogin.SignedIn})
	a := siteApp(t)
	b := withBrowsers(t, a)
	b.unreadable = []browsercookies.Unreadable{
		{Profile: browsercookies.Profile{Browser: "Chrome", Name: "Default"}, Err: browsercookies.ErrAppBound},
		{Profile: browsercookies.Profile{Browser: "Chrome", Name: "Profile 1"}, Err: browsercookies.ErrAppBound},
	}
	if s := stateOf(t, a, "youtube"); !slices.Equal(s.Unreadable, []string{"Chrome"}) {
		t.Errorf("unreadable = %q, want Chrome once", s.Unreadable)
	}
}

func TestWithoutBrowserReadingTheListingSaysSo(t *testing.T) {
	stubSiteCheck(t, sitelogin.Status{State: sitelogin.SignedIn})
	a := siteApp(t)
	s := stateOf(t, a, "patreon")
	if s.Source != "" || s.Browser != "" || s.Unreadable == nil || len(s.Unreadable) != 0 {
		t.Errorf("patreon = %+v, want no source and an empty list of unreadable browsers", s)
	}
}

func TestABrowserSignInEndsTheWait(t *testing.T) {
	stubSiteCheck(t, sitelogin.Status{State: sitelogin.SignedIn})
	a := siteApp(t)
	b := withBrowsers(t, a)
	if s, _ := a.BeginSiteLogin("patreon"); !s.Waiting {
		t.Fatalf("BeginSiteLogin = %+v, want it waiting", s)
	}
	b.set("patreon.com", patreonIn(firefoxProfile, "JUST-SIGNED-IN", time.Now()))
	st := a.siteLogins()
	st.mu.Lock()
	scan := st.scans["patreon"]
	scan.at = scan.at.Add(-browserWaitFresh)
	st.scans["patreon"] = scan
	st.mu.Unlock()
	if s := stateOf(t, a, "patreon"); s.Waiting || s.State != sitelogin.SignedIn {
		t.Errorf("patreon = %+v, want signed in and no longer waiting", s)
	}
}

func TestYouTubesSessionWaitsUntilAVideoAsksForIt(t *testing.T) {
	stubSiteCheck(t, sitelogin.Status{State: sitelogin.SignedIn})
	a := siteApp(t)
	withBrowsers(t, a).set("youtube.com", youtubeIn(firefoxProfile, "YT-SESSION"))

	if got := a.siteSession("https://www.youtube.com/watch?v=x"); got != "" {
		t.Errorf("every YouTube link gets the session: %q", got)
	}
	if got := a.signInSession("https://youtu.be/x"); !strings.Contains(got, "YT-SESSION") {
		t.Errorf("a YouTube video asking for a sign-in gets %q", got)
	}
	yb := a.ytdlpBackend("")
	if yb.SignIn == nil || yb.Sessions == nil {
		t.Error("the yt-dlp backend lacks the session hooks")
	}
}

func vimeoIn(p browsercookies.Profile, cookies ...browsercookies.Cookie) browsercookies.Found {
	return browsercookies.Found{Profile: p, Cookies: cookies}
}

func TestAnySiteSignsInFromTheBrowserWhenALinkAsks(t *testing.T) {
	a := siteApp(t)
	b := withBrowsers(t, a)
	b.set("vimeo.com", vimeoIn(firefoxProfile,
		browsercookies.Cookie{Host: ".vimeo.com", Name: "vimeo", Value: "VIMEO-SESSION", Path: "/", Expires: time.Now().Add(time.Hour), LastUsed: anHourAgo},
		browsercookies.Cookie{Host: ".vimeo.com", Name: "vuid", Value: "pl1", Path: "/"},
		browsercookies.Cookie{Host: ".vimeo.com", Name: "vimeo_session_id", Value: "S", Path: "/"},
	))
	got := a.signInSession("https://player.vimeo.com/video/123")
	if !strings.Contains(got, "VIMEO-SESSION") {
		t.Errorf("signInSession = %q, want the browser's vimeo.com cookies", got)
	}
	if last := b.asked[len(b.asked)-1]; !slices.Equal(last, []string{"vimeo.com"}) {
		t.Errorf("the browsers were asked for %q, want the link's registrable domain", last)
	}
}

func TestASiteWithoutASignedInLookingCookieGetsNothing(t *testing.T) {
	a := siteApp(t)
	withBrowsers(t, a).set("vimeo.com", vimeoIn(firefoxProfile,
		browsercookies.Cookie{Host: ".vimeo.com", Name: "_ga", Value: "GA1", Path: "/"},
		browsercookies.Cookie{Host: ".vimeo.com", Name: "vuid", Value: "pl1", Path: "/"},
	))
	if got := a.signInSession("https://vimeo.com/123"); got != "" {
		t.Errorf("signInSession = %q, want nothing for cookies that sign nobody in", got)
	}
}

func TestWithoutBrowserReadingAnySiteGetsNothing(t *testing.T) {
	a := siteApp(t)
	if got := a.signInSession("https://vimeo.com/123"); got != "" {
		t.Errorf("signInSession = %q", got)
	}
}

func TestAContainerReadsNoBrowser(t *testing.T) {
	prev := buildinfo.InContainer
	buildinfo.InContainer = func() bool { return true }
	t.Cleanup(func() { buildinfo.InContainer = prev })
	a := siteApp(t)
	a.StartBrowserSessions()
	st := a.siteLogins()
	st.mu.Lock()
	defer st.mu.Unlock()
	if st.read != nil {
		t.Error("a container reads the browsers on its host")
	}
}

func TestThePastedCookieJarsAreGoneAfterAStart(t *testing.T) {
	dir := t.TempDir()
	acc, err := accounts.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := acc.SetCredential("ytdlpcookies", "youtube.com", accounts.Credential{APIKey: "# Netscape HTTP Cookie File\n"}); err != nil {
		t.Fatal(err)
	}
	a, err := newApp(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { a.Close() })
	if ids := a.Accounts.AccountIDs("ytdlpcookies"); len(ids) != 0 {
		t.Errorf("stored jars survived the start: %q", ids)
	}
}

func TestTheListingSaysWhetherThisInstanceReadsBrowsers(t *testing.T) {
	stubSiteCheck(t, sitelogin.Status{State: sitelogin.SignedIn})
	a := siteApp(t)
	if s := stateOf(t, a, "patreon"); s.ReadsBrowsers {
		t.Errorf("an instance that reads no browser says it does: %+v", s)
	}
	withBrowsers(t, a)
	if s := stateOf(t, a, "patreon"); !s.ReadsBrowsers {
		t.Errorf("an instance that reads the browsers says it does not: %+v", s)
	}
}

// The extension hands over an empty set once the browser has signed out.
func TestAnEmptyHandOverSignsTheExtensionsSessionOut(t *testing.T) {
	stubSiteCheck(t, sitelogin.Status{State: sitelogin.SignedIn, Account: "Jane"})
	a := siteApp(t)
	if _, err := a.storeSiteSession("patreon", patreonJar); err != nil {
		t.Fatal(err)
	}
	got, err := a.HandOverSiteSession("patreon", nil)
	if err != nil {
		t.Fatalf("an empty hand-over = %v, want it taken as signing out", err)
	}
	if got.State != sitelogin.SignedOut || got.Source != "" {
		t.Errorf("after an empty hand-over = %+v, want signed out", got)
	}
	if s := sitelogin.NewStore(a.Accounts).Session("patreon"); s != "" {
		t.Errorf("the handed-over session is still stored: %q", s)
	}
}
