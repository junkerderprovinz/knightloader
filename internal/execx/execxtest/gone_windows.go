package execxtest

import "golang.org/x/sys/windows"

// Gone reports whether pid has ended.
func Gone(pid int) bool {
	proc, err := windows.OpenProcess(windows.SYNCHRONIZE, false, uint32(pid))
	if err != nil {
		return true
	}
	defer windows.CloseHandle(proc)
	event, _ := windows.WaitForSingleObject(proc, 0)
	return event == windows.WAIT_OBJECT_0
}
