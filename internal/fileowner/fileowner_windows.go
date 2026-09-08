//go:build windows

package fileowner

// Windows has no file owner this package can talk about, and saying so is the
// answer rather than the absence of one.
//
// It is not that nobody owns a file here - a Windows file has a security
// descriptor with an owner SID and a full ACL, which is a genuinely richer
// model than a uid and nine bits. It is that NONE of it maps onto the question
// this package asks. There is no number to put in a chown line, the permission
// bits os.Stat synthesises (0666 for a writable file, 0444 for a read-only one)
// are derived from the read-only ATTRIBUTE and describe nothing about who may
// open it, and a readout that printed them would be inventing a unix folder on
// a machine that has none.
//
// Reading the real ACL was considered and rejected. It would mean
// golang.org/x/sys/windows, a SID-to-name lookup that can hit a domain
// controller, and a readout whose answer is a sentence about inherited ACEs -
// for a deployment where the problem this package exists for cannot occur: the
// desktop build runs as the person sitting in front of it, writing into folders
// that person owns. The honest "not applicable" is the more useful answer and
// costs nothing to keep correct.

import "io/fs"

// supported says this build has no implementation behind who or statOwner. See
// the unix file's copy of this constant.
const supported = false

// who answers with the zero Identity, whose Known is false. Every number in it
// then means NOTHING - not uid 0, not root, not "no umask". Whatever draws it
// has to say that in words, the same duty a DiskReport row has when its own
// Known is false.
func who() Identity { return Identity{} }

// statOwner reports that this build cannot tell, which is what makes Check
// answer VerdictUnknown here rather than comparing two zeroes and pronouncing
// them equal.
func statOwner(fs.FileInfo) (int, int, bool) { return 0, 0, false }

// names has nothing to look up, because there was no number to look up.
func names(int, int) (string, string) { return "", "" }
