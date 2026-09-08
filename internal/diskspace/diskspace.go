// Package diskspace answers the two questions the standard library has no
// portable answer for: how many bytes are still free on the volume a given path
// sits on, and how much of that volume is already occupied.
//
// Go ships nothing for this. Unix needs statfs(2) and Windows needs
// GetDiskFreeSpaceEx, the two disagree about what a "block" is, and the
// Statfs_t struct is a different shape on almost every kernel - so the split
// is by build tag, in files small enough to read in one go, and the rest of
// the tree never sees any of it.
//
// THE SECOND RETURN VALUE IS THE WHOLE DESIGN. Free and Usage answer with
// (figures, true) when the platform gave numbers and (zero, false) when it did
// not, and those are two different answers rather than one number with a sad
// value. A caller that treats "I do not know" as "zero bytes free" is a guard
// that stops a perfectly healthy machine the first time it is run on a kernel
// nobody compiled a branch for, which is a far worse failure than the full disk
// it was protecting against. Every caller in this tree is expected to fail
// OPEN: no answer means no opinion, and the download goes ahead exactly as it
// did before this package existed. A readout has the same duty in its own
// currency - it says it cannot measure rather than drawing a bar at nought per
// cent, which is a picture of an empty disk.
package diskspace

import (
	"os"
	"path/filepath"
)

// Space is one volume as the three numbers a readout needs.
//
// FREE PLUS USED MAY BE LESS THAN TOTAL, and that is not a rounding error.
// Free is what THIS process may still write, Used is what somebody's files
// occupy, and the slice between the two belongs to neither: the blocks a unix
// filesystem holds back for root, or the part of a Windows volume that another
// account's quota keeps out of reach. spaceFrom is where that gap is put, and
// is the one place in this package where the three figures are assembled.
type Space struct {
	Free  uint64
	Used  uint64
	Total uint64
}

// Free reports the bytes still available at path, and whether this build could
// find out at all.
//
// What "available" means differs by platform, deliberately, and both readings
// are the same idea: what THIS process may still write. On unix that is
// f_bavail, which excludes the blocks reserved for root; on Windows it is
// GetDiskFreeSpaceEx's first output, which honours a per-user quota. Reporting
// the larger, absolute figure instead would let a guard wave through a
// download that the filesystem will refuse anyway.
func Free(path string) (uint64, bool) {
	dir, ok := existingAncestor(path)
	if !ok {
		return 0, false
	}
	return free(dir)
}

// Usage reports what the volume at path holds: what is free for this process,
// what is occupied, and how big it is - and whether this build could find out
// at all.
//
// It is a second entry point rather than a wider Free because the guard that
// hangs off Free wants one number and must keep working unchanged; and because
// the used half cannot be derived from the free half by anybody, however
// tempting the subtraction looks. See Space and spaceFrom.
//
// IT WALKS UP EXACTLY AS Free DOES, AND SAYS NOTHING ABOUT HAVING DONE SO.
// That is right for a guard and a silent lie for a readout: a download folder
// whose mount did not come up walks all the way to the volume root, and the
// caller is handed the container's own filesystem with no hint that it asked
// about a different disk. A caller that puts these numbers in front of a person
// must do its own walk first and show which folder the figures describe -
// internal/app's DiskReport is the one that does, and the reason it does.
func Usage(path string) (Space, bool) {
	dir, ok := existingAncestor(path)
	if !ok {
		return Space{}, false
	}
	return usage(dir)
}

// existingAncestor is path itself when it is there, else the nearest thing
// above it that is, and false when nothing along the way exists.
//
// The walk is not a convenience - it is the normal case. A download folder is
// created lazily, by whoever writes the first file into it, so the guard that
// wants to know whether a task fits is asking about a directory that does not
// exist yet nine times out of ten. statfs on a missing path answers ENOENT,
// which would be reported as "unknown" and would switch the whole check off for
// precisely the fresh install it is most worth having on. The parent is on the
// same volume, which is the only thing the question is actually about.
//
// The walk terminates at the volume root, where filepath.Dir answers its own
// argument: a path on a drive that is not mounted at all reaches that point and
// answers false, which is the honest reading of "there is nothing here to
// measure".
func existingAncestor(path string) (string, bool) {
	dir := filepath.Clean(path)
	for {
		if _, err := os.Stat(dir); err == nil {
			return dir, true
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", false
		}
		dir = parent
	}
}

// spaceFrom assembles the three figures from what one platform measured, and it
// is the ONE place the arithmetic happens.
//
// ours is what this process may still write (statfs's f_bavail, Windows's
// lpFreeBytesAvailableToCaller). anyones is what the volume has left for
// anybody at all (f_bfree, lpTotalNumberOfFreeBytes). total is its size.
//
// USED IS BUILT FROM anyones AND NEVER FROM ours, which is the whole reason
// this function exists rather than a subtraction at each call site. The gap
// between the two - the root reserve on unix, somebody else's quota on Windows
// - then counts as neither free nor used, which is exactly what it is. Deriving
// used as total minus ours paints that gap as somebody's files: on a default
// ext4 that is five per cent of the volume shown as space nobody can account
// for, and the person looking at it goes hunting for files that are not there.
//
// It lives in this file rather than in the two platform ones because it is
// arithmetic and not a syscall, and because a test of it here runs on every
// platform: the same rule written twice behind build tags would be pinned only
// on whichever machine happened to run it.
func spaceFrom(ours, anyones, total uint64) Space {
	sp := Space{Free: ours, Total: total}
	if total > anyones {
		sp.Used = total - anyones
	}
	return sp
}
