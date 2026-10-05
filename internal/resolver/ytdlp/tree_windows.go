package ytdlp

import (
	"os/exec"

	"golang.org/x/sys/windows"
)

// processTree puts yt-dlp into a job object, which the ffmpeg it starts joins,
// so kill ends both.
type processTree struct {
	cmd *exec.Cmd
	job windows.Handle
}

func newProcessTree(cmd *exec.Cmd) (*processTree, error) {
	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return nil, err
	}
	return &processTree{cmd: cmd, job: job}, nil
}

// started puts the process into the job. A child it started before this call
// escapes the job, and yt-dlp does not start ffmpeg that quickly.
func (p *processTree) started() {
	proc, err := windows.OpenProcess(windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE, false, uint32(p.cmd.Process.Pid))
	if err != nil {
		return
	}
	_ = windows.AssignProcessToJobObject(p.job, proc)
	windows.CloseHandle(proc)
}

// kill ends the job, and yt-dlp itself, which a stop that comes before
// started finds outside it.
func (p *processTree) kill() error {
	err := windows.TerminateJobObject(p.job, 1)
	_ = p.cmd.Process.Kill()
	return err
}

func (p *processTree) close() {
	windows.CloseHandle(p.job)
}
