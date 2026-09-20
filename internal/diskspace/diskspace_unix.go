//go:build linux || darwin || freebsd

package diskspace

import "syscall"

// supported lets the test tell a platform that cannot answer from an
// implementation that stopped working.
const supported = true

// The kernels are listed rather than using the unix constraint, which also
// matches systems without this statfs (NetBSD has statvfs, OpenBSD other
// field names). Everything else gets the stub.

// free reads statfs(2). The fields go through int64 because their types
// differ on each kernel in the tag.
func free(path string) (uint64, bool) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(path, &st); err != nil {
		return 0, false
	}
	bsize := int64(st.Bsize)
	if bsize <= 0 {
		// A nonsensical block size, not a full disk.
		return 0, false
	}
	avail := int64(st.Bavail)
	if avail < 0 {
		// The BSDs report a negative f_bavail once root writes into the
		// reserve. That really is nothing left for us.
		return 0, true
	}
	return uint64(avail) * uint64(bsize), true
}

// usage reads f_bavail, f_bfree and f_blocks from statfs(2) and leaves the
// arithmetic to spaceFrom.
func usage(path string) (Space, bool) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(path, &st); err != nil {
		return Space{}, false
	}
	bsize := int64(st.Bsize)
	if bsize <= 0 {
		return Space{}, false
	}
	// A negative count (the BSD reserve again) is zero; uint64(-1) would be
	// eighteen exabytes.
	bytes := func(n int64) uint64 {
		if n <= 0 {
			return 0
		}
		return uint64(n) * uint64(bsize)
	}
	return spaceFrom(bytes(int64(st.Bavail)), bytes(int64(st.Bfree)), bytes(int64(st.Blocks))), true
}
