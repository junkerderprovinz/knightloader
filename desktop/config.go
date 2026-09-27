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
	// Words are the tray menu's labels in the interface's language.
	Words TrayWords `json:"words"`
	// The size somebody gave the small window at the tray icon, zero for the
	// default.
	OverviewWidth  int `json:"overviewWidth,omitempty"`
	OverviewHeight int `json:"overviewHeight,omitempty"`
}

// TrayWords are the tray menu's labels, handed over by the page in the
// language it shows; see useTrayWords in web/src/lib/desktop.ts.
type TrayWords struct {
	Show           string `json:"show"`
	Hide           string `json:"hide"`
	StartHidden    string `json:"startHidden"`
	CloseToTray    string `json:"closeToTray"`
	MinimiseToTray string `json:"minimiseToTray"`
	Captcha        string `json:"captcha"`
	RaiseOff       string `json:"raiseOff"`
	RaiseFront     string `json:"raiseFront"`
	RaiseFocus     string `json:"raiseFocus"`
	StopQueue      string `json:"stopQueue"`
	StartQueue     string `json:"startQueue"`
	Quit           string `json:"quit"`
}

// withDefaults fills every label the page has not given, as on a first start,
// with its English words.
func (w TrayWords) withDefaults() TrayWords {
	or := func(s *string, english string) {
		if *s == "" {
			*s = english
		}
	}
	or(&w.Show, "Show KnightLoader")
	or(&w.Hide, "Hide window")
	or(&w.StartHidden, "Start hidden")
	or(&w.CloseToTray, "Close button sends to tray")
	or(&w.MinimiseToTray, "Minimise button sends to tray")
	or(&w.Captcha, "When a captcha needs you")
	or(&w.RaiseOff, "Do nothing")
	or(&w.RaiseFront, "Bring window to front")
	or(&w.RaiseFocus, "Bring to front and focus")
	or(&w.StopQueue, "Stop queue")
	or(&w.StartQueue, "Start queue")
	or(&w.Quit, "Quit KnightLoader")
	return w
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
