//go:build !windows && !linux && !darwin

package cnl

import (
	"context"
	"errors"
	"syscall"
)

func addrInUse(err error) bool {
	return errors.Is(err, syscall.EADDRINUSE)
}

// portOwner names no process on a system KnightLoader cannot ask.
func portOwner(context.Context, int) (Process, bool) {
	return Process{}, false
}
