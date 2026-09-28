package nowindow

import (
	"os/exec"
	"syscall"

	"golang.org/x/sys/windows"
)

// Apply marks cmd to start without a console window. It keeps whatever else
// cmd.SysProcAttr already holds, so it goes last before Start.
func Apply(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.CreationFlags |= windows.CREATE_NO_WINDOW
}
