//go:build !windows

package main

import "github.com/wailsapp/wails/v3/pkg/application"

// clearTaskbar corrects a Windows placement. Elsewhere Wails places the window
// from the menu bar or the pointer.
func clearTaskbar(*application.WebviewWindow, int) {}
