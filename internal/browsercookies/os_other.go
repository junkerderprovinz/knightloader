//go:build !windows

package browsercookies

import (
	"errors"
	"os"
)

func dpapiUnprotect([]byte) ([]byte, error) {
	return nil, errors.New("DPAPI is only on Windows")
}

func openShared(path string) (*os.File, error) { return os.Open(path) }

func lockedOut(error) bool { return false }
