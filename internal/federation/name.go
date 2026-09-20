package federation

// nameRe is narrow because a peer name is a URL path segment, a key in
// instances.json and a UI label at once. A name typed by a person is checked
// against it as is; a name derived from elsewhere (an instance's display
// name for pairing or discovery) goes through SanitiseName first, or a name
// such as "Bürglers Keller" could not be added at all.

import (
	"strings"
	"unicode"

	"golang.org/x/text/runes"
	"golang.org/x/text/transform"
	"golang.org/x/text/unicode/norm"
)

// maxNameLen matches nameRe's own ceiling: one leading character plus 31 more.
const maxNameLen = 32

// deAccent drops combining marks after decomposition, so "Bürglers" becomes
// "Burglers".
var deAccent = runes.Remove(runes.In(unicode.Mn))

// standIn covers letters that decomposition cannot reduce to ASCII, using
// each language's own ASCII transliteration. Without it "Ærø" would become
// "r".
var standIn = map[rune]string{
	'æ': "ae", 'Æ': "AE",
	'ø': "o", 'Ø': "O",
	'ß': "ss", 'ẞ': "SS",
	'ð': "d", 'Ð': "D",
	'þ': "th", 'Þ': "TH",
	'ł': "l", 'Ł': "L",
	'đ': "d", 'Đ': "D",
	'ı': "i", 'œ': "oe", 'Œ': "OE",
}

// SanitiseName turns any string into one nameRe accepts. Accents are folded,
// anything else not permitted becomes a single hyphen, and the result is cut
// to fit. It returns "" when nothing usable is left, and callers then offer no
// name rather than inventing one.
func SanitiseName(s string) string {
	folded, _, err := transform.String(transform.Chain(norm.NFD, deAccent, norm.NFC), s)
	if err != nil {
		folded = s
	}

	var b strings.Builder
	lastHyphen := false
	for _, r := range folded {
		switch {
		case r < unicode.MaxASCII && (unicode.IsLetter(r) || unicode.IsDigit(r)):
			b.WriteRune(r)
			lastHyphen = false
		case standIn[r] != "":
			b.WriteString(standIn[r])
			lastHyphen = false
		case r == ' ' || r == '_' || r == '.' || r == '-':
			// Allowed by nameRe, but runs of them collapse.
			if !lastHyphen {
				b.WriteRune(r)
				lastHyphen = true
			}
		default:
			// No ASCII form (CJK, emoji, punctuation, controls). A run becomes
			// one hyphen, and a wholly non-Latin name ends up "".
			if !lastHyphen && b.Len() > 0 {
				b.WriteRune('-')
				lastHyphen = true
			}
		}
	}

	out := strings.Trim(b.String(), " _.-")
	// The first character must be alphanumeric; trim again after cutting in
	// case the cut landed on a separator.
	for out != "" && !isAlnumASCII(rune(out[0])) {
		out = out[1:]
	}
	if len(out) > maxNameLen {
		out = strings.Trim(out[:maxNameLen], " _.-")
	}
	if !nameRe.MatchString(out) {
		return ""
	}
	return out
}

func isAlnumASCII(r rune) bool {
	return (r >= '0' && r <= '9') || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z')
}
