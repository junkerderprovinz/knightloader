package main

import (
	"context"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/hub"
)

// newTestController builds a trayController without probing the OS for a tray
// or registering with a hub.
//
// Every wailsruntime call exits the test binary through log.Fatalf on a context
// Wails did not create, so these tests keep ctx nil or drive only branches that
// return before using it.
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
	tc := newTestController(t) // with a nil ctx raiseIfNeeded does nothing

	tc.handleHubMessage(captchaMsg("c1"))
	if _, ok := tc.seenCaptcha["c1"]; !ok {
		t.Fatalf("c1 not tracked after a captcha message")
	}

	tc.handleHubMessage(captchaMsg("c1"))
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
		[]byte(`{"type":"captcha","data":{}}`),
		[]byte(`{"type":"captcha","data":{"id":""}}`),
	} {
		tc.handleHubMessage(raw)
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

// TestSpawnRacingShutdownNeverMisusesTheWaitGroup races spawn against
// onShutdown. A spawn that registers after onShutdown returned either starts a
// goroutine nobody waits for, which the counter below catches, or panics with
// "Add called concurrently with Wait". The race is probabilistic, so it runs
// many rounds; any hit fails hard.
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
		// Released together so the spawns are already running when the
		// shutdown flips.
		barrier.Wait()
		close(start)
		wg.Wait()
		// A second onShutdown waits for any stray goroutine, so the check
		// below sees it.
		tc.onShutdown()
		if n := afterDown.Load(); n != 0 {
			t.Fatalf("round %d: %d goroutine(s) started after onShutdown() returned; spawn registered past the shutdown", i, n)
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

// The page inside the window never reports whether it is visible, so the
// window itself is what counts as watching captchas, and only while it is on
// screen.
func TestTheWindowWatchesCaptchasWhileItIsOnScreen(t *testing.T) {
	h := hub.New()
	tc := newTestController(t)
	tc.joinHub(h)
	t.Cleanup(func() { h.Remove(tc.hubConn) })

	if h.Watched("captcha", 0) {
		t.Fatal("the window watched captchas before it reported itself on screen")
	}
	tc.reportVisible(true)
	if !h.Watched("captcha", 0) {
		t.Error("the window on screen does not count as watching captchas")
	}
	tc.reportVisible(false)
	if h.Watched("captcha", 0) {
		t.Error("the window hidden or minimised still counts as watching captchas")
	}
}

// Asking for captcha events by name must not cost the tray the captchas that
// raise the window.
func TestTheWindowStillHearsOfNewCaptchas(t *testing.T) {
	h := hub.New()
	tc := newTestController(t)
	tc.joinHub(h)
	t.Cleanup(func() { h.Remove(tc.hubConn) })

	h.Broadcast("captcha", map[string]string{"id": "c1"})
	deadline := time.Now().Add(5 * time.Second)
	for {
		tc.mu.Lock()
		_, seen := tc.seenCaptcha["c1"]
		tc.mu.Unlock()
		if seen {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("a captcha broadcast never reached the tray")
		}
		time.Sleep(5 * time.Millisecond)
	}
}
