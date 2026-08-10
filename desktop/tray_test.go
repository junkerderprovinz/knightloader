package main

import (
	"context"
	"path/filepath"
	"testing"
)

// newTestController builds a trayController without probing for a real tray
// or registering with a real hub - the constructor a unit test wants, since
// newTrayController itself talks to the OS (D-Bus on Linux) and a live
// *hub.Hub.
//
// Every wailsruntime.* call (WindowShow, WindowHide, Quit, ...) calls
// log.Fatalf when its context carries none of Wails' own internal values
// (verified against v2.13.0's runtime.go: getFrontend falls through to
// log.Fatalf, which os.Exits the whole test binary, unrecoverable by
// defer/recover) - context.Background() is exactly such a context, so no
// test here may exercise a code path that reaches one of those calls with
// tc.ctx set. Every test below either keeps ctx nil (the early-return guard
// every handler starts with) or only drives branches that return before
// touching ctx at all.
func newTestController(t *testing.T) *trayController {
	t.Helper()
	return &trayController{
		cfg:         defaultConfig(),
		cfgPath:     filepath.Join(t.TempDir(), "desktop.json"),
		seenCaptcha: map[string]struct{}{},
		closed:      make(chan struct{}),
	}
}

func captchaMsg(id string) []byte {
	return []byte(`{"type":"captcha","data":{"id":"` + id + `"}}`)
}

func captchaResolvedMsg(id string) []byte {
	return []byte(`{"type":"captchaResolved","data":{"id":"` + id + `"}}`)
}

func TestNoteCaptchaFirstSeenIsNew(t *testing.T) {
	tc := newTestController(t)
	if !tc.noteCaptcha("c1") {
		t.Errorf("first sighting of c1 reported as not new")
	}
	if tc.noteCaptcha("c1") {
		t.Errorf("second sighting of c1 reported as new")
	}
	if _, ok := tc.seenCaptcha["c1"]; !ok {
		t.Errorf("c1 not retained in seenCaptcha")
	}
}

func TestForgetCaptchaAllowsReRaise(t *testing.T) {
	tc := newTestController(t)
	tc.noteCaptcha("c1")
	tc.forgetCaptcha("c1")
	if _, ok := tc.seenCaptcha["c1"]; ok {
		t.Errorf("c1 still present after forgetCaptcha")
	}
	if !tc.noteCaptcha("c1") {
		t.Errorf("c1 not treated as new after being forgotten")
	}
}

func TestHandleHubMessageTracksNewCaptchaOnly(t *testing.T) {
	tc := newTestController(t) // ctx stays nil: raiseIfNeeded no-ops safely

	tc.handleHubMessage(captchaMsg("c1"))
	if _, ok := tc.seenCaptcha["c1"]; !ok {
		t.Fatalf("c1 not tracked after a captcha message")
	}

	tc.handleHubMessage(captchaMsg("c1")) // duplicate must not error or double-track
	if got := len(tc.seenCaptcha); got != 1 {
		t.Errorf("seenCaptcha has %d entries after a duplicate, want 1", got)
	}

	tc.handleHubMessage(captchaResolvedMsg("c1"))
	if _, ok := tc.seenCaptcha["c1"]; ok {
		t.Errorf("c1 still tracked after captchaResolved")
	}
}

func TestHandleHubMessageIgnoresOtherBroadcastTypes(t *testing.T) {
	tc := newTestController(t)
	for _, raw := range [][]byte{
		[]byte(`{"type":"task","data":{"id":"t1"}}`),
		[]byte(`{"type":"queue","data":{}}`),
		[]byte(`{"type":"activity","data":{"kind":"crawl","active":1,"total":2}}`),
	} {
		tc.handleHubMessage(raw)
	}
	if got := len(tc.seenCaptcha); got != 0 {
		t.Errorf("seenCaptcha has %d entries after non-captcha broadcasts, want 0", got)
	}
}

func TestHandleHubMessageToleratesGarbage(t *testing.T) {
	tc := newTestController(t)
	for _, raw := range [][]byte{
		nil,
		[]byte(""),
		[]byte("{not json"),
		[]byte(`{"type":"captcha","data":"not an object"}`),
		[]byte(`{"type":"captcha","data":{}}`), // no id field at all
		[]byte(`{"type":"captcha","data":{"id":""}}`),
	} {
		tc.handleHubMessage(raw) // must not panic
	}
	if got := len(tc.seenCaptcha); got != 0 {
		t.Errorf("seenCaptcha has %d entries after malformed input, want 0", got)
	}
}

func TestEffectiveStartHiddenRequiresTray(t *testing.T) {
	cases := []struct {
		name          string
		startHidden   bool
		trayAvailable bool
		want          bool
	}{
		{"wants hidden, tray present", true, true, true},
		{"wants hidden, tray absent", true, false, false},
		{"does not want hidden, tray present", false, true, false},
		{"does not want hidden, tray absent", false, false, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			tc := newTestController(t)
			tc.cfg.StartHidden = c.startHidden
			tc.trayAvailable = c.trayAvailable
			if got := tc.effectiveStartHidden(); got != c.want {
				t.Errorf("effectiveStartHidden() = %v, want %v", got, c.want)
			}
		})
	}
}

func TestStartupNoticeOnlyFiresWhenTrayWasWanted(t *testing.T) {
	t.Run("tray available: never shown", func(t *testing.T) {
		tc := newTestController(t)
		tc.trayAvailable = true
		tc.cfg.OnClose = CloseTray
		if _, show := tc.startupNotice(); show {
			t.Errorf("notice shown despite tray being available")
		}
	})
	t.Run("tray absent, nothing wanted it: not shown", func(t *testing.T) {
		tc := newTestController(t)
		tc.trayAvailable = false
		tc.unavailReason = "no tray host"
		if _, show := tc.startupNotice(); show {
			t.Errorf("notice shown despite no preference wanting tray behaviour")
		}
	})
	t.Run("tray absent, start hidden wanted it: shown with reason", func(t *testing.T) {
		tc := newTestController(t)
		tc.trayAvailable = false
		tc.unavailReason = "no tray host registered"
		tc.cfg.StartHidden = true
		msg, show := tc.startupNotice()
		if !show {
			t.Fatalf("notice not shown despite StartHidden wanting tray behaviour")
		}
		if !contains(msg, "no tray host registered") {
			t.Errorf("notice %q does not carry the probe's reason", msg)
		}
	})
}

func TestOnBeforeCloseQuittingAlwaysWinsOverTrayPreference(t *testing.T) {
	// Only the branches that return before touching ctx are safe to drive
	// here - see newTestController's doc comment. Both cases below return
	// false before ever calling wailsruntime.WindowHide.
	tc := newTestController(t)
	tc.quitting = true
	tc.trayAvailable = true
	tc.cfg.OnClose = CloseTray

	if prevented := tc.onBeforeClose(context.Background()); prevented {
		t.Errorf("onBeforeClose prevented close while quitting, want it to let the real close through")
	}
}

func TestOnBeforeCloseExitPreferenceNeverPrevents(t *testing.T) {
	tc := newTestController(t)
	tc.quitting = false
	tc.trayAvailable = true
	tc.cfg.OnClose = CloseExit

	if prevented := tc.onBeforeClose(context.Background()); prevented {
		t.Errorf("onBeforeClose prevented close with OnClose=exit")
	}
}

func TestOnBeforeCloseTrayUnavailableNeverPrevents(t *testing.T) {
	// The exact case the package brief warns about: a saved "close to tray"
	// preference must not prevent a real close when this run found no tray.
	tc := newTestController(t)
	tc.quitting = false
	tc.trayAvailable = false
	tc.cfg.OnClose = CloseTray

	if prevented := tc.onBeforeClose(context.Background()); prevented {
		t.Errorf("onBeforeClose prevented close although the tray is unavailable this run")
	}
}

func TestMutatePersistsAndReturnsNewValue(t *testing.T) {
	tc := newTestController(t)
	got := tc.mutate(func(c *Config) { c.OnClose = CloseTray })
	if got.OnClose != CloseTray {
		t.Fatalf("mutate returned %+v, want OnClose=%q", got, CloseTray)
	}

	reloaded := loadConfig(tc.cfgPath)
	if reloaded.OnClose != CloseTray {
		t.Errorf("reloaded config = %+v, want the mutation to have been persisted", reloaded)
	}
}

func TestIsTrayAvailableReflectsField(t *testing.T) {
	tc := newTestController(t)
	tc.trayAvailable = true
	if !tc.isTrayAvailable() {
		t.Errorf("isTrayAvailable() = false, want true")
	}
	tc.trayAvailable = false
	if tc.isTrayAvailable() {
		t.Errorf("isTrayAvailable() = true, want false")
	}
}

func contains(s, substr string) bool {
	for i := 0; i+len(substr) <= len(s); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
