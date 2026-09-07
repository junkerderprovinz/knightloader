// Package diskspace answers the one question the standard library has no
// portable answer for: how many bytes are still free on the volume a given
// path sits on.
//
// Go ships nothing for this. Unix needs statfs(2) and Windows needs
// GetDiskFreeSpaceEx, the two disagree about what a "block" is, and the
// Statfs_t struct is a different shape on almost every kernel - so the split
// is by build tag, in files small enough to read in one go, and the rest of
// the tree never sees any of it.
//
// THE SECOND RETURN VALUE IS THE WHOLE DESIGN. Free answers (bytes, true)
// when the platform gave a number and (0, false) when it did not, and those
// are two different answers rather than one number with a sad value. A caller
// that treats "I do not know" as "zero bytes free" is a guard that stops a
// perfectly healthy machine the first time it is run on a kernel nobody
// compiled a branch for, which is a far worse failure than the full disk it
// was protecting against. Every caller in this tree is expected to fail OPEN:
// no answer means no opinion, and the download goes ahead exactly as it did
// before this package existed.
package diskspace

import (
	"os"
	"path/filepath"
)

// Free reports the bytes still available at path, and whether this build could
// find out at all.
//
// It walks UP to the nearest ancestor that exists, and that is not a
// convenience - it is the normal case. A download folder is created lazily, by
// whoever writes the first file into it, so the guard that wants to know
// whether a task fits is asking about a directory that does not exist yet
// nine times out of ten. statfs on a missing path answers ENOENT, which would
// be reported as "unknown" and would switch the whole check off for precisely
// the fresh install it is most worth having on. The parent is on the same
// volume, which is the only thing the question is actually about.
//
// The walk terminates at the volume root, where filepath.Dir answers its own
// argument: a path on a drive that is not mounted at all reaches that point
// and answers false, which is the honest reading of "there is nothing here to
// measure".
//
// What "available" means differs by platform, deliberately, and both readings
// are the same idea: what THIS process may still write. On unix that is
// f_bavail, which excludes the blocks reserved for root; on Windows it is
// GetDiskFreeSpaceEx's first output, which honours a per-user quota. Reporting
// the larger, absolute figure instead would let a guard wave through a
// download that the filesystem will refuse anyway.
func Free(path string) (uint64, bool) {
	dir := filepath.Clean(path)
	for {
		if _, err := os.Stat(dir); err == nil {
			return free(dir)
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return 0, false
		}
		dir = parent
	}
}
