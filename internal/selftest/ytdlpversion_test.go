package selftest

// The version parser, against strings real yt-dlp builds actually print.
//
// The point of the table is the THIRD column of cases: the ones that must come
// back false. A parser that quietly turned a distribution's own numbering into
// a date would put "your yt-dlp is 700 days old" in front of somebody whose
// yt-dlp is current, and the advice attached to that row tells them to go and
// replace a working binary.

import (
	"testing"
	"time"
)

func TestParseVersionReadsWhatRealBuildsPrint(t *testing.T) {
	cases := []struct {
		name string
		out  string
		want string // "" means the parser must refuse it
	}{
		{"a plain release", "2026.08.15\n", "2026-08-15"},
		{"a nightly, which appends the build time", "2026.08.15.232336\n", "2026-08-15"},
		{"the same with no trailing newline", "2024.04.09.232719", "2024-04-09"},
		{"a distribution's patched numbering", "2023.07.06-1\n", "2023-07-06"},
		{"a build that appends a commit", "2025.01.15 [a1b2c3d]\n", "2025-01-15"},
		{"an unpadded month and day from a repackaged build", "2023.7.6\n", "2023-07-06"},
		{"the oldest shape still in the wild", "2021.12.27\n", "2021-12-27"},

		// Everything below must be refused. Each one is a string that LOOKS
		// enough like a version to tempt a looser parser.
		{"nothing at all", "", ""},
		{"only whitespace", "  \n\n", ""},
		{"a word", "unknown\n", ""},
		{"a semantic version, which yt-dlp has never used", "1.2.3\n", ""},
		{"an epoch-shaped date that is not a release", "1970.01.01\n", ""},
		{"a placeholder year", "9999.12.31\n", ""},
		{"a month that does not exist", "2026.13.01\n", ""},
		{"a day February does not have", "2026.02.31\n", ""},
		{"a year with the wrong number of digits", "202.08.15\n", ""},
		{"a leading space before the date", " 2026.08.15\n", "2026-08-15"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, ok := ParseVersion(c.out)
			if c.want == "" {
				if ok {
					t.Fatalf("ParseVersion(%q) accepted it as %s; a string that is not a release date must be refused, "+
						"because a wrong age sends somebody to replace a binary that was fine", c.out, got.Format(time.DateOnly))
				}
				return
			}
			if !ok {
				t.Fatalf("ParseVersion(%q) refused a version real yt-dlp builds print; the row would then say "+
					"\"cannot judge its age\" about a perfectly ordinary release", c.out)
			}
			if s := got.Format(time.DateOnly); s != c.want {
				t.Fatalf("ParseVersion(%q) = %s, want %s", c.out, s, c.want)
			}
			if got.Location() != time.UTC {
				t.Fatalf("ParseVersion(%q) came back in %v, want UTC - the day count must not depend on the "+
					"reader's own zone, which on a container with no TZ is one of the things being tested", c.out, got.Location())
			}
		})
	}
}

func TestAgeVerdictMarksTheTwoThresholdsWhereTheyAreDocumented(t *testing.T) {
	now := time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)
	day := 24 * time.Hour

	cases := []struct {
		age       time.Duration
		wantState Status
		wantDays  int
	}{
		{0, StatusPass, 0},
		{time.Duration(FreshDays-1) * day, StatusPass, FreshDays - 1},
		{time.Duration(FreshDays) * day, StatusWarn, FreshDays},
		{time.Duration(StaleDays-1) * day, StatusWarn, StaleDays - 1},
		{time.Duration(StaleDays) * day, StatusFail, StaleDays},
		{400 * day, StatusFail, 400},
	}
	for _, c := range cases {
		got, days := AgeVerdict(now.Add(-c.age), now)
		if got != c.wantState || days != c.wantDays {
			t.Errorf("AgeVerdict for a build %v old = (%s, %d), want (%s, %d)", c.age, got, days, c.wantState, c.wantDays)
		}
	}
}

// TestAFutureVersionIsNotReportedAsNegativeDaysOld pins the clamp, which is not
// hypothetical: a nightly is built somewhere else, and the clock check next
// door exists precisely because this machine's own idea of the date is one of
// the things that can be wrong. "-1 days old" on that screen is a bug report
// about this feature rather than about anybody's yt-dlp.
func TestAFutureVersionIsNotReportedAsNegativeDaysOld(t *testing.T) {
	now := time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)
	got, days := AgeVerdict(now.Add(48*time.Hour), now)
	if got != StatusPass {
		t.Errorf("AgeVerdict for a build dated two days ahead = %s, want %s", got, StatusPass)
	}
	if days != 0 {
		t.Errorf("AgeVerdict reported %d days for a build dated in the future, want 0", days)
	}
}
