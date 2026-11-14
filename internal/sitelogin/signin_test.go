package sitelogin

import (
	"testing"
	"time"
)

func TestAJarHoldsASignInOnlyWithALiveSignInCookie(t *testing.T) {
	patreon, _ := Lookup("patreon")
	for name, c := range map[string]struct {
		jar  string
		want bool
	}{
		"a live session":     {jar(jarLine(".patreon.com", "session_id", "S", now.Add(time.Hour))), true},
		"a browser session":  {jar(jarLine(".patreon.com", "session_id", "S", time.Time{})), true},
		"a session ran out":  {jar(jarLine(".patreon.com", "session_id", "S", now.Add(-time.Hour))), false},
		"only other cookies": {jar(jarLine(".patreon.com", "patreon_device_id", "D", now.Add(time.Hour))), false},
		"nothing":            {"", false},
	} {
		if got := HoldsSignIn(patreon, c.jar, now); got != c.want {
			t.Errorf("%s: HoldsSignIn = %v, want %v", name, got, c.want)
		}
	}
}

func TestAHosterKnowsItsSignInCookieAndHandsItsSessionOnAsACookieHeader(t *testing.T) {
	site := Hoster("katfile", "KatFile", "katfile.com", "https://katfile.com/login.html", []string{"katfile.com", "katfile.cloud"}, "xfss")
	now := time.Now()
	jar := JarFromCookies(site, []BrowserCookie{
		{Domain: "katfile.com", HostOnly: true, Path: "/", Session: true, Name: "login", Value: "fox"},
		{Domain: ".katfile.com", Path: "/", Expires: float64(now.Add(time.Hour).Unix()), Name: "xfss", Value: "s3cr3t"},
		{Domain: "example.net", HostOnly: true, Path: "/", Session: true, Name: "xfss", Value: "other"},
	}, now)

	if !HoldsSignIn(site, jar, now) {
		t.Error("a jar with the session cookie does not hold the sign-in")
	}
	if got := CookieHeader(jar, now); got != "login=fox; xfss=s3cr3t" {
		t.Errorf("CookieHeader = %q, want the site's own cookies", got)
	}
	if got := CookieHeader(jar, now.Add(2*time.Hour)); got != "login=fox" {
		t.Errorf("CookieHeader after the session ran out = %q, want the live cookie alone", got)
	}
}
