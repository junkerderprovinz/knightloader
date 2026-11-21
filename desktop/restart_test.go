package main

import (
	"os"
	"os/exec"
	"strconv"
	"testing"
	"time"
)

func TestARestartWaitsOnlyForAPreviousThatStillRuns(t *testing.T) {
	ended := exec.Command(os.Args[0], "-test.run=^$")
	if err := ended.Run(); err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	waitForPrevious([]string{"program"})
	waitForPrevious([]string{"program", afterFlag, "not-a-number"})
	waitForPrevious([]string{"program", afterFlag, strconv.Itoa(ended.Process.Pid)})
	if took := time.Since(start); took > 5*time.Second {
		t.Errorf("waited %v for programs that are not running", took)
	}
}
