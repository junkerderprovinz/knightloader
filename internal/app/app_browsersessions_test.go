package app

import (
	"errors"
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
	addSites(t, a, "patreon")
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
	addSites(t, a, "patreon")
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
	addSites(t, a, "patreon")
	withBrowsers(t, a).set("patreon.com", patreonIn(firefoxProfile, "SIGNED-OUT", anHourAgo), patreonIn(edgeProfile, "WORKS", lastWeek))

	if s := stateOf(t, a, "patreon"); s.State != sitelogin.SignedIn || s.Browser != "Edge (Personal)" {
		t.Errorf("patreon = %+v, want the Edge session the site signs in with", s)
	}
}

func TestOnlyASessionTheSiteTurnsDownReadsAsExpired(t *testing.T) {
	stubSiteCheckByJar(t, "SIGNED-OUT")
	a := siteApp(t)
	addSites(t, a, "patreon")
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
	addSites(t, a, "patreon")
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
	addSites(t, a, "patreon")
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
	addSites(t, a, "patreon")
	b := withBrowsers(t, a)
	b.set("patreon.com", patreonIn(firefoxProfile, "FROM-FIREFOX", anHourAgo))
	for range 3 {
		a.SiteLogins()
		a.siteSession("https://www.patreon.com/posts/1")
	}
	if n := b.reads(); n != 1 {
		t.Errorf("the browsers were read %d times, want once for the one site added", n)
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
	addSites(t, a, "patreon")
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
	addSites(t, a, "youtube")
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
	addSites(t, a, "patreon")
	s := stateOf(t, a, "patreon")
	if s.Source != "" || s.Browser != "" || s.Unreadable == nil || len(s.Unreadable) != 0 {
		t.Errorf("patreon = %+v, want no source and an empty list of unreadable browsers", s)
	}
}

func TestABrowserSignInEndsTheWait(t *testing.T) {
	stubSiteCheck(t, sitelogin.Status{State: sitelogin.SignedIn})
	a := siteApp(t)
	addSites(t, a, "patreon")
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
	addSites(t, a, "youtube")
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

func dailymotionIn(p browsercookies.Profile, cookies ...browsercookies.Cookie) browsercookies.Found {
	return browsercookies.Found{Profile: p, Cookies: cookies}
}

func TestAnySiteSignsInFromTheBrowserWhenALinkAsks(t *testing.T) {
	a := siteApp(t)
	b := withBrowsers(t, a)
	b.set("dailymotion.com", dailymotionIn(firefoxProfile,
		browsercookies.Cookie{Host: ".dailymotion.com", Name: "sdx", Value: "DM-SESSION", Path: "/", Expires: time.Now().Add(time.Hour), LastUsed: anHourAgo},
		browsercookies.Cookie{Host: ".dailymotion.com", Name: "v1st", Value: "pl1", Path: "/"},
		browsercookies.Cookie{Host: ".dailymotion.com", Name: "access_token", Value: "S", Path: "/"},
	))
	got := a.signInSession("https://www.dailymotion.com/video/x8abc")
	if !strings.Contains(got, "DM-SESSION") {
		t.Errorf("signInSession = %q, want the browser's dailymotion.com cookies", got)
	}
	if last := b.asked[len(b.asked)-1]; !slices.Equal(last, []string{"dailymotion.com"}) {
		t.Errorf("the browsers were asked for %q, want the link's registrable domain", last)
	}
}

func TestASiteWithoutASignedInLookingCookieGetsNothing(t *testing.T) {
	a := siteApp(t)
	withBrowsers(t, a).set("dailymotion.com", dailymotionIn(firefoxProfile,
		browsercookies.Cookie{Host: ".dailymotion.com", Name: "_ga", Value: "GA1", Path: "/"},
		browsercookies.Cookie{Host: ".dailymotion.com", Name: "v1st", Value: "pl1", Path: "/"},
	))
	if got := a.signInSession("https://www.dailymotion.com/video/x8abc"); got != "" {
		t.Errorf("signInSession = %q, want nothing for cookies that sign nobody in", got)
	}
}

// A site on offer that was never added is tried once more like any other,
// with the cookies of all its domains.
func TestASiteNotAddedStillGetsTheSecondTry(t *testing.T) {
	stubSiteCheck(t, sitelogin.Status{State: sitelogin.SignedIn})
	a := siteApp(t)
	b := withBrowsers(t, a)
	b.set("instagram.com", browsercookies.Found{Profile: firefoxProfile, Cookies: []browsercookies.Cookie{
		{Host: ".instagram.com", Name: "sessionid", Value: "IG-SESSION", Path: "/", Secure: true, Expires: time.Now().Add(time.Hour), LastUsed: anHourAgo},
	}})
	if s := stateOf(t, a, "instagram"); s.Added || s.State != sitelogin.SignedOut {
		t.Errorf("instagram = %+v, want no row and nothing read", s)
	}
	if got := a.signInSession("https://instagr.am/p/abc/"); !strings.Contains(got, "IG-SESSION") {
		t.Errorf("an instagr.am link asking for a sign-in gets %q", got)
	}
	if last := b.asked[len(b.asked)-1]; !slices.Equal(last, []string{"instagram.com"}) {
		t.Errorf("the browsers were asked for %q, want Instagram's domains", last)
	}
	if got := a.siteSession("https://www.patreon.com/posts/1"); got != "" {
		t.Errorf("a Patreon that was never added sends %q with every link", got)
	}
}

func TestWithoutBrowserReadingAnySiteGetsNothing(t *testing.T) {
	a := siteApp(t)
	if got := a.signInSession("https://www.dailymotion.com/video/x8abc"); got != "" {
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
	addSites(t, a, "patreon")
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

// Removing a site the browser signs in takes its row away and leaves the
// browser as it is.
func TestRemovingASiteDropsItsRowAndLeavesTheBrowserAlone(t *testing.T) {
	stubSiteCheck(t, sitelogin.Status{State: sitelogin.SignedIn})
	a := siteApp(t)
	addSites(t, a, "patreon")
	b := withBrowsers(t, a)
	b.set("patreon.com", patreonIn(firefoxProfile, "FROM-FIREFOX", anHourAgo))
	if s := stateOf(t, a, "patreon"); s.State != sitelogin.SignedIn {
		t.Fatalf("patreon = %+v", s)
	}
	if err := a.RemoveSiteLogin("patreon"); err != nil {
		t.Fatal(err)
	}
	reads := b.reads()
	s, err := a.CheckSiteLogin("patreon")
	if err != nil {
		t.Fatal(err)
	}
	if s.Added || s.State != sitelogin.SignedOut || s.Source != "" || s.Browser != "" {
		t.Errorf("after removing = %+v, want no row and signed out", s)
	}
	if got := a.siteSession(patreonPost); got != "" {
		t.Errorf("yt-dlp still gets the browser's session: %q", got)
	}
	if got := a.signInSession(patreonPost); got != "" {
		t.Errorf("the second try still gets the browser's session: %q", got)
	}
	if b.reads() != reads {
		t.Errorf("the browsers were read %d more times for a removed site", b.reads()-reads)
	}
}

func TestARemovedSiteStaysRemovedAfterARestart(t *testing.T) {
	stubSiteCheck(t, sitelogin.Status{State: sitelogin.SignedIn})
	dir := t.TempDir()
	a, err := newApp(dir)
	if err != nil {
		t.Fatal(err)
	}
	addSites(t, a, "youtube")
	if err := a.RemoveSiteLogin("youtube"); err != nil {
		t.Fatal(err)
	}
	a.Close()
	again, err := newApp(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { again.Close() })
	withBrowsers(t, again).set("youtube.com", youtubeIn(firefoxProfile, "YT-SESSION"))
	if s := stateOf(t, again, "youtube"); s.Added || s.State != sitelogin.SignedOut {
		t.Errorf("after a restart youtube = %+v, want still removed", s)
	}
	if got := again.signInSession("https://youtu.be/x"); got != "" {
		t.Errorf("the second try reads a removed site: %q", got)
	}
}

func TestSigningInThroughKnightLoaderAddsTheSiteAgain(t *testing.T) {
	stubSiteCheck(t, sitelogin.Status{State: sitelogin.SignedIn})
	a := siteApp(t)
	addSites(t, a, "patreon")
	withBrowsers(t, a).set("patreon.com", patreonIn(firefoxProfile, "FROM-FIREFOX", anHourAgo))
	if err := a.RemoveSiteLogin("patreon"); err != nil {
		t.Fatal(err)
	}
	s, err := a.BeginSiteLogin("patreon")
	if err != nil {
		t.Fatal(err)
	}
	if !s.Added || s.State != sitelogin.SignedIn || s.Source != SourceBrowser {
		t.Errorf("after signing in again = %+v, want the row and the browser's session back", s)
	}
}

func TestAddingASiteUsesTheBrowsersSignIn(t *testing.T) {
	stubSiteCheck(t, sitelogin.Status{State: sitelogin.SignedIn})
	a := siteApp(t)
	withBrowsers(t, a).set("patreon.com", patreonIn(firefoxProfile, "FROM-FIREFOX", anHourAgo))
	s, err := a.AddSiteLogin("patreon")
	if err != nil {
		t.Fatal(err)
	}
	if !s.Added || s.State != sitelogin.SignedIn || s.Waiting {
		t.Errorf("after adding = %+v, want signed in from the browser and not waiting", s)
	}
	if _, err := a.AddSiteLogin("example"); !errors.Is(err, ErrNoSuchSite) {
		t.Errorf("adding an unknown site = %v, want ErrNoSuchSite", err)
	}
}

// The extension hands over only what the instance lists, so a site that is
// not added gets no row from a hand-over.
func TestAHandOverForASiteThatIsNotAddedIsRefused(t *testing.T) {
	stubSiteCheck(t, sitelogin.Status{State: sitelogin.SignedIn})
	a := siteApp(t)
	addSites(t, a, "patreon")
	if err := a.RemoveSiteLogin("patreon"); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"patreon", "instagram"} {
		_, err := a.HandOverSiteSession(id, []sitelogin.BrowserCookie{
			{Domain: ".patreon.com", Path: "/", Secure: true, Expires: 1.9e9, Name: "session_id", Value: "PATREON-SECRET"},
			{Domain: ".instagram.com", Path: "/", Secure: true, Expires: 1.9e9, Name: "sessionid", Value: "IG-SECRET"},
		})
		if !errors.Is(err, ErrSiteNotAdded) {
			t.Errorf("handing %s over = %v, want ErrSiteNotAdded", id, err)
		}
		if s := stateOf(t, a, id); s.Added || a.siteStore().Session(id) != "" {
			t.Errorf("the hand-over gave %s %+v", id, s)
		}
	}
}

// The extension signing out says nothing about the browsers here.
func TestAnEmptyHandOverLeavesTheBrowsersSignInInUse(t *testing.T) {
	stubSiteCheck(t, sitelogin.Status{State: sitelogin.SignedIn})
	a := siteApp(t)
	addSites(t, a, "patreon")
	withBrowsers(t, a).set("patreon.com", patreonIn(firefoxProfile, "FROM-FIREFOX", anHourAgo))
	s, err := a.HandOverSiteSession("patreon", nil)
	if err != nil {
		t.Fatal(err)
	}
	if !s.Added || s.Source != SourceBrowser {
		t.Errorf("after the extension signed out = %+v, want the browser's session still used", s)
	}
}

// An install from before sites were added one by one keeps the rows it used:
// a stored session, and a browser here that signs Patreon or YouTube in.
func TestTheSitesInUseKeepTheirRowsAfterTheUpdate(t *testing.T) {
	stubSiteCheck(t, sitelogin.Status{State: sitelogin.SignedIn})
	a := siteApp(t)
	store := a.siteStore()
	if err := store.Set("patreon", patreonJar); err != nil {
		t.Fatal(err)
	}
	if err := store.SetDisconnected("youtube", true); err != nil {
		t.Fatal(err)
	}
	b := &fakeBrowsers{found: map[string][]browsercookies.Found{}}
	b.set("youtube.com", youtubeIn(firefoxProfile, "YT-SESSION"))
	b.set("instagram.com", browsercookies.Found{Profile: firefoxProfile, Cookies: []browsercookies.Cookie{
		{Host: ".instagram.com", Name: "sessionid", Value: "IG", Path: "/", Expires: time.Now().Add(time.Hour)},
	}})
	a.takeOverSiteRows(b.read)
	for id, want := range map[string]bool{"patreon": true, "youtube": false, "instagram": false} {
		if got := store.Added(id); got != want {
			t.Errorf("%s added %v, want %v", id, got, want)
		}
	}

	fresh := siteApp(t)
	fb := &fakeBrowsers{found: map[string][]browsercookies.Found{}}
	fb.set("youtube.com", youtubeIn(firefoxProfile, "YT-SESSION"))
	fresh.takeOverSiteRows(fb.read)
	if !fresh.siteStore().Added("youtube") || fresh.siteStore().Added("patreon") {
		t.Errorf("a browser's YouTube sign-in did not keep its row alone")
	}
	if err := fresh.RemoveSiteLogin("youtube"); err != nil {
		t.Fatal(err)
	}
	if err := fresh.siteStore().SetDisconnected("youtube", false); err != nil {
		t.Fatal(err)
	}
	fresh.takeOverSiteRows(fb.read)
	if fresh.siteStore().Added("youtube") {
		t.Error("the rows were taken over a second time")
	}
}

// A container reads no browser, so its stored sessions alone keep a row.
func TestAContainerKeepsTheRowsOfItsStoredSessions(t *testing.T) {
	prev := buildinfo.InContainer
	buildinfo.InContainer = func() bool { return true }
	t.Cleanup(func() { buildinfo.InContainer = prev })
	stubSiteCheck(t, sitelogin.Status{State: sitelogin.SignedIn})
	a := siteApp(t)
	if err := a.siteStore().Set("youtube", "# Netscape HTTP Cookie File\n.youtube.com\tTRUE\t/\tTRUE\t1900000000\tSID\tYT\n"); err != nil {
		t.Fatal(err)
	}
	a.StartBrowserSessions()
	if s := stateOf(t, a, "youtube"); !s.Added {
		t.Errorf("youtube = %+v, want its row kept", s)
	}
}
