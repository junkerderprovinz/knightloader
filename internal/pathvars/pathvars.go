// Package pathvars expands JDownloader-style path variables, so a download
// folder can be written as "/downloads/<jd:packagename>/<jd:simpledate:yyyy-MM>"
// instead of a literal path. Every expanded value is sanitised to a single path
// segment, which means a template can only ever add the folder levels it spells
// out itself.
package pathvars

import (
	"strings"
	"time"
)

// Vars are the values a template can refer to.
type Vars struct {
	Package string
	Host    string // the hoster/domain the link came from
	Name    string // file name
	// Date is what the date placeholders format. Expand never reads the clock,
	// so the result stays a pure function of its input: a caller that wants
	// "now" substitutes it here, and the zero value formats as the zero time.
	Date time.Time
}

const (
	openTag  = "<jd:"
	closeTag = '>'
)

// maxSegment caps an expanded value. Long package names are common (release
// titles), and some filesystems reject a segment beyond 255 bytes.
const maxSegment = 120

// Expand replaces every <jd:...> placeholder in the template.
// Unknown placeholders are left untouched rather than blanked, so a typo is
// visible in the resulting path instead of silently producing "/downloads//".
func Expand(template string, v Vars) string {
	if !HasVars(template) {
		return template
	}
	var b strings.Builder
	b.Grow(len(template))
	rest := template
	for {
		start, end, ok := nextPlaceholder(rest)
		if !ok {
			b.WriteString(rest)
			return b.String()
		}
		b.WriteString(rest[:start])
		raw := rest[start:end]
		if value, known := lookup(raw[len(openTag):len(raw)-1], v); known {
			b.WriteString(value)
		} else {
			b.WriteString(raw)
		}
		rest = rest[end:]
	}
}

// HasVars reports whether a template contains any placeholder at all, so the
// caller can skip the whole machinery for a plain path.
func HasVars(template string) bool {
	_, _, ok := nextPlaceholder(template)
	return ok
}

// nextPlaceholder returns the half-open byte range of the next complete
// "<jd:...>" in s. The tag itself is matched case-insensitively because a
// hand-edited template may capitalise it, but the text inside keeps its case:
// date formats depend on it, MM and mm are not the same field.
func nextPlaceholder(s string) (start, end int, ok bool) {
	for i := strings.IndexByte(s, '<'); i >= 0; {
		if i+len(openTag) <= len(s) && strings.EqualFold(s[i:i+len(openTag)], openTag) {
			// An unterminated tag is not a placeholder; leaving it alone keeps
			// the broken template visible in the path.
			j := strings.IndexByte(s[i:], closeTag)
			if j < 0 {
				return 0, 0, false
			}
			return i, i + j + 1, true
		}
		next := strings.IndexByte(s[i+1:], '<')
		if next < 0 {
			return 0, 0, false
		}
		i += 1 + next
	}
	return 0, 0, false
}

// lookup resolves the text between "<jd:" and ">". The second result is false
// for anything unrecognised, which is what keeps a typo in the output. Each
// value carries its own fallback word, so a placeholder whose value sanitises
// away still contributes a named segment instead of an empty one.
func lookup(key string, v Vars) (string, bool) {
	if format, ok := cutPrefixFold(key, "simpledate:"); ok {
		return segment(formatDate(v.Date, format), "date"), true
	}
	switch strings.ToLower(strings.TrimSpace(key)) {
	case "packagename":
		return segment(v.Package, "package"), true
	case "hoster":
		return segment(v.Host, "hoster"), true
	case "filename":
		return segment(v.Name, "file"), true
	case "date":
		return segment(formatDate(v.Date, "yyyy-MM-dd"), "date"), true
	case "year":
		return segment(formatDate(v.Date, "yyyy"), "date"), true
	case "month":
		return segment(formatDate(v.Date, "MM"), "date"), true
	case "day":
		return segment(formatDate(v.Date, "dd"), "date"), true
	}
	return "", false
}

func cutPrefixFold(s, prefix string) (string, bool) {
	if len(s) >= len(prefix) && strings.EqualFold(s[:len(prefix)], prefix) {
		return s[len(prefix):], true
	}
	return "", false
}

func segment(value, fallback string) string {
	if out := sanitizeSegment(value); out != "" {
		return out
	}
	return fallback
}

// sanitizeSegment turns an expanded value into something safe to use as one
// path segment on any platform, and returns "" when nothing usable is left.
//
// These rules must agree with sanitizeSegment in internal/app/app.go: both
// build download paths from the same package names, so a package has to end up
// in the same folder whether it got there through a template or through the
// plain per-package subfolder option. The only intended difference is the empty
// result, which lets the caller pick a fallback word per placeholder instead of
// always saying "package". The length cap counts bytes, not runes, for the same
// reason - it has to cut where app.go cuts.
func sanitizeSegment(s string) string {
	// Anything that cannot appear in one path segment on some platform becomes
	// a dash; control characters become spaces.
	const bad = `/\:*?"<>|`
	out := strings.Map(func(r rune) rune {
		if r < 32 {
			return ' '
		}
		if strings.ContainsRune(bad, r) {
			return '-'
		}
		return r
	}, s)
	out = strings.Trim(strings.TrimSpace(out), ". ")
	if len(out) > maxSegment {
		out = out[:maxSegment]
	}
	return out
}

// javaLayouts maps SimpleDateFormat letter runs to Go reference-time layouts.
// Only the runs a folder template realistically uses are listed; anything else
// is copied through verbatim, for the same reason an unknown placeholder is.
// Go has no unpadded 24-hour hour, so H behaves like HH.
var javaLayouts = map[string]string{
	"yyyy": "2006",
	"yy":   "06",
	"y":    "2006",
	"MMMM": "January",
	"MMM":  "Jan",
	"MM":   "01",
	"M":    "1",
	"dd":   "02",
	"d":    "2",
	"EEEE": "Monday",
	"EEE":  "Mon",
	"E":    "Mon",
	"HH":   "15",
	"H":    "15",
	"hh":   "03",
	"h":    "3",
	"mm":   "04",
	"m":    "4",
	"ss":   "05",
	"s":    "5",
	"SSS":  "000",
	"a":    "PM",
	"zzz":  "MST",
	"z":    "MST",
	"Z":    "-0700",
}

// formatDate renders t with a SimpleDateFormat-style pattern. Every run of
// pattern letters is formatted on its own rather than translating the pattern
// into one Go layout up front: Go layouts are made of digits, so literal text
// would be read as layout and a pattern like "yyyy-MM-dd_2" would turn its
// trailing "2" into a day number.
func formatDate(t time.Time, pattern string) string {
	var b strings.Builder
	b.Grow(len(pattern) + 8)
	for i := 0; i < len(pattern); {
		c := pattern[i]
		switch {
		case c == '\'':
			// SimpleDateFormat quotes literal text so it can contain pattern
			// letters; '' is a literal apostrophe.
			j := i + 1
			if j < len(pattern) && pattern[j] == '\'' {
				b.WriteByte('\'')
				i = j + 1
				continue
			}
			for j < len(pattern) && pattern[j] != '\'' {
				b.WriteByte(pattern[j])
				j++
			}
			i = j + 1 // steps past the closing quote, or past the end if unclosed
		case isPatternLetter(c):
			j := i
			for j < len(pattern) && pattern[j] == c {
				j++
			}
			run := pattern[i:j]
			if layout, ok := javaLayouts[run]; ok {
				b.WriteString(t.Format(layout))
			} else {
				b.WriteString(run)
			}
			i = j
		default:
			// Separators and any non-ASCII bytes pass through untouched; copying
			// byte by byte reproduces multi-byte runes unchanged.
			b.WriteByte(c)
			i++
		}
	}
	return b.String()
}

func isPatternLetter(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z'
}
