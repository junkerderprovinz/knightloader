//go:build !linux && !freebsd && !openbsd && !netbsd

package main

// probeTray reports the tray as available: Windows and macOS always provide
// one in a desktop session.
func probeTray() (ok bool, reason string) {
	return true, ""
}
