//go:build darwin

package main

import (
	"context"
	"errors"
	"os/exec"
	"time"
)

// readAccent asks `defaults` rather than NSColor, which keeps this file free
// of cgo.
func readAccent() string {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "defaults", "read", "-g", "AppleAccentColor").Output()
	var exit *exec.ExitError
	switch {
	case err == nil:
		return appleAccent(string(out), true)
	case errors.As(err, &exit) && ctx.Err() == nil:
		// `defaults` fails on a key that is not there.
		return appleAccent("", false)
	default:
		return ""
	}
}
