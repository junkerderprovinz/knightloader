package settings

// The RSS and Atom subscriptions (Settings.Feeds). This is the unattended
// intake with no folder behind it: somebody else publishes, and this instance
// picks up what is new.

import "github.com/junkerderprovinz/knightloader/internal/feed"

// sanitizeFeeds hands the list to the package that implements it, the way
// sanitizeNetwork hands its two lists to proxycfg and reconnect. What a
// subscription may contain belongs next to the code that acts on it: a second
// copy of "an interval is at least a minute" here is the copy that is forgotten
// when the bound moves.
//
// Two things are left in place: a row whose address will not parse, and a row
// whose title filter will not compile. Both are refused at save time by the API
// (validateRows) and one row at a time by the runner, the rule sanitizeRules
// states for the rule lists and the timetable. A vanished title filter would be
// worse than a vanished rule, because the subscription then runs unfiltered and
// stages the whole feed.
func sanitizeFeeds(n Settings) Settings {
	n.Feeds = feed.Sanitize(n.Feeds)
	return n
}
