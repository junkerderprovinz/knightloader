package nox

import (
	"os/exec"
	"syscall"
)

// tieToParent has the kernel end qBittorrent when KnightLoader dies, so a
// crash leaves no qBittorrent holding the profile.
func tieToParent(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Pdeathsig: syscall.SIGTERM}
}

func afterStart(cmd *exec.Cmd) func() { return func() {} }

func askToQuit(cmd *exec.Cmd) error { return cmd.Process.Signal(syscall.SIGTERM) }
