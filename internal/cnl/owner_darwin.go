package cnl

import (
	"context"
	"errors"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"time"
)

func addrInUse(err error) bool {
	return errors.Is(err, syscall.EADDRINUSE)
}

// ownerTimeout bounds lsof and ps, which answer at once on a healthy system.
const ownerTimeout = 3 * time.Second

// portOwners asks lsof which processes listen on port, and ps for their paths
// and command lines, since lsof gives the name cut to 15 characters.
func portOwners(ctx context.Context, port int) []Process {
	ctx, cancel := context.WithTimeout(ctx, ownerTimeout)
	defer cancel()
	out, err := exec.CommandContext(ctx, "lsof", "-nP", "-iTCP:"+strconv.Itoa(port), "-sTCP:LISTEN", "-Fpc").Output()
	if err != nil {
		return nil
	}
	owners := lsofOwners(string(out))
	for i := range owners {
		pid := strconv.Itoa(owners[i].PID)
		if b, err := exec.CommandContext(ctx, "ps", "-p", pid, "-o", "comm=").Output(); err == nil && strings.TrimSpace(string(b)) != "" {
			owners[i].Path = strings.TrimSpace(string(b))
		}
		if b, err := exec.CommandContext(ctx, "ps", "-p", pid, "-o", "args=").Output(); err == nil {
			owners[i].Cmdline = strings.TrimSpace(string(b))
		}
	}
	return owners
}

// lsofOwners reads the processes from lsof's field output, where a line
// starting with p begins a process with its id and one starting with c gives
// its name.
func lsofOwners(out string) []Process {
	var owners []Process
	for _, line := range strings.Split(out, "\n") {
		switch {
		case strings.HasPrefix(line, "p"):
			pid, _ := strconv.Atoi(line[1:])
			owners = append(owners, Process{PID: pid})
		case strings.HasPrefix(line, "c") && len(owners) > 0:
			owners[len(owners)-1].Path = line[1:]
		}
	}
	return owners
}
