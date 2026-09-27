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

// newOverview makes the small window at the tray icon. It stays hidden until
// the icon is clicked and is only hidden again, never closed, so it opens at
// once.
func (t *Tray) newOverview(wails *application.App) *application.WebviewWindow {
	w := wails.Window.NewWithOptions(application.WebviewWindowOptions{
		Name:             overviewName,
		Title:            "KnightLoader",
		Width:            360,
		Height:           480,
		URL:              "/tray",
		Frameless:        true,
		AlwaysOnTop:      true,
		Hidden:           true,
		DisableResize:    true,
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
	return w
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
