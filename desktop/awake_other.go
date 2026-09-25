//go:build !windows && !darwin && !linux

package main

import "errors"

// preventSleep keeps the module compiling on platforms without a way to hold
// off sleep. It says so rather than pretending, and the guard logs it once per
// download.
func preventSleep() (func() error, error) {
	return nil, errors.New("this build has no way to keep the computer awake on this platform")
}
