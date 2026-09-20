//go:build windows

package fileowner

// Windows files have owner SIDs and ACLs, but none of it maps onto a chown,
// and the mode bits os.Stat synthesises only reflect the read-only attribute.
// The desktop build runs as the user who owns its folders, so the ownership
// problem this package explains does not arise, and it reports Known false.

import "io/fs"

const supported = false

func who() Identity { return Identity{} }

func statOwner(fs.FileInfo) (int, int, bool) { return 0, 0, false }

func names(int, int) (string, string) { return "", "" }
