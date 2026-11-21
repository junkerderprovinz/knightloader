//go:build !windows

package main

import (
	"syscall"
	"time"
)

// waitGone waits up to within for the process pid to end.
func waitGone(pid int, within time.Duration) {
	deadline := time.Now().Add(within)
	for syscall.Kill(pid, 0) == nil && time.Now().Before(deadline) {
		time.Sleep(100 * time.Millisecond)
	}
}
