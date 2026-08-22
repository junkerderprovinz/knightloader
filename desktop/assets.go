package main

import _ "embed"

// The tray icon, two formats: SetIcon expects genuine ICO bytes on Windows
// (wt.setIcon loads the file through the Win32 icon APIs) and accepts
// PNG/JPG elsewhere (unix decodes via image.Decode; darwin via NSImage's own
// format sniffing) - see trayIconForPlatform in tray.go. Generated from the
// real logo (.github/assets/logo.svg) by .github/assets/gen-tray.mjs.
//
//go:embed assets/tray.png
var trayIconPNG []byte

//go:embed assets/tray.ico
var trayIconICO []byte
