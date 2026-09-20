package main

import _ "embed"

// The tray icon in two formats, since SetIcon needs real ICO bytes on Windows
// and takes PNG elsewhere. .github/assets/gen-tray.mjs generates both.
//
//go:embed assets/tray.png
var trayIconPNG []byte

//go:embed assets/tray.ico
var trayIconICO []byte
