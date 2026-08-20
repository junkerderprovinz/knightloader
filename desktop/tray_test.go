package main

import (
	"context"
	"path/filepath"
	"sync"
	"sync/atomic"
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

// TestSpawnRacingShutdownNeverMisusesTheWaitGroup hammers the window that used
// to be open between spawn's tc.closed check and its tc.wg.Add(1). With those
// two apart, an onShutdown landing in the gap could close the channel, find
// the WaitGroup counter at zero, return - and only then would the spawn
// register and start a goroutine nothing was waiting for any more, free to go
// on reading tc.ctx after main has torn the Wails context down. Both of that
// bug's outcomes fail this test: the goroutine that starts late trips the
// counter below, and the WaitGroup's own "Add called concurrently with Wait"
// panic takes the binary down.
//
// The exposure is real: raiseIfNeeded spawns from the hub's own writer
// goroutine, so a captcha arriving while the user quits is this interleaving
// with nothing synthetic about it.
//
// Probabilistic on purpose, and worth saying plainly: the window is two
// adjacent statements wide, so no amount of test-side scheduling makes hitting
// it a certainty. What the rounds buy is repeated exposure under -race; what
// the invariant buys is that any hit at all is a hard, non-flaky failure.
//
// That it detects the real thing was established rather than assumed: with a
// throwaway 2ms sleep dropped between the old code's channel check and its Add
// - the window widened, nothing else changed - this failed inside the first
// two rounds in three runs out of three, on the count below. The count is the
// detector rather than the WaitGroup's own misuse panic because that is what
// the probe produced: a late Add lands after Wait has returned, not during it.
// The panic is the same window's other documented outcome, reached when Wait
// is still in progress, and no test can force which of the two a given
// interleaving gives.
//
// The spawned closure deliberately touches nothing but its own counter: every
// wailsruntime call log.Fatalf's on a context that is not Wails' own, per
// newTestController's comment. Nor is the controller built by that helper -
// nothing here reads cfg or cfgPath, and a round is cheap enough that its
// t.TempDir() would be the most expensive thing in the loop.
func TestSpawnRacingShutdownNeverMisusesTheWaitGroup(t *testing.T) {
	const (
		rounds     = 400
		spawners   = 4
		spawnsEach = 25
	)
	for i := 0; i < rounds; i++ {
		tc := &trayController{
			cfg:         defaultConfig(),
			seenCaptcha: map[string]struct{}{},
			closed:      make(chan struct{}),
		}

		var (
			down      atomic.Bool
			afterDown atomic.Int64
			wg        sync.WaitGroup
			barrier   sync.WaitGroup
			start     = make(chan struct{})
		)
		barrier.Add(spawners + 1)
		wg.Add(spawners + 1)
		for j := 0; j < spawners; j++ {
			go func() {
				defer wg.Done()
				barrier.Done()
				<-start
				for k := 0; k < spawnsEach; k++ {
					tc.spawn(func() {
						if down.Load() {
							afterDown.Add(1)
						}
					})
				}
			}()
		}
		go func() {
			defer wg.Done()
			barrier.Done()
			<-start
			tc.onShutdown()
			down.Store(true)
		}()
		// Released together rather than one after the other: the spawn stream
		// has to already be running when the shutdown flips, or every call
		// lands on the same side of it and the window never gets exercised.
		barrier.Wait()
		close(start)
		wg.Wait()
		// A second onShutdown, purely to drain: on a build where spawn could
		// still register past the first one, this waits for that stray
		// goroutine so the check below actually sees it. On a correct build
		// there is nothing left to wait for and it returns at once.
		tc.onShutdown()
		if n := afterDown.Load(); n != 0 {
			t.Fatalf("round %d: %d goroutine(s) started after onShutdown() returned - spawn registered past the shutdown", i, n)
		}
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
