package federation

// Turning a name a person chose for THEMSELVES into one this package can
// address a peer by.
//
// nameRe is deliberately narrow: a peer name is a path segment
// (/api/instances/{name}/...), a key in instances.json and a label in the UI,
// and widening it would widen all three at once. The cost was invisible until
// something started deriving a peer name automatically rather than asking for
// one: an instance called "Bürglers Keller", or one on a host whose name runs
// past 32 characters, could not be added as a peer AT ALL - not by pairing
// (pairingSelf hands instanceDisplayName straight over), and not from the
// discovery card. The rejection surfaced as "federation: invalid instance
// name" on the far side, about a name the user never typed and cannot see.
//
// So: the rule stays, and everything that DERIVES a name runs it through
// SanitiseName first. Where a person types the name themselves - the manual
// add form - the rule is still enforced as written, because there the error
// message lands next to the field that caused it and is the fastest way to
// learn the rule.

import (
	"strings"
	"unicode"

	"golang.org/x/text/runes"
	"golang.org/x/text/transform"
	"golang.org/x/text/unicode/norm"
)

// maxNameLen matches nameRe's own ceiling: one leading character plus 31 more.
const maxNameLen = 32

// deAccent decomposes and drops combining marks, so "Bürglers" becomes
// "Burglers" rather than "B-rglers". Built once - the transformer is stateful,
// so it is cloned per call rather than shared.
var deAccent = runes.Remove(runes.In(unicode.Mn))

// standIn covers the letters that decomposition CANNOT help with, because they
// are distinct letters rather than an ASCII letter plus a mark: Nordic, Polish
// and Icelandic ones, and the German sharp s. Without this, Ærø sanitises to
// "r" - a Danish instance losing most of its name to a rule about URL
// segments. Everything here uses the transliteration its own language already
// uses when ASCII is all that is available.
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

// SanitiseName turns any string into one nameRe accepts.
//
// Accents are folded rather than replaced, so a European name survives as
// something its owner recognises. Anything left that the rule does not permit
// becomes a hyphen, runs of hyphens collapse, and the result is trimmed to fit.
// A string with nothing usable in it at all returns "", which callers treat as
// "no name to offer" rather than substituting an invented one - a peer called
// "instance-1" that the user never chose is worse than an honest refusal.
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
			// Permitted verbatim by nameRe, but a run of them is noise.
			if !lastHyphen {
				b.WriteRune(r)
				lastHyphen = true
			}
		default:
			// Anything else - CJK, emoji, punctuation, a control character -
			// has no ASCII form to fold to. One hyphen stands in for a run of
			// them, so a fully non-Latin name collapses to "" below rather
			// than to a row of dashes.
			if !lastHyphen && b.Len() > 0 {
				b.WriteRune('-')
				lastHyphen = true
			}
		}
	}

	out := strings.Trim(b.String(), " _.-")
	// The rule's first character must be alphanumeric, and the whole thing has
	// to fit. Trimmed again after cutting, in case the cut landed on a space.
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
