package crossseed

import (
	"os"

	"golang.org/x/sys/unix"
)

func reflink(from, to string) error {
	src, err := os.Open(from)
	if err != nil {
		return err
	}
	defer src.Close()
	dst, err := os.OpenFile(to, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	err = unix.IoctlFileClone(int(dst.Fd()), int(src.Fd()))
	if cerr := dst.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		os.Remove(to)
	}
	return err
}
