//go:build !windows

package app

import "os"

func openShared(name string) (*os.File, error) {
	return os.Open(name)
}
