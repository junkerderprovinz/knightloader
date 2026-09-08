//go:build !windows && !darwin && !linux

package main

import "errors"

// suspend on a platform desktop.yml does not build, and which therefore has
// no verified way to sleep a machine.
//
// It exists so this module still compiles there rather than failing with an
// undefined symbol - the same arrangement tray_probe_other.go already makes
// beside tray_probe_linux.go. Returning an error rather than doing nothing
// and reporting success is the whole point: the operator watched a countdown
// that promised something, and "this build has no sleep call for this
// platform" is an answer they can act on, while a silent success is one they
// would go on trusting.
func suspend() error {
	return errors.New("this build has no way to sleep a machine on this platform")
}
