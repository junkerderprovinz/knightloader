//go:build unix

package execx

import (
	"os/exec"
	"syscall"
)

// run runs cmd to the end as the leader of a process group of its own, so a
// time limit or a shutdown kills the group: a shell's background job or the
// second command on its line goes with it, and the next run cannot start
// while an orphan of the last one is still at work.
func run(cmd *exec.Cmd) error {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
	return cmd.Run()
}
