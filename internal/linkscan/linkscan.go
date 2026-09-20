// Package linkscan finds the links inside whatever a person actually pastes,
// drops or forwards, so every intake path can hand it a raw blob instead of
// each inventing its own splitting rule.
//
// A paste box rarely holds a clean one-URL-per-line list: a forum post wraps
// a link in a sentence, a chat client lets several links share one line, and
// a mail client hard-wraps a long one across two. Extract handles all three,
// plus the case a scheme-anchored scan cannot see at all: a bare "host/path"
// with no http(s):// in front of it. That fallback follows JDownloader's own
// last resort, which retries the pasted text with "http://" glued on the
// front when a first pass finds nothing (AddLinksDialog.asyncAnalyse).
package linkscan

import (
	"regexp"
	"strings"
	"unicode/utf8"
)

// byteOrderMark leads every text file Windows writes. Left in place it fuses
// with the first link and makes one link per paste fail. Built from its code
// point rather than typed as a literal, which would be invisible in a diff.
var byteOrderMark = string(rune(0xFEFF))

// schemes are the entrances this app can act on. The slice's order does not
// matter, since nextScheme checks all three and the earliest match wins. ftp
// is absent because every resolver under internal/resolver refuses it, so
// finding one here would only stage a task that fails later with a worse
// error.
var schemes = []string{"https://", "http://", "magnet:?"}

// bareHost matches a line that is a domain and an optional path and nothing
// else, the fallback for a paste that named a host with no scheme.
//
// The final label must be alphabetic, because a numeric one is more often a
// version string ("2.0.1") than a host. The accepted cost is that
// "update.zip" reads as a host under the real ".zip" gTLD, which spelling
// alone cannot tell from a filename.
var bareHost = regexp.MustCompile(`(?i)^(?:[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?\.)+[a-z]{2,24}(?::[0-9]{1,5})?(?:/\S*)?$`)

// maxJoin bounds how long a rejoined line may grow, so a paste built of many
// short continuing lines cannot make the rejoin the slow part. No legitimate
// URL runs anywhere near it.
const maxJoin = 4096

// Extract scans blob for links, in first-seen order, none repeated.
//
// It runs two passes per logical line: a scheme-anchored scan first, because
// a token found that way is unambiguous, and only when that finds nothing is
// the whole line tried against bareHost. The scopes differ on purpose. A
// scheme is looked for mid-prose, a bare host only against a whole line,
// because a domain floating inside a sentence ("visit example.org for
// details") is the false positive a download manager cannot afford.
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
// Quoted-printable's soft break (a trailing "=" before the newline, RFC 2045)
// is undone first and unconditionally: a genuine soft break is far more
// common than a URL whose query string ends a display line on a base64
// padding "=", which is the only case this costs a character.
//
// The rest is heuristic, because a wrapped URL carries no marker saying so.
// The rule kept is the narrowest that still catches the common case: the
// previous line contains a recognised scheme and ends, with no trailing
// whitespace, in a character a URL can contain, and the next line starts,
// with no leading whitespace, in one too. Prose that ends a line with a URL
// and starts the next flush left with a URL-shaped word is still joined
// wrongly, which is rarer than a wrapped link that half-extracts.
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
	// A line that starts a scheme of its own is a new link. Without this,
	// two URLs pasted one per line fuse as soon as the first ends in a
	// lower-case character, which is nearly always.
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

// continuationStart is stricter than isURLChar: the set a wrapped URL's path
// or query plausibly resumes with, minus capital ASCII letters. A path is
// conventionally lower-case, digits and symbols, while Latin-script prose
// capitalises the first letter of a sentence, so requiring lower-case stops
// an email's closing line ("Thanks!", "Best regards,") from being read as
// more of the link above it. The cost is a wrap that breaks right before an
// upper-case path segment.
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
		// An IRI pasted in its own script must not be truncated
		// mid-character because this app's schemes are ASCII.
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
// closing paren because the token holds the opening one too, while
// "(see https://example.org/page)" loses its, since the scan started at
// "https", after the site's opening paren.
//
// Bracket counts are taken once and only decremented while stripping.
// Recomputing strings.Count per character would cost time quadratic in the
// length of a long run of trailing brackets.
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
