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

// portOwners names no process on a system KnightLoader cannot ask.
func portOwners(context.Context, int) []Process {
	return nil
}
