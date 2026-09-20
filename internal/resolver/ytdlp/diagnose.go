package ytdlp

import (
	"strings"

	"github.com/junkerderprovinz/knightloader/internal/core"
)

// The causes of a failed run are named here rather than by internal/app's
// shared classifier. The classifier only sees one shortened line, which loses
// the bot check's phrase, and it matches HTTP statuses first, which the URLs
// in yt-dlp's output trigger. The verdict travels as core.Update.Reason, which
// app_dispatch.go prefers over the classifier.
//
// Anything not recognised stays core.ReasonUnknown: a reason becomes advice,
// and a wrong "gone at the source" gets a working link deleted.

// diagnoses are the phrases matched against yt-dlp's stderr, in order, so the
// more specific reading wins. Each is wording yt-dlp or the site actually
// prints.
var diagnoses = []struct {
	phrase string
	reason core.Reason
}{
	// "not a bot" rather than "sign in to confirm", which also starts the
	// age-gate message.
	{"not a bot", core.ReasonBotCheck},

	// YouTube words memberships two ways, depending on channel levels.
	{"join this channel to get access to members-only", core.ReasonMembersOnly},
	{"available to this channel's members", core.ReasonMembersOnly},

	// Geo before gone: YouTube's regional block starts with "Video
	// unavailable.", and filing it as gone would get the link deleted.
	{"not available from your location", core.ReasonGeoBlocked},
	{"not made this video available in your country", core.ReasonGeoBlocked},
	{"blocked it in your country", core.ReasonGeoBlocked},
	{"geo restriction", core.ReasonGeoBlocked},

	// A removed video, a terminated account and a 404 all have the same
	// remedy, so they share one reason.
	{"has been removed by the uploader", core.ReasonGone},
	{"account associated with this video has been terminated", core.ReasonGone},
	{"video unavailable", core.ReasonGone},
	{"no longer available", core.ReasonGone},

	// Never the bare "DRM", which appears in titles and paths.
	{"drm protected", core.ReasonDRM},
	{"drm protection", core.ReasonDRM},

	// Last: yt-dlp adds its bug-report footer only to unexpected errors, none
	// of those above, and "unable to extract" also appears when a page simply
	// has nothing playable.
	{"please report this issue on", core.ReasonExtractorBroken},
	{"unable to extract", core.ReasonExtractorBroken},
}

// Diagnose names the cause of a failed yt-dlp run from its whole stderr, or
// returns core.ReasonUnknown. It reads the whole buffer because which line
// comes last depends on which post-processors ran.
func Diagnose(stderr string) core.Reason {
	low := strings.ToLower(stderr)
	for _, d := range diagnoses {
		if strings.Contains(low, d.phrase) {
			return d.reason
		}
	}
	return core.ReasonUnknown
}
