package selftest

// How old the yt-dlp on this machine is, worked out from the only thing it
// will tell you about itself.
//
// `yt-dlp --version` is already run at boot
// (internal/resolver/ytdlp.Backend.Available) and its output thrown away: the
// call keeps the exit code and nothing else, so the app knows whether yt-dlp
// exists and nothing knows which one it is. In the container it comes from the
// Alpine package that was current when the image was built and never updates
// itself, and the big video sites break an old yt-dlp within weeks, with a
// failure that looks like a dead link rather than stale software.
//
// A date and not a version comparison, because yt-dlp's versions are dates:
// the project releases as YYYY.MM.DD and its nightlies append a build time, so
// "how far behind is this" needs no release feed, no network call and no
// hardcoded "latest" that would go stale inside this binary. Comparing against
// a version compiled in here would report a current yt-dlp as out of date the
// moment KnightLoader stopped being rebuilt.
//
// Anything that does not start with a four-digit year and two more dotted
// parts is reported as "cannot judge its age" rather than guessed at.
// Distributions do patch this string, and a wrong age sends somebody to
// replace a binary that was fine.

import (
	"regexp"
	"strconv"
	"strings"
	"time"
)

// FreshDays and StaleDays are the two marks the age verdict turns on.
//
// Thirty and ninety, both judgement rather than measurement. yt-dlp releases
// roughly monthly, so a build inside thirty days is current and saying
// anything about it would be noise. Ninety days is where the project's own
// issue tracker stops accepting reports without an update first. Named
// constants rather than literals, because these are the two numbers somebody
// will want to argue with.
const (
	FreshDays = 30
	StaleDays = 90
)

// versionDate matches the leading YYYY.M.D of a yt-dlp version string.
//
// Anchored at the start and not at the end: a nightly is "2026.08.15.232336"
// and a distribution build can be "2023.07.06-1" or carry a suffix in
// brackets. Everything after the third component is noise here, since the day
// is the whole answer, and refusing those strings would report the two build
// kinds most likely to be old as unjudgeable.
//
// One or two digits for month and day rather than exactly two, because
// unpadded forms do occur in repackaged builds. The range check in
// ParseVersion is what actually rejects nonsense; a regexp that tried to
// express "01-12" would be unreadable and would still need that check.
var versionDate = regexp.MustCompile(`^(\d{4})\.(\d{1,2})\.(\d{1,2})(?:\D|$)`)

// ParseVersion reads the release date out of what `yt-dlp --version` printed,
// and reports false for anything it cannot read as a date.
//
// The date is taken as UTC midnight. The comparison is a day count in the
// tens, so the few hours a real release time would move it by cannot change a
// verdict, and UTC keeps the answer independent of the reader's own zone,
// which on a container with no TZ is one of the things this self-test is
// trying to find out about.
//
// false is a real answer and the caller must render it as one. "2023.7.6.dev"
// from somebody's own build, a distribution string that starts with an epoch,
// an empty output from a binary that printed to stderr: all of them mean "this
// is a yt-dlp and its age cannot be judged", which is a different sentence
// from "this yt-dlp is old" and from "there is no yt-dlp".
func ParseVersion(out string) (time.Time, bool) {
	// The first line and its first token: a wrapper script can print a banner
	// after the version, and some builds append a git hash separated by a
	// space. Neither is a reason to give up on a string that starts with a
	// perfectly good date.
	line := strings.TrimSpace(out)
	if i := strings.IndexAny(line, "\r\n"); i >= 0 {
		line = line[:i]
	}
	line = strings.TrimSpace(line)
	if i := strings.IndexAny(line, " \t"); i >= 0 {
		line = line[:i]
	}

	m := versionDate.FindStringSubmatch(line)
	if m == nil {
		return time.Time{}, false
	}
	year, _ := strconv.Atoi(m[1])
	month, _ := strconv.Atoi(m[2])
	day, _ := strconv.Atoi(m[3])
	// A plausibility window rather than a bare "is it a valid date". yt-dlp's
	// first release was 2021 and youtube-dl's numbering before it started in
	// 2008; a "1970.01.01" out of an epoch-shaped string, or a "9999.12.31"
	// out of a placeholder, is not a release date and must not become one.
	if year < 2008 || year > 2100 {
		return time.Time{}, false
	}
	if month < 1 || month > 12 || day < 1 || day > 31 {
		return time.Time{}, false
	}
	t := time.Date(year, time.Month(month), day, 0, 0, 0, 0, time.UTC)
	// time.Date normalises rather than refusing, so "2026.02.31" comes back as
	// the 3rd of March. Round-tripping the day is what catches that: a string
	// that does not survive the trip was never a date.
	if t.Day() != day || int(t.Month()) != month {
		return time.Time{}, false
	}
	return t, true
}

// AgeVerdict turns a release date into a status and the day count that goes in
// the sentence.
//
// A future date is clamped rather than reported. A nightly built a few hours
// ahead of this machine's idea of the date, or a clock that is behind, which
// is itself one of the seven checks, would otherwise produce a negative age,
// and "-1 days old" is a bug report about this feature rather than about
// somebody's yt-dlp. Zero days and "fine" is the reading of a version that is
// not behind.
func AgeVerdict(released, now time.Time) (Status, int) {
	days := int(now.Sub(released).Hours() / 24)
	if days < 0 {
		days = 0
	}
	switch {
	case days < FreshDays:
		return StatusPass, days
	case days < StaleDays:
		return StatusWarn, days
	default:
		// Fail rather than a second warn. Past ninety days the big sites have
		// changed something, and the failure that produces looks like a dead
		// link, so the one place that can name the cause has to say so.
		return StatusFail, days
	}
}
