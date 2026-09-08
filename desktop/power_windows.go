//go:build windows

package main

import (
	"fmt"
	"syscall"
)

// powrprof holds SetSuspendState, the documented Win32 call for "sleep or
// hibernate this machine now".
//
// NewLazyDLL rather than a linked import so nothing is loaded until the one
// moment somebody actually configures a sleep action - a desktop build that
// never uses this must not carry the DLL handle around for the whole run.
var (
	powrprof            = syscall.NewLazyDLL("powrprof.dll")
	procSetSuspendState = powrprof.NewProc("SetSuspendState")
)

// suspend asks Windows to sleep.
//
// THE CALL IS MADE THROUGH THE DLL AND NOT THROUGH rundll32, and that is the
// whole point of this file. `rundll32.exe powrprof.dll,SetSuspendState 0,1,0`
// is the invocation every search result offers first, and it HIBERNATES
// rather than sleeps on any machine where hibernation is enabled, which is
// most of them. Somebody asking for sleep at the end of a download queue and
// getting hibernation instead gets a machine that takes a minute to come back
// and a hiberfil the size of their RAM, and nothing anywhere would say why.
// Same class of quirk as Explorer's "/select," argument already documented in
// files_windows.go.
//
// The three arguments are bHibernate, bForce and bWakeupEventsDisabled, all
// FALSE: sleep rather than hibernate, let the system ask its drivers rather
// than forcing them (a forced suspend is how an application ends up killing
// an unsaved document belonging to somebody else), and leave wake timers
// alone, because a machine that was told to sleep after its downloads should
// still wake for whatever the user scheduled.
func suspend() error {
	// syscall.Proc.Call always returns a non-nil error - a zero Errno reads
	// as "The operation completed successfully" - so it is only meaningful
	// when the call itself reported failure.
	r, _, err := procSetSuspendState.Call(0, 0, 0)
	if r == 0 {
		return fmt.Errorf("Windows refused to suspend: %w", err)
	}
	return nil
}
