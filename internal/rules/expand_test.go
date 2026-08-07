package rules

import (
	"strconv"
	"strings"
	"testing"

	"github.com/junkerderprovinz/knightloader/internal/pathvars"
)

// TestSanitisingAgreesWithPathvars is the invariant the duplicated helper in
// expand.go claims. Both packages cut values that end up in the same download
// paths, so the day they disagree is the day one package name produces two
// folders and half a release lands in each. Comparing against pathvars itself
// rather than against a pinned string is what makes the test notice a change on
// either side.
func TestSanitisingAgreesWithPathvars(t *testing.T) {
	names := []string{
		"plain name",
		"",
		"   ",
		`a/b\c:d*e?f"g<h>i|j`,
		"trailing dots...",
		"\x01\x02control chars",
		strings.Repeat("a", 200),
		// 200 bytes of two-byte runes, so the cut lands mid-rune. Agreeing on
		// the mangled result still beats disagreeing on a tidy one.
		strings.Repeat("ä", 100),
	}
	for _, n := range names {
		want := pathvars.Expand("<jd:filename>", pathvars.Vars{Name: n})
		if got := segment(n, "file"); got != want {
			t.Errorf("segment(%q) = %q, but pathvars expands the same value to %q", n, got, want)
		}
	}
}

// TestExpandVariables pins the whole placeholder set a rule can use, including
// the ones internal/pathvars owns: this package hands the template to pathvars
// first, so a broken handover would lose those and nothing else would say so.
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
		// Both sets in one template, which is the shape a real folder takes.
		{"/dl/<jd:hoster>/<jd:source:2>/<jd:packagename>", "/dl/example.org/season-1/The Show"},
		{"<jd:orgfilenamewithoutext>.<jd:orgfiletype>", "The.Show.S01E02.1080p.mkv"},
		// Nothing to do at all.
		{"/dl/plain", "/dl/plain"},
		// An unknown placeholder is left where the user can see it, rather than
		// collapsing to nothing and leaving a folder called "//" behind.
		{"/dl/<jd:nosuchthing>", "/dl/<jd:nosuchthing>"},
	}
	m := &Matcher{}
	for _, tc := range cases {
		if got := m.expand(tc.template, "test", c, nil); got != tc.want {
			t.Errorf("expand(%q) = %q, want %q", tc.template, got, tc.want)
		}
	}
}

// TestExpandSourceOutOfRange keeps a template the user got wrong visible. A
// blank would silently move every download of a whole site one folder up.
func TestExpandSourceOutOfRange(t *testing.T) {
	c := testCandidate().filled()
	m := &Matcher{}
	for _, template := range []string{"<jd:source:0>", "<jd:source:4>", "<jd:source:99>"} {
		if got := m.expand(template, "test", c, nil); got != template {
			t.Errorf("expand(%q) = %q, want the tag left in place", template, got)
		}
	}
	// A link that was never crawled has no source at all, and every segment of
	// it is out of range for the same reason.
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
		// A host with no path has no segments; splitting the raw string would
		// otherwise hand back "https:" as the first one.
		{"https://tracker.example.net", "1", "", false},
		{"https://tracker.example.net/", "1", "", false},
		// Percent escapes are decoded, because the segment ends up in a folder
		// name a person reads.
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

// TestExpandCannotAddPathLevels is the security-shaped one. A hoster that
// serves a file called "../../etc/passwd" must not be able to walk out of the
// folder the template spelled out.
func TestExpandCannotAddPathLevels(t *testing.T) {
	c := Candidate{Filename: `../../etc/passwd`, Package: `..\..\windows`, Source: "https://x.test/a/../b"}
	m := &Matcher{}
	for _, template := range []string{
		"/dl/<jd:orgfilename>",
		"/dl/<jd:orgfilenamewithoutext>",
		"/dl/<jd:packagename>",
		"/dl/<jd:filename>",
		// The second segment of that source is "..", which sanitises away
		// entirely and has to become a word rather than nothing.
		"/dl/<jd:source:2>",
	} {
		got := m.expand(template, "test", c.filled(), nil)
		if rest, _ := strings.CutPrefix(got, "/dl/"); strings.ContainsAny(rest, `/\`) {
			t.Errorf("expand(%q) = %q, which adds a path level the template never named", template, got)
		}
	}
}

// TestExpandValueIsNotRescanned proves the order of the two passes: pathvars
// runs first, so a value it substitutes can never be read back as one of the
// placeholders this package resolves.
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

// TestOrgFileNameFallsBackToAWord covers the other side: a link with no name at
// all still contributes a named segment rather than an empty one, because an
// empty segment is how a template ends up producing "/downloads//".
func TestOrgFileNameFallsBackToAWord(t *testing.T) {
	m := &Matcher{}
	got := m.expand("/dl/<jd:orgfilename>", "test", Candidate{}.filled(), nil)
	if got != "/dl/file" {
		t.Errorf("expand = %q, want %q", got, "/dl/file")
	}
}

// TestAppendDeduplicates walks the counter through the sequence it exists for:
// the first link keeps the plain name, every repeat gets numbered.
func TestAppendDeduplicates(t *testing.T) {
	m := &Matcher{}
	c := testCandidate().filled()
	want := []string{"The Show", "The Show_2", "The Show_3"}
	for i, w := range want {
		if got := m.expand("<jd:packagename><jd:append>", "package", c, nil); got != w {
			t.Errorf("call %d = %q, want %q", i+1, got, w)
		}
	}
	// A different target field is a different namespace: a package and a file
	// name that read the same are not a collision with each other.
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

// TestAppendIgnoresAPlantedMarker: the counter holds its place with a NUL, so a
// NUL arriving in the template itself would take a second suffix and produce
// "name_2_2".
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

// TestAppendCounterStopsGrowing pins the ceiling on the only state a Matcher
// keeps. Past the cap a name the counter has not seen gets no suffix, which is
// what the first link carrying any name gets anyway; names already counted keep
// counting.
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

// TestAppendCountsOncePerLink is the reason Apply expands after the loop rather
// than inside it. Two rules writing an <jd:append> package name is an ordinary
// Packagizer list - a broad rule and a narrower one below it - and while every
// matching rule expanded its own template, each link burned one counter per rule.
// The very first link came out as "X_2", a de-duplication of nothing, and the
// suffix counted how many rules had touched the field instead of how often that
// name had been seen.
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

// TestApplyDoesNotExpandOverwrittenTemplates is the other half of that: a
// template a later rule replaces must not be expanded at all, or a discarded
// value still takes a counter and still pays for the substitution.
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
	// The overwritten template never ran, so its name is still unused: a rule
	// that starts producing it now gets the unnumbered form.
	if got := m.expand("discarded<jd:append>", string(FieldPackage), Candidate{}, nil); got != "discarded" {
		t.Errorf("the discarded template consumed a counter: %q", got)
	}
}

// TestApplyExpandsEveryStringAction ties the expander to the actions, so a
// field that forgets to run through it is caught here rather than by a user
// staring at a folder literally called "<jd:packagename>".
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

// TestCheckExpandsTheReason keeps the rejection message as useful as the rest:
// naming the link that was dropped is the point of showing it at all.
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
