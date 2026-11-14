//go:build unix

package crossseed

import (
	"os"
	"syscall"
)

func onSameFilesystem(a, b string) (bool, error) {
	fa, err := os.Stat(a)
	if err != nil {
		return false, err
	}
	fb, err := os.Stat(b)
	if err != nil {
		return false, err
	}
	return fa.Sys().(*syscall.Stat_t).Dev == fb.Sys().(*syscall.Stat_t).Dev, nil
}
