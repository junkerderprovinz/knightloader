//go:build !windows && !darwin && !linux

package main

import "errors"

// suspend keeps the module compiling on platforms without a sleep call. It
// fails rather than pretending to succeed, since the operator was promised a
// sleep by the countdown.
func suspend() error {
	return errors.New("this build has no way to sleep a machine on this platform")
}
