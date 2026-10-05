//go:build unix

package ytdlp

import (
	"os/exec"
	"syscall"
)

// processTree runs yt-dlp as the leader of a process group of its own, which
// the ffmpeg it starts joins, so kill ends both.
type processTree struct {
	cmd *exec.Cmd
}

func newProcessTree(cmd *exec.Cmd) (*processTree, error) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	return &processTree{cmd: cmd}, nil
}

func (p *processTree) started() {}

func (p *processTree) kill() error {
	return syscall.Kill(-p.cmd.Process.Pid, syscall.SIGKILL)
}

func (p *processTree) close() {}
