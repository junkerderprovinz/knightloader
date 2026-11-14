package signinbrowser

import (
	"os/exec"
	"time"
)

// reaper returns the stop of a started browser: it waits a moment for one
// already closing, then kills it.
func reaper(cmd *exec.Cmd) func() {
	exited := make(chan struct{})
	go func() {
		_ = cmd.Wait()
		close(exited)
	}()
	return func() {
		select {
		case <-exited:
		case <-time.After(5 * time.Second):
			_ = cmd.Process.Kill()
			<-exited
		}
	}
}
