//go:build windows

package diskspace

import "golang.org/x/sys/windows"

// supported says this build has a real implementation behind free - see the
// unix file's own copy of this constant.
const supported = true

// free calls GetDiskFreeSpaceExW.
//
// The FIRST output is the one taken, not the third. They differ on a volume
// with per-user disk quotas: lpTotalNumberOfFreeBytes is what the volume has
// left, lpFreeBytesAvailableToCaller is what this account may still write, and
// only the second answers the question a download guard is asking. On a volume
// with no quota the two are equal, so nothing is given up by taking the
// stricter one.
//
// golang.org/x/sys/windows rather than a hand-rolled syscall.NewLazyDLL:
// the module is already in this build's dependency graph, and its generated
// wrapper carries the UTF-16 signature and the error conversion that a
// hand-written LazyProc call would have to get right here instead.
func free(path string) (uint64, bool) {
	p, err := windows.UTF16PtrFromString(path)
	if err != nil {
		// The only way this fails is an embedded NUL, which cannot be a real
		// path. Reported as "no answer" rather than as zero, for the reason the
		// package comment gives.
		return 0, false
	}
	var availToCaller, total, totalFree uint64
	if err := windows.GetDiskFreeSpaceEx(p, &availToCaller, &total, &totalFree); err != nil {
		return 0, false
	}
	return availToCaller, true
}
