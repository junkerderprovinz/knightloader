// Package appimage tells whether the program runs from an AppImage, whose
// runtime starts it from a read-only mount of the file.
package appimage

import "os"

// Path returns the AppImage file the program runs from, or "" outside one. The
// runtime sets APPIMAGE for every program it starts.
func Path() string { return os.Getenv("APPIMAGE") }
