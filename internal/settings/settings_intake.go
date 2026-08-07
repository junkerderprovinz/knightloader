package settings

// What happens to a link on the way in and to a file on the way out: when two
// URLs are the same download, and what to do when the destination is taken.

import (
	"github.com/junkerderprovinz/knightloader/internal/collide"
	"github.com/junkerderprovinz/knightloader/internal/dedupe"
)

func sanitizeIntake(n Settings) Settings {
	// Both policies fold an unknown value onto their package's default instead of
	// failing, so a settings file written by another build — or hand-edited with a
	// typo — can never stop links from being added.
	n.MirrorPolicy = string(dedupe.ParsePolicy(n.MirrorPolicy))
	n.CollisionPolicy = string(collide.ParsePolicy(n.CollisionPolicy))
	// Zero stays zero: that is "use the package's own cap". A number above it is
	// not a bigger allowance, it is the runaway guard switched off, which is how a
	// watch folder re-reading one list fills a directory nobody can open.
	if n.CollisionMaxAttempts < 0 {
		n.CollisionMaxAttempts = 0
	}
	if n.CollisionMaxAttempts > collide.DefaultMaxAttempts {
		n.CollisionMaxAttempts = collide.DefaultMaxAttempts
	}
	return n
}
