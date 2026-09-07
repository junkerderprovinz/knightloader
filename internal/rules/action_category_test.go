package rules

// The category action: filing a link in one of settings' named drawers.
//
// What this file may check is bounded by what the package can see. Whether the
// drawer EXISTS is settings.ValidateCategories' question - settings imports
// this package, so this one cannot import settings - and the tests for that
// live beside it. Here: that the id survives a match unchanged, that it behaves
// like every other action field when several rules have an opinion, and that
// the two values which could never name a drawer are refused at compile time
// rather than failing silently on every link.

import (
	"strings"
	"testing"
)

// TestARuleFilesALinkInADrawer is the feature at its simplest, and the one
// claim everything else rests on: what the rule wrote is what the caller reads.
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

// TestTheIDIsNotExpanded is the check with teeth. Every other string on an
// Action is a template, so <jd:hoster> in a category is the natural thing to
// try - and it would hand the link's own host the choice of which drawer it
// lands in, which is to say its download folder, its priority and its collision
// rule. The far end of the connection does not get a vote on where its bytes
// are written.
//
// Refused at compile time rather than left to expand, because an id assembled
// at match time cannot be checked against the table by anything: it does not
// exist until a link arrives, and a link whose expansion names no drawer is
// simply not filed anywhere, with no error to see.
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

// TestADrawerNothingCouldNameIsRefused covers the two remaining shapes that can
// never match a stored id no matter what the table holds: one too long to
// address one, and one made entirely of punctuation, which normalises to
// nothing on the settings side.
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

// TestAnEmptyCategoryIsNotAProblem is the ordinary case: almost every rule ever
// written says nothing about a drawer, and a rule with a problem is DROPPED
// WHOLE by Compile - so a false alarm here would stop applying the folder and
// the package name that rule also sets.
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

// TestTheLastRuleToNameADrawerWins is the per-field "later rule wins" the whole
// Packagizer works by, checked on this field specifically. The second half is
// the one that would break quietly: an empty category is "this rule has no
// opinion", never "clear it", so a rule setting only the folder must leave an
// earlier rule's drawer standing.
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

// TestACategoryIsNotTheFiletypeCategory is the word collision written down as a
// test, because this is the one mistake somebody reading only half of it will
// make. rules.Category is a group of extensions that expands into a filetype
// condition; Action.Category is a drawer of defaults a task is filed in. They
// share a word, they are not related, and neither could be renamed without
// breaking a stored rule set or the grammar the editor is built from.
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
	// And the condition still recognises itself as the filetype shorthand, so
	// the editor reopens the rule showing the chip rather than the extensions.
	if got := CategoryOf(pattern); got != "video" {
		t.Errorf("CategoryOf = %q, want the filetype category %q", got, "video")
	}
}

// TestTheGrammarStillDescribesNoCategoryAction is a guard rather than a
// feature, and it is here so that adding the action to the editor is a
// deliberate act.
//
// web/src/components/RuleEditor.tsx switches on ActionGrammar.Kind and FALLS
// THROUGH to the reject control for a kind it does not know. Describing this
// action today would therefore not put a missing control on the Packagizer tab,
// it would put a working accept/reject switch there, wired to Action.Reject - a
// control that quietly edits the wrong field, which is the exact failure
// grammar.go exists to prevent and the same trap Action.Headers is already held
// out of the list by.
//
// The line to add, once that renderer has a branch that offers the stored
// category list, is:
//
//	{ID: "category", Kind: "category", Flavour: "packagizer", Max: intPtr(MaxCategoryRef)},
func TestTheGrammarStillDescribesNoCategoryAction(t *testing.T) {
	for _, a := range Describe().Actions {
		if a.ID == "category" && a.Kind != "category" {
			t.Fatalf("the category action is described as kind %q; the editor renders an unknown kind as a reject switch", a.Kind)
		}
	}
}
