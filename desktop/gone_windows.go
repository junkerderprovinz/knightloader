package main

import (
	"time"

	"golang.org/x/sys/windows"
)

// waitGone waits up to within for the process pid to end.
func waitGone(pid int, within time.Duration) {
	h, err := windows.OpenProcess(windows.SYNCHRONIZE, false, uint32(pid))
	if err != nil {
		return
	}
	defer windows.CloseHandle(h)
	_, _ = windows.WaitForSingleObject(h, uint32(within.Milliseconds()))
}
