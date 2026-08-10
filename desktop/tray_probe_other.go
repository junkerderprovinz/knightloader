//go:build !linux && !freebsd && !openbsd && !netbsd

package main

// probeTray always reports the tray as available on Windows and macOS: both
// provide a notification area / menu bar item as an OS-level guarantee
// whenever a desktop session exists at all, unlike Linux where the tray host
// itself is optional (see tray_probe_linux.go). If there is no desktop
// session at all, Wails could not have opened a window either, so there is
// nothing this probe could usefully add.
func probeTray() (ok bool, reason string) {
	return true, ""
}
