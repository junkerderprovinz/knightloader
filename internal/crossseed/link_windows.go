package crossseed

import (
	"strings"

	"golang.org/x/sys/windows"
)

// onSameFilesystem compares the volumes the two paths are mounted from.
func onSameFilesystem(a, b string) (bool, error) {
	va, err := volumeOf(a)
	if err != nil {
		return false, err
	}
	vb, err := volumeOf(b)
	if err != nil {
		return false, err
	}
	return strings.EqualFold(va, vb), nil
}

func volumeOf(p string) (string, error) {
	path, err := windows.UTF16PtrFromString(p)
	if err != nil {
		return "", err
	}
	buf := make([]uint16, windows.MAX_PATH+1)
	if err := windows.GetVolumePathName(path, &buf[0], uint32(len(buf))); err != nil {
		return "", err
	}
	return windows.UTF16ToString(buf), nil
}
