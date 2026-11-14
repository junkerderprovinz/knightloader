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
