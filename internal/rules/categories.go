package rules

// File-type categories let a user say "is this a video" without listing twenty
// extensions. A category is not an operator: the editor expands it into an
// ordinary `filetype matches <pattern>` condition and stores it that way, so
// the engine has nothing extra to maintain, exported rule sets stay in the
// plain grammar, and a user can start from a category and edit the pattern.
// The cost is that adding an extension here does not change rules already
// saved, which is preferable to a stored rule changing what it matches on an
// update.

import (
	"sort"
	"strings"
)

// Category is one named group of extensions, with the condition value it
// stands for.
type Category struct {
	ID string `json:"id"`
	// Extensions is what the category covers, lower case without dots, in the
	// order the editor lists them.
	Extensions []string `json:"extensions"`
	// Pattern is the exact value the editor writes into Condition.Value with
	// OpMatches on FieldFiletype. It is generated here so recognising a stored
	// rule as a category is a plain string comparison.
	Pattern string `json:"pattern"`
}

// categoryExtensions is the vocabulary, lower case since the pattern carries
// (?i). Each group starts with the most common extensions, because the editor
// prints the list next to the chip.
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

// categoryOrder is the order the picker lists the categories in.
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

// CategoryPattern is one category's condition value. The second result is
// false for an unknown id.
func CategoryPattern(id string) (string, bool) {
	exts, ok := categoryExtensions[strings.ToLower(strings.TrimSpace(id))]
	if !ok {
		return "", false
	}
	return categoryPattern(exts), true
}

// CategoryOf names the category a stored condition value came from, or "" when
// the pattern was written or edited by hand. The editor uses it to reopen a
// rule with its chip, and to show the raw pattern once it has been changed.
func CategoryOf(pattern string) string {
	for _, id := range categoryOrder {
		if categoryPattern(categoryExtensions[id]) == pattern {
			return id
		}
	}
	return ""
}

// categoryPattern builds the anchored, case-insensitive alternation. The
// anchors keep "ts" from matching "mts", and (?i) is needed because OpMatches
// does not fold case.
func categoryPattern(exts []string) string {
	// Sorted so reordering the list above does not stop CategoryOf from
	// recognising saved rules.
	sorted := append([]string(nil), exts...)
	sort.Strings(sorted)
	return `(?i)^(` + strings.Join(sorted, "|") + `)$`
}
