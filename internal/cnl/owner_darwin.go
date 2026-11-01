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

// portOwner asks lsof which process listens on port, and ps for its path and
// command line, since lsof gives the name cut to 15 characters.
func portOwner(ctx context.Context, port int) (Process, bool) {
	ctx, cancel := context.WithTimeout(ctx, ownerTimeout)
	defer cancel()
	out, err := exec.CommandContext(ctx, "lsof", "-nP", "-iTCP:"+strconv.Itoa(port), "-sTCP:LISTEN", "-Fpc").Output()
	if err != nil {
		return Process{}, false
	}
	pid, name := lsofOwner(string(out))
	if pid == 0 {
		return Process{}, false
	}
	p := Process{PID: pid, Path: name}
	if b, err := exec.CommandContext(ctx, "ps", "-p", strconv.Itoa(pid), "-o", "comm=").Output(); err == nil && strings.TrimSpace(string(b)) != "" {
		p.Path = strings.TrimSpace(string(b))
	}
	if b, err := exec.CommandContext(ctx, "ps", "-p", strconv.Itoa(pid), "-o", "args=").Output(); err == nil {
		p.Cmdline = strings.TrimSpace(string(b))
	}
	return p, true
}

// lsofOwner reads the first process from lsof's field output, where a line
// starting with p carries the process id and one starting with c its name.
func lsofOwner(out string) (pid int, name string) {
	for _, line := range strings.Split(out, "\n") {
		switch {
		case strings.HasPrefix(line, "p") && pid == 0:
			pid, _ = strconv.Atoi(line[1:])
		case strings.HasPrefix(line, "c") && pid != 0:
			return pid, line[1:]
		}
	}
	return pid, name
}
