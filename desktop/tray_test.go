package main

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/hub"
)

// newTestTray builds a Tray without probing the OS for a tray, registering
// with a hub or attaching a window.
func newTestTray(t *testing.T) *Tray {
	t.Helper()
	return &Tray{
		cfg:         defaultConfig(),
		cfgPath:     filepath.Join(t.TempDir(), "desktop.json"),
		seenCaptcha: map[string]struct{}{},
	}
}

func captchaMsg(id string) []byte {
	return []byte(`{"type":"captcha","data":{"id":"` + id + `"}}`)
}

func captchaResolvedMsg(id string) []byte {
	return []byte(`{"type":"captchaResolved","data":{"id":"` + id + `"}}`)
}

func queueMsg(halted bool) []byte {
	if halted {
		return []byte(`{"type":"queue","data":{"halted":true,"running":2}}`)
	}
	return []byte(`{"type":"queue","data":{"halted":false,"running":2}}`)
}

func TestNoteCaptchaFirstSeenIsNew(t *testing.T) {
	tr := newTestTray(t)
	if !tr.noteCaptcha("c1") {
		t.Errorf("first sighting of c1 reported as not new")
	}
	if tr.noteCaptcha("c1") {
		t.Errorf("second sighting of c1 reported as new")
	}
	if _, ok := tr.seenCaptcha["c1"]; !ok {
		t.Errorf("c1 not retained in seenCaptcha")
	}
}

func TestForgetCaptchaAllowsReRaise(t *testing.T) {
	tr := newTestTray(t)
	tr.noteCaptcha("c1")
	tr.forgetCaptcha("c1")
	if _, ok := tr.seenCaptcha["c1"]; ok {
		t.Errorf("c1 still present after forgetCaptcha")
	}
	if !tr.noteCaptcha("c1") {
		t.Errorf("c1 not treated as new after being forgotten")
	}
}

func TestHandleHubMessageTracksNewCaptchaOnly(t *testing.T) {
	tr := newTestTray(t) // without a window raiseIfNeeded does nothing

	tr.handleHubMessage(captchaMsg("c1"))
	if _, ok := tr.seenCaptcha["c1"]; !ok {
		t.Fatalf("c1 not tracked after a captcha message")
	}

	tr.handleHubMessage(captchaMsg("c1"))
	if got := len(tr.seenCaptcha); got != 1 {
		t.Errorf("seenCaptcha has %d entries after a duplicate, want 1", got)
	}

	tr.handleHubMessage(captchaResolvedMsg("c1"))
	if _, ok := tr.seenCaptcha["c1"]; ok {
		t.Errorf("c1 still tracked after captchaResolved")
	}
}

func TestHandleHubMessageIgnoresOtherBroadcastTypes(t *testing.T) {
	tr := newTestTray(t)
	for _, raw := range [][]byte{
		[]byte(`{"type":"task","data":{"id":"t1"}}`),
		[]byte(`{"type":"queue","data":{}}`),
		[]byte(`{"type":"activity","data":{"kind":"crawl","active":1,"total":2}}`),
	} {
		tr.handleHubMessage(raw)
	}
	if got := len(tr.seenCaptcha); got != 0 {
		t.Errorf("seenCaptcha has %d entries after non-captcha broadcasts, want 0", got)
	}
}

func TestHandleHubMessageToleratesGarbage(t *testing.T) {
	tr := newTestTray(t)
	for _, raw := range [][]byte{
		nil,
		[]byte(""),
		[]byte("{not json"),
		[]byte(`{"type":"captcha","data":"not an object"}`),
		[]byte(`{"type":"captcha","data":{}}`),
		[]byte(`{"type":"captcha","data":{"id":""}}`),
		[]byte(`{"type":"queue","data":"not an object"}`),
	} {
		tr.handleHubMessage(raw)
	}
	if got := len(tr.seenCaptcha); got != 0 {
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
			tr := newTestTray(t)
			tr.cfg.StartHidden = c.startHidden
			tr.trayAvailable = c.trayAvailable
			if got := tr.effectiveStartHidden(); got != c.want {
				t.Errorf("effectiveStartHidden() = %v, want %v", got, c.want)
			}
		})
	}
}

func TestStartupNoticeOnlyFiresWhenTrayWasWanted(t *testing.T) {
	t.Run("tray available: never shown", func(t *testing.T) {
		tr := newTestTray(t)
		tr.trayAvailable = true
		tr.cfg.OnClose = CloseTray
		if _, show := tr.startupNotice(); show {
			t.Errorf("notice shown despite tray being available")
		}
	})
	t.Run("tray absent, nothing wanted it: not shown", func(t *testing.T) {
		tr := newTestTray(t)
		tr.trayAvailable = false
		tr.unavailReason = "no tray host"
		if _, show := tr.startupNotice(); show {
			t.Errorf("notice shown despite no preference wanting tray behaviour")
		}
	})
	t.Run("tray absent, start hidden wanted it: shown with reason", func(t *testing.T) {
		tr := newTestTray(t)
		tr.trayAvailable = false
		tr.unavailReason = "no tray host registered"
		tr.cfg.StartHidden = true
		msg, show := tr.startupNotice()
		if !show {
			t.Fatalf("notice not shown despite StartHidden wanting tray behaviour")
		}
		if !strings.Contains(msg, "no tray host registered") {
			t.Errorf("notice %q does not carry the probe's reason", msg)
		}
	})
}

func TestTheCloseButtonHidesOnlyWhenAskedAndATrayIsThere(t *testing.T) {
	cases := []struct {
		name          string
		onClose       string
		trayAvailable bool
		want          bool
	}{
		{"close to tray, tray present", CloseTray, true, true},
		{"close to tray, tray absent", CloseTray, false, false},
		{"exit, tray present", CloseExit, true, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			tr := newTestTray(t)
			tr.cfg.OnClose = c.onClose
			tr.trayAvailable = c.trayAvailable
			if got := tr.closeHides(); got != c.want {
				t.Errorf("closeHides() = %v, want %v", got, c.want)
			}
		})
	}
}

func TestMutatePersistsAndReturnsNewValue(t *testing.T) {
	tr := newTestTray(t)
	got := tr.mutate(func(c *Config) { c.OnClose = CloseTray })
	if got.OnClose != CloseTray {
		t.Fatalf("mutate returned %+v, want OnClose=%q", got, CloseTray)
	}

	reloaded := loadConfig(tr.cfgPath)
	if reloaded.OnClose != CloseTray {
		t.Errorf("reloaded config = %+v, want the mutation to have been persisted", reloaded)
	}
}

func TestIsTrayAvailableReflectsField(t *testing.T) {
	tr := newTestTray(t)
	tr.trayAvailable = true
	if !tr.isTrayAvailable() {
		t.Errorf("isTrayAvailable() = false, want true")
	}
	tr.trayAvailable = false
	if tr.isTrayAvailable() {
		t.Errorf("isTrayAvailable() = true, want false")
	}
}

// The page inside the window never reports whether it is visible, so the
// window itself is what counts as watching captchas, and only while it is on
// screen.
func TestTheWindowWatchesCaptchasWhileItIsOnScreen(t *testing.T) {
	h := hub.New()
	tr := newTestTray(t)
	tr.joinHub(h)
	t.Cleanup(func() { h.Remove(tr.hubConn) })

	if h.Watched("captcha", 0) {
		t.Fatal("the window watched captchas before it reported itself on screen")
	}
	tr.show()
	if !h.Watched("captcha", 0) {
		t.Error("the window on screen does not count as watching captchas")
	}
	tr.minimisedTo(true)
	if h.Watched("captcha", 0) {
		t.Error("the minimised window still counts as watching captchas")
	}
	tr.minimisedTo(false)
	if !h.Watched("captcha", 0) {
		t.Error("the restored window does not count as watching captchas")
	}
	tr.hide()
	if h.Watched("captcha", 0) {
		t.Error("the window in the tray still counts as watching captchas")
	}
}

// Minimising to the tray takes the window off the screen, so it stops
// watching even though the window reports no hide of its own.
func TestMinimisingToTheTrayHidesTheWindow(t *testing.T) {
	tr := newTestTray(t)
	tr.trayAvailable = true
	tr.cfg.OnMinimize = MinimizeTray
	tr.show()
	tr.minimisedTo(true)
	if tr.shown {
		t.Error("the window minimised to the tray still counts as shown")
	}
}

// Asking for captcha events by name must not cost the tray the captchas that
// raise the window.
func TestTheWindowStillHearsOfNewCaptchas(t *testing.T) {
	h := hub.New()
	tr := newTestTray(t)
	tr.joinHub(h)
	t.Cleanup(func() { h.Remove(tr.hubConn) })

	h.Broadcast("captcha", map[string]string{"id": "c1"})
	deadline := time.Now().Add(5 * time.Second)
	for {
		tr.mu.Lock()
		_, seen := tr.seenCaptcha["c1"]
		tr.mu.Unlock()
		if seen {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("a captcha broadcast never reached the tray")
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestTheMenuSpeaksEnglishUntilThePageHandsItWords(t *testing.T) {
	tr := newTestTray(t)
	_, items := tr.buildMenu()
	if got := items.show.Label(); got != "Show KnightLoader" {
		t.Errorf("show entry reads %q", got)
	}
	if got := items.queue.Label(); got != "Stop queue" {
		t.Errorf("queue entry reads %q on a running queue", got)
	}
}

func TestNewWordsReachTheMenuAndTheNextStart(t *testing.T) {
	tr := newTestTray(t)
	_, tr.items = tr.buildMenu()

	tr.SetWords(TrayWords{Show: "KnightLoader anzeigen", StopQueue: "Warteschlange stoppen", Captcha: "Wenn ein Captcha dich braucht"})
	if got := tr.items.show.Label(); got != "KnightLoader anzeigen" {
		t.Errorf("show entry reads %q after new words", got)
	}
	if got := tr.items.captcha.Label(); got != "Wenn ein Captcha dich braucht" {
		t.Errorf("captcha submenu reads %q after new words", got)
	}
	if got := tr.items.quit.Label(); got != "Quit KnightLoader" {
		t.Errorf("a word the page left out reads %q instead of the English", got)
	}

	next := loadConfig(tr.cfgPath).Words.withDefaults()
	if next.Show != "KnightLoader anzeigen" || next.StopQueue != "Warteschlange stoppen" {
		t.Errorf("the next start would read %+v", next)
	}
}

func TestTheQueueEntryFollowsTheMasterSwitch(t *testing.T) {
	tr := newTestTray(t)
	_, tr.items = tr.buildMenu()

	tr.handleHubMessage(queueMsg(true))
	if got := tr.items.queue.Label(); got != "Start queue" {
		t.Errorf("queue entry reads %q on a halted queue", got)
	}
	tr.handleHubMessage(queueMsg(false))
	if got := tr.items.queue.Label(); got != "Stop queue" {
		t.Errorf("queue entry reads %q on a running queue", got)
	}
}

func TestTheMenuTicksWhatIsSaved(t *testing.T) {
	tr := newTestTray(t)
	tr.cfg.StartHidden = true
	tr.cfg.OnMinimize = MinimizeTray
	tr.cfg.RaiseOnAttention = RaiseFocus
	_, items := tr.buildMenu()
	if !items.startHidden.Checked() || items.closeToTray.Checked() || !items.minimiseToTray.Checked() {
		t.Errorf("ticks: start hidden %v, close to tray %v, minimise to tray %v",
			items.startHidden.Checked(), items.closeToTray.Checked(), items.minimiseToTray.Checked())
	}
	if items.raiseOff.Checked() || items.raiseFront.Checked() || !items.raiseFocus.Checked() {
		t.Error("the captcha level ticked is not the saved one")
	}
}

func TestTheTrayWindowComesBackAtTheSizeItWasGiven(t *testing.T) {
	tr := newTestTray(t)
	if w, h := tr.overviewSize(); w != overviewWidth || h != overviewHeight {
		t.Fatalf("a first start opens at %dx%d, want the default", w, h)
	}
	tr.keepOverviewSize(420, 640)
	next := &Tray{cfg: loadConfig(tr.cfgPath)}
	if w, h := next.overviewSize(); w != 420 || h != 640 {
		t.Errorf("the next start opens at %dx%d, want 420x640", w, h)
	}
}

// A hand edit below the least size still opens a window the content fits.
func TestTheTrayWindowNeverOpensSmallerThanItsContent(t *testing.T) {
	tr := newTestTray(t)
	tr.cfg.OverviewWidth, tr.cfg.OverviewHeight = 100, 50
	if w, h := tr.overviewSize(); w != overviewMinWidth || h != overviewMinHeight {
		t.Errorf("opens at %dx%d, want at least %dx%d", w, h, overviewMinWidth, overviewMinHeight)
	}
}
