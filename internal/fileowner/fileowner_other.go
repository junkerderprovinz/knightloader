//go:build !unix && !windows

package fileowner

// The honest silence for a platform this package has no call for: js/wasm,
// wasip1, plan9.
//
// It answers false rather than a plausible number, and the difference matters in
// exactly one direction. False makes Check report VerdictUnknown, which every
// caller in this tree renders as "this system has no file owners, so there is
// nothing to compare" - a sentence that is true and leads nowhere. Returning
// uid 0 with Known true would look identical in the JSON and would tell somebody
// their downloads belong to root, which is a confident answer to a question that
// was never asked. Same rule as diskspace_other.go, word for word: a made-up
// answer and a missing one are not interchangeable just because they are the
// same size.

import "io/fs"

// supported says this build has no implementation behind who or statOwner. See
// the unix file's copy of this constant.
const supported = false

func who() Identity { return Identity{} }

func statOwner(fs.FileInfo) (int, int, bool) { return 0, 0, false }

func names(int, int) (string, string) { return "", "" }
