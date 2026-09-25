package main

import (
	"context"
	"encoding/json"
	"log"
	"os"
	"runtime"
	"sync"
	"time"

	systray "github.com/cardinalby/go-systray"
	"github.com/coder/websocket"
	"github.com/junkerderprovinz/knightloader/internal/hub"
	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

// raisePulse is how long always-on-top is held for the "front and focused"
// level. Wails v2 has no focus call, and a short always-on-top pulse makes the
// window manager raise the window without pinning it.
const raisePulse = 300 * time.Millisecond

// minimizePollInterval is how often the window state is polled for
// minimize-to-tray and for whether the window is on screen. Wails v2 has no
// minimize hook, and document.visibilitychange is unreliable across engines.
const minimizePollInterval = 500 * time.Millisecond

// trayController owns the desktop preferences and the tray icon, and decides
// what the close button, the minimize button and a new captcha do to the
// window. Menu clicks, the minimize poll and the hub writer all touch its
// fields, so every field goes through mu.
type trayController struct {
	mu  sync.Mutex
	cfg Config
	// ctx is nil until onWailsStartup, which can come after the tray menu is
	// already clickable. Handlers skip a nil ctx because the Wails runtime
	// calls log.Fatalf on one.
	ctx context.Context

	trayAvailable bool
	unavailReason string

	quitting bool // set before wailsruntime.Quit so onBeforeClose lets it through

	// shown is false while the window is hidden to the tray. Wails v2 can say
	// whether a window is minimised but not whether it is hidden, so every
	// hide and show goes through hide and show below.
	shown bool

	seenCaptcha map[string]struct{}

	cfgPath string
	hub     *hub.Hub
	hubConn *deskHubConn

	closed    chan struct{}
	closeOnce sync.Once
	wg        sync.WaitGroup

	// closeMu guards only closing, kept apart from mu so the state lock and
	// the lifecycle cannot deadlock each other. closed stays as well, since
	// the loops select on it.
	closeMu sync.Mutex
	closing bool
}

func newTrayController(h *hub.Hub, cfgPath string) *trayController {
	ok, reason := probeTray()
	tc := &trayController{
		cfg:           loadConfig(cfgPath),
		cfgPath:       cfgPath,
		trayAvailable: ok,
		unavailReason: reason,
		seenCaptcha:   map[string]struct{}{},
		closed:        make(chan struct{}),
	}
	tc.shown = !tc.effectiveStartHidden()
	if h != nil {
		tc.joinHub(h)
	}
	return tc
}

// joinHub registers the window with the hub. It asks for the captcha events
// by name, so the window counts as watching captchas while it reports itself
// on screen (see reportVisible); the page inside it does not report, since
// the webview's own visibility is unreliable.
func (tc *trayController) joinHub(h *hub.Hub) {
	tc.hub = h
	tc.hubConn = &deskHubConn{tc: tc}
	h.Add(tc.hubConn)
	h.Subscribe(tc.hubConn, []string{"captcha", "captchaResolved"})
}

// reportVisible tells the hub whether the window is on screen, which decides
// whether the paid captcha solvers wait for an answer at the prompt.
func (tc *trayController) reportVisible(visible bool) {
	if tc.hub != nil {
		tc.hub.SetVisible(tc.hubConn, visible)
	}
}

// spawn runs f in a goroutine that onShutdown waits for, so nothing touches
// the controller's state after shutdown.
func (tc *trayController) spawn(f func()) {
	if !tc.track() {
		return
	}
	go func() {
		defer tc.wg.Done()
		f()
	}()
}

// track adds the caller to wg, or reports false once shutdown has begun, in
// which case the caller must not touch wg. The check and the Add share one lock
// because sync.WaitGroup forbids an Add from zero that races a Wait, and a
// captcha arriving on the hub writer while the user quits hits exactly that.
func (tc *trayController) track() bool {
	tc.closeMu.Lock()
	defer tc.closeMu.Unlock()
	if tc.closing {
		return false
	}
	tc.wg.Add(1)
	return true
}

// isTrayAvailable reports the startup probe's result.
func (tc *trayController) isTrayAvailable() bool {
	tc.mu.Lock()
	defer tc.mu.Unlock()
	return tc.trayAvailable
}

// effectiveStartHidden reports whether the window starts hidden. Without a
// tray there would be no way to bring it back, so the preference applies only
// when the tray is available.
func (tc *trayController) effectiveStartHidden() bool {
	tc.mu.Lock()
	defer tc.mu.Unlock()
	return tc.cfg.StartHidden && tc.trayAvailable
}

// startupNotice returns the message shown when the saved preferences want the
// tray but this run found none. A machine that never asked for the tray gets
// no message.
func (tc *trayController) startupNotice() (msg string, show bool) {
	tc.mu.Lock()
	defer tc.mu.Unlock()
	if tc.trayAvailable {
		return "", false
	}
	wantsTray := tc.cfg.StartHidden || tc.cfg.OnClose == CloseTray || tc.cfg.OnMinimize == MinimizeTray
	if !wantsTray {
		return "", false
	}
	return "System tray unavailable on this desktop (" + tc.unavailReason + ")." +
		" \"Start hidden\", \"close to tray\" and \"minimize to tray\" are disabled for this run;" +
		" KnightLoader will use its normal window and taskbar behaviour instead.", true
}

// onWailsStartup is Wails' OnStartup hook, the first point ctx exists.
func (tc *trayController) onWailsStartup(ctx context.Context) {
	tc.mu.Lock()
	tc.ctx = ctx
	tc.mu.Unlock()

	tc.spawn(tc.pollWindow)

	if msg, show := tc.startupNotice(); show {
		tc.spawn(func() {
			_, _ = wailsruntime.MessageDialog(ctx, wailsruntime.MessageDialogOptions{
				Type:    wailsruntime.WarningDialog,
				Title:   "System tray unavailable",
				Message: msg,
			})
		})
	}
}

// onBeforeClose decides what the close button does from the live preference.
// Every platform routes the native close through it while HideWindowOnClose is
// false.
func (tc *trayController) onBeforeClose(ctx context.Context) bool {
	tc.mu.Lock()
	quitting := tc.quitting
	toTray := tc.cfg.OnClose == CloseTray && tc.trayAvailable
	tc.mu.Unlock()

	if quitting || !toTray {
		return false
	}
	tc.hide(ctx)
	return true
}

// quit is the tray menu's Quit action. runtime.Quit also passes through
// onBeforeClose, so quitting is set first to get past "close to tray".
func (tc *trayController) quit() {
	tc.mu.Lock()
	tc.quitting = true
	ctx := tc.ctx
	tc.mu.Unlock()

	if ctx == nil {
		// Wails never finished starting, so nothing is running yet.
		os.Exit(0)
		return
	}
	wailsruntime.Quit(ctx)
}

// onShutdown runs from main's OnShutdown before a.Close. It stops the loops and
// waits for every spawned goroutine, so none reads ctx after Wails invalidates
// it.
func (tc *trayController) onShutdown() {
	// The flag refuses new work before Wait; the channel stops the running
	// loops.
	tc.closeMu.Lock()
	tc.closing = true
	tc.closeMu.Unlock()
	tc.closeOnce.Do(func() { close(tc.closed) })
	tc.wg.Wait()
	if tc.hub != nil && tc.hubConn != nil {
		tc.hub.Remove(tc.hubConn)
	}
	if tc.trayAvailable {
		systray.Quit()
	}
}

// pollWindow hides a newly minimised window to the tray when the preference
// asks for it, and reports whether the window is on screen.
func (tc *trayController) pollWindow() {
	ticker := time.NewTicker(minimizePollInterval)
	defer ticker.Stop()

	wasMinimised := false
	for {
		select {
		case <-tc.closed:
			return
		case <-ticker.C:
		}

		tc.mu.Lock()
		ctx := tc.ctx
		toTray := tc.cfg.OnMinimize == MinimizeTray && tc.trayAvailable
		tc.mu.Unlock()
		if ctx == nil {
			continue
		}

		isMin := wailsruntime.WindowIsMinimised(ctx)
		if isMin && !wasMinimised && toTray {
			tc.hide(ctx)
		}
		wasMinimised = isMin

		tc.mu.Lock()
		shown := tc.shown
		tc.mu.Unlock()
		tc.reportVisible(shown && !isMin)
	}
}

// hide puts the window away and show brings it back, recording which for
// reportVisible.
func (tc *trayController) hide(ctx context.Context) {
	tc.mu.Lock()
	tc.shown = false
	tc.mu.Unlock()
	wailsruntime.WindowHide(ctx)
}

func (tc *trayController) show(ctx context.Context) {
	tc.mu.Lock()
	tc.shown = true
	tc.mu.Unlock()
	wailsruntime.WindowShow(ctx)
	wailsruntime.WindowUnminimise(ctx)
}

// raiseIfNeeded brings the window forward for a new captcha at the configured
// level. It runs on the hub's writer goroutine.
func (tc *trayController) raiseIfNeeded() {
	tc.mu.Lock()
	ctx := tc.ctx
	level := tc.cfg.RaiseOnAttention
	tc.mu.Unlock()

	if ctx == nil || level == RaiseOff {
		return
	}
	tc.show(ctx)
	if level != RaiseFocus {
		return
	}
	wailsruntime.WindowSetAlwaysOnTop(ctx, true)
	tc.spawn(func() {
		time.Sleep(raisePulse)
		tc.mu.Lock()
		c := tc.ctx
		tc.mu.Unlock()
		if c != nil {
			wailsruntime.WindowSetAlwaysOnTop(c, false)
		}
	})
}

// mutate applies f to the config, saves it and returns the new value for the
// menu checkmarks. The write happens outside the lock so a slow disk does not
// block the hub listener or the poll.
func (tc *trayController) mutate(f func(*Config)) Config {
	tc.mu.Lock()
	f(&tc.cfg)
	cfg := tc.cfg
	tc.mu.Unlock()

	if err := cfg.save(tc.cfgPath); err != nil {
		log.Printf("desktop: saving preferences: %v", err)
	}
	return cfg
}

// handleHubMessage reads one hub broadcast frame and raises the window for a
// captcha seen for the first time. Only the id is read from the payload.
func (tc *trayController) handleHubMessage(raw []byte) {
	var env struct {
		Type string          `json:"type"`
		Data json.RawMessage `json:"data"`
	}
	if json.Unmarshal(raw, &env) != nil {
		return
	}

	var withID struct {
		ID string `json:"id"`
	}
	switch env.Type {
	case "captcha":
		if json.Unmarshal(env.Data, &withID) != nil || withID.ID == "" {
			return
		}
		if tc.noteCaptcha(withID.ID) {
			tc.raiseIfNeeded()
		}
	case "captchaResolved":
		if json.Unmarshal(env.Data, &withID) == nil && withID.ID != "" {
			tc.forgetCaptcha(withID.ID)
		}
	}
}

// noteCaptcha records a challenge id and reports whether it is new.
func (tc *trayController) noteCaptcha(id string) (isNew bool) {
	tc.mu.Lock()
	defer tc.mu.Unlock()
	_, known := tc.seenCaptcha[id]
	tc.seenCaptcha[id] = struct{}{}
	return !known
}

// forgetCaptcha drops a resolved challenge so seenCaptcha holds only open ones.
func (tc *trayController) forgetCaptcha(id string) {
	tc.mu.Lock()
	defer tc.mu.Unlock()
	delete(tc.seenCaptcha, id)
}

// deskHubConn is an in-process hub.Conn that feeds the tray the captcha
// broadcasts a browser tab gets, so a captcha can raise the native window
// without changes to the shared frontend.
type deskHubConn struct {
	tc *trayController
}

func (c *deskHubConn) Write(_ context.Context, _ websocket.MessageType, p []byte) error {
	c.tc.handleHubMessage(p)
	return nil
}

func (c *deskHubConn) CloseNow() error { return nil }

// trayIconForPlatform returns the icon format systray expects: ICO on Windows,
// PNG elsewhere.
func trayIconForPlatform() []byte {
	if runtime.GOOS == "windows" {
		return trayIconICO
	}
	return trayIconPNG
}

// runTray builds the menu and blocks in systray's event loop until Quit.
func runTray(tc *trayController) {
	systray.Run(tc.onReady, tc.onExit)
}

func (tc *trayController) onReady() {
	systray.SetIcon(trayIconForPlatform())
	systray.SetTooltip("KnightLoader")

	mShow := systray.AddMenuItem("Show KnightLoader", "Show the main window")
	mHide := systray.AddMenuItem("Hide window", "Send the window to the tray")
	systray.AddSeparator()

	tc.mu.Lock()
	cfg := tc.cfg
	tc.mu.Unlock()

	mStartHidden := systray.AddMenuItemCheckbox("Start hidden", "Start KnightLoader without showing the window", cfg.StartHidden)
	mCloseTray := systray.AddMenuItemCheckbox("Close button sends to tray", "The window close button hides to the tray instead of exiting", cfg.OnClose == CloseTray)
	mMinTray := systray.AddMenuItemCheckbox("Minimize button sends to tray", "Minimizing sends the window to the tray instead of the taskbar", cfg.OnMinimize == MinimizeTray)

	systray.AddSeparator()
	raiseParent := systray.AddMenuItem("When a captcha needs you", "How hard the window asks for attention")
	mRaiseOff := raiseParent.AddSubMenuItemCheckbox("Do nothing", "", cfg.RaiseOnAttention == RaiseOff)
	mRaiseFront := raiseParent.AddSubMenuItemCheckbox("Bring window to front", "", cfg.RaiseOnAttention == RaiseFront)
	mRaiseFocus := raiseParent.AddSubMenuItemCheckbox("Bring to front and focus", "", cfg.RaiseOnAttention == RaiseFocus)

	systray.AddSeparator()
	mQuit := systray.AddMenuItem("Quit KnightLoader", "Stop KnightLoader completely")

	for {
		select {
		case <-tc.closed:
			return
		case <-mShow.ClickedCh:
			tc.showWindow()
		case <-mHide.ClickedCh:
			tc.hideWindow()
		case <-mStartHidden.ClickedCh:
			cfg := tc.mutate(func(c *Config) { c.StartHidden = !c.StartHidden })
			setChecked(mStartHidden, cfg.StartHidden)
		case <-mCloseTray.ClickedCh:
			cfg := tc.mutate(func(c *Config) {
				if c.OnClose == CloseTray {
					c.OnClose = CloseExit
				} else {
					c.OnClose = CloseTray
				}
			})
			setChecked(mCloseTray, cfg.OnClose == CloseTray)
		case <-mMinTray.ClickedCh:
			cfg := tc.mutate(func(c *Config) {
				if c.OnMinimize == MinimizeTray {
					c.OnMinimize = MinimizeTaskbar
				} else {
					c.OnMinimize = MinimizeTray
				}
			})
			setChecked(mMinTray, cfg.OnMinimize == MinimizeTray)
		case <-mRaiseOff.ClickedCh:
			tc.setRaiseLevel(RaiseOff, mRaiseOff, mRaiseFront, mRaiseFocus)
		case <-mRaiseFront.ClickedCh:
			tc.setRaiseLevel(RaiseFront, mRaiseFront, mRaiseOff, mRaiseFocus)
		case <-mRaiseFocus.ClickedCh:
			tc.setRaiseLevel(RaiseFocus, mRaiseFocus, mRaiseOff, mRaiseFront)
		case <-mQuit.ClickedCh:
			tc.quit()
		}
	}
}

// onExit has nothing to do; onShutdown owns the teardown.
func (tc *trayController) onExit() {}

func (tc *trayController) showWindow() {
	tc.mu.Lock()
	ctx := tc.ctx
	tc.mu.Unlock()
	if ctx == nil {
		return
	}
	tc.show(ctx)
}

func (tc *trayController) hideWindow() {
	tc.mu.Lock()
	ctx := tc.ctx
	tc.mu.Unlock()
	if ctx == nil {
		return
	}
	tc.hide(ctx)
}

func (tc *trayController) setRaiseLevel(level string, on *systray.MenuItem, offs ...*systray.MenuItem) {
	tc.mutate(func(c *Config) { c.RaiseOnAttention = level })
	on.Check()
	for _, o := range offs {
		o.Uncheck()
	}
}

func setChecked(item *systray.MenuItem, on bool) {
	if on {
		item.Check()
	} else {
		item.Uncheck()
	}
}
