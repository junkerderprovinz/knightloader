//go:build unix

package execxtest

import (
	"bytes"
	"errors"
	"os"
	"strconv"
	"syscall"
)

// Gone reports whether pid has ended. A killed process that nobody has reaped
// yet is a zombie and counts as ended: it runs nothing any more.
func Gone(pid int) bool {
	if err := syscall.Kill(pid, 0); errors.Is(err, syscall.ESRCH) {
		return true
	}
	stat, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
	if err != nil {
		return false
	}
	// The state follows the command name, which is in parentheses and may
	// hold spaces of its own.
	i := bytes.LastIndexByte(stat, ')')
	return i >= 0 && len(stat) > i+2 && stat[i+2] == 'Z'
}
