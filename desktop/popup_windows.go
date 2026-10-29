//go:build windows

package main

import (
	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/w32"
)

// quietExStyle is the popup's extended style: Wails' own for a topmost,
// transparent window kept off the taskbar, plus WS_EX_NOACTIVATE, so showing
// it leaves the keyboard focus where somebody is typing.
func quietExStyle() int {
	return w32.WS_EX_CONTROLPARENT | w32.WS_EX_NOREDIRECTIONBITMAP | w32.WS_EX_TOPMOST |
		w32.WS_EX_TOOLWINDOW | w32.WS_EX_NOACTIVATE
}

// showQuietly shows w without activating it. Wails shows a window with
// SW_SHOW, which activates it even with WS_EX_NOACTIVATE, and once the window
// is visible its Show only brings up the webview.
func showQuietly(w *application.WebviewWindow) {
	if h := w.NativeWindow(); h != nil {
		w32.ShowWindow(w32.HWND(h), w32.SW_SHOWNOACTIVATE)
	}
	w.Show()
}

// pointerScreen is the screen the mouse pointer is on.
func pointerScreen() *application.Screen {
	x, y, ok := w32.GetCursorPos()
	if !ok {
		return nil
	}
	return application.ScreenNearestPhysicalPoint(application.Point{X: x, Y: y})
}
