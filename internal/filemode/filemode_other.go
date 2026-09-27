//go:build !unix

package filemode

import "io/fs"

// Windows has no umask: a new file gets its folder's ACL, and the mode bits
// only carry the read-only attribute.
const supported = false

func setUmask(fs.FileMode) {}

func currentUmask() fs.FileMode { return 0 }
