package main

import (
	"log"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
)

// overviewName is the tray window's name. Its page passes the same name to
// HubBridge, so its streams reach it alone; see web/src/lib/desktop.ts.
const overviewName = "tray"

// A click on the tray icon takes the focus from the small window, which hides
// it, before the click itself arrives. A click this soon after is the one that
// hid it, and means close rather than open again.
const refocusGrace = 400 * time.Millisecond

// trayGap is the room between the small window and the taskbar or the icon.
const trayGap = 8

// The small window's size on a first start, and the least it can be dragged
// to, below which the counts and the buttons no longer fit.
const (
	overviewWidth     = 360
	overviewHeight    = 480
	overviewMinWidth  = 300
	overviewMinHeight = 360
)

// sizeSettles is how long a resize has to rest before the size is saved, so a
// drag writes the file once rather than on every step.
const sizeSettles = 500 * time.Millisecond

// newOverview makes the small window at the tray icon. It stays hidden until
// the icon is clicked and is only hidden again, never closed, so it opens at
// once. It comes back at the size it was last dragged to.
func (t *Tray) newOverview(wails *application.App) *application.WebviewWindow {
	width, height := t.overviewSize()
	w := wails.Window.NewWithOptions(application.WebviewWindowOptions{
		Name:             overviewName,
		Title:            "KnightLoader",
		Width:            width,
		Height:           height,
		MinWidth:         overviewMinWidth,
		MinHeight:        overviewMinHeight,
		URL:              "/tray",
		Frameless:        true,
		AlwaysOnTop:      true,
		Hidden:           true,
		HideOnEscape:     true,
		BackgroundColour: application.NewRGB(22, 22, 22),
		Windows:          application.WindowsWindow{HiddenOnTaskbar: true},
	})
	// Alt+F4 on the small window would otherwise destroy it for good.
	w.RegisterHook(events.Common.WindowClosing, func(e *application.WindowEvent) {
		if t.ending() {
			return
		}
		e.Cancel()
		w.Hide()
	})
	// A hidden window stays the foreground one until something else takes
	// over, and losing that must not count as the click that closed it.
	w.OnWindowEvent(events.Common.WindowLostFocus, func(*application.WindowEvent) {
		if !w.IsVisible() {
			return
		}
		t.overviewHid.Store(time.Now().UnixNano())
		w.Hide()
	})
	var settle *time.Timer
	w.OnWindowEvent(events.Common.WindowDidResize, func(*application.WindowEvent) {
		if settle != nil {
			settle.Stop()
		}
		settle = time.AfterFunc(sizeSettles, func() { t.keepOverviewSize(w.Size()) })
	})
	return w
}

// overviewSize is the size the small window opens at: the saved one, or the
// default on a first start.
func (t *Tray) overviewSize() (int, int) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.cfg.OverviewWidth == 0 || t.cfg.OverviewHeight == 0 {
		return overviewWidth, overviewHeight
	}
	return max(t.cfg.OverviewWidth, overviewMinWidth), max(t.cfg.OverviewHeight, overviewMinHeight)
}

// keepOverviewSize saves the size the small window was dragged to.
func (t *Tray) keepOverviewSize(width, height int) {
	if width <= 0 || height <= 0 {
		return
	}
	t.mu.Lock()
	same := t.cfg.OverviewWidth == width && t.cfg.OverviewHeight == height
	t.mu.Unlock()
	if !same {
		t.mutate(func(c *Config) { c.OverviewWidth, c.OverviewHeight = width, height })
	}
}

// toggleOverview opens the small window at the icon, or closes it. It runs on
// the main thread.
func (t *Tray) toggleOverview(tray *application.SystemTray) {
	w := t.overview
	if w.IsVisible() {
		w.Hide()
		return
	}
	if time.Since(time.Unix(0, t.overviewHid.Load())) < refocusGrace {
		return
	}
	if err := tray.PositionWindow(w, trayGap); err != nil {
		log.Printf("tray: place the window at the icon: %v", err)
	}
	clearTaskbar(w, trayGap)
	w.Show().Focus()
}
