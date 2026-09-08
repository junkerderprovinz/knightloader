package ytdlp

// diagnose.go: naming the six failures a person can actually do something
// about, from what yt-dlp itself printed.
//
// WHY THIS IS NOT IN internal/app's shared classifier, where every other
// failure in this build is named. Two things stand between yt-dlp's stderr and
// that classifier, and both of them destroy the answer:
//
//   - What reaches it is ONE line, cut to fit a task's Err field. The bot
//     check's own line is about four hundred characters, so the phrase that
//     names it does not survive the trip - see errorLine in backend.go, which
//     is the other half of this change.
//   - classify() pulls an HTTP status out of any sentence it is handed
//     (statusPattern, app_errors.go) and answers from that BEFORE it looks at
//     a single phrase. A yt-dlp line carries URLs, and one with "/status/503"
//     or a site's own "code: 404" in it therefore wins that match. Widening
//     the text that reaches the classifier would have made this worse, not
//     better.
//
// So the process that read the whole of its own tool's output answers the
// question here, on the untouched buffer, and sends the verdict as
// core.Update.Reason. app_dispatch.go honours that over the classifier, which
// is the contract core.Update.Unsupported already had.
//
// THE RULE FROM app_errors.go HOLDS UNCHANGED: a failure nothing recognises is
// core.ReasonUnknown. The reason becomes advice in the interface, and advice is
// acted on - a wrong "this is gone at the source" makes somebody delete a link
// that a stored cookie jar would have fetched on the next attempt. A missing
// label costs them nothing.

import (
	"strings"

	"github.com/junkerderprovinz/knightloader/internal/core"
)

// diagnoses are the phrases yt-dlp's stderr is matched against, IN ORDER.
//
// Every entry is wording yt-dlp or the site behind it actually prints, taken
// from the failing runs rather than from what a site might plausibly say -
// the same standard textReasons (app_errors.go) holds itself to, and for the
// same reason: this table's output is an instruction to a person.
//
// The ORDER is load-bearing in two places, and both are marked below. A
// phrase table read top to bottom is what lets the more specific reading of a
// message win over the more general one that is also true of it.
var diagnoses = []struct {
	phrase string
	reason core.Reason
}{
	// The bot check leads, because it is the failure this whole file exists
	// for and the one the cookie jar in cookies.go was built to answer.
	//
	// "not a bot" and NOT "sign in to confirm", which is the front of that
	// same sentence: "Sign in to confirm your age" is a different failure
	// with a different word on the row, and matching the prefix would file
	// every age-gated video as a bot check.
	{"not a bot", core.ReasonBotCheck},

	// YouTube phrases a membership two ways depending on whether the channel
	// uses levels, and only one of them is the one most people ever see.
	// Both, or half the cases fall through to unknown.
	{"join this channel to get access to members-only", core.ReasonMembersOnly},
	{"available to this channel's members", core.ReasonMembersOnly},

	// GEO BEFORE GONE, and this is the first of the two places the order
	// carries the meaning. YouTube's copyright block opens with "Video
	// unavailable." and only then says who blocked it where, so a table that
	// asked the gone question first would file a video any VPN fetches
	// perfectly well as deleted - and the advice that follows from that is
	// "remove the link".
	{"not available from your location", core.ReasonGeoBlocked},
	{"not made this video available in your country", core.ReasonGeoBlocked},
	{"blocked it in your country", core.ReasonGeoBlocked},
	{"geo restriction", core.ReasonGeoBlocked},

	// Gone at the source. There is deliberately no ReasonDeleted beside
	// core.ReasonGone: a video the uploader removed, a terminated account and
	// a plain 404 differ in nothing a person can act on differently, and one
	// word for one remedy is the taxonomy's own rule.
	{"has been removed by the uploader", core.ReasonGone},
	{"account associated with this video has been terminated", core.ReasonGone},
	{"video unavailable", core.ReasonGone},
	{"no longer available", core.ReasonGone},

	// Never the three bare letters. "DRM" turns up in album names, channel
	// names and file paths, and a video called "DRM Free Mixtape" failing to
	// download for any reason at all would be told it is encrypted and that
	// nothing can be done. These two are the phrases yt-dlp itself uses when
	// it means it.
	{"drm protected", core.ReasonDRM},
	{"drm protection", core.ReasonDRM},

	// LAST, and that placement is the second load-bearing bit of ordering.
	// yt-dlp appends its bug-report footer to failures it did NOT expect, and
	// to none of the ones above, which it raises as expected errors - so
	// nothing above can be stolen by this entry. The other way round it
	// could: "unable to extract" is also how yt-dlp reports a page it read
	// fine and found nothing playable on, so anything the table has already
	// recognised keeps its own name.
	{"please report this issue on", core.ReasonExtractorBroken},
	{"unable to extract", core.ReasonExtractorBroken},
}

// Diagnose names the cause of a failed yt-dlp run from its whole stderr, or
// answers core.ReasonUnknown.
//
// It takes the WHOLE buffer and not one line on purpose. yt-dlp says what went
// wrong on its ERROR line and then keeps talking, and which of those lines is
// the last one depends on which post-processors happened to run; a diagnosis
// that depended on that would work on a plain video and not on a merged one.
func Diagnose(stderr string) core.Reason {
	low := strings.ToLower(stderr)
	for _, d := range diagnoses {
		if strings.Contains(low, d.phrase) {
			return d.reason
		}
	}
	return core.ReasonUnknown
}
