package signinbrowser

import (
	"context"
	"testing"

	"github.com/junkerderprovinz/knightloader/internal/httpx"
	"github.com/junkerderprovinz/knightloader/internal/sitelogin"
)

func site(t *testing.T, id string) sitelogin.Site {
	t.Helper()
	s, ok := sitelogin.Lookup(id)
	if !ok {
		t.Fatalf("no site %q", id)
	}
	return s
}

// lanNames stands in for DNS: the names listed resolve into the LAN, every
// other name to the internet.
func lanNames(names ...string) func(context.Context, string) httpx.Scope {
	return func(_ context.Context, host string) httpx.Scope {
		for _, n := range names {
			if n == host {
				return httpx.ScopeLocal
			}
		}
		return httpx.ScopePublic
	}
}

func TestAWindowShowsOnlyTheSiteAndItsSignInProvidersAtTheTop(t *testing.T) {
	for id, cases := range map[string]map[string]bool{
		"patreon": {
			"https://www.patreon.com/login":                    true,
			"https://patreon.com/":                             true,
			"https://accounts.google.com/o/oauth2/auth?x=1":    true,
			"https://appleid.apple.com/auth/authorize":         true,
			"https://www.facebook.com/dialog/oauth":            true,
			"https://www.youtube.com/":                         false,
			"https://evilpatreon.com/":                         false,
			"https://accounts.google.com.example.com/":         false,
			"https://www.apple.com/":                           false,
			"http://www.patreon.com/login":                     false,
			"https://example.com/":                             false,
			"file:///C:/":                                      false,
			"data:text/html,hello":                             false,
			"chrome://settings/":                               false,
			"https://translate.google.com/?u=https://evil.com": false,
		},
		"youtube": {
			"https://www.youtube.com/":                     true,
			"https://accounts.youtube.com/accounts/SetSID": true,
			"https://consent.youtube.com/m":                true,
			"https://accounts.google.com/ServiceLogin":     true,
			"https://consent.google.com/":                  true,
			"https://gds.google.com/web/recoveryoptions":   true,
			"https://myaccount.google.com/":                true,
			"https://www.google.com/search?q=x":            false,
			"https://www.patreon.com/":                     false,
		},
		"instagram": {
			"https://www.instagram.com/accounts/login/":           true,
			"https://accountscenter.instagram.com/":               true,
			"https://www.facebook.com/dialog/oauth?client_id=124": true,
			"https://m.facebook.com/login.php":                    true,
			"https://accounts.google.com/":                        false,
			"https://www.threads.net/":                            false,
		},
		"facebook": {
			"https://www.facebook.com/login/":              true,
			"https://www.facebook.com/checkpoint/":         true,
			"https://accountscenter.facebook.com/":         true,
			"https://www.instagram.com/accounts/login/":    false,
			"https://accounts.google.com/ServiceLogin":     false,
			"https://l.facebook.com.example.com/l.php?u=x": false,
		},
		"x": {
			"https://x.com/i/flow/login":               true,
			"https://twitter.com/i/flow/login":         true,
			"https://accounts.google.com/gsi/select":   true,
			"https://appleid.apple.com/auth/authorize": true,
			"https://t.co/abc":                         false,
			"https://www.facebook.com/":                false,
		},
		"tiktok": {
			"https://www.tiktok.com/login":              true,
			"https://accounts.google.com/o/oauth2/auth": true,
			"https://appleid.apple.com/auth/authorize":  true,
			"https://www.facebook.com/dialog/oauth":     true,
			"https://www.douyin.com/":                   false,
		},
		"reddit": {
			"https://www.reddit.com/login/":             true,
			"https://accounts.google.com/o/oauth2/auth": true,
			"https://appleid.apple.com/auth/authorize":  true,
			"https://www.facebook.com/":                 false,
		},
		"vimeo": {
			"https://vimeo.com/log_in":                  true,
			"https://accounts.google.com/o/oauth2/auth": true,
			"https://appleid.apple.com/auth/authorize":  true,
			"https://www.facebook.com/":                 false,
		},
		// Twitch signs in on its own pages alone.
		"twitch": {
			"https://www.twitch.tv/login":      true,
			"https://passport.twitch.tv/login": true,
			"https://accounts.google.com/":     false,
			"https://www.amazon.com/ap/signin": false,
		},
	} {
		list := allowListFor(site(t, id))
		for link, want := range cases {
			if got := list.topLevel(link); got != want {
				t.Errorf("%s: may %s be shown at the top? %v, want %v", id, link, got, want)
			}
		}
	}
}

func TestTheGuardKeepsTheTopFrameOnTheListAndEveryRequestOutOfTheLAN(t *testing.T) {
	g := newGuard(site(t, "patreon"), lanNames("nas.fritz.box"))
	for _, c := range []struct {
		url, kind string
		top       bool
		want      bool
	}{
		{"https://www.patreon.com/login", "Document", true, true},
		{"https://example.com/", "Document", true, false},
		{"http://192.168.20.1/", "Document", true, false},
		{"file:///C:/", "Document", true, false},
		// A frame the page embeds, such as a captcha, comes from wherever the
		// site loads it from.
		{"https://challenges.cloudflare.com/turnstile", "Document", false, true},
		{"https://c10.patreonusercontent.com/a.png", "Image", false, true},
		{"https://cdn.example.net/app.js", "Script", false, true},
		{"http://192.168.20.1/", "Image", false, false},
		{"http://10.0.0.5:8080/api", "XHR", false, false},
		{"http://[::1]:8749/api/health", "Fetch", false, false},
		{"http://169.254.169.254/latest/meta-data/", "XHR", false, false},
		{"http://localhost:8749/", "Script", false, false},
		{"http://kl.localhost/", "Document", false, false},
		{"https://nas.fritz.box/", "Document", false, false},
		{"ftp://example.com/file", "Other", false, false},
	} {
		if got := g.allows(context.Background(), c.url, c.kind, c.top); got != c.want {
			t.Errorf("%s %s (top %v): allowed %v, want %v", c.kind, c.url, c.top, got, c.want)
		}
	}
}

func TestTheGuardAsksDNSOnceForAName(t *testing.T) {
	asked := 0
	g := newGuard(site(t, "patreon"), func(context.Context, string) httpx.Scope {
		asked++
		return httpx.ScopePublic
	})
	for range 3 {
		g.allows(context.Background(), "https://cdn.example.net/a.js", "Script", false)
	}
	if asked != 1 {
		t.Errorf("DNS was asked %d times for one name, want once", asked)
	}
}
