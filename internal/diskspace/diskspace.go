// Package diskspace reports how many bytes are free on the volume a path
// sits on and how much of it is occupied, which the standard library has no
// portable call for. Unix uses statfs(2) and Windows GetDiskFreeSpaceEx,
// split by build tag.
//
// Free and Usage return false when the platform gave no figures. Callers must
// fail open on that: "unknown" is not "zero bytes free", and a guard that
// treated it so would stop a healthy machine. A readout should say it cannot
// measure rather than draw an empty bar.
package diskspace

import (
	"os"
	"path/filepath"
)

// Space is one volume as a readout needs it. Free plus Used may be less than
// Total: Free is what this process may write, Used is what files occupy, and
// the root reserve or another account's quota is neither (see spaceFrom).
type Space struct {
	Free  uint64
	Used  uint64
	Total uint64
}

// Free reports the bytes this process may still write at path, and whether
// this build could find out. On unix that excludes the root reserve
// (f_bavail); on Windows it honours per-user quotas.
func Free(path string) (uint64, bool) {
	dir, ok := existingAncestor(path)
	if !ok {
		return 0, false
	}
	return free(dir)
}

// Usage reports the free, used and total bytes of the volume at path, and
// whether this build could find out.
//
// Like Free it silently walks up to the nearest existing directory. A readout
// must do its own walk and show which folder the figures describe, since an
// unmounted download folder resolves to the container's own filesystem
// (internal/app's DiskReport does this).
func Usage(path string) (Space, bool) {
	dir, ok := existingAncestor(path)
	if !ok {
		return Space{}, false
	}
	return usage(dir)
}

// existingAncestor is path itself when it exists, else the nearest existing
// directory above it, or false when there is none. Download folders are
// created lazily, so asking about a missing one is the normal case, and the
// parent is on the same volume.
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

// spaceFrom assembles a Space from what this process may write (ours), what
// the volume has left for anybody (anyones) and its size. Used comes from
// anyones, never ours, so the reserve between them is not shown as files. It
// lives here rather than in the platform files so its test runs everywhere.
func spaceFrom(ours, anyones, total uint64) Space {
	sp := Space{Free: ours, Total: total}
	if total > anyones {
		sp.Used = total - anyones
	}
	return sp
}
