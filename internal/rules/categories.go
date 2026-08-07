package rules

// File-type categories: "is this a video" without making the user list twenty
// extensions and get one of them wrong.
//
// A category is deliberately NOT a new operator. It expands, in the editor, into
// an ordinary `filetype matches <pattern>` condition and is stored as one, so:
//
//   - the engine gains nothing to maintain, and a rule written through the
//     category picker is the same rule as one typed by hand;
//   - an exported rule set is readable by anything that understands the plain
//     grammar, including a future client that never heard of categories;
//   - a user can start from a category and then edit the pattern, which is the
//     usual next thing somebody wants and would be impossible if the category
//     were an opaque token the engine resolved at match time.
//
// The cost is that a category cannot be widened after the fact — adding a new
// extension here does not change rules already saved. That is the right trade:
// a stored rule quietly changing what it matches because the binary was updated
// is exactly the kind of invisible change this package exists to avoid.

import (
	"sort"
	"strings"
)

// Category is one named group of extensions, together with the condition value
// it stands for.
type Category struct {
	ID string `json:"id"`
	// Extensions is what the category covers, without dots, lower case, in the
	// order the editor should list them. It is shown so somebody can check
	// whether their format is in there before trusting the chip.
	Extensions []string `json:"extensions"`
	// Pattern is the exact value the editor writes into Condition.Value with
	// OpMatches on FieldFiletype. Generated here rather than in the interface so
	// that recognising a stored rule as a category is a string comparison against
	// this same field, and cannot drift into "nearly the same pattern".
	Pattern string `json:"pattern"`
}

// categoryExtensions is the whole of the vocabulary. Case folding is handled by
// the pattern's own (?i), so everything here is lower case.
//
// Order inside a group is roughly by how often the extension turns up, because
// the list is also what the editor prints next to the chip and the first few
// entries are what somebody actually reads.
var categoryExtensions = map[string][]string{
	"video":    {"mkv", "mp4", "avi", "mov", "m4v", "mpg", "mpeg", "wmv", "flv", "webm", "ts", "m2ts", "vob", "ogv", "divx", "rmvb"},
	"audio":    {"mp3", "flac", "m4a", "aac", "ogg", "opus", "wav", "wma", "alac", "aiff", "ape", "dsf", "mka"},
	"image":    {"jpg", "jpeg", "png", "gif", "webp", "bmp", "tif", "tiff", "heic", "avif", "svg", "raw", "cr2", "nef"},
	"archive":  {"rar", "zip", "7z", "tar", "gz", "bz2", "xz", "zst", "tgz", "lzma", "arj", "cab", "ace"},
	"document": {"pdf", "epub", "mobi", "azw3", "djvu", "cbz", "cbr", "doc", "docx", "odt", "rtf", "txt", "xls", "xlsx", "ods", "ppt", "pptx"},
	"subtitle": {"srt", "sub", "idx", "ass", "ssa", "vtt", "sup"},
	"disc":     {"iso", "img", "nrg", "mdf", "cue", "bin"},
	"program":  {"exe", "msi", "apk", "dmg", "pkg", "deb", "rpm", "appimage", "jar"},
}

// categoryOrder is the order the picker lists them in: what a download manager
// mostly handles first. A map's iteration order would reshuffle the picker on
// every process start, which reads as a rendering fault.
var categoryOrder = []string{"video", "audio", "image", "archive", "document", "subtitle", "disc", "program"}

// Categories is every category, in menu order.
func Categories() []Category {
	out := make([]Category, 0, len(categoryOrder))
	for _, id := range categoryOrder {
		exts := categoryExtensions[id]
		out = append(out, Category{
			ID:         id,
			Extensions: exts,
			Pattern:    categoryPattern(exts),
		})
	}
	return out
}

// CategoryPattern is one category's condition value. The second result is false
// for an unknown id, so a client asking for a category this build does not have
// gets nothing rather than a pattern that matches everything.
func CategoryPattern(id string) (string, bool) {
	exts, ok := categoryExtensions[strings.ToLower(strings.TrimSpace(id))]
	if !ok {
		return "", false
	}
	return categoryPattern(exts), true
}

// CategoryOf names the category a stored condition value came from, or "" when
// the pattern was written or edited by hand. It is what lets the editor reopen a
// rule showing the chip it was created with instead of a wall of extensions —
// and, just as importantly, show the raw pattern once somebody has changed it,
// rather than a chip that no longer says what the rule does.
func CategoryOf(pattern string) string {
	for _, id := range categoryOrder {
		if categoryPattern(categoryExtensions[id]) == pattern {
			return id
		}
	}
	return ""
}

// categoryPattern builds the anchored, case-insensitive alternation.
//
// Anchored at both ends because FieldFiletype holds the extension alone: without
// the anchors "ts" would match "mts" and a category would quietly pull in
// neighbouring formats. (?i) because OpMatches is the one operator that does not
// fold case — the pattern carries its own flags, and a category has to work on
// ".MKV" from a hoster that upper-cases everything.
func categoryPattern(exts []string) string {
	// Sorted so the pattern is a pure function of the set and not of the order
	// somebody happened to type the extensions in above: CategoryOf compares
	// strings, and a reordered list here would stop recognising every rule
	// already saved.
	sorted := append([]string(nil), exts...)
	sort.Strings(sorted)
	return `(?i)^(` + strings.Join(sorted, "|") + `)$`
}
