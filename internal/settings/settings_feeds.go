package settings

// The RSS and Atom subscriptions (Settings.Feeds, declared in settings.go
// beside every other field). This is the unattended intake that has no folder
// behind it: somebody else publishes, and this instance picks up what is new.

import "github.com/junkerderprovinz/knightloader/internal/feed"

// sanitizeFeeds hands the list to the package that implements it, the same way
// sanitizeNetwork hands its two lists to proxycfg and reconnect. The rules about
// what a subscription may contain belong next to the code that acts on them: a
// second copy of "an interval is at least a minute" here is the copy that is
// forgotten when the bound moves.
//
// Deliberately NOT dropped here: a row whose address will not parse, and a row
// whose title filter will not compile. Both are refused at save time by the API
// (validateRows) and refused one row at a time by the runner, which is the same
// rule sanitizeRules already states for the rule lists and the timetable: a row
// that vanishes on save is a row the user goes on believing in. For a title
// filter that would be worse than for a rule, because the subscription would
// then run unfiltered and stage the whole feed.
func sanitizeFeeds(n Settings) Settings {
	n.Feeds = feed.Sanitize(n.Feeds)
	return n
}
