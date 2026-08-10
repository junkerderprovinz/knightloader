package main

import _ "embed"

// The tray icon, two formats: SetIcon expects genuine ICO bytes on Windows
// (wt.setIcon loads the file through the Win32 icon APIs) and accepts
// PNG/JPG elsewhere (unix decodes via image.Decode; darwin via NSImage's own
// format sniffing) - see trayIconForPlatform in tray.go. Generated from a
// small standalone script using the app's own accent/contrast colors
// (web/src/index.css --accent / --accent-contrast); a placeholder until the
// project's own logo rollout replaces it.
//
//go:embed assets/tray.png
var trayIconPNG []byte

//go:embed assets/tray.ico
var trayIconICO []byte
