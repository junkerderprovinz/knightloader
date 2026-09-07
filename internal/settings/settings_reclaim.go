package settings

// What the app is allowed to believe about a file it finds on the disk, and
// what it is allowed to do about one without being asked.

import "github.com/junkerderprovinz/knightloader/internal/reclaim"

// The three tiers, as the string form this document stores them in. The
// vocabulary belongs to internal/reclaim the same way the collision policy
// belongs to internal/collide and the mirror policy to internal/dedupe; this
// file only decides what a settings document may say and what an unusable
// value becomes.
const (
	ReclaimTrustChecksum = string(reclaim.TrustChecksum)
	ReclaimTrustRecord   = string(reclaim.TrustRecord)
	ReclaimTrustSize     = string(reclaim.TrustSize)
)

// ReclaimTrustModes lists them in the order an interface should offer them,
// strictest first. Built fresh per call so a caller cannot reorder the menu
// for everybody else, the same as ResumeModes just along.
func ReclaimTrustModes() []string {
	out := make([]string, 0, 3)
	for _, m := range reclaim.Modes() {
		out = append(out, string(m))
	}
	return out
}

// sanitizeReclaim folds anything unusable onto the package's own default,
// which is also what an install from before this key existed has in the file:
// nothing. See reclaim.ParseTrust for why an unknown value here folds onto the
// default rather than onto the strictest tier, unlike the resume policy two
// files along.
func sanitizeReclaim(n Settings) Settings {
	n.ReclaimTrust = string(reclaim.ParseTrust(n.ReclaimTrust))
	return n
}
