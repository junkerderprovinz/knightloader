package main

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// Close-button behaviour, after JD's TrayConfig.OnCloseAction without "ask
// every time", which the tray menu toggle covers.
const (
	CloseExit = "exit"
	CloseTray = "tray"
)

// Minimize-button behaviour (JD's OnMinimizeAction).
const (
	MinimizeTaskbar = "taskbar"
	MinimizeTray    = "tray"
)

// How hard the window asks for attention when a captcha or similar dialog
// appears while it is hidden or behind others. JD's "Show new Dialogs" has
// four states; Wails can tell apart only these.
const (
	RaiseOff   = "off"
	RaiseFront = "front"
	// RaiseFocus additionally pulses always-on-top to force the window above
	// whatever has focus; see raiseIfNeeded in tray.go for why.
	RaiseFocus = "front-focused"
)

// Config holds the window and tray behaviour of this one installation. It
// stays out of settings.Settings, which every connected browser reads and
// replaces whole, so a phone on the LAN cannot decide whether this window
// starts hidden.
type Config struct {
	StartHidden      bool   `json:"startHidden"`
	OnClose          string `json:"onClose"`
	OnMinimize       string `json:"onMinimize"`
	RaiseOnAttention string `json:"raiseOnAttention"`
}

func defaultConfig() Config {
	return Config{
		StartHidden:      false,
		OnClose:          CloseExit,
		OnMinimize:       MinimizeTaskbar,
		RaiseOnAttention: RaiseOff,
	}
}

// sanitize resets any value this build does not know, from a hand edit or a
// newer version, to its default.
func (c Config) sanitize() Config {
	switch c.OnClose {
	case CloseExit, CloseTray:
	default:
		c.OnClose = CloseExit
	}
	switch c.OnMinimize {
	case MinimizeTaskbar, MinimizeTray:
	default:
		c.OnMinimize = MinimizeTaskbar
	}
	switch c.RaiseOnAttention {
	case RaiseOff, RaiseFront, RaiseFocus:
	default:
		c.RaiseOnAttention = RaiseOff
	}
	return c
}

// loadConfig reads the preference file and falls back to the defaults when it
// is missing or not valid JSON, so a broken hand edit cannot stop the app.
func loadConfig(path string) Config {
	b, err := os.ReadFile(path)
	if err != nil {
		return defaultConfig()
	}
	var c Config
	if err := json.Unmarshal(b, &c); err != nil {
		return defaultConfig()
	}
	return c.sanitize()
}

// save writes through a temp file and a rename so a crash mid-write cannot
// leave a half-written file.
func (c Config) save(path string) error {
	b, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
