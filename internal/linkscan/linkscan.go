// Package linkscan finds the links inside whatever a person actually pastes,
// drops or forwards, so every intake path can hand it a raw blob instead of
// each inventing its own splitting rule.
//
// A paste box rarely holds a clean one-URL-per-line list: a forum post wraps
// a link in a sentence, a chat client lets several links share one line, and
// a mail client hard-wraps a long one across two. Extract handles all three,
// plus the case a scheme-anchored scan cannot see at all: a bare "host/path"
// with no http(s):// in front of it. That fallback mirrors JDownloader's own
// last resort - its AddLinksDialog retries the whole pasted text with
// "http://" glued on the front when a first pass finds nothing - verified
// against JDownloader's own source (AddLinksDialog.asyncAnalyse) rather than
// assumed.
package linkscan

import (
	"regexp"
	"strings"
	"unicode/utf8"
)

// byteOrderMark leads every text file Windows writes. Left in place it fuses
// with the first link and makes exactly one link per paste fail - the kind
// of bug that gets blamed on the site rather than on the leading three bytes
// nobody can see. Built from its code point rather than typed as a literal:
// a literal zero-width character sitting in a .go file is invisible in a
// diff and indistinguishable from an editor mistake.
var byteOrderMark = string(rune(0xFEFF))

// schemes are the entrances this app can act on, earliest-match-wins order
// does not depend on this slice's order since nextScheme checks all three.
// ftp is deliberately absent: every resolver under internal/resolver refuses
// it already, so finding one here would only stage a task certain to fail at
// resolve time with a worse, later error than simply never finding it.
var schemes = []string{"https://", "http://", "magnet:?"}

// bareHost matches a line that IS a domain and an optional path and nothing
// else: the fallback for a paste that named a host with no scheme at all.
//
// The final label must be alphabetic. A numeric one is far more often a
// version string ("2.0.1") than a host, and requiring letters there rejects
// it for free. The trade is a known, accepted one: "update.zip" reads as a
// host named "update" under the real ".zip" gTLD, indistinguishable from a
// bare filename by spelling alone - JD's own last-resort retry has exactly
// the same blind spot, and nothing short of asking the user closes it.
var bareHost = regexp.MustCompile(`(?i)^(?:[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?\.)+[a-z]{2,24}(?::[0-9]{1,5})?(?:/\S*)?$`)

// maxJoin bounds how long a rejoined line may grow. No legitimate URL runs
// anywhere near this; the cap exists so that a paste built entirely of many
// short "continuing" lines cannot turn the rejoin step itself into the slow
// part of handling it.
const maxJoin = 4096

// Extract scans blob for links, in first-seen order, none repeated.
//
// It runs two passes per logical line: a scheme-anchored scan first, because
// a token found that way is unambiguous, and only when that finds nothing is
// the whole line tried against bareHost. Mixing the two scopes this way -
// scanning for a scheme mid-prose, but only trying a bare host against a
// WHOLE line - is deliberate: a bare domain floating inside a sentence
// ("visit example.org for details") is exactly the false-positive case a
// download manager cannot afford, while a line that is nothing BUT a domain
// is what "fall back to line-splitting" means.
func Extract(blob string) []string {
	blob = strings.TrimPrefix(blob, byteOrderMark)

	var out []string
	seen := make(map[string]bool)
	add := func(s string) {
		if s == "" || seen[s] {
			return
		}
		seen[s] = true
		out = append(out, s)
	}

	for _, line := range logicalLines(blob) {
		found := scanTokens(line)
		if len(found) == 0 {
			if u, ok := bareFallback(line); ok {
				found = []string{u}
			}
		}
		for _, u := range found {
			add(u)
		}
	}
	return out
}

// logicalLines splits blob on real line breaks, then rejoins a break a mail
// client inserted mid-URL back into the line it broke.
//
// Quoted-printable's own soft break (a trailing "=" right before the
// newline, RFC 2045) is undone unconditionally first, on the reasoning that
// a genuine soft break is far more common than a URL whose own query string
// happens to end a display line on a base64 padding "=" - the one case this
// costs a trailing character from, and only when a paste breaks a line at
// exactly that byte. What is left after that is heuristic on purpose - a
// wrapped URL carries no marker saying so. The rule kept is the narrowest
// one that still catches
// the common case: the previous line already contains a recognised scheme
// AND ends, with no trailing whitespace, in a character a URL can contain,
// AND the next line starts, with no leading whitespace, in one too. Prose
// that happens to end a line with a URL and then starts a new sentence flush
// left with another URL-shaped word is the one case this still joins
// wrongly; it is rare enough to accept against the alternative of a wrapped
// link that silently only half-extracts.
func logicalLines(blob string) []string {
	blob = strings.ReplaceAll(blob, "=\r\n", "")
	blob = strings.ReplaceAll(blob, "=\n", "")
	blob = strings.ReplaceAll(blob, "\r\n", "\n")
	blob = strings.ReplaceAll(blob, "\r", "\n")
	raw := strings.Split(blob, "\n")

	lines := make([]string, 0, len(raw))
	for _, r := range raw {
		if n := len(lines); n > 0 && len(lines[n-1]) < maxJoin && continuesURL(lines[n-1], r) {
			lines[n-1] += r
		} else {
			lines = append(lines, r)
		}
	}
	return lines
}

func continuesURL(prev, next string) bool {
	prev = strings.TrimRight(prev, " \t")
	if prev == "" || next == "" || !containsScheme(prev) {
		return false
	}
	// A line that starts a scheme of its own is a new link, never a
	// continuation of the one above - without this, two ordinary URLs
	// pasted one per line would fuse into one the moment the first happened
	// to end in a lower-case character, which is nearly always.
	if _, ok := startsScheme(next); ok {
		return false
	}
	last, _ := utf8.DecodeLastRuneInString(prev)
	if !isURLChar(last) {
		return false
	}
	first, _ := utf8.DecodeRuneInString(next)
	return continuationStart(first)
}

func startsScheme(s string) (string, bool) {
	for _, sch := range schemes {
		if len(s) >= len(sch) && strings.EqualFold(s[:len(sch)], sch) {
			return sch, true
		}
	}
	return "", false
}

// continuationStart is stricter than isURLChar: it is the set a wrapped
// URL's path or query plausibly resumes with, deliberately minus capital
// ASCII letters. A URL's own path is conventionally lower-case, digits and
// symbols, while prose in virtually every Latin-script language capitalises
// the first letter of a new sentence - so requiring lower-case here is what
// stops an email's own closing line ("Thanks!", "Best regards,") sitting
// flush against a wrapped link above it from being read as more of that
// link. The cost is the rare wrap that genuinely breaks right before an
// upper-case path segment, which is accepted rather than chased: nothing
// short of understanding the sentence can tell the two apart for certain.
func continuationStart(r rune) bool {
	if r >= 'A' && r <= 'Z' {
		return false
	}
	return isURLChar(r)
}

func containsScheme(s string) bool {
	for _, sch := range schemes {
		if indexFold(s, sch) >= 0 {
			return true
		}
	}
	return false
}

// scanTokens finds every scheme-anchored link on one line, in order.
func scanTokens(line string) []string {
	var out []string
	pos := 0
	for pos < len(line) {
		start, schemeLen := nextScheme(line, pos)
		if start < 0 {
			break
		}
		end := start + schemeLen
		for end < len(line) {
			r, size := utf8.DecodeRuneInString(line[end:])
			if !isURLChar(r) {
				break
			}
			end += size
		}
		if tok := trimToken(line[start:end]); len(tok) > schemeLen {
			out = append(out, tok)
		}
		pos = end
	}
	return out
}

// nextScheme finds the earliest recognised scheme at or after from, folding
// case: a site's own CnL button and a pasted mail signature both spell it
// every which way.
func nextScheme(s string, from int) (start, length int) {
	start = -1
	for _, sch := range schemes {
		i := indexFold(s[from:], sch)
		if i < 0 {
			continue
		}
		if abs := from + i; start == -1 || abs < start {
			start, length = abs, len(sch)
		}
	}
	return start, length
}

// indexFold is strings.Index with ASCII case folding, kept local rather than
// lower-casing the whole line first: strings.ToLower can change a string's
// byte length for some runes, which would desync every index this function
// returns from the original (correctly-cased) bytes callers slice out of.
func indexFold(s, sub string) int {
	n := len(sub)
	if n == 0 || n > len(s) {
		return -1
	}
	for i := 0; i+n <= len(s); i++ {
		if strings.EqualFold(s[i:i+n], sub) {
			return i
		}
	}
	return -1
}

func isURLChar(r rune) bool {
	switch {
	case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
		return true
	case r > 127:
		// Generous on purpose: an IRI pasted in its own script must not be
		// truncated mid-character just because this app's schemes are ASCII.
		return true
	}
	switch r {
	case '-', '.', '_', '~', ':', '/', '?', '#', '[', ']', '@', '!', '$', '&', '\'', '(', ')', '*', '+', ',', ';', '=', '%':
		return true
	}
	return false
}

// trailingPunct is what prose puts right after a link and a URL never ends
// in: sentence and clause punctuation, plus the markdown/chat wrapping
// (*emphasis*, `code`) a link is routinely typed inside.
const trailingPunct = ".,;:!?'\"*`‘’“”"

type bracketPair struct{ open, close byte }

var brackets = []bracketPair{{'(', ')'}, {'[', ']'}, {'{', '}'}}

// trimToken strips what prose wrapped around a link and leaves what is
// balanced alone: a Wikipedia URL ending "_(disambiguation)" keeps its
// closing paren, because the token also holds the opening one, while
// "(see https://example.org/page)" loses its, because the token does not -
// the scan started at "https", after the site's own opening paren.
//
// Bracket counts are taken once, up front, and only ever decremented while
// stripping: recomputing strings.Count on every character considered would
// make a token with a long run of trailing brackets cost time quadratic in
// that run's length for no reason.
func trimToken(tok string) string {
	counts := make([]int, len(brackets)*2)
	for i, b := range brackets {
		counts[i*2] = strings.Count(tok, string(b.open))
		counts[i*2+1] = strings.Count(tok, string(b.close))
	}
	for {
		r, size := utf8.DecodeLastRuneInString(tok)
		if size == 0 {
			return tok
		}
		stripped := false
		if r < 128 {
			for i, b := range brackets {
				if byte(r) == b.close && counts[i*2+1] > counts[i*2] {
					counts[i*2+1]--
					stripped = true
					break
				}
			}
		}
		if !stripped && strings.ContainsRune(trailingPunct, r) {
			stripped = true
		}
		if !stripped {
			return tok
		}
		tok = tok[:len(tok)-size]
	}
}

// bareFallback tries a whole trimmed line as an unscheme'd host. Only ever
// called after scanTokens found nothing on that line, so there is no
// scheme anywhere on it to anchor a narrower match to.
func bareFallback(line string) (string, bool) {
	trimmed := strings.Trim(strings.TrimSpace(line), `<>"'`)
	if trimmed == "" || !bareHost.MatchString(trimmed) {
		return "", false
	}
	return "https://" + trimmed, true
}
