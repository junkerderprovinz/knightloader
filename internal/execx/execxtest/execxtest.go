// Package execxtest gives the tests of every runner built on execx a program
// that starts a program of its own, so a test can check what becomes of that
// child. The program is the test binary: the package's TestMain calls Main,
// which plays it when a test started the binary as one.
package execxtest

import (
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"
)

// Mode is what the program does.
type Mode string

const (
	// Spawn starts a child with output of its own, prints "child:PID" and
	// waits a minute, like a shell with a job in the background.
	Spawn Mode = "spawn"
	// Detach starts a child that writes to the same output, prints
	// "child:PID" and exits at once, like a script ending in `nohup job &`.
	Detach Mode = "detach"
	// ListKL prints "KL variables:" and every KL_ variable it was given, one
	// per line, and exits with status 1, so a runner that quotes the output
	// only when the program fails quotes it too.
	ListKL Mode = "list-kl"
)

const (
	modeEnv = "EXECXTEST_MODE"
	sleep   = "sleep"
)

// Main plays the program when the environment asks for it and returns at once
// otherwise. Call it first in TestMain.
func Main() {
	switch Mode(os.Getenv(modeEnv)) {
	case Spawn:
		fmt.Printf("child:%d\n", startChild(false))
		time.Sleep(time.Minute)
		os.Exit(0)
	case Detach:
		fmt.Printf("child:%d\n", startChild(true))
		os.Exit(0)
	case ListKL:
		fmt.Println("KL variables:")
		for _, kv := range os.Environ() {
			if strings.HasPrefix(strings.ToUpper(kv), "KL_") {
				fmt.Println(kv)
			}
		}
		os.Exit(1)
	case sleep:
		time.Sleep(time.Minute)
		os.Exit(0)
	}
}

func startChild(shareOutput bool) int {
	self, err := os.Executable()
	if err != nil {
		fmt.Println(err)
		os.Exit(3)
	}
	child := exec.Command(self)
	child.Env = append(os.Environ(), modeEnv+"="+sleep)
	if shareOutput {
		child.Stdout = os.Stdout
		child.Stderr = os.Stderr
	}
	if err := child.Start(); err != nil {
		fmt.Println(err)
		os.Exit(3)
	}
	return child.Process.Pid
}

// Program returns the program to start for mode, and sets the environment
// that selects it for the rest of the test, so a runner that passes on this
// process's environment starts it too.
func Program(t *testing.T, mode Mode) string {
	t.Helper()
	t.Setenv(modeEnv, string(mode))
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	return self
}

var childLine = regexp.MustCompile(`child:(\d+)`)

// Child reads the child's pid out of what the program printed and makes sure
// the child is gone when the test ends, whatever the test found.
func Child(t *testing.T, out string) int {
	t.Helper()
	m := childLine.FindStringSubmatch(out)
	if m == nil {
		t.Fatalf("the program did not say which child it started:\n%s", out)
	}
	pid, _ := strconv.Atoi(m[1])
	t.Cleanup(func() {
		if p, err := os.FindProcess(pid); err == nil {
			_ = p.Kill()
		}
	})
	return pid
}

// AwaitGone fails the test unless pid ends within half a minute. The child
// sleeps for a whole one.
func AwaitGone(t *testing.T, pid int) {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for !Gone(pid) {
		if time.Now().After(deadline) {
			t.Fatalf("child %d is still running", pid)
		}
		time.Sleep(10 * time.Millisecond)
	}
}
