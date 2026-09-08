package mediatools

import (
	"strconv"
	"strings"
)

// The four answers a version comparison can give. Four, and not a bool, because
// the third and fourth are real and are not the same as the second.
const (
	// CompareNewer: the published release is newer than what is installed.
	CompareNewer = "newer"
	// CompareSame: the two are the same version.
	CompareSame = "same"
	// CompareOlder: what is installed is newer than the newest published
	// release. Not an error - it is what a nightly build, or a distribution
	// that ships from git, looks like.
	CompareOlder = "older"
	// CompareUnknown: the two strings cannot be ordered against each other.
	// This is the state that has to exist, and the reason this function is not
	// internal/update's isNewer.
	CompareUnknown = "unknown"
)

// CompareVersions orders a published yt-dlp version against an installed one.
//
// WHY NOT internal/update's isNewer, WHICH ALREADY DOES THIS. Because it does
// not. yt-dlp versions are dates, and a same-day rerelease adds a fourth
// segment: "2026.08.11" one day and "2026.08.11.1" a few hours later. update.go
// parses with SplitN(v, ".", 3), so the fourth segment lands inside the third
// as "11.1", Atoi fails, ok comes back false - and update.Check's own handling
// of ok==false is to return Available:false, which the settings page draws as
// "you are current". A comparison that could not be made would render as "no
// update needed", which is the one wrong answer this whole card exists to
// avoid, and it would be wrong precisely on the day a hotfix was published.
//
// So: variable-length numeric segments, and a fourth state that says the
// comparison could not be made rather than guessing at either direction.
// "2026.08.11.1" against "2026.08.11" is perfectly orderable under that rule
// and answers "newer"; what genuinely cannot be ordered - an empty string, a
// segment that is not a number, a version from a source checkout that reads
// "2026.08.11.dev0" - answers "unknown", and the page then says so out loud
// rather than claiming anything.
//
// Missing segments count as zero, so "2026.08" and "2026.08.0" are the same
// version. That is the arithmetic every dotted numbering scheme uses and the
// alternative (calling them incomparable) would make a two-segment tag
// unreadable for no gain.
func CompareVersions(latest, current string) string {
	lp, lok := numericSegments(latest)
	cp, cok := numericSegments(current)
	if !lok || !cok {
		return CompareUnknown
	}
	n := len(lp)
	if len(cp) > n {
		n = len(cp)
	}
	for i := 0; i < n; i++ {
		l, c := 0, 0
		if i < len(lp) {
			l = lp[i]
		}
		if i < len(cp) {
			c = cp[i]
		}
		if l != c {
			if l > c {
				return CompareNewer
			}
			return CompareOlder
		}
	}
	return CompareSame
}

// numericSegments splits a dotted version into integers. A leading "v" is
// tolerated - yt-dlp's own tags have none, but a mirror or a repackager's tag
// might, and refusing over one character would turn a comparable pair into
// "unknown" for no reason.
//
// Leading zeros are fine and are what a date-based version is full of: Atoi
// reads "08" as 8.
func numericSegments(v string) ([]int, bool) {
	v = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(v), "v"))
	if v == "" {
		return nil, false
	}
	parts := strings.Split(v, ".")
	out := make([]int, 0, len(parts))
	for _, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 {
			return nil, false
		}
		out = append(out, n)
	}
	return out, true
}
