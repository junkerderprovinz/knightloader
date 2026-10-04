//go:build windows

package app

import (
	"io/fs"
	"os"

	"golang.org/x/sys/windows"
)

// openShared opens name for reading with FILE_SHARE_DELETE, which os.Open
// leaves out. A player still reading a finished file would otherwise keep
// the rename and the delivery that follow the download from moving it.
func openShared(name string) (*os.File, error) {
	p, err := windows.UTF16PtrFromString(name)
	if err != nil {
		return nil, &fs.PathError{Op: "open", Path: name, Err: err}
	}
	h, err := windows.CreateFile(p, windows.GENERIC_READ,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE,
		nil, windows.OPEN_EXISTING, windows.FILE_ATTRIBUTE_NORMAL, 0)
	if err != nil {
		return nil, &fs.PathError{Op: "open", Path: name, Err: err}
	}
	return os.NewFile(uintptr(h), name), nil
}
