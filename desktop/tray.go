package main

import (
	"context"
	"encoding/json"
	"log"
	"runtime"
	"sync"
	"sync/atomic"
	"time"

	"github.com/coder/websocket"
	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"

	"github.com/junkerderprovinz/knightloader/internal/app"
	"github.com/junkerderprovinz/knightloader/internal/hub"
)

// raisePulse is how long always-on-top is held for the "front and focused"
// level. Windows refuses a focus request from a program in the background, and
// a short always-on-top pulse makes the window manager raise the window
// without pinning it.
const raisePulse = 300 * time.Millisecond

// Tray owns the desktop preferences and the tray icon, and decides what the
// close button, the minimise button and a new captcha do to the window. Menu
// clicks, window events, the hub writer and the page all reach it, so every
// field goes through mu.
//
// The pages call SetWords and ShowMain as main.Tray.SetWords and
// main.Tray.ShowMain.
type Tray struct {
	mu  sync.Mutex
	cfg Config

	kl      *app.App
	cfgPath string
	hub     *hub.Hub
	hubConn *deskHubConn

	// Nil until attach, and in tests.
	wails *application.App
	win   *application.WebviewWindow
	items *trayItems

	// overview is the small window at the tray icon, nil without a tray.
	overview *application.WebviewWindow
	// When a lost focus last hid it, in Unix nanoseconds. It is read on the
	// main thread, which must not wait on mu.
	overviewHid atomic.Int64

	trayAvailable bool
	unavailReason string

	// shown is false while the window is hidden to the tray, and minimised
	// while it sits in the taskbar or the Dock. The window counts as watching
	// captchas only while it is shown and not minimised.
	shown     bool
	minimised bool

	// halted mirrors the queue's master switch for the menu's Stop queue and
	// Start queue entry.
	halted bool

	seenCaptcha map[string]struct{}
	stopped     bool
}

// trayItems are the menu entries whose words or ticks change while it runs.
type trayItems struct {
	show, hide                                *application.MenuItem
	startHidden, closeToTray, minimiseToTray  *application.MenuItem
	captcha, raiseOff, raiseFront, raiseFocus *application.MenuItem
	queue, quit                               *application.MenuItem
}

func newTray(a *app.App, cfgPath string) *Tray {
	ok, reason := probeTray()
	t := &Tray{
		cfg:           loadConfig(cfgPath),
		kl:            a,
		cfgPath:       cfgPath,
		trayAvailable: ok,
		unavailReason: reason,
		halted:        a.Queue().Halted,
		seenCaptcha:   map[string]struct{}{},
	}
	t.shown = !t.effectiveStartHidden()
	t.joinHub(a.Hub)
	return t
}

// joinHub registers the window with the hub. It asks for the captcha events
// by name, so the window counts as watching captchas while it reports itself
// on screen (see reportVisible); the page inside it does not report, since
// the webview's own visibility is unreliable. The queue events keep the menu's
// Stop queue entry in step with the interface.
func (t *Tray) joinHub(h *hub.Hub) {
	t.hub = h
	t.hubConn = &deskHubConn{t: t}
	h.Add(t.hubConn)
	h.Subscribe(t.hubConn, []string{"captcha", "captchaResolved", "queue"})
}

// reportVisible tells the hub whether the window is on screen, which decides
// whether the paid captcha solvers wait for an answer at the prompt.
func (t *Tray) reportVisible() {
	t.mu.Lock()
	visible := t.shown && !t.minimised
	t.mu.Unlock()
	if t.hub != nil {
		t.hub.SetVisible(t.hubConn, visible)
	}
}

// isTrayAvailable reports the startup probe's result.
func (t *Tray) isTrayAvailable() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.trayAvailable
}

// effectiveStartHidden reports whether the window starts hidden. Without a
// tray there would be no way to bring it back, so the preference applies only
// when the tray is available.
func (t *Tray) effectiveStartHidden() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.cfg.StartHidden && t.trayAvailable
}

// startupNotice returns the message shown when the saved preferences want the
// tray but this run found none. A machine that never asked for the tray gets
// no message.
func (t *Tray) startupNotice() (msg string, show bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.trayAvailable {
		return "", false
	}
	wantsTray := t.cfg.StartHidden || t.cfg.OnClose == CloseTray || t.cfg.OnMinimize == MinimizeTray
	if !wantsTray {
		return "", false
	}
	return "System tray unavailable on this desktop (" + t.unavailReason + ")." +
		" \"Start hidden\", \"close to tray\" and \"minimize to tray\" are disabled for this run;" +
		" KnightLoader will use its normal window and taskbar behaviour instead.", true
}

// attach wires the window and, where there is one, the tray icon. It runs
// before wails.Run, which starts both.
func (t *Tray) attach(wails *application.App, win *application.WebviewWindow) {
	t.mu.Lock()
	t.wails = wails
	t.win = win
	t.mu.Unlock()

	win.RegisterHook(events.Common.WindowClosing, t.closing)
	win.OnWindowEvent(events.Common.WindowMinimise, func(*application.WindowEvent) { t.minimisedTo(true) })
	win.OnWindowEvent(events.Common.WindowUnMinimise, func(*application.WindowEvent) { t.minimisedTo(false) })
	win.OnWindowEvent(events.Common.WindowRestore, func(*application.WindowEvent) { t.minimisedTo(false) })

	if t.isTrayAvailable() {
		t.overview = t.newOverview(wails)
		t.startTray(wails)
	}
	wails.Event.OnApplicationEvent(events.Common.ApplicationStarted, func(*application.ApplicationEvent) {
		t.reportVisible()
		if msg, show := t.startupNotice(); show {
			wails.Dialog.Warning().SetTitle("System tray unavailable").SetMessage(msg).Show()
		}
	})
}

// ending reports whether the program is on its way out, when the window
// closes for real.
func (t *Tray) ending() bool {
	return t.wails != nil && t.wails.Context().Err() != nil
}

// closeHides decides from the live preference whether the close button hides
// the window rather than ending the program.
func (t *Tray) closeHides() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.cfg.OnClose == CloseTray && t.trayAvailable
}

// closing is the window's close hook. Quitting from the tray menu closes the
// window as well, and gets through because the program is ending by then.
func (t *Tray) closing(e *application.WindowEvent) {
	if t.ending() {
		return
	}
	e.Cancel()
	if t.closeHides() {
		t.hide()
		return
	}
	t.wails.Quit()
}

// minimisedTo records a minimise or restore, and hides a newly minimised
// window to the tray when the preference asks for it.
func (t *Tray) minimisedTo(minimised bool) {
	t.mu.Lock()
	t.minimised = minimised
	toTray := minimised && t.cfg.OnMinimize == MinimizeTray && t.trayAvailable
	t.mu.Unlock()
	if toTray {
		t.hide()
		return
	}
	t.reportVisible()
}

// hide puts the window away and show brings it back, recording which for
// reportVisible.
func (t *Tray) hide() {
	t.mu.Lock()
	t.shown = false
	win := t.win
	t.mu.Unlock()
	if win != nil {
		win.Hide()
	}
	t.reportVisible()
}

func (t *Tray) show() {
	t.mu.Lock()
	t.shown = true
	t.minimised = false
	win := t.win
	t.mu.Unlock()
	if win != nil {
		win.Show()
		win.UnMinimise()
	}
	t.reportVisible()
}

// ShowMain brings the main window forward and hands it the focus. The menu's
// Show entry, a double click on the icon and the tray window's button all
// call it.
func (t *Tray) ShowMain() {
	if t.overview != nil {
		t.overview.Hide()
	}
	t.show()
	if win := t.window(); win != nil {
		win.Focus()
	}
}

func (t *Tray) window() *application.WebviewWindow {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.win
}

// raiseIfNeeded brings the window forward for a new captcha at the configured
// level. It runs on the hub's writer goroutine.
func (t *Tray) raiseIfNeeded() {
	t.mu.Lock()
	win := t.win
	level := t.cfg.RaiseOnAttention
	stopped := t.stopped
	t.mu.Unlock()

	if win == nil || stopped || level == RaiseOff {
		return
	}
	t.show()
	if level != RaiseFocus {
		return
	}
	win.SetAlwaysOnTop(true)
	win.Focus()
	time.AfterFunc(raisePulse, func() { win.SetAlwaysOnTop(false) })
}

// emit sends a Wails event to the pages in every window, and drops it before
// the windows exist or once the program is shutting down.
func (t *Tray) emit(name string, data ...any) {
	if wails := t.running(); wails != nil {
		wails.Event.Emit(name, data...)
	}
}

// emitTo sends a Wails event to the page in the named window alone.
func (t *Tray) emitTo(window, name string, data ...any) {
	wails := t.running()
	if wails == nil {
		return
	}
	w, _ := wails.Window.GetByName(window)
	target, ok := w.(*application.WebviewWindow)
	if !ok {
		return
	}
	event := &application.CustomEvent{Name: name}
	if len(data) == 1 {
		event.Data = data[0]
	} else if len(data) > 1 {
		event.Data = data
	}
	target.DispatchWailsEvent(event)
}

// running returns the Wails app while events may go to its windows.
func (t *Tray) running() *application.App {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.stopped {
		return nil
	}
	return t.wails
}

// onShutdown runs from main's OnShutdown before a.Close. From here on no
// captcha raises the window and no event goes to it.
func (t *Tray) onShutdown() {
	t.mu.Lock()
	t.stopped = true
	t.mu.Unlock()
	if t.hub != nil && t.hubConn != nil {
		t.hub.Remove(t.hubConn)
	}
}

// mutate applies f to the config, saves it and returns the new value for the
// menu's ticks. The write happens outside the lock so a slow disk does not
// block the hub listener or a window event.
func (t *Tray) mutate(f func(*Config)) Config {
	t.mu.Lock()
	f(&t.cfg)
	cfg := t.cfg
	t.mu.Unlock()

	if err := cfg.save(t.cfgPath); err != nil {
		log.Printf("desktop: saving preferences: %v", err)
	}
	return cfg
}

// SetWords gives the menu the words of the language the interface shows. They
// are kept in desktop.json, so the next start shows them before the page has
// loaded.
func (t *Tray) SetWords(words TrayWords) {
	t.mutate(func(c *Config) { c.Words = words })
	t.relabel()
}

// relabel puts the current words on the menu.
func (t *Tray) relabel() {
	t.mu.Lock()
	items := t.items
	words := t.cfg.Words.withDefaults()
	halted := t.halted
	t.mu.Unlock()
	if items == nil {
		return
	}
	items.show.SetLabel(words.Show)
	items.hide.SetLabel(words.Hide)
	items.startHidden.SetLabel(words.StartHidden)
	items.closeToTray.SetLabel(words.CloseToTray)
	items.minimiseToTray.SetLabel(words.MinimiseToTray)
	items.captcha.SetLabel(words.Captcha)
	items.raiseOff.SetLabel(words.RaiseOff)
	items.raiseFront.SetLabel(words.RaiseFront)
	items.raiseFocus.SetLabel(words.RaiseFocus)
	items.queue.SetLabel(queueLabel(words, halted))
	items.quit.SetLabel(words.Quit)
}

// queueLabel names what the queue entry does from here: start a halted queue,
// or stop a running one.
func queueLabel(words TrayWords, halted bool) string {
	if halted {
		return words.StartQueue
	}
	return words.StopQueue
}

// handleHubMessage reads one hub broadcast frame. A captcha seen for the first
// time raises the window, and a change of the queue's master switch relabels
// the menu.
func (t *Tray) handleHubMessage(raw []byte) {
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
		if t.noteCaptcha(withID.ID) {
			t.raiseIfNeeded()
		}
	case "captchaResolved":
		if json.Unmarshal(env.Data, &withID) == nil && withID.ID != "" {
			t.forgetCaptcha(withID.ID)
		}
	case "queue":
		var q struct {
			Halted bool `json:"halted"`
		}
		if json.Unmarshal(env.Data, &q) != nil {
			return
		}
		t.mu.Lock()
		changed := t.halted != q.Halted
		t.halted = q.Halted
		t.mu.Unlock()
		if changed {
			t.relabel()
		}
	}
}

// noteCaptcha records a challenge id and reports whether it is new.
func (t *Tray) noteCaptcha(id string) (isNew bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	_, known := t.seenCaptcha[id]
	t.seenCaptcha[id] = struct{}{}
	return !known
}

// forgetCaptcha drops a resolved challenge so seenCaptcha holds only open ones.
func (t *Tray) forgetCaptcha(id string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	delete(t.seenCaptcha, id)
}

// deskHubConn is an in-process hub.Conn that feeds the tray the broadcasts a
// browser tab gets, so a captcha can raise the native window without changes
// to the shared frontend.
type deskHubConn struct {
	t *Tray
}

func (c *deskHubConn) Write(_ context.Context, _ websocket.MessageType, p []byte) error {
	c.t.handleHubMessage(p)
	return nil
}

func (c *deskHubConn) CloseNow() error { return nil }

// trayIconForPlatform returns ICO bytes on Windows, whose several sizes let the
// notification area pick a sharp one, and PNG elsewhere.
func trayIconForPlatform() []byte {
	if runtime.GOOS == "windows" {
		return trayIconICO
	}
	return trayIconPNG
}

// startTray puts the icon in the notification area or the menu bar. The tray
// starts with wails.Run.
func (t *Tray) startTray(wails *application.App) {
	menu, items := t.buildMenu()
	t.mu.Lock()
	t.items = items
	t.mu.Unlock()

	tray := wails.SystemTray.New()
	tray.SetIcon(trayIconForPlatform())
	tray.SetTooltip("KnightLoader")
	tray.SetMenu(menu)
	// A click opens the small window at the icon, a double click the main
	// window. A right click keeps Wails' own handler, which opens the menu.
	tray.OnClick(func() { t.toggleOverview(tray) })
	// By the time a double click arrives, its first click has opened the small
	// window and its second has closed it again.
	tray.OnDoubleClick(t.ShowMain)
}

// buildMenu lays out the menu with the saved words and ticks.
func (t *Tray) buildMenu() (*application.Menu, *trayItems) {
	t.mu.Lock()
	cfg := t.cfg
	halted := t.halted
	t.mu.Unlock()
	words := cfg.Words.withDefaults()

	menu := application.NewMenu()
	items := &trayItems{}
	items.show = menu.Add(words.Show).OnClick(func(*application.Context) { t.ShowMain() })
	items.hide = menu.Add(words.Hide).OnClick(func(*application.Context) { t.hide() })
	menu.AddSeparator()

	items.startHidden = menu.AddCheckbox(words.StartHidden, cfg.StartHidden)
	items.startHidden.OnClick(func(*application.Context) {
		cfg := t.mutate(func(c *Config) { c.StartHidden = !c.StartHidden })
		items.startHidden.SetChecked(cfg.StartHidden)
	})
	items.closeToTray = menu.AddCheckbox(words.CloseToTray, cfg.OnClose == CloseTray)
	items.closeToTray.OnClick(func(*application.Context) {
		cfg := t.mutate(func(c *Config) {
			if c.OnClose == CloseTray {
				c.OnClose = CloseExit
			} else {
				c.OnClose = CloseTray
			}
		})
		items.closeToTray.SetChecked(cfg.OnClose == CloseTray)
	})
	items.minimiseToTray = menu.AddCheckbox(words.MinimiseToTray, cfg.OnMinimize == MinimizeTray)
	items.minimiseToTray.OnClick(func(*application.Context) {
		cfg := t.mutate(func(c *Config) {
			if c.OnMinimize == MinimizeTray {
				c.OnMinimize = MinimizeTaskbar
			} else {
				c.OnMinimize = MinimizeTray
			}
		})
		items.minimiseToTray.SetChecked(cfg.OnMinimize == MinimizeTray)
	})
	menu.AddSeparator()

	items.captcha = application.NewSubMenuItem(words.Captcha)
	menu.Append(application.NewMenuFromItems(items.captcha))
	raise := items.captcha.GetSubmenu()
	level := func(label, value string) *application.MenuItem {
		return raise.AddRadio(label, cfg.RaiseOnAttention == value).OnClick(func(*application.Context) {
			t.mutate(func(c *Config) { c.RaiseOnAttention = value })
		})
	}
	items.raiseOff = level(words.RaiseOff, RaiseOff)
	items.raiseFront = level(words.RaiseFront, RaiseFront)
	items.raiseFocus = level(words.RaiseFocus, RaiseFocus)
	menu.AddSeparator()

	items.queue = menu.Add(queueLabel(words, halted)).OnClick(func(*application.Context) {
		t.kl.SetHalted(!t.kl.Queue().Halted)
	})
	menu.AddSeparator()
	items.quit = menu.Add(words.Quit).OnClick(func(*application.Context) { t.wails.Quit() })
	return menu, items
}
