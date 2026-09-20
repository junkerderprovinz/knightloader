//go:build windows

package diskspace

import "golang.org/x/sys/windows"

const supported = true

// free calls GetDiskFreeSpaceExW and returns its first output, the bytes
// available to this account, which is stricter than the volume's free bytes
// under a per-user quota and equal otherwise.
func free(path string) (uint64, bool) {
	p, err := windows.UTF16PtrFromString(path)
	if err != nil {
		// Only an embedded NUL fails here, which is no real path.
		return 0, false
	}
	var availToCaller, total, totalFree uint64
	if err := windows.GetDiskFreeSpaceEx(p, &availToCaller, &total, &totalFree); err != nil {
		return 0, false
	}
	return availToCaller, true
}

// usage makes the same call and keeps all three outputs. Used is built from
// the volume's free bytes, so another account's quota does not count as
// files.
func usage(path string) (Space, bool) {
	p, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return Space{}, false
	}
	var availToCaller, total, totalFree uint64
	if err := windows.GetDiskFreeSpaceEx(p, &availToCaller, &total, &totalFree); err != nil {
		return Space{}, false
	}
	return spaceFrom(availToCaller, totalFree, total), true
}
