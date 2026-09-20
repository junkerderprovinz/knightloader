package main

// Putting the machine to sleep is the desktop half of the end-of-queue actions
// (internal/idleaction.ActionSuspend). The container cannot do it: its PID 1
// has no reach onto the host's power state, so there RequestSuspend stays nil
// and the action is never offered. suspend has one implementation per OS, and
// power_other.go keeps other platforms compiling.

import "fmt"

// requestSuspend is what main.go assigns to app.App.RequestSuspend. It keeps
// the operating system's error underneath its own prefix, because on Linux
// "Interactive authentication required" is what tells the operator what to fix.
func requestSuspend() error {
	if err := suspend(); err != nil {
		return fmt.Errorf("could not put this machine to sleep: %w", err)
	}
	return nil
}
