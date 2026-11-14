//go:build unix && !linux

package nox

import (
	"os/exec"
	"syscall"
)

// macOS has no parent-death signal; a qBittorrent left by a crash is found on
// the next start and asked to quit (see Instance.evict).
func tieToParent(cmd *exec.Cmd) {}

func afterStart(cmd *exec.Cmd) func() { return func() {} }

func askToQuit(cmd *exec.Cmd) error { return cmd.Process.Signal(syscall.SIGTERM) }
