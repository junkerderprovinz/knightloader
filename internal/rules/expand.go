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

// packagizerVar matches the placeholders internal/pathvars does not know about.
// Scanning with a pattern rather than a hand-written parser is deliberate: the
// only tags handled here are the ones listed, and everything else stays exactly
// as pathvars left it. The tag is matched case-insensitively for the same
// reason pathvars matches it that way — a template may have been hand-edited or
// carried over from a JDownloader config with different capitalisation.
//
// The longer alternative is listed before the shorter one it starts with, so
// the pattern reads the way it behaves.
var packagizerVar = regexp.MustCompile(`(?i)<jd:(orgfilenamewithoutext|orgfilename|orgfiletype|append|source:[0-9]{1,3}|match:[a-z]+:[0-9]{1,2})>`)

// matchTag is the capture-group placeholder on its own, with the field and the
// group number captured, so Compile can check a rule's action against its
// conditions before the rule is ever run. It has to accept exactly what
// packagizerVar accepts, or a tag would validate here and not resolve there.
var matchTag = regexp.MustCompile(`(?i)<jd:match:([a-z]+):([0-9]{1,2})>`)

// appendMark holds the place of <jd:append> until the rest of the value is
// known: whether a counter is needed depends on the finished string, so the
// suffix cannot be decided while it is still being built. A NUL is used because
// it cannot survive sanitizeSegment, so no expanded value can ever contain one.
const appendMark = "\x00"

// expand resolves one template. target names the field being written and is
// only used to key the <jd:append> counter, so a package and a file name that
// happen to produce the same text do not count as a collision with each other.
func (m *Matcher) expand(template, target string, c Candidate, g groups) string {
	if !strings.Contains(template, "<") {
		return template
	}
	// A NUL arriving in the template itself would look like a second append
	// slot and take a counter that belongs to nothing.
	template = strings.ReplaceAll(template, appendMark, "")

	// pathvars runs first. It leaves the placeholders it does not know
	// untouched, which is exactly the handover this needs — and doing it in
	// this order means a value pathvars substitutes can never be re-read as a
	// placeholder by the pass below.
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
		// The only placeholder with no fallback word. A file without an
		// extension is perfectly normal, and a template like
		// "<jd:orgfilenamewithoutext>.<jd:orgfiletype>" turning into
		// "movie.type" would be a lie about what the file is.
		return sanitizeSegment(c.Filetype)
	}
	if n, ok := strings.CutPrefix(key, "source:"); ok {
		if seg, ok := sourceSegment(c.Source, n); ok {
			return segment(seg, "source")
		}
	}
	if rest, ok := strings.CutPrefix(key, "match:"); ok {
		if v, ok := matchGroup(g, rest); ok {
			// sanitizeSegment and not segment: a capture group that matched an
			// empty string is an ordinary thing for an optional group to do, and
			// standing a fallback word in its place would put "match" into a folder
			// name the pattern deliberately left blank.
			return sanitizeSegment(v)
		}
	}
	return raw
}

// matchGroup resolves the field and number out of "match:FIELD:N". The second
// result is false when the rule produced no groups for that field or the number
// is past the end, which leaves the tag visible in the text — the same treatment
// every other unresolvable placeholder gets, and a case Compile has usually
// refused the rule for already.
func matchGroup(g groups, rest string) (string, bool) {
	field, num, ok := strings.Cut(rest, ":")
	if !ok {
		return "", false
	}
	// Group 0 is the whole match, which is what the pattern as a whole found and
	// is worth having: it saves wrapping an entire expression in brackets.
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
	// Only a parse that found a host tells us where the path starts. Splitting
	// an unparsed URL on "/" would hand back "https:" as the first segment.
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

// maxSegment mirrors the cap in internal/pathvars, as does sanitizeSegment
// below. Both are duplicated rather than shared because pathvars keeps them
// unexported, and they have to agree: the values below end up in the same
// download paths as the ones pathvars expands, so a name has to be cut at the
// same byte either way or one template produces two folders for one package.
//
// The sanitising is not cosmetic. A file name of "../../etc/passwd" fed into a
// folder template would otherwise add path levels the template never spelled
// out, and the whole point of a one-segment guarantee is that a template can
// only ever create the folders it names itself.
const maxSegment = 120

// FileSegment cuts a value down to the one path segment a file name is allowed
// to be - the very cut Apply gives Action.Filename, exported so a rename typed
// into the interface and a rename written by a rule cannot disagree about what
// a name is.
//
// Two cuts drift, and the one that drifts is the one that lets "../../etc/x"
// through. A value that sanitises away entirely comes back as "file" rather than
// empty, for the same reason it does inside a template: a rename has to end in a
// name, and the caller can see this one and correct it.
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
