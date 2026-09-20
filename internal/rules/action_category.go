package rules

// Whether an Action.Category names an existing category is checked by
// settings.ValidateCategories at save time, since settings imports this
// package and not the other way round. A missing category is refused there,
// unlike a missing header profile (headers.go): the whole category table is
// in the document being saved, and a rule filing links under an unknown id
// would silently do nothing. This file only refuses values that could never
// name a category, following settings.CategoryID.

import (
	"fmt"
	"strings"
	"unicode"
)

// MaxCategoryRef bounds the id a rule may name. It matches
// settings.MaxCategoryID, so a longer string could never address a category.
const MaxCategoryRef = 64

// categoryProblem reports why a category id cannot be used, or "" when it can.
// An empty id means the rule has no opinion.
func categoryProblem(raw string) string {
	id := strings.TrimSpace(raw)
	if id == "" {
		return ""
	}
	if len(id) > MaxCategoryRef {
		return fmt.Sprintf("the category id is %d characters, the limit is %d", len(id), MaxCategoryRef)
	}
	// A template would let a link's own host pick its category, and with it
	// the download folder, priority and collision rule. An expanded id also
	// cannot be validated at save time, so a miss would fail silently.
	if strings.Contains(strings.ToLower(id), openTag) {
		return fmt.Sprintf(
			"the category %q holds a %s... placeholder; a drawer is picked by name, not assembled from the link", id, openTag)
	}
	// Pure punctuation normalises to nothing and can never match a stored id.
	if !strings.ContainsFunc(id, func(r rune) bool { return unicode.IsLetter(r) || unicode.IsDigit(r) }) {
		return fmt.Sprintf("the category %q holds no letter or digit, so it can never name one", id)
	}
	return ""
}
