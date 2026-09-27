package main

import _ "embed"

// The tray icon in two formats: ICO for Windows, whose several sizes let the
// notification area pick a sharp one, and PNG elsewhere.
// .github/assets/gen-tray.mjs generates both.
//
//go:embed assets/tray.png
var trayIconPNG []byte

//go:embed assets/tray.ico
var trayIconICO []byte

// The application icon, the one the executable and the installer carry. It
// becomes the window icon where the platform takes it from the program.
//
//go:embed build/appicon.png
var appIcon []byte
