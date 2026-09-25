//go:build windows

package main

import (
	"fmt"
	"runtime"
	"sync"
	"syscall"
)

var procSetThreadExecutionState = syscall.NewLazyDLL("kernel32.dll").NewProc("SetThreadExecutionState")

// ES_CONTINUOUS and ES_SYSTEM_REQUIRED from winbase.h. ES_DISPLAY_REQUIRED is
// left out: the screen may turn off, only the machine has to stay up.
const (
	esContinuous     = 0x80000000
	esSystemRequired = 0x00000001
)

// preventSleep keeps Windows from sleeping until the returned release is
// called.
//
// SetThreadExecutionState belongs to the thread that called it, and Go moves a
// goroutine between threads as it likes, so the request is made and cleared on
// a goroutine locked to its thread for as long as the hold lasts. That
// goroutine never unlocks, so its thread exits with it and takes along any
// request the last call failed to clear.
func preventSleep() (func() error, error) {
	started := make(chan error, 1)
	stop := make(chan struct{})
	stopped := make(chan error, 1)
	go func() {
		runtime.LockOSThread()
		if err := setExecutionState(esContinuous | esSystemRequired); err != nil {
			started <- err
			return
		}
		started <- nil
		<-stop
		stopped <- setExecutionState(esContinuous)
	}()
	if err := <-started; err != nil {
		return nil, err
	}
	var once sync.Once
	var err error
	return func() error {
		once.Do(func() {
			close(stop)
			err = <-stopped
		})
		return err
	}, nil
}

// setExecutionState calls SetThreadExecutionState. It returns the previous
// state, which may be zero, so only a zero with an error code set is a
// refusal.
func setExecutionState(flags uintptr) error {
	r, _, errno := procSetThreadExecutionState.Call(flags)
	if r == 0 && errno != syscall.Errno(0) {
		return fmt.Errorf("Windows refused to keep the computer awake: %w", errno)
	}
	return nil
}
