package settings

// The event targets (Settings.EventTargets): where this instance reports to
// when something happens.
//
// The mirror image of settings_feeds.go, which is why the field sits beside
// Feeds in the struct. A feed is an intake, somebody else publishes and links
// arrive here; an event target is an outtake, something happens here and a
// message goes out. Neither is a setting about how downloads behave, so each
// gets a file rather than a paragraph inside sanitizeNetwork.

import "github.com/junkerderprovinz/knightloader/internal/notify"

// sanitizeEventTargets hands the list to the package that implements it, the
// way sanitizeFeeds hands its list to feed.Sanitize. What a target may contain
// belongs next to the code that sends it: a second copy of "the method is one
// of three" here is the copy that is forgotten when the list moves.
//
// Two things are left in place: a row whose address will not parse, and a row
// with an unclosed placeholder. Both are refused at save time by the API
// (validateRows), the rule settings_feeds.go writes down, and it matters more
// here. A subscription that vanishes stops adding links, which somebody
// notices; a target that vanishes stops telling them anything again, and
// silence is what a working target looks like when nothing has happened.
func sanitizeEventTargets(n Settings) Settings {
	n.EventTargets = notify.Sanitize(n.EventTargets)
	return n
}
