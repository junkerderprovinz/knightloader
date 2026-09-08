//go:build linux || darwin || freebsd

package diskspace

import "syscall"

// supported says this build has a real implementation behind free and usage.
// Read only by the package's own test, which otherwise could not tell a
// platform that genuinely cannot answer from an implementation that has stopped
// working.
const supported = true

// The kernels are enumerated rather than covered by the `unix` constraint, and
// the list is short on purpose. `unix` also matches solaris, illumos, aix and
// the two BSDs that do not have this call at all - NetBSD exposes statvfs and
// OpenBSD spells the fields F_bsize/F_bavail - so a build tag that claims all
// of them is a build tag that stops compiling on a platform nobody here can
// test. Anything not named above falls through to the stub, which answers "no
// opinion", and no opinion is a working guard rather than a broken one.

// free reads statfs(2). Bavail and Bsize are converted through int64 rather
// than used as declared because their types are different on every kernel in
// the tag above: Linux has Bsize int64 with Bavail uint64, Darwin has Bsize
// uint32, FreeBSD has Bsize uint64 with Bavail int64. One conversion that is
// valid for all six combinations is worth more than three files that each know
// one of them.
func free(path string) (uint64, bool) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(path, &st); err != nil {
		return 0, false
	}
	bsize := int64(st.Bsize)
	if bsize <= 0 {
		// A block size of zero is a filesystem that has answered nonsense, not
		// one that is full. Multiplying by it would report a comfortable disk as
		// having nothing left.
		return 0, false
	}
	avail := int64(st.Bavail)
	if avail < 0 {
		// The BSDs really do report a negative f_bavail: the reserve is a
		// signed figure and root writing into it takes it below zero. That is a
		// genuine "nothing left for us", so it is reported as an answer of zero
		// and NOT as a platform that cannot say - the difference decides whether
		// the caller blocks or waves the download through.
		return 0, true
	}
	return uint64(avail) * uint64(bsize), true
}

// usage reads the same statfs(2) call free does and takes three of its counts,
// none of which is worked out from the others.
//
// f_bavail is what this process may write, f_bfree is what the filesystem has
// left for anybody, and f_blocks is the size. The first two differ by the
// reserve, so both are read and handed to spaceFrom, which is where the reserve
// is placed. Anything that instead subtracted the free bytes from the total
// would file that reserve under somebody's files.
//
// Every count goes through int64 for the reason free's own comment gives:
// Blocks and Bfree are as type-unstable across the kernels in this build tag as
// Bavail and Bsize already are.
func usage(path string) (Space, bool) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(path, &st); err != nil {
		return Space{}, false
	}
	bsize := int64(st.Bsize)
	if bsize <= 0 {
		// Nonsense rather than a full disk, exactly as in free: multiplying by
		// it would report a comfortable volume as having nothing at all.
		return Space{}, false
	}
	// A negative count is read as zero, which is the same call free makes for
	// f_bavail on the BSDs: the reserve is signed there and root writing into
	// it takes the figure below zero. That is a genuine "nothing", and the
	// conversion has to swallow it - uint64(-1) is eighteen exabytes.
	bytes := func(n int64) uint64 {
		if n <= 0 {
			return 0
		}
		return uint64(n) * uint64(bsize)
	}
	return spaceFrom(bytes(int64(st.Bavail)), bytes(int64(st.Bfree)), bytes(int64(st.Blocks))), true
}
