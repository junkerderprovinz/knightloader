package main

import (
	"os"
	"os/exec"
	"strconv"
	"time"
)

// afterFlag hands a restarted program the id of the one it replaces.
const afterFlag = "--after"

// waitForPrevious waits for the program a restart replaces to end, so this one
// can take the single-instance lock, the data folder and the Click'n'Load
// port. A start without the flag returns at once.
func waitForPrevious(args []string) {
	if len(args) != 3 || args[1] != afterFlag {
		return
	}
	if pid, err := strconv.Atoi(args[2]); err == nil {
		waitGone(pid, 30*time.Second)
	}
}

// startAgain starts the program anew from the file an update replaced: the
// AppImage it was started from, or its own executable.
func startAgain() error {
	exe := os.Getenv("APPIMAGE")
	if exe == "" {
		var err error
		if exe, err = os.Executable(); err != nil {
			return err
		}
	}
	c := exec.Command(exe, afterFlag, strconv.Itoa(os.Getpid()))
	c.Env = os.Environ()
	return c.Start()
}
