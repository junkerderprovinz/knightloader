package rules

// Whether a category exists is checked by settings.ValidateCategories and
// tested there. Here: the id survives a match unchanged, behaves like every
// other action field across several rules, and the values that could never
// name a category are refused at compile time.

import (
	"strings"
	"testing"
)

func TestARuleFilesALinkInADrawer(t *testing.T) {
	m, probs := Compile(Set{Rules: []Rule{{
		Name:       "Serien",
		Conditions: []Condition{{Field: FieldFilename, Op: OpContains, Value: "s01e"}},
		Action:     Action{Category: "serien"},
	}}})
	if len(probs) != 0 {
		t.Fatalf("the rule did not compile: %v", probs)
	}
	if got := m.Apply(Candidate{Filename: "Doctor.Who.S01E03.mkv"}).Category; got != "serien" {
		t.Errorf("category = %q, want %q", got, "serien")
	}
	if got := m.Apply(Candidate{Filename: "holiday.mp4"}).Category; got != "" {
		t.Errorf("a link the rule does not match came back filed in %q", got)
	}
}

// TestTheIDIsNotExpanded: a template would let the link's host pick its own
// category, and an id assembled at match time could not be checked against
// the table.
func TestTheIDIsNotExpanded(t *testing.T) {
	_, probs := Compile(Set{Rules: []Rule{{
		Name:   "vom hoster",
		Action: Action{Category: "<jd:hoster>"},
	}}})
	if len(probs) != 1 {
		t.Fatalf("a category assembled from the link produced %d problems, want 1", len(probs))
	}
	if !strings.Contains(probs[0].Message, "<jd:") {
		t.Errorf("the message %q does not say what is wrong with it", probs[0].Message)
	}
}

// TestADrawerNothingCouldNameIsRefused covers an id too long to address a
// category and one of pure punctuation, which normalises to nothing on the
// settings side.
func TestADrawerNothingCouldNameIsRefused(t *testing.T) {
	for _, c := range []struct{ name, id string }{
		{"too long", strings.Repeat("a", MaxCategoryRef+1)},
		{"no letter or digit", "###"},
	} {
		t.Run(c.name, func(t *testing.T) {
			_, probs := Compile(Set{Rules: []Rule{{Name: c.name, Action: Action{Category: c.id}}}})
			if len(probs) != 1 {
				t.Fatalf("got %d problems, want 1: %v", len(probs), probs)
			}
		})
	}
}

// TestAnEmptyCategoryIsNotAProblem: Compile drops a rule with any problem, so a
// false alarm here would also drop the rule's folder and package name.
func TestAnEmptyCategoryIsNotAProblem(t *testing.T) {
	m, probs := Compile(Set{Rules: []Rule{{
		Name:   "nur ein ordner",
		Action: Action{DownloadDir: "/dl/x"},
	}}})
	if len(probs) != 0 {
		t.Fatalf("a rule with no category was refused: %v", probs)
	}
	if got := m.Apply(Candidate{Filename: "x.mkv"}).Dir; got != "/dl/x" {
		t.Errorf("dir = %q, want the rule's own folder", got)
	}
}

// TestTheLastRuleToNameADrawerWins: a later rule wins, and a rule setting only
// the folder leaves an earlier rule's category standing.
func TestTheLastRuleToNameADrawerWins(t *testing.T) {
	m, probs := Compile(Set{Rules: []Rule{
		{Name: "alles", Action: Action{Category: "misc"}},
		{Name: "serien", Action: Action{Category: "serien"}},
		{Name: "nur ordner", Action: Action{DownloadDir: "/dl/x"}},
	}})
	if len(probs) != 0 {
		t.Fatalf("the set did not compile: %v", probs)
	}
	e := m.Apply(Candidate{Filename: "x.mkv"})
	if e.Category != "serien" {
		t.Errorf("category = %q, want the later rule's %q", e.Category, "serien")
	}
	if e.Dir != "/dl/x" {
		t.Errorf("dir = %q, want the third rule to still have set it", e.Dir)
	}
}

// TestACategoryIsNotTheFiletypeCategory: rules.Category is a group of
// extensions that expands into a filetype condition, Action.Category files a
// task under a category of defaults, and one rule can use both.
func TestACategoryIsNotTheFiletypeCategory(t *testing.T) {
	pattern, ok := CategoryPattern("video")
	if !ok {
		t.Fatal("the filetype category vanished")
	}
	m, probs := Compile(Set{Rules: []Rule{{
		Name:       "Filme",
		Conditions: []Condition{{Field: FieldFiletype, Op: OpMatches, Value: pattern}},
		Action:     Action{Category: "filme"},
	}}})
	if len(probs) != 0 {
		t.Fatalf("a rule using both meanings at once did not compile: %v", probs)
	}
	e := m.Apply(Candidate{Filename: "film.mkv"})
	if e.Category != "filme" {
		t.Errorf("category = %q, want the drawer %q", e.Category, "filme")
	}
	// The condition still reads as the filetype shorthand, so the editor
	// reopens the rule with its chip.
	if got := CategoryOf(pattern); got != "video" {
		t.Errorf("CategoryOf = %q, want the filetype category %q", got, "video")
	}
}
