package nox

import (
	"errors"
	"os/exec"
	"unsafe"

	"golang.org/x/sys/windows"

	"github.com/junkerderprovinz/knightloader/internal/nowindow"
)

func tieToParent(cmd *exec.Cmd) { nowindow.Apply(cmd) }

// afterStart puts qBittorrent in a job object that ends it when the last
// handle to the job closes, which is when KnightLoader exits, however it
// exits. The returned func closes the handle.
func afterStart(cmd *exec.Cmd) func() {
	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return func() {}
	}
	info := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{}
	info.BasicLimitInformation.LimitFlags = windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE
	_, err = windows.SetInformationJobObject(job, windows.JobObjectExtendedLimitInformation, uintptr(unsafe.Pointer(&info)), uint32(unsafe.Sizeof(info)))
	if err == nil {
		var proc windows.Handle
		proc, err = windows.OpenProcess(windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE, false, uint32(cmd.Process.Pid))
		if err == nil {
			err = windows.AssignProcessToJobObject(job, proc)
			windows.CloseHandle(proc)
		}
	}
	if err != nil {
		windows.CloseHandle(job)
		return func() {}
	}
	return func() { windows.CloseHandle(job) }
}

// Windows delivers no signal to a console program started without a console,
// so the API's shutdown is the only polite way and this one fails.
func askToQuit(cmd *exec.Cmd) error { return errors.ErrUnsupported }
