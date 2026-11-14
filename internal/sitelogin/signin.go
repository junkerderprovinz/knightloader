package sitelogin

import (
	"slices"
	"strings"
	"time"
)

// HoldsSignIn reports whether a jar carries one of the site's sign-in cookies
// that still lives, without asking the site. It is cheap enough to ask while
// somebody is signing in.
func HoldsSignIn(site Site, jar string, now time.Time) bool {
	return slices.ContainsFunc(parseJar(jar), func(c jarCookie) bool {
		return slices.Contains(site.signIn, c.Name) && c.live(now)
	})
}

// Hoster is a file hoster's sign-in page for the sign-in browser. It is no
// entry of Sites, since its session goes to the hoster's own client rather
// than to yt-dlp. signIn names the cookies only a signed-in session carries.
func Hoster(id, name, host, loginURL string, domains []string, signIn ...string) Site {
	return Site{ID: id, Name: name, Host: host, LoginURL: loginURL, Domains: domains, signIn: signIn}
}

// CookieHeader writes the live cookies of jar as a Cookie header, which is
// how a hoster's own client takes up a session.
func CookieHeader(jar string, now time.Time) string {
	var parts []string
	for _, c := range parseJar(jar) {
		if c.live(now) {
			parts = append(parts, c.Name+"="+c.Value)
		}
	}
	return strings.Join(parts, "; ")
}
