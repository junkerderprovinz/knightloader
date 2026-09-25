//go:build darwin

package main

import (
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"sync"
)

// preventSleep keeps macOS from idle sleep until the returned release is
// called, by running caffeinate for as long as the hold lasts. -i holds off
// system sleep and leaves the display alone; -w makes caffeinate end with this
// process, so a crash cannot leave a Mac that never sleeps again.
//
// caffeinate rather than an IOKit assertion through cgo: it takes the same
// assertion, ships with every macOS, and keeps this file plain Go, so the
// desktop module still vets for darwin from any machine.
func preventSleep() (func() error, error) {
	cmd := exec.Command("caffeinate", "-i", "-w", strconv.Itoa(os.Getpid()))
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("could not start caffeinate: %w", err)
	}
	exited := make(chan struct{})
	go func() {
		_ = cmd.Wait()
		close(exited)
	}()
	var once sync.Once
	return func() error {
		once.Do(func() {
			// Killing it is the release; an error here only means it had
			// already gone.
			_ = cmd.Process.Kill()
			<-exited
		})
		return nil
	}, nil
}
