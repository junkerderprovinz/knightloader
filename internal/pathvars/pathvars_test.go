package pathvars

import (
	"strings"
	"testing"
	"time"
)

// stamp is single-digit in every field on purpose, so a missing zero pad or a
// swapped month/minute shows up instead of being hidden by two-digit values.
var stamp = time.Date(2026, 3, 7, 9, 5, 4, 0, time.UTC)

func testVars() Vars {
	return Vars{Package: "Season 1", Host: "example.org", Name: "ep01.mkv", Date: stamp}
}

// TestExpandReplacesEveryPlaceholder pins the whole placeholder set. If it
// failed, a folder template carried over from JDownloader would either lose a
// value or leave the raw tag sitting in the download path.
func TestExpandReplacesEveryPlaceholder(t *testing.T) {
	cases := []struct {
		name     string
		template string
		want     string
	}{
		{"package", "<jd:packagename>", "Season 1"},
		{"hoster", "<jd:hoster>", "example.org"},
		{"filename", "<jd:filename>", "ep01.mkv"},
		{"date", "<jd:date>", "2026-03-07"},
		{"year", "<jd:year>", "2026"},
		{"month", "<jd:month>", "03"},
		{"day", "<jd:day>", "07"},
		{"simpledate", "<jd:simpledate:yyyy-MM>", "2026-03"},
		{"time fields", "<jd:simpledate:HH-mm-ss>", "09-05-04"},
		{"whole path", "/downloads/<jd:packagename>/<jd:simpledate:yyyy-MM>", "/downloads/Season 1/2026-03"},
		{"repeated", "<jd:year>/<jd:year>", "2026/2026"},
		// The tag may be capitalised, but the format argument may not be
		// case-folded or MM would become minutes.
		{"tag case", "<JD:PackageName>", "Season 1"},
		{"format case kept", "<JD:SimpleDate:MM-mm>", "03-05"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := Expand(c.template, testVars()); got != c.want {
				t.Errorf("Expand(%q) = %q, want %q", c.template, got, c.want)
			}
		})
	}
}

// TestExpandCustomSimpleDateFormat covers a pattern with separators, quoted
// literal text and a bare digit. The digit is the interesting part: Go layouts
// are made of digits, so a naive whole-pattern translation would read the "2"
// as a day number.
func TestExpandCustomSimpleDateFormat(t *testing.T) {
	cases := map[string]string{
		"<jd:simpledate:dd.MM.yyyy>":       "07.03.2026",
		"<jd:simpledate:yyyy_MM_dd_HHmm>":  "2026_03_07_0905",
		"<jd:simpledate:HH'h'mm>":          "09h05",
		"<jd:simpledate:yyyy-MM-dd_2>":     "2026-03-07_2",
		"<jd:simpledate:EEE MMM dd yyyy>":  "Sat Mar 07 2026",
		"<jd:simpledate:yyyy 'part' MMMM>": "2026 part March",
	}
	for template, want := range cases {
		if got := Expand(template, testVars()); got != want {
			t.Errorf("Expand(%q) = %q, want %q", template, got, want)
		}
	}
}

// TestExpandLeavesUnknownPlaceholders keeps a typo visible. If it failed, a
// mistyped tag would expand to nothing and every download of that template
// would collapse into the same "/downloads//" folder.
func TestExpandLeavesUnknownPlaceholders(t *testing.T) {
	cases := []string{
		"/dl/<jd:packagenam>/x",
		"/dl/<jd:hostname>",
		"/dl/<jd:simpledat:yyyy>",
		"/dl/<other:packagename>",
		"/dl/<jd:filename", // never closed, so not a placeholder
		"/dl/<jd:>",        // empty key
	}
	for _, template := range cases {
		if got := Expand(template, testVars()); got != template {
			t.Errorf("Expand(%q) = %q, want it untouched", template, got)
		}
	}
	// A known placeholder next to an unknown one still expands.
	const mixed = "/dl/<jd:nope>/<jd:packagename>"
	if got, want := Expand(mixed, testVars()), "/dl/<jd:nope>/Season 1"; got != want {
		t.Errorf("Expand(%q) = %q, want %q", mixed, got, want)
	}
}

// TestExpandSanitizesValuesToOneSegment is what stops a package name from
// inventing folder levels or emitting characters Windows rejects. These rules
// have to stay in step with sanitizeSegment in internal/app/app.go.
func TestExpandSanitizesValuesToOneSegment(t *testing.T) {
	cases := []struct {
		name string
		vars Vars
		want string
	}{
		{"slash and colon", Vars{Package: "Doctor Who: S1/E2"}, "Doctor Who- S1-E2"},
		{"backslash", Vars{Package: `C:\temp`}, "C--temp"},
		{"windows reserved", Vars{Package: `why? "x" <y>|z*`}, "why- -x- -y--z-"},
		{"control characters", Vars{Package: "line\nbreak"}, "line break"},
		{"trailing dots and spaces", Vars{Package: "  Season 1.  "}, "Season 1"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := Expand("<jd:packagename>", c.vars); got != c.want {
				t.Errorf("Expand = %q, want %q", got, c.want)
			}
		})
	}
}

// TestExpandNeverProducesAnEmptySegment covers values that sanitise away
// entirely. Without the fallback words the path would contain "//" and every
// such download would land one level too high.
func TestExpandNeverProducesAnEmptySegment(t *testing.T) {
	cases := []struct {
		name     string
		template string
		vars     Vars
		want     string
	}{
		{"dots only", "/dl/<jd:packagename>/f", Vars{Package: " ... "}, "/dl/package/f"},
		{"empty package", "/dl/<jd:packagename>/f", Vars{}, "/dl/package/f"},
		{"empty hoster", "/dl/<jd:hoster>/f", Vars{}, "/dl/hoster/f"},
		{"empty filename", "/dl/<jd:filename>/f", Vars{}, "/dl/file/f"},
		{"empty date format", "/dl/<jd:simpledate:>/f", Vars{Date: stamp}, "/dl/date/f"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := Expand(c.template, c.vars); got != c.want {
				t.Errorf("Expand(%q) = %q, want %q", c.template, got, c.want)
			}
		})
	}
}

// TestExpandLeavesPlainPathsAlone guards the common case: most folders contain
// no variables at all and must come back byte for byte, brackets included.
func TestExpandLeavesPlainPathsAlone(t *testing.T) {
	cases := []string{
		"/downloads",
		`D:\downloads\movies`,
		"",
		"/downloads/season <1>/x",
	}
	for _, template := range cases {
		if got := Expand(template, testVars()); got != template {
			t.Errorf("Expand(%q) = %q, want it unchanged", template, got)
		}
	}
}

// TestExpandDoesNotReadTheClock pins the documented contract: a zero Date
// formats as the zero time rather than as now, so callers that want the current
// time have to pass it in and results stay reproducible.
func TestExpandDoesNotReadTheClock(t *testing.T) {
	if got, want := Expand("<jd:year>", Vars{}), "0001"; got != want {
		t.Errorf("Expand with zero Date = %q, want %q", got, want)
	}
}

// TestExpandCapsSegmentLength keeps a release-title package name from blowing
// past the filesystem's per-segment limit. The cap must match app.go's.
func TestExpandCapsSegmentLength(t *testing.T) {
	got := Expand("<jd:packagename>", Vars{Package: strings.Repeat("a", 200)})
	if len(got) != maxSegment {
		t.Errorf("length %d, want %d", len(got), maxSegment)
	}
}

// TestHasVars decides whether the caller runs the expander at all, so a false
// negative would silently leave "<jd:packagename>" in a real folder name.
func TestHasVars(t *testing.T) {
	cases := map[string]bool{
		"/downloads/<jd:packagename>": true,
		"<JD:hoster>":                 true,
		"<jd:anything at all>":        true,
		"/downloads":                  false,
		"":                            false,
		"<jd:filename":                false,
		"/downloads/<not a tag>":      false,
	}
	for template, want := range cases {
		if got := HasVars(template); got != want {
			t.Errorf("HasVars(%q) = %v, want %v", template, got, want)
		}
	}
}
