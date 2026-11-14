package sitelogin

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"
)

var now = time.Date(2026, 11, 13, 12, 0, 0, 0, time.UTC)

func jarLine(domain, name, value string, expires time.Time) string {
	sub := "FALSE"
	if strings.HasPrefix(domain, ".") {
		sub = "TRUE"
	}
	exp := "0"
	if !expires.IsZero() {
		exp = strconv.FormatInt(expires.Unix(), 10)
	}
	return strings.Join([]string{domain, sub, "/", "TRUE", exp, name, value}, "\t")
}

func jar(lines ...string) string {
	return "# Netscape HTTP Cookie File\n" + strings.Join(lines, "\n") + "\n"
}

func TestPatreonAndYouTubeAreTheSitesOnOffer(t *testing.T) {
	var ids []string
	for _, s := range Sites {
		ids = append(ids, s.ID)
		if s.Name == "" || s.Host == "" || !strings.HasPrefix(s.LoginURL, "https://") || len(s.Domains) == 0 {
			t.Errorf("%s is missing what the page or the extension needs: %+v", s.ID, s)
		}
	}
	if strings.Join(ids, ",") != "patreon,youtube" {
		t.Errorf("sites = %v, want patreon then youtube", ids)
	}
}

// The page offers signing in again for a failed youtu.be link too, so it
// learns the hosts a site's links come from.
func TestTheSiteListingNamesTheHostsItsLinksComeFrom(t *testing.T) {
	site, _ := Lookup("youtube")
	raw, err := json.Marshal(site)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"links":["youtu.be"]`) {
		t.Errorf("YouTube is listed as %s, want its youtu.be links named", raw)
	}
}

func TestALinkFindsTheSiteOfItsHostOrAParentDomain(t *testing.T) {
	for link, want := range map[string]string{
		"https://www.patreon.com/posts/a-post-123": "patreon",
		"https://patreon.com/c/someone":            "patreon",
		"https://music.youtube.com/watch?v=x":      "youtube",
		"https://youtu.be/abc":                     "youtube",
		"https://notpatreon.com/posts/1":           "",
		"https://example.com/":                     "",
		"not a link":                               "",
	} {
		got := ""
		if s, ok := ForURL(link); ok {
			got = s.ID
		}
		if got != want {
			t.Errorf("ForURL(%q) = %q, want %q", link, got, want)
		}
	}
}

func TestTheJarKeepsOnlyTheSitesOwnCookiesThatStillLive(t *testing.T) {
	site, _ := Lookup("patreon")
	later := float64(now.Add(30 * 24 * time.Hour).Unix())
	got := JarFromCookies(site, []BrowserCookie{
		{Domain: ".patreon.com", Path: "/", Secure: true, Expires: later, Name: "session_id", Value: "S1"},
		{Domain: "www.patreon.com", HostOnly: true, Path: "/", Secure: true, Session: true, Name: "csrf", Value: "C1"},
		{Domain: ".google.com", Path: "/", Secure: true, Expires: later, Name: "SID", Value: "NOT-PATREON"},
		{Domain: "evilpatreon.com", Path: "/", Expires: later, Name: "x", Value: "NOT-PATREON-EITHER"},
		{Domain: ".patreon.com", Path: "/", Expires: float64(now.Add(-time.Hour).Unix()), Name: "old", Value: "GONE"},
		{Domain: ".patreon.com", Path: "/", Expires: later, Name: "bad\tname", Value: "v"},
	}, now)
	if !strings.HasPrefix(got, "# Netscape HTTP Cookie File\n") {
		t.Fatalf("the jar does not start with the header yt-dlp insists on:\n%s", got)
	}
	for _, want := range []string{
		".patreon.com\tTRUE\t/\tTRUE\t" + strconv.FormatInt(int64(later), 10) + "\tsession_id\tS1",
		"www.patreon.com\tFALSE\t/\tTRUE\t",
		"\tcsrf\tC1",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("the jar lacks %q:\n%s", want, got)
		}
	}
	for _, leak := range []string{"NOT-PATREON", "GONE", "bad"} {
		if strings.Contains(got, leak) {
			t.Errorf("the jar carries %q, which is not a live cookie of the site:\n%s", leak, got)
		}
	}
}

func TestAJarWithoutAnyCookieOfTheSiteIsEmpty(t *testing.T) {
	site, _ := Lookup("patreon")
	if got := JarFromCookies(site, []BrowserCookie{{Domain: ".google.com", Name: "SID", Value: "x", Expires: 2e9}}, now); got != "" {
		t.Errorf("JarFromCookies = %q, want empty", got)
	}
}

func TestReadingAJarTakesTheHttpOnlyLinesAndSkipsComments(t *testing.T) {
	got := parseJar(jar(
		"# a comment",
		"#HttpOnly_.patreon.com\tTRUE\t/\tTRUE\t1900000000\tsession_id\tS1",
		"www.patreon.com\tFALSE\t/\tFALSE\t0\tlang\ten",
		"too\tfew\tfields",
	))
	if len(got) != 2 || got[0].Domain != ".patreon.com" || got[0].Name != "session_id" || got[0].Expires.Unix() != 1900000000 ||
		got[1].Name != "lang" || !got[1].Expires.IsZero() {
		t.Errorf("parseJar = %+v", got)
	}
}

func TestNoJarIsSignedOut(t *testing.T) {
	site, _ := Lookup("patreon")
	if got := Check(context.Background(), http.DefaultClient, site, "", now); got.State != SignedOut {
		t.Errorf("Check without a jar = %+v, want signedOut", got)
	}
}

// patreonAt points the Patreon entry's check at a test server.
func patreonAt(t *testing.T, h http.HandlerFunc) Site {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	site, _ := Lookup("patreon")
	site.checkURL = srv.URL + "/api/current_user"
	return site
}

func TestPatreonSaysWhoTheSessionBelongsTo(t *testing.T) {
	var sent string
	site := patreonAt(t, func(w http.ResponseWriter, r *http.Request) {
		sent = r.Header.Get("Cookie")
		_, _ = w.Write([]byte(`{"data":{"id":"7","type":"user","attributes":{"full_name":"Jane Doe","vanity":"jane"}}}`))
	})
	expires := now.Add(20 * 24 * time.Hour)
	got := Check(context.Background(), http.DefaultClient, site, jar(
		jarLine(".patreon.com", "session_id", "S1", expires),
		jarLine(".patreon.com", "old", "GONE", now.Add(-time.Hour)),
	), now)
	if got.State != SignedIn || got.Account != "Jane Doe" {
		t.Errorf("Check = %+v, want signed in as Jane Doe", got)
	}
	if got.Expires != expires.Format(time.RFC3339) {
		t.Errorf("Expires = %q, want the session cookie's %s", got.Expires, expires.Format(time.RFC3339))
	}
	if sent != "session_id=S1" {
		t.Errorf("the check sent Cookie %q, want only the live session cookie", sent)
	}
}

func TestPatreonRefusingTheSessionMeansItExpired(t *testing.T) {
	site := patreonAt(t, func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusUnauthorized) })
	got := Check(context.Background(), http.DefaultClient, site, jar(jarLine(".patreon.com", "session_id", "S1", now.Add(time.Hour))), now)
	if got.State != Expired || got.Expires != "" {
		t.Errorf("Check = %+v, want expired, with no date it holds until", got)
	}
}

// A challenge page answers 403 to clients it does not trust, which says
// nothing about the session.
func TestPatreonNotAnsweringLeavesTheSessionUnchecked(t *testing.T) {
	site := patreonAt(t, func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusForbidden) })
	got := Check(context.Background(), http.DefaultClient, site, jar(jarLine(".patreon.com", "session_id", "S1", now.Add(time.Hour))), now)
	if got.State != Unchecked || got.Account != "" {
		t.Errorf("Check = %+v, want unchecked", got)
	}
}

func TestAPatreonJarWhoseSessionCookieRanOutIsExpiredWithoutAsking(t *testing.T) {
	asked := false
	site := patreonAt(t, func(http.ResponseWriter, *http.Request) { asked = true })
	got := Check(context.Background(), http.DefaultClient, site, jar(jarLine(".patreon.com", "session_id", "S1", now.Add(-time.Minute))), now)
	if got.State != Expired || asked {
		t.Errorf("Check = %+v (asked %v), want expired without a request", got, asked)
	}
}

func TestYouTubeGoesByItsSignInCookies(t *testing.T) {
	site, _ := Lookup("youtube")
	soon, later := now.Add(48*time.Hour), now.Add(300*24*time.Hour)
	signedIn := Check(context.Background(), nil, site, jar(
		jarLine(".youtube.com", "SID", "a", later),
		jarLine(".youtube.com", "__Secure-3PSID", "b", soon),
		jarLine(".youtube.com", "PREF", "c", now.Add(time.Hour)),
	), now)
	if signedIn.State != SignedIn || signedIn.Expires != soon.Format(time.RFC3339) {
		t.Errorf("Check = %+v, want signed in until the first sign-in cookie runs out", signedIn)
	}
	expired := Check(context.Background(), nil, site, jar(jarLine(".youtube.com", "SID", "a", now.Add(-time.Hour))), now)
	if expired.State != Expired {
		t.Errorf("Check with a run-out SID = %+v, want expired", expired)
	}
	anonymous := Check(context.Background(), nil, site, jar(jarLine(".youtube.com", "PREF", "c", later)), now)
	if anonymous.State != Expired {
		t.Errorf("Check without any sign-in cookie = %+v, want expired", anonymous)
	}
}

// Patreon lists and plays posts only for a session, while YouTube flags
// accounts that download a lot while signed in.
func TestPatreonsSessionAlwaysGoesAlongAndYouTubesOnlyWhenAVideoAsks(t *testing.T) {
	for id, want := range map[string]Use{"patreon": Always, "youtube": OnNeed} {
		site, _ := Lookup(id)
		if site.Use != want {
			t.Errorf("%s uses its session %q, want %q", id, site.Use, want)
		}
	}
}

func TestTheSignInCookiesAreTheOnesASessionCarries(t *testing.T) {
	yt, _ := Lookup("youtube")
	if !yt.SignInCookie("SID") || yt.SignInCookie("PREF") {
		t.Errorf("SID and PREF read as %v and %v", yt.SignInCookie("SID"), yt.SignInCookie("PREF"))
	}
}
