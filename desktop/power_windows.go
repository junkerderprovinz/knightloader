//go:build windows

package main

import (
	"fmt"
	"syscall"
)

// powrprof holds SetSuspendState. It is loaded lazily, so a build that never
// sleeps the machine never loads the DLL.
var (
	powrprof            = syscall.NewLazyDLL("powrprof.dll")
	procSetSuspendState = powrprof.NewProc("SetSuspendState")
)

// suspend asks Windows to sleep. It calls the DLL directly because the usual
// "rundll32 powrprof.dll,SetSuspendState" hibernates instead wherever
// hibernation is enabled.
//
// The arguments bHibernate, bForce and bWakeupEventsDisabled are all false:
// sleep, let drivers veto rather than force it, and keep wake timers the user
// scheduled.
func suspend() error {
	// Proc.Call always returns a non-nil error, so err only counts when r is 0.
	r, _, err := procSetSuspendState.Call(0, 0, 0)
	if r == 0 {
		return fmt.Errorf("Windows refused to suspend: %w", err)
	}
	return nil
}
