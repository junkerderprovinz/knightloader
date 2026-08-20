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

// raisePulse is how long WindowSetAlwaysOnTop is held on for the "front and
// focused" attention level before releasing it. Wails v2 exposes no direct
// window-focus call (verified against v2.13.0's runtime package); pulsing
// always-on-top is the documented trick for forcing the window manager to
// raise a window above whatever currently holds focus, without leaving it
// permanently pinned above everything.
const raisePulse = 300 * time.Millisecond

// minimizePollInterval is how often the minimize-to-tray poll checks window
// state. Wails v2 has no OnMinimize hook (verified: neither options.App nor
// pkg/options/windows exposes one, and the frontend implementations only
// call OnBeforeClose from the close path) - WindowIsMinimised is the only
// way to observe it, so this trades a small, constant poll for a real signal
// instead of the frontend-only, cross-engine-unreliable alternative
// (document.visibilitychange), which would also require editing the shared
// web frontend that the browser build serves unmodified.
const minimizePollInterval = 500 * time.Millisecond

// trayController owns the desktop-local window/tray preferences (config.go)
// and the running tray icon, and is the one place that decides what the
// close button, the minimize button and a newly-arrived captcha challenge do
// to the window. Every field below is read or written from at least three
// goroutines that do not share a call stack - a systray menu click, the
// minimize-poll loop, and the hub's own writer goroutine delivering a
// captcha broadcast - so, per this codebase's established rule for shared
// mutable state, every one of them goes through mu.
type trayController struct {
	mu  sync.Mutex
	cfg Config
	// ctx is the Wails-provided lifecycle context. It is nil until
	// onWailsStartup fires, which can be after the tray menu is already up
	// and clickable (systray.Run is started before wails.Run - see
	// main.go), so every ctx-consuming handler below must treat nil as
	// "too early" and no-op rather than pass it on: runtime.WindowShow and
	// friends call log.Fatalf on a nil context (verified against
	// v2.13.0's runtime.go), which would hard-crash the whole process.
	ctx context.Context

	trayAvailable bool
	unavailReason string

	quitting bool // set before wailsruntime.Quit so onBeforeClose lets it through

	seenCaptcha map[string]struct{}

	cfgPath string
	hub     *hub.Hub
	hubConn *deskHubConn

	closed    chan struct{}
	closeOnce sync.Once
	wg        sync.WaitGroup

	// closeMu guards closing and nothing else, and is deliberately not mu:
	// that one is this file's state lock, taken by every handler here, and
	// giving one mutex two unrelated jobs is how a later change to one of them
	// becomes a lifecycle deadlock. closing is what makes "is this controller
	// still accepting work?" and "count me in" one atomic step; see track. It
	// does not replace the closed channel - that is what the poll loop and the
	// hub listener select on, which a flag cannot do.
	closeMu sync.Mutex
	closing bool
}

func newTrayController(h *hub.Hub, cfgPath string) *trayController {
	ok, reason := probeTray()
	tc := &trayController{
		cfg:           loadConfig(cfgPath),
		cfgPath:       cfgPath,
		hub:           h,
		trayAvailable: ok,
		unavailReason: reason,
		seenCaptcha:   map[string]struct{}{},
		closed:        make(chan struct{}),
	}
	if h != nil {
		tc.hubConn = &deskHubConn{tc: tc}
		h.Add(tc.hubConn)
	}
	return tc
}

// spawn tracks a goroutine so shutdown can wait for it, the same discipline
// app.spawn enforces for App-owned state - this package has its own mutable
// state (cfg, ctx, seenCaptcha) subject to exactly the same "a write after
// Close" hazard. Which side of the shutdown a given call falls on is track's
// decision, taken as one atomic step - not a check onShutdown can overtake
// between the check and the register.
func (tc *trayController) spawn(f func()) {
	if !tc.track() {
		return
	}
	go func() {
		defer tc.wg.Done()
		f()
	}()
}

// track counts the caller in as work onShutdown has to wait for, or reports
// false if the shutdown has already begun - in which case the caller must not
// touch tc.wg at all. Every tc.wg.Add(1) in this package goes through here.
//
// The check and the Add are one step under one lock on purpose. Written the
// obvious way instead - a non-blocking receive on tc.closed and then a bare
// tc.wg.Add(1), which is what this used to do - the two halves are a
// check-then-act with a gap onShutdown can land in: the caller finds the
// channel still open, onShutdown closes it and reaches tc.wg.Wait() with the
// counter already at zero so Wait returns at once, and only then does the
// caller's Add(1) run. sync.WaitGroup names that case as misuse in so many
// words ("calls with a positive delta that occur when the counter is zero must
// happen before a Wait"), and it costs one of two things: a goroutine still
// reading tc.ctx after main has gone on to tear the Wails context down, which
// is the exact hazard this spawn exists to prevent - or an outright "sync:
// WaitGroup misuse: Add called concurrently with Wait" panic taking the
// process down. raiseIfNeeded is the live way in: it runs on the hub's own
// writer goroutine, so a captcha arriving as the user quits is that race with
// nothing synthetic about it.
//
// The same shape, and the same fix, as App.track in internal/app and
// Host.track in internal/script.
func (tc *trayController) track() bool {
	tc.closeMu.Lock()
	defer tc.closeMu.Unlock()
	if tc.closing {
		return false
	}
	tc.wg.Add(1)
	return true
}

// isTrayAvailable reports the startup probe's result. Read-only after
// construction, but taken under mu anyway - see this file's package-level
// doc comment on why every field here goes through the lock uniformly
// rather than trusting a "this one never changes" exception that a later
// change could quietly invalidate.
func (tc *trayController) isTrayAvailable() bool {
	tc.mu.Lock()
	defer tc.mu.Unlock()
	return tc.trayAvailable
}

// effectiveStartHidden reports whether the window should start hidden this
// run. A saved preference is honoured only when the tray is confirmed
// available - otherwise the window opens with no way to bring it back at
// all, which is precisely the failure mode this package exists to avoid,
// not a fallback worth risking silently.
func (tc *trayController) effectiveStartHidden() bool {
	tc.mu.Lock()
	defer tc.mu.Unlock()
	return tc.cfg.StartHidden && tc.trayAvailable
}

// startupNotice reports a one-time message to show the user when their
// saved preferences wanted tray behaviour but this run's tray probe failed -
// "disable the dependent options with the reason shown", not a silent
// fallback. Nothing is shown when the user never asked for tray behaviour in
// the first place, so a tray-less machine that was never configured to want
// one is not nagged every launch.
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

// onWailsStartup is called once by Wails' own OnStartup hook. It is the
// first point ctx exists, so it is also the first point any of the
// ctx-consuming behaviour below can safely run.
func (tc *trayController) onWailsStartup(ctx context.Context) {
	tc.mu.Lock()
	tc.ctx = ctx
	tc.mu.Unlock()

	tc.spawn(tc.pollMinimize)

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

// onBeforeClose is the single gate for the window close button, on every
// platform (verified against the Windows, macOS and Linux frontend
// implementations at v2.13.0: WindowClose/Quit all route the native close
// signal through OnBeforeClose once HideWindowOnClose is left false, which
// main.go does deliberately - see its own comment). Everything the close
// button does lives here, driven by the live preference, rather than in a
// static HideWindowOnClose flag decided once at startup.
func (tc *trayController) onBeforeClose(ctx context.Context) bool {
	tc.mu.Lock()
	quitting := tc.quitting
	toTray := tc.cfg.OnClose == CloseTray && tc.trayAvailable
	tc.mu.Unlock()

	if quitting || !toTray {
		return false
	}
	wailsruntime.WindowHide(ctx)
	return true
}

// quit is the tray menu's Quit action. It must reach the real shutdown path
// even when the live preference is "close to tray", which is exactly what
// onBeforeClose would otherwise apply to a plain runtime.Quit call
// (verified: Frontend.Quit on every platform checks OnBeforeClose first) -
// so it sets quitting before calling it, the standard bypass-flag pattern
// for telling "the tray's own Quit item" apart from "the window's close
// button" when both ultimately funnel through the same hook.
func (tc *trayController) quit() {
	tc.mu.Lock()
	tc.quitting = true
	ctx := tc.ctx
	tc.mu.Unlock()

	if ctx == nil {
		// Wails never finished starting; there is no lifecycle left to call
		// into gracefully, and nothing has started downloading yet either.
		os.Exit(0)
		return
	}
	wailsruntime.Quit(ctx)
}

// onShutdown runs from main's OnShutdown, before a.Close(). It stops the
// poll loop and waits for every goroutine spawn tracked, so nothing this
// package started can still be reading tc.ctx after the Wails context it
// holds becomes invalid.
func (tc *trayController) onShutdown() {
	// Flipped first, under the same lock track takes, so that from here on no
	// spawn can still slip a wg.Add(1) past the Wait below - see track's own
	// comment for what that used to cost. Closing the channel stays after it:
	// the flag is what refuses new work, the channel is what stops the loops
	// already running.
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

// pollMinimize watches for the window entering the minimised state and, when
// the live preference says so, sends it to the tray instead - see this
// file's package-level doc comment on minimizePollInterval for why polling,
// not an event, is the honest way to build this today.
func (tc *trayController) pollMinimize() {
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
			wailsruntime.WindowHide(ctx)
		}
		wasMinimised = isMin
	}
}

// raiseIfNeeded brings the window forward for a newly-arrived captcha
// challenge, per the configured level. Called from the hub's own writer
// goroutine (see deskHubConn.Write), never from a goroutine this package
// spawned itself.
func (tc *trayController) raiseIfNeeded() {
	tc.mu.Lock()
	ctx := tc.ctx
	level := tc.cfg.RaiseOnAttention
	tc.mu.Unlock()

	if ctx == nil || level == RaiseOff {
		return
	}
	wailsruntime.WindowShow(ctx)
	wailsruntime.WindowUnminimise(ctx)
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

// mutate applies f to the live config under lock, persists the result, and
// returns the new value for the caller to reflect into the menu's checkmarks
// - the file write happens outside the lock so a slow disk never blocks the
// hub listener or the minimize poll.
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

// handleHubMessage is the pure, directly-testable half of the hub
// subscription: given one raw broadcast frame, decide whether it represents
// a captcha challenge appearing for the first time. Broadcast type strings
// and the {type,data} envelope are internal/hub's own wire format
// (hub.go's Broadcast); only the "id" field is read from the payload, so
// this does not need to import internal/captcha's full Challenge type to
// stay correct.
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

// noteCaptcha records a challenge id as seen and reports whether this call
// is the one that first saw it - separated from handleHubMessage so a test
// can drive the bookkeeping directly without a live Wails context.
func (tc *trayController) noteCaptcha(id string) (isNew bool) {
	tc.mu.Lock()
	defer tc.mu.Unlock()
	_, known := tc.seenCaptcha[id]
	tc.seenCaptcha[id] = struct{}{}
	return !known
}

// forgetCaptcha drops a resolved challenge so seenCaptcha stays bounded by
// the number of concurrently open challenges rather than growing for the
// life of the process.
func (tc *trayController) forgetCaptcha(id string) {
	tc.mu.Lock()
	defer tc.mu.Unlock()
	delete(tc.seenCaptcha, id)
}

// deskHubConn is a hub.Conn that never leaves the process: it subscribes the
// desktop tray to the exact broadcast stream every connected browser tab
// receives (internal/hub.Hub.Add takes any Conn, and the test suite already
// registers a fake one the same way - see app_activity_test.go), so a new
// captcha challenge can raise the window without any change to the shared
// frontend, which the browser build also serves unmodified and which has no
// concept of a native window to raise.
type deskHubConn struct {
	tc *trayController
}

func (c *deskHubConn) Write(_ context.Context, _ websocket.MessageType, p []byte) error {
	c.tc.handleHubMessage(p)
	return nil
}

func (c *deskHubConn) CloseNow() error { return nil }

// trayIconForPlatform picks the embedded asset systray's own decoder expects
// per platform - see assets.go and tray.png/.ico's own generation notes.
func trayIconForPlatform() []byte {
	if runtime.GOOS == "windows" {
		return trayIconICO
	}
	return trayIconPNG
}

// runTray is main's single entry point into this file: build the menu and
// block in systray's own event loop until Quit(). Only called when the
// startup probe found a tray host - see main.go.
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

func (tc *trayController) onExit() {
	// Nothing left to do here: onShutdown (driven by Wails' own OnShutdown)
	// already owns real teardown. This only satisfies systray.Run's
	// Run(onReady, onExit) signature.
}

func (tc *trayController) showWindow() {
	tc.mu.Lock()
	ctx := tc.ctx
	tc.mu.Unlock()
	if ctx == nil {
		return
	}
	wailsruntime.WindowShow(ctx)
	wailsruntime.WindowUnminimise(ctx)
}

func (tc *trayController) hideWindow() {
	tc.mu.Lock()
	ctx := tc.ctx
	tc.mu.Unlock()
	if ctx == nil {
		return
	}
	wailsruntime.WindowHide(ctx)
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
