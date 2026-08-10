package main

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// Close-button behaviour. Named after JD's own TrayConfig.OnCloseAction, minus
// "ask every time": a tray menu toggle answers that more cleanly than a modal
// dialog on the way out, and it removes a whole class of "what if the ask
// dialog itself can't be shown" edge cases.
const (
	CloseExit = "exit"
	CloseTray = "tray"
)

// Minimize-button behaviour (JD's OnMinimizeAction).
const (
	MinimizeTaskbar = "taskbar"
	MinimizeTray    = "tray"
)

// How hard the window asks for attention when a new captcha challenge (or
// similar in-page modal) appears while the window is hidden or in the
// background - JD's "Show new Dialogs..." (per OS default / behind / in
// front / in front and focused), narrowed to the two states that are both
// meaningfully different and achievable through Wails' runtime API.
const (
	RaiseOff   = "off"
	RaiseFront = "front"
	// RaiseFocus additionally pulses always-on-top to force the window above
	// whatever has focus; see raiseIfNeeded in tray.go for why.
	RaiseFocus = "front-focused"
)

// Config is a DESKTOP-LOCAL preference file: window/tray behaviour for this
// one installation on this one machine. It deliberately never lives in
// settings.Settings, which is served whole to every browser that connects to
// this same server and replaced whole on every PUT - a phone on the LAN
// looking at this instance has no business deciding whether the desktop
// window on the machine running it starts hidden.
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

// sanitize coerces any unrecognised or leftover value back to its default
// rather than propagating it, the same rule rules.Disabled and the settings
// sanitize hooks already use: a config file edited by hand, or carried over
// from a future version with a value this build does not know, degrades to
// the safe default instead of tripping on an enum switch with no case for it.
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

// loadConfig reads the desktop-local preference file, falling back to
// defaults for a missing file (first run) or a corrupt one (a config a user
// hand-edited into invalid JSON must not stop the app from starting).
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

// save writes the config atomically (temp file + rename) so a crash or a
// second process mid-write never leaves a half-written, unparseable file
// behind - the same failure mode loadConfig's corrupt-JSON fallback exists
// to absorb, but there is no reason to invite it.
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
