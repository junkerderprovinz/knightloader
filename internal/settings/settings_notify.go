package settings

// The event targets (Settings.EventTargets, declared in settings.go beside
// every other field): where this instance reports to when something happens.
//
// This is the mirror image of settings_feeds.go, and the field sits beside
// Feeds in the struct for that reason. A feed is an intake - somebody else
// publishes and links arrive here; an event target is an outtake - something
// happens here and a message goes out. Neither one is a setting about how
// downloads behave, which is why each gets a file of its own rather than a
// paragraph inside sanitizeNetwork.

import "github.com/junkerderprovinz/knightloader/internal/notify"

// sanitizeEventTargets hands the list to the package that implements it, the
// same way sanitizeFeeds hands its list to feed.Sanitize and sanitizeNetwork
// hands its two to proxycfg and reconnect. What a target may contain belongs
// next to the code that sends it: a second copy of "the method is one of three"
// here is the copy that is forgotten when the list moves.
//
// Deliberately NOT dropped here: a row whose address will not parse, and a row
// with an unclosed placeholder. Both are refused at save time by the API
// (validateRows), which is the same rule settings_feeds.go writes down and it
// matters more here than it does there. A subscription that vanishes stops
// adding links, which somebody notices; a target that vanishes stops telling
// them anything ever again, and silence is exactly what a working target looks
// like when nothing has happened yet.
func sanitizeEventTargets(n Settings) Settings {
	n.EventTargets = notify.Sanitize(n.EventTargets)
	return n
}
