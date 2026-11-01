package cnl

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

func addrInUse(err error) bool {
	return errors.Is(err, syscall.EADDRINUSE)
}

// portOwner finds the socket listening on port in /proc/net and the process
// holding that socket among the open files in /proc. Another user's process
// keeps its files to itself, so it is found only when this one runs as root.
func portOwner(_ context.Context, port int) (Process, bool) {
	inodes := map[string]bool{}
	for _, table := range []string{"/proc/net/tcp", "/proc/net/tcp6"} {
		b, err := os.ReadFile(table)
		if err != nil {
			continue
		}
		for _, inode := range listenInodes(string(b), port) {
			inodes[inode] = true
		}
	}
	if len(inodes) == 0 {
		return Process{}, false
	}
	fds, _ := filepath.Glob("/proc/[0-9]*/fd/*")
	for _, fd := range fds {
		link, err := os.Readlink(fd)
		if err != nil || !strings.HasPrefix(link, "socket:[") {
			continue
		}
		if inodes[strings.TrimSuffix(strings.TrimPrefix(link, "socket:["), "]")] {
			pid, _ := strconv.Atoi(strings.Split(fd, "/")[2])
			return describe(pid), true
		}
	}
	return Process{}, false
}

// listenInodes reads a /proc/net/tcp table and returns the inodes of the
// sockets listening on port. A row is "sl local rem st ... inode", the local
// address ends in the port in hex and state 0A is LISTEN.
func listenInodes(table string, port int) []string {
	suffix := fmt.Sprintf(":%04X", port)
	var out []string
	for _, line := range strings.Split(table, "\n") {
		f := strings.Fields(line)
		if len(f) < 10 || f[3] != "0A" || !strings.HasSuffix(f[1], suffix) {
			continue
		}
		out = append(out, f[9])
	}
	return out
}

func describe(pid int) Process {
	dir := fmt.Sprintf("/proc/%d/", pid)
	p := Process{PID: pid}
	if exe, err := os.Readlink(dir + "exe"); err == nil {
		p.Path = strings.TrimSuffix(exe, " (deleted)")
	} else if comm, err := os.ReadFile(dir + "comm"); err == nil {
		p.Path = strings.TrimSpace(string(comm))
	}
	if cmd, err := os.ReadFile(dir + "cmdline"); err == nil {
		p.Cmdline = strings.TrimSpace(strings.ReplaceAll(string(cmd), "\x00", " "))
	}
	return p
}
