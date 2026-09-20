package rules

import (
	"net/url"
	"path"
	"regexp"
	"strconv"
	"strings"

	"github.com/junkerderprovinz/knightloader/internal/pathvars"
)

const openTag = "<jd:"

// packagizerVar matches the placeholders internal/pathvars does not know about;
// everything else stays as pathvars left it. It is case-insensitive like
// pathvars, since templates are hand-edited or copied from JDownloader. The
// longer alternative comes before the shorter one it starts with.
var packagizerVar = regexp.MustCompile(`(?i)<jd:(orgfilenamewithoutext|orgfilename|orgfiletype|append|source:[0-9]{1,3}|match:[a-z]+:[0-9]{1,2})>`)

// matchTag is the capture-group placeholder with the field and group number
// captured, so Compile can check an action against its conditions. It has to
// accept exactly what packagizerVar accepts.
var matchTag = regexp.MustCompile(`(?i)<jd:match:([a-z]+):([0-9]{1,2})>`)

// appendMark holds the place of <jd:append> until the finished value is known,
// since the suffix depends on it. A NUL cannot survive sanitizeSegment, so no
// expanded value contains one otherwise.
const appendMark = "\x00"

// expand resolves one template. target names the field being written and keys
// the <jd:append> counter, so a package and a file name with the same text do
// not count against each other.
func (m *Matcher) expand(template, target string, c Candidate, g groups) string {
	if !strings.Contains(template, "<") {
		return template
	}
	// A NUL in the template itself would look like a second append slot.
	template = strings.ReplaceAll(template, appendMark, "")

	// pathvars runs first and leaves unknown placeholders alone, and a value it
	// substitutes is never re-read as a placeholder by the pass below.
	out := pathvars.Expand(template, pathvars.Vars{
		Package: c.Package,
		Host:    c.Hoster,
		Name:    c.Filename,
		Date:    c.Added,
	})
	out = packagizerVar.ReplaceAllStringFunc(out, func(raw string) string {
		return packagizerValue(raw, c, g)
	})
	if !strings.Contains(out, appendMark) {
		return out
	}
	plain := strings.ReplaceAll(out, appendMark, "")
	return strings.ReplaceAll(out, appendMark, m.nextAppend(target+"\x00"+plain))
}

// packagizerValue resolves one matched placeholder. raw is the whole tag, so an
// out-of-range or unusable one can be returned unchanged and stay visible.
func packagizerValue(raw string, c Candidate, g groups) string {
	key := strings.ToLower(raw[len(openTag) : len(raw)-1])
	switch key {
	case "append":
		return appendMark
	case "orgfilename":
		return segment(c.Filename, "file")
	case "orgfilenamewithoutext":
		return segment(strings.TrimSuffix(c.Filename, path.Ext(c.Filename)), "file")
	case "orgfiletype":
		// No fallback word: a file without an extension is normal, and
		// "movie.type" would misstate what the file is.
		return sanitizeSegment(c.Filetype)
	}
	if n, ok := strings.CutPrefix(key, "source:"); ok {
		if seg, ok := sourceSegment(c.Source, n); ok {
			return segment(seg, "source")
		}
	}
	if rest, ok := strings.CutPrefix(key, "match:"); ok {
		if v, ok := matchGroup(g, rest); ok {
			// No fallback word: an optional group that matched nothing should
			// stay empty.
			return sanitizeSegment(v)
		}
	}
	return raw
}

// matchGroup resolves the field and number out of "match:FIELD:N". The second
// result is false when the rule produced no groups for that field or the
// number is past the end, which leaves the tag visible in the text.
func matchGroup(g groups, rest string) (string, bool) {
	field, num, ok := strings.Cut(rest, ":")
	if !ok {
		return "", false
	}
	// Group 0 is the whole match.
	n, err := strconv.Atoi(num)
	if err != nil || n < 0 {
		return "", false
	}
	sub := g[Field(field)]
	if n >= len(sub) {
		return "", false
	}
	return sub[n], true
}

// sourceSegment returns the index'th path segment of the source URL, counting
// from 1 and skipping empty segments, so "https://site.org/tv/s01/list.html"
// has segments "tv", "s01" and "list.html". The second result is false when
// there is no such segment, which leaves the placeholder in the text.
func sourceSegment(source, index string) (string, bool) {
	n, err := strconv.Atoi(index)
	if err != nil || n < 1 {
		return "", false
	}
	// Without a parsed host, splitting on "/" would return "https:" as the
	// first segment.
	p := source
	if u, err := url.Parse(source); err == nil && u.Host != "" {
		p = u.Path
	}
	var segs []string
	for _, s := range strings.Split(p, "/") {
		if s != "" {
			segs = append(segs, s)
		}
	}
	if n > len(segs) {
		return "", false
	}
	return segs[n-1], true
}

// nextAppend is the suffix <jd:append> resolves to for one key: empty the first
// time that value is produced, then "_2", "_3" and so on.
func (m *Matcher) nextAppend(key string) string {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.seen == nil {
		m.seen = make(map[string]int)
	}
	n, known := m.seen[key]
	if !known && len(m.seen) >= maxAppendKeys {
		return ""
	}
	m.seen[key] = n + 1
	if n == 0 {
		return ""
	}
	return "_" + strconv.Itoa(n+1)
}

// maxSegment and sanitizeSegment mirror their unexported counterparts in
// internal/pathvars. They have to agree, because both expand into the same
// download paths and a name cut at a different byte would split one package
// into two folders. Sanitising keeps a value like "../../etc/passwd" from
// adding path levels the template never spelled out.
const maxSegment = 120

// FileSegment cuts a value down to the one path segment a file name may be,
// the same cut Apply gives Action.Filename, so a rename typed into the
// interface and one written by a rule agree. A value that sanitises away
// entirely becomes "file".
func FileSegment(value string) string { return segment(value, "file") }

// segment is sanitizeSegment with a fallback word, so a placeholder whose value
// sanitises away still contributes a named segment instead of an empty one.
func segment(value, fallback string) string {
	if out := sanitizeSegment(value); out != "" {
		return out
	}
	return fallback
}

func sanitizeSegment(s string) string {
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
