//go:build !windows

package main

import "github.com/wailsapp/wails/v3/pkg/application"

// quietExStyle is a Windows style; zero leaves Wails' default.
func quietExStyle() int { return 0 }

// showQuietly shows w. On macOS the popup is a non-activating panel, which
// leaves the focus alone by itself; on Linux the window manager decides.
func showQuietly(w *application.WebviewWindow) { w.Show() }

// pointerScreen has no portable way to read the pointer, so the popup goes to
// the main window's screen instead.
func pointerScreen() *application.Screen { return nil }
