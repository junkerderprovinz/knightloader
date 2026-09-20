package rules

import (
	"strconv"
	"strings"
	"testing"

	"github.com/junkerderprovinz/knightloader/internal/pathvars"
)

// TestSanitisingAgreesWithPathvars: both packages cut values that end up in
// the same download paths, and a disagreement would split one package into two
// folders. Comparing against pathvars itself catches a change on either side.
func TestSanitisingAgreesWithPathvars(t *testing.T) {
	names := []string{
		"plain name",
		"",
		"   ",
		`a/b\c:d*e?f"g<h>i|j`,
		"trailing dots...",
		"\x01\x02control chars",
		strings.Repeat("a", 200),
		// 200 bytes of two-byte runes, so the cut lands mid-rune; both sides
		// still have to agree.
		strings.Repeat("ä", 100),
	}
	for _, n := range names {
		want := pathvars.Expand("<jd:filename>", pathvars.Vars{Name: n})
		if got := segment(n, "file"); got != want {
			t.Errorf("segment(%q) = %q, but pathvars expands the same value to %q", n, got, want)
		}
	}
}

// TestExpandVariables pins every placeholder a rule can use, including the ones
// internal/pathvars resolves, so a broken handover to pathvars shows up.
func TestExpandVariables(t *testing.T) {
	c := testCandidate().filled()
	cases := []struct {
		template string
		want     string
	}{
		// Owned by internal/pathvars.
		{"<jd:packagename>", "The Show"},
		{"<jd:hoster>", "example.org"},
		{"<jd:filename>", "The.Show.S01E02.1080p.mkv"},
		{"<jd:date>", "2026-03-07"},
		{"<jd:simpledate:yyyy-MM>", "2026-03"},
		// Added here.
		{"<jd:orgfilename>", "The.Show.S01E02.1080p.mkv"},
		{"<jd:orgfilenamewithoutext>", "The.Show.S01E02.1080p"},
		{"<jd:orgfiletype>", "mkv"},
		{"<jd:source:1>", "tv"},
		{"<jd:source:2>", "season-1"},
		{"<jd:source:3>", "index.html"},
		// The tag may be capitalised, exactly as pathvars allows.
		{"<JD:OrgFileType>", "mkv"},
		{"<Jd:Source:2>", "season-1"},
		// Both sets in one template.
		{"/dl/<jd:hoster>/<jd:source:2>/<jd:packagename>", "/dl/example.org/season-1/The Show"},
		{"<jd:orgfilenamewithoutext>.<jd:orgfiletype>", "The.Show.S01E02.1080p.mkv"},
		{"/dl/plain", "/dl/plain"},
		// An unknown placeholder stays visible.
		{"/dl/<jd:nosuchthing>", "/dl/<jd:nosuchthing>"},
	}
	m := &Matcher{}
	for _, tc := range cases {
		if got := m.expand(tc.template, "test", c, nil); got != tc.want {
			t.Errorf("expand(%q) = %q, want %q", tc.template, got, tc.want)
		}
	}
}

// TestExpandSourceOutOfRange: a blank would silently move every download of a
// site one folder up, so the tag stays.
func TestExpandSourceOutOfRange(t *testing.T) {
	c := testCandidate().filled()
	m := &Matcher{}
	for _, template := range []string{"<jd:source:0>", "<jd:source:4>", "<jd:source:99>"} {
		if got := m.expand(template, "test", c, nil); got != template {
			t.Errorf("expand(%q) = %q, want the tag left in place", template, got)
		}
	}
	// A link that was never crawled has no source at all.
	noSource := testCandidate()
	noSource.Source = ""
	if got := m.expand("<jd:source:1>", "test", noSource.filled(), nil); got != "<jd:source:1>" {
		t.Errorf("expand with no source = %q, want the tag left in place", got)
	}
}

func TestSourceSegment(t *testing.T) {
	cases := []struct {
		source, index string
		want          string
		ok            bool
	}{
		{"https://tracker.example.net/tv/season-1/index.html", "1", "tv", true},
		{"https://tracker.example.net/tv/season-1/index.html", "3", "index.html", true},
		// Repeated and trailing slashes are not segments of their own.
		{"https://tracker.example.net//tv///s01/", "2", "s01", true},
		// A host with no path has no segments, not "https:".
		{"https://tracker.example.net", "1", "", false},
		{"https://tracker.example.net/", "1", "", false},
		// Percent escapes are decoded for the folder name.
		{"https://tracker.example.net/tv/season%201/x", "2", "season 1", true},
		{"", "1", "", false},
		{"https://tracker.example.net/tv", "x", "", false},
	}
	for _, tc := range cases {
		got, ok := sourceSegment(tc.source, tc.index)
		if got != tc.want || ok != tc.ok {
			t.Errorf("sourceSegment(%q, %q) = %q, %v; want %q, %v", tc.source, tc.index, got, ok, tc.want, tc.ok)
		}
	}
}

// TestExpandCannotAddPathLevels: a hoster serving "../../etc/passwd" must not
// walk out of the folder the template spelled out.
func TestExpandCannotAddPathLevels(t *testing.T) {
	c := Candidate{Filename: `../../etc/passwd`, Package: `..\..\windows`, Source: "https://x.test/a/../b"}
	m := &Matcher{}
	for _, template := range []string{
		"/dl/<jd:orgfilename>",
		"/dl/<jd:orgfilenamewithoutext>",
		"/dl/<jd:packagename>",
		"/dl/<jd:filename>",
		// The second segment of that source is "..", which sanitises away
		// and becomes a word.
		"/dl/<jd:source:2>",
	} {
		got := m.expand(template, "test", c.filled(), nil)
		if rest, _ := strings.CutPrefix(got, "/dl/"); strings.ContainsAny(rest, `/\`) {
			t.Errorf("expand(%q) = %q, which adds a path level the template never named", template, got)
		}
	}
}

// TestExpandValueIsNotRescanned: pathvars runs first, so a value it
// substitutes is never read back as a placeholder.
func TestExpandValueIsNotRescanned(t *testing.T) {
	c := Candidate{Package: "<jd:orgfilename>", Filename: "secret.mkv"}
	m := &Matcher{}
	got := m.expand("<jd:packagename>", "test", c.filled(), nil)
	if strings.Contains(got, "secret") {
		t.Errorf("expand = %q, want the substituted value left alone", got)
	}
}

// TestOrgFileTypeHasNoFallbackWord: a file without an extension is normal, and
// a fallback word would turn "movie" into "movie.type".
func TestOrgFileTypeHasNoFallbackWord(t *testing.T) {
	c := Candidate{Filename: "README"}
	m := &Matcher{}
	if got := m.expand("<jd:orgfilenamewithoutext>.<jd:orgfiletype>", "test", c.filled(), nil); got != "README." {
		t.Errorf("expand = %q, want %q", got, "README.")
	}
}

// TestOrgFileNameFallsBackToAWord: a link with no name still contributes a
// named segment, not an empty one that would produce "/downloads//".
func TestOrgFileNameFallsBackToAWord(t *testing.T) {
	m := &Matcher{}
	got := m.expand("/dl/<jd:orgfilename>", "test", Candidate{}.filled(), nil)
	if got != "/dl/file" {
		t.Errorf("expand = %q, want %q", got, "/dl/file")
	}
}

// TestAppendDeduplicates: the first link keeps the plain name and every repeat
// is numbered.
func TestAppendDeduplicates(t *testing.T) {
	m := &Matcher{}
	c := testCandidate().filled()
	want := []string{"The Show", "The Show_2", "The Show_3"}
	for i, w := range want {
		if got := m.expand("<jd:packagename><jd:append>", "package", c, nil); got != w {
			t.Errorf("call %d = %q, want %q", i+1, got, w)
		}
	}
	// Each target field counts on its own.
	if got := m.expand("<jd:packagename><jd:append>", "filename", c, nil); got != "The Show" {
		t.Errorf("first call on a second field = %q, want an unnumbered name", got)
	}
	// A different value in the same field starts its own count.
	other := c
	other.Package = "Other Show"
	if got := m.expand("<jd:packagename><jd:append>", "package", other, nil); got != "Other Show" {
		t.Errorf("first call for a second value = %q, want an unnumbered name", got)
	}
	m.ResetAppend()
	if got := m.expand("<jd:packagename><jd:append>", "package", c, nil); got != "The Show" {
		t.Errorf("after ResetAppend = %q, want the count to start over", got)
	}
}

// TestAppendIgnoresAPlantedMarker: a NUL in the template itself would
// otherwise take a second suffix and produce "name_2_2".
func TestAppendIgnoresAPlantedMarker(t *testing.T) {
	m := &Matcher{}
	c := Candidate{}
	if got := m.expand("a"+appendMark+"<jd:append>", "package", c, nil); got != "a" {
		t.Errorf("first call = %q, want %q", got, "a")
	}
	if got := m.expand("a"+appendMark+"<jd:append>", "package", c, nil); got != "a_2" {
		t.Errorf("second call = %q, want %q", got, "a_2")
	}
}

// TestAppendCounterStopsGrowing: past the cap a new name gets no suffix, and
// names already counted keep counting.
func TestAppendCounterStopsGrowing(t *testing.T) {
	m := &Matcher{}
	const template = "<jd:packagename><jd:append>"
	for i := range maxAppendKeys {
		name := "pkg" + strconv.Itoa(i)
		if got := m.expand(template, "package", Candidate{Package: name}, nil); got != name {
			t.Fatalf("filling the counter: call %d = %q, want %q", i, got, name)
		}
	}
	fresh := Candidate{Package: "brand new"}
	for i := range 2 {
		if got := m.expand(template, "package", fresh, nil); got != "brand new" {
			t.Errorf("call %d past the cap = %q, want an unnumbered name", i+1, got)
		}
	}
	if got := m.expand(template, "package", Candidate{Package: "pkg0"}, nil); got != "pkg0_2" {
		t.Errorf("a name already counted = %q, want pkg0_2", got)
	}
}

// TestAppendCountsOncePerLink: with a broad and a narrow rule both writing an
// <jd:append> package name, the counter still advances once per link, not
// once per matching rule.
func TestAppendCountsOncePerLink(t *testing.T) {
	m, problems := Compile(Set{Rules: []Rule{
		{Name: "broad", Action: Action{PackageName: "X<jd:append>"}},
		{Name: "narrow", Action: Action{PackageName: "X<jd:append>"}},
	}})
	if len(problems) != 0 {
		t.Fatalf("Compile: %v", problems)
	}
	for i, want := range []string{"X", "X_2", "X_3"} {
		if got := m.Apply(Candidate{}).Package; got != want {
			t.Errorf("link %d packaged as %q, want %q", i+1, got, want)
		}
	}
}

// TestApplyDoesNotExpandOverwrittenTemplates: a template a later rule replaces
// is never expanded, so it takes no counter.
func TestApplyDoesNotExpandOverwrittenTemplates(t *testing.T) {
	m, problems := Compile(Set{Rules: []Rule{
		{Name: "broad", Action: Action{PackageName: "discarded<jd:append>"}},
		{Name: "narrow", Action: Action{PackageName: "kept"}},
	}})
	if len(problems) != 0 {
		t.Fatalf("Compile: %v", problems)
	}
	if got := m.Apply(Candidate{}).Package; got != "kept" {
		t.Fatalf("Package = %q, want the later rule's value", got)
	}
	// The overwritten template never ran, so its name is still unused.
	if got := m.expand("discarded<jd:append>", string(FieldPackage), Candidate{}, nil); got != "discarded" {
		t.Errorf("the discarded template consumed a counter: %q", got)
	}
}

// TestApplyExpandsEveryStringAction catches a string action that skips the
// expander.
func TestApplyExpandsEveryStringAction(t *testing.T) {
	m, problems := Compile(Set{Rules: []Rule{{
		Name: "tv",
		Action: Action{
			PackageName: "<jd:orgfilenamewithoutext>",
			DownloadDir: "/dl/<jd:hoster>/<jd:source:1>",
			Filename:    "<jd:orgfilename>",
			Comment:     "from <jd:source:2> on <jd:date>",
		},
	}}})
	if len(problems) != 0 {
		t.Fatalf("Compile: %v", problems)
	}
	e := m.Apply(testCandidate())
	want := Effect{
		Package:  "The.Show.S01E02.1080p",
		Dir:      "/dl/example.org/tv",
		Filename: "The.Show.S01E02.1080p.mkv",
		Comment:  "from season-1 on 2026-03-07",
	}
	if e.Package != want.Package || e.Dir != want.Dir || e.Filename != want.Filename || e.Comment != want.Comment {
		t.Errorf("Apply = %+v, want %+v", e, want)
	}
}

func TestCheckExpandsTheReason(t *testing.T) {
	m, problems := Compile(Set{Rules: []Rule{{
		Name:   "too big",
		Action: Action{Reject: true, Reason: "<jd:orgfilename> is larger than the limit"},
	}}})
	if len(problems) != 0 {
		t.Fatalf("Compile: %v", problems)
	}
	v := m.Check(testCandidate())
	if v.Reason != "The.Show.S01E02.1080p.mkv is larger than the limit" {
		t.Errorf("Reason = %q", v.Reason)
	}
}
