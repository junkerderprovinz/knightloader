package rules

import (
	"strings"
	"testing"
)

// TestEveryCategoryCompilesAsARule is the property that makes a category sugar
// rather than a second mechanism: what the picker writes has to be a condition
// the engine already accepts, or the chip produces a rule that is refused the
// moment it is saved.
func TestEveryCategoryCompilesAsARule(t *testing.T) {
	for _, cat := range Categories() {
		set := Set{Rules: []Rule{{
			Name:       cat.ID,
			Conditions: []Condition{{Field: FieldFiletype, Op: OpMatches, Value: cat.Pattern}},
			Action:     Action{PackageName: cat.ID},
		}}}
		if _, problems := Compile(set); len(problems) > 0 {
			t.Errorf("category %q does not compile: %v", cat.ID, problems)
		}
		if len(cat.Pattern) > maxPattern {
			t.Errorf("category %q is %d bytes, past the %d Compile refuses", cat.ID, len(cat.Pattern), maxPattern)
		}
		if len(cat.Extensions) == 0 {
			t.Errorf("category %q lists no extensions, so the chip explains nothing", cat.ID)
		}
	}
}

// TestCategoryMatchesItsOwnExtensionsAndNothingAdjacent covers the anchors. The
// filetype field holds the extension alone, so an unanchored "ts" would quietly
// pull in every .mts as well and the category would match more than it says.
func TestCategoryMatchesItsOwnExtensionsAndNothingAdjacent(t *testing.T) {
	pattern, ok := CategoryPattern("video")
	if !ok {
		t.Fatal("the video category is missing")
	}
	m, problems := Compile(Set{Rules: []Rule{{
		Conditions: []Condition{{Field: FieldFiletype, Op: OpMatches, Value: pattern}},
		Action:     Action{PackageName: "Video"},
	}}})
	if len(problems) > 0 {
		t.Fatal(problems)
	}
	cases := []struct {
		name string
		want bool
	}{
		{"a.mkv", true},
		{"a.MKV", true}, // hosters upper-case names; a category has to survive it
		{"a.ts", true},
		{"a.mts", false}, // the near miss the anchors exist for
		{"a.mp3", false},
		{"a.mkv.txt", false}, // the extension is the last one, not a substring
	}
	for _, c := range cases {
		e := m.Apply(Candidate{Filename: c.name, URL: "https://x.example/" + c.name})
		if matched := e.Package == "Video"; matched != c.want {
			t.Errorf("%s matched=%v, want %v", c.name, matched, c.want)
		}
	}
}

// TestCategoryOfRecognisesWhatCategoryPatternWrote is what lets the editor
// reopen a rule showing the chip it was made with — and, once somebody edits the
// pattern, stop showing a chip that no longer says what the rule does.
func TestCategoryOfRecognisesWhatCategoryPatternWrote(t *testing.T) {
	for _, cat := range Categories() {
		if got := CategoryOf(cat.Pattern); got != cat.ID {
			t.Errorf("CategoryOf(%q) = %q, want %q", cat.Pattern, got, cat.ID)
		}
	}
	edited := strings.Replace(mustPattern(t, "video"), "|mkv", "", 1)
	if got := CategoryOf(edited); got != "" {
		t.Errorf("an edited pattern still reads as category %q, so the editor would hide the user's own change", got)
	}
	if _, ok := CategoryPattern("nonsense"); ok {
		t.Error("an unknown category answered with a pattern, which would match everything or nothing at random")
	}
}

func mustPattern(t *testing.T, id string) string {
	t.Helper()
	p, ok := CategoryPattern(id)
	if !ok {
		t.Fatalf("category %q is missing", id)
	}
	return p
}
