package sitelogin

import (
	"slices"
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
