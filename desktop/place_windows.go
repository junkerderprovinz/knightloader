//go:build windows

package main

import (
	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/w32"
)

// clearTaskbar moves the tray window off the taskbar. An auto-hiding taskbar
// leaves the work area the whole screen, so Wails places the window over the
// icon, and the click meant to close it lands on the window instead.
func clearTaskbar(w *application.WebviewWindow, gap int) {
	bar := w32.GetTaskbarPosition()
	if bar == nil {
		return
	}
	tb := application.PhysicalToDipRect(application.Rect{
		X: int(bar.Rc.Left), Y: int(bar.Rc.Top),
		Width: int(bar.Rc.Right - bar.Rc.Left), Height: int(bar.Rc.Bottom - bar.Rc.Top),
	})
	b := w.Bounds()
	switch bar.UEdge {
	case w32.ABE_BOTTOM:
		b.Y = min(b.Y, tb.Y-gap-b.Height)
	case w32.ABE_TOP:
		b.Y = max(b.Y, tb.Y+tb.Height+gap)
	case w32.ABE_LEFT:
		b.X = max(b.X, tb.X+tb.Width+gap)
	case w32.ABE_RIGHT:
		b.X = min(b.X, tb.X-gap-b.Width)
	}
	w.SetBounds(b)
}
