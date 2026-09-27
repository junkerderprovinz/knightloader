//go:build unix

package filemode

import (
	"io/fs"
	"os"
	"strconv"
	"strings"
	"syscall"
)

const supported = true

func setUmask(m fs.FileMode) { syscall.Umask(int(m)) }

// currentUmask reads the mask from /proc/self/status where Linux offers it.
// Elsewhere the only way to read it is to set it and put it back, and a file
// another goroutine creates in between gets the wrong mode, which is why Mask
// does this once.
func currentUmask() fs.FileMode {
	if b, err := os.ReadFile("/proc/self/status"); err == nil {
		for _, line := range strings.Split(string(b), "\n") {
			if rest, ok := strings.CutPrefix(line, "Umask:"); ok {
				if n, err := strconv.ParseUint(strings.TrimSpace(rest), 8, 32); err == nil {
					return fs.FileMode(n)
				}
			}
		}
	}
	old := syscall.Umask(0)
	syscall.Umask(old)
	return fs.FileMode(old)
}
