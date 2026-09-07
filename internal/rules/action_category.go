package rules

// action_category.go: the one thing this package checks about Action.Category.
//
// The drawer itself lives in internal/settings (settings.Category), and this
// package deliberately does not import it - it could not, settings imports
// this one. So the table cannot be consulted here, and "does this category
// exist" is answered where both halves are in scope, by
// settings.ValidateCategories, which refuses the save and names the rule.
//
// That split is the same one headers.go already makes, arrived at from the
// opposite direction and worth reading beside it. A missing HEADER profile is
// left alone here AND there, because a rule naming a profile somebody has not
// pasted a credential for yet is a rule mid-construction, and Compile drops a
// rule with any problem at all - so refusing it would also stop applying the
// folder and the package name that same rule sets. A missing CATEGORY is
// refused, but at save time and by the settings package: nothing about it is
// half-finished, the whole table is in the same document being saved, and a
// rule filing links in a drawer that does not exist does nothing on every link
// it matches, for ever, with no error anywhere.
//
// What is checked here is the SHAPE, and the shape is settings.CategoryID's:
// this file's job is to refuse the values that could never name a drawer no
// matter what the table holds.

import (
	"fmt"
	"strings"
	"unicode"
)

// MaxCategoryRef bounds the id a rule may name. It matches
// settings.MaxCategoryID; a longer string cannot address a stored category, so
// accepting it here would produce a rule that compiles cleanly and can never do
// anything.
const MaxCategoryRef = 64

// categoryProblem reports why a category id cannot be used, or "" when it can.
// An empty id is not a problem: it is the ordinary "this rule has no opinion
// about which drawer".
func categoryProblem(raw string) string {
	id := strings.TrimSpace(raw)
	if id == "" {
		return ""
	}
	if len(id) > MaxCategoryRef {
		return fmt.Sprintf("the category id is %d characters, the limit is %d", len(id), MaxCategoryRef)
	}
	// A TEMPLATE IS REFUSED, and this is the check worth having. Every other
	// string on an Action is expanded, so <jd:hoster> in a category id is the
	// natural thing to try - and it would hand the link's own host the choice
	// of which drawer it lands in, which is to say its download folder, its
	// priority and its collision rule. The far end of the connection does not
	// get a vote on where its bytes are written.
	//
	// It would also be a silent no-op nearly every time: an expanded id has to
	// match a stored category exactly, settings.ValidateCategories cannot check
	// a value that does not exist until match time, and a link whose expansion
	// names nothing simply is not filed anywhere, with no error to see.
	if strings.Contains(strings.ToLower(id), openTag) {
		return fmt.Sprintf(
			"the category %q holds a %s... placeholder; a drawer is picked by name, not assembled from the link", id, openTag)
	}
	// A value made entirely of punctuation normalises to nothing, so it can
	// never match a stored id. Refused here rather than left to fail silently
	// at match time, which is where every unmatched id fails.
	if !strings.ContainsFunc(id, func(r rune) bool { return unicode.IsLetter(r) || unicode.IsDigit(r) }) {
		return fmt.Sprintf("the category %q holds no letter or digit, so it can never name one", id)
	}
	return ""
}
