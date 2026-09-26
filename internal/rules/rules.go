// Package rules is KnightLoader's rule engine. It serves as the Packagizer,
// which rewrites a link's package, folder, name and download options before it
// is queued, and as the LinkGrabber filter, which decides whether a link is
// taken at all. The two differ only in what a matching rule does.
//
// A Set is compiled once and the resulting Matcher is asked about every
// candidate link, which keeps the per-link cost bounded on a paste of
// thousands. Compile never fails: it returns a working Matcher plus the rules
// it could not use, so a broken rule costs the user that rule only. Check
// never returns a bare "no"; a rejection names the rule that made it and
// carries a reason.
//
// # Variables
//
// Every string an action sets is a template. It goes through internal/pathvars
// first, which resolves <jd:packagename>, <jd:hoster>, <jd:filename> and the
// date placeholders, and then through this package, which adds:
//
//	<jd:orgfilename>            the link's file name as it arrived
//	<jd:orgfilenamewithoutext>  the same, with the extension cut off
//	<jd:orgfiletype>            the extension without its dot, empty when there is none
//	<jd:source:N>               the Nth path segment of the source page's URL, counting from 1
//	<jd:match:FIELD:N>          capture group N of this rule's "matches" pattern on FIELD
//	<jd:append>                 empty the first time a value is produced, "_2", "_3" ... after
//
// Unknown or out-of-range placeholders are left in the text, as in
// internal/pathvars, so a typo stays visible in the folder name.
//
// In a JDownloader template <jd:source:1> is capture group 1 of the source
// URL's pattern; here it is the first path segment, which is what stored rules
// already use and works in a rule without a pattern. JDownloader's meaning is
// spelled <jd:match:FIELD:N>, and Compile refuses one that names a field the
// rule has no "matches" condition on, so a template copied from JDownloader
// fails when it is saved rather than in a folder name.
//
// Every variable resolves against the link as it arrived, so rules do not
// chain onto each other's output and <jd:filename> equals <jd:orgfilename>.
// What a rule does can be read off that rule alone.
package rules

import (
	"errors"
	"fmt"
	"net/url"
	"path"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Field is what a condition looks at on a candidate link.
type Field string

const (
	FieldFilename Field = "filename"
	FieldURL      Field = "url"
	FieldHoster   Field = "hoster"
	FieldSource   Field = "source" // the page a crawl found the link on
	FieldFiletype Field = "filetype"
	FieldFilesize Field = "filesize"
	FieldPackage  Field = "package"
)

// Op is how a condition compares. Every operator except OpMatches folds case,
// since "sample" typed into a form is meant to catch "Sample.mkv"; a pattern
// carries its own flags.
type Op string

const (
	OpContains    Op = "contains"
	OpEquals      Op = "equals"
	OpContainsNot Op = "contains-not"
	OpEqualsNot   Op = "equals-not"
	OpMatches     Op = "matches"    // regular expression, unanchored
	OpBetween     Op = "is-between" // numeric, Min..Max, file size only
)

// Condition is one test against a candidate link. Value carries the text for
// the string operators and the byte count for OpEquals/OpEqualsNot on a file
// size; Min and Max carry the range for OpBetween. Sizes are plain bytes;
// parsing "700 MB" is the interface's job.
type Condition struct {
	Field Field  `json:"field"`
	Op    Op     `json:"op"`
	Value string `json:"value,omitempty"`
	Min   int64  `json:"min,omitempty"`
	// Max of zero means no upper bound, which is how "at least 500 MB" is
	// written.
	Max int64 `json:"max,omitempty"`
}

// Action is what a matching rule does. The Packagizer uses the fields above
// Reject, the filter uses Reject and Reason, and each ignores the other's.
//
// Every string field except Headers and Category is a template (see the
// package documentation). An empty string means "leave this alone", never
// "clear it", so a later rule that sets only the folder keeps an earlier
// package name. The optional values are pointers because priority 0, zero
// chunks and auto-extract off are all real settings.
//
// Only DownloadDir and ExtractDir may name path levels. Filename is cut back
// to a single segment after expansion, since a separator in a file name is a
// way out of the folder the caller picked.
type Action struct {
	PackageName string `json:"packageName,omitempty"`
	DownloadDir string `json:"downloadDir,omitempty"`
	Filename    string `json:"filename,omitempty"`
	Comment     string `json:"comment,omitempty"`
	Priority    *int   `json:"priority,omitempty"`
	AutoExtract *bool  `json:"autoExtract,omitempty"`
	Chunks      *int   `json:"chunks,omitempty"`

	// ExtractDir is where the content of this link's finished extraction is
	// moved afterwards; empty leaves it to settings.ExtractMoveTo. It differs
	// from DownloadDir, where the archive is fetched to, and from the
	// ExtractTo setting, where unpacking writes while it runs: a folder a
	// media server watches should only ever see finished files. The content
	// moves without its top folder, so "Show.S01.COMPLETE.WEB/ep01.mkv"
	// arrives as "ep01.mkv" in the folder named here.
	ExtractDir string `json:"extractDir,omitempty"`

	// Headers names a stored header profile (internal/resolver/hostheaders)
	// and never holds a header itself: rule sets live in settings.json, which
	// the diagnostics bundle includes, and the values are sealed in the
	// encrypted account store. It is not a template, because a name built from
	// <jd:hoster> would let a link's own host pick which credential is
	// attached. Empty falls back to the profile stored for the link's origin.
	Headers string `json:"headers,omitempty"`

	// Category files the link under one of settings.Categories by its stable
	// id. It is unrelated to rules.Category, the file-type shorthand that
	// expands into a filetype condition; both names are fixed by stored rule
	// sets and the editor grammar. See action_category.go for the id format
	// and why it is not a template.
	Category string `json:"category,omitempty"`

	// Reject drops the link instead of taking it. Reason is shown alongside
	// the rule's name; when it is empty Check writes one.
	Reject bool   `json:"reject,omitempty"`
	Reason string `json:"reason,omitempty"`
}

// Rule is one entry in a Set. All of its conditions must hold for it to
// match, so an "either/or" is written as two rules and the list reads top to
// bottom without precedence. A rule with no conditions matches every link,
// which is how a catch-all folder or a final blanket reject is written.
//
// The flag is Disabled rather than Enabled so a rule posted without the field
// is live.
type Rule struct {
	Name       string      `json:"name,omitempty"`
	Disabled   bool        `json:"disabled,omitempty"`
	Conditions []Condition `json:"conditions,omitempty"`
	Action     Action      `json:"action"`
}

// Set is one ordered rule list, persisted as part of the settings.
type Set struct {
	Rules []Rule `json:"rules,omitempty"`
	// Disabled switches the whole list off without deleting it. It is not
	// Enabled because settings files written before the field existed would
	// then switch the Packagizer and the filter off on upgrade.
	Disabled bool `json:"disabled,omitempty"`
	// StopAfterMatch ends evaluation at the first rule that matches. The
	// Packagizer wants it off, so every matching rule contributes and a later
	// rule wins per field; a filter usually wants it on, so an accept placed
	// above a broad reject protects the link.
	StopAfterMatch bool `json:"stopAfterMatch,omitempty"`
}

// Candidate is the link a rule set is asked about. Hoster and Filetype are
// derived from URL and Filename when left empty. Added is what the date
// variables format; it is passed in so the output is a pure function of the
// input.
type Candidate struct {
	Filename string
	URL      string
	Hoster   string
	Source   string
	Filetype string
	Filesize int64
	Package  string
	Added    time.Time
}

// filled derives what the caller did not supply.
func (c Candidate) filled() Candidate {
	if c.Hoster == "" {
		c.Hoster = hostOf(c.URL)
	}
	if c.Filetype == "" {
		c.Filetype = strings.TrimPrefix(path.Ext(c.Filename), ".")
	}
	return c
}

// hostOf mirrors the helper of the same name in internal/app, so a rule
// written against "example.org" matches the links <jd:hoster> files under that
// name.
func hostOf(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.Hostname() == "" {
		return raw
	}
	return strings.TrimPrefix(strings.ToLower(u.Hostname()), "www.")
}

// Effect is what the Packagizer decided for one link. Empty strings and nil
// pointers mean no rule had an opinion, so the caller applies only what is
// set.
type Effect struct {
	Package     string `json:"package,omitempty"`
	Dir         string `json:"dir,omitempty"`
	Filename    string `json:"filename,omitempty"`
	Comment     string `json:"comment,omitempty"`
	Priority    *int   `json:"priority,omitempty"`
	AutoExtract *bool  `json:"autoExtract,omitempty"`
	Chunks      *int   `json:"chunks,omitempty"`
	// ExtractDir is where the finished extraction's content is moved; see
	// Action.ExtractDir.
	ExtractDir string `json:"extractDir,omitempty"`
	// Headers is the stored header profile a rule attached, by name.
	Headers string `json:"headers,omitempty"`
	// Category is the category id a rule filed the link under.
	Category string `json:"category,omitempty"`
	// Matched names the rules that fired, in order, so the interface can say
	// why a link landed where it did.
	Matched []string `json:"matched,omitempty"`
}

// Verdict is what the filter decided. Rule is set whenever a rule decided the
// outcome, including an explicit accept.
type Verdict struct {
	Rejected bool   `json:"rejected"`
	Rule     string `json:"rule,omitempty"`
	Reason   string `json:"reason,omitempty"`
	// Code keys Reason for an interface with words of its own, and Params
	// holds the values its wording needs. It is empty for a reason somebody
	// wrote, which is shown as written.
	Code   string            `json:"code,omitempty"`
	Params map[string]string `json:"params,omitempty"`
}

// CodeFilterRule is the code of the reason a rule without one of its own is
// refused with. Its one value is "rule", the rule's name.
const CodeFilterRule = "filterRule"

// Problem is one rule Compile could not use, in words meant for the user.
type Problem struct {
	Index   int    `json:"index"` // position in Set.Rules, zero-based
	Rule    string `json:"rule"`  // the rule's name, or its position when unnamed
	Message string `json:"message"`
	// Condition is which condition the problem is about, counting from 1, and
	// 0 when it is about the action or the rule as a whole. The editor uses it
	// to mark the row instead of parsing it out of Message.
	Condition int `json:"condition,omitempty"`
}

func (p Problem) Error() string { return fmt.Sprintf("%s: %s", p.Rule, p.Message) }

// Bounds a rule may not exceed. Priority spans the seven values the interface
// offers (app.Priorities), so a rule can reach every one and cannot set one
// the interface could not undo. MaxChunks is a guard rail: more connections
// buy nothing on a hoster that rate-limits per file and get accounts flagged.
const (
	PriorityMin = -3
	PriorityMax = 3
	MaxChunks   = 16
)

// maxPattern caps the source length of a user's regular expression. RE2
// cannot backtrack catastrophically, but a long pattern compiles to a large
// program that runs once per condition per link.
const maxPattern = 512

// cond is a compiled Condition. The comparison value is lower-cased once here.
type cond struct {
	field  Field
	op     Op
	value  string
	re     *regexp.Regexp
	num    int64 // file size for OpEquals/OpEqualsNot
	min    int64
	max    int64
	hasMax bool
}

// compiled is a Rule that survived validation.
type compiled struct {
	// index is the rule's position in the Set it came from, which differs from
	// its position in the Matcher once Compile has dropped rules.
	index int
	name  string
	conds []cond
	act   Action
	// wantsGroups is set when any of this rule's templates reads a capture
	// group. Collecting submatches allocates where MatchString does not, so
	// only rules that need them pay.
	wantsGroups bool
}

// groups holds the submatches of a matching rule's regular-expression
// conditions, keyed by the field each pattern ran against. It backs
// <jd:match:FIELD:N>.
type groups map[Field][]string

// Matcher is a compiled Set. It is safe for concurrent use, since links are
// staged from several goroutines.
type Matcher struct {
	stopAfterMatch bool
	rules          []compiled

	// seen backs <jd:append>, the only state a Matcher keeps between calls.
	mu   sync.Mutex
	seen map[string]int
}

// maxAppendKeys caps the <jd:append> memory on a long-running server. Past
// the cap a new name gets no suffix, as the first link with any name does.
const maxAppendKeys = 4096

// Compile validates a rule set and returns a Matcher plus every rule it had to
// leave out. The Matcher is never nil. A rule with any problem is dropped
// whole, because a rule missing a condition matches links the user never
// meant, and in a filter that loses links on a typo.
//
// A disabled set compiles to an empty Matcher and reports nothing, so a page
// badge does not stay lit for rules that are not applied. The editor gets its
// problems from Preview, which validates the set either way.
func Compile(s Set) (*Matcher, []Problem) {
	m := &Matcher{stopAfterMatch: s.StopAfterMatch}
	if s.Disabled {
		return m, nil
	}
	var problems []Problem
	for i, r := range s.Rules {
		if r.Disabled {
			continue
		}
		c, probs := compileRule(r, i)
		problems = append(problems, probs...)
		if len(probs) > 0 {
			continue
		}
		m.rules = append(m.rules, c)
	}
	return m, problems
}

// compileRule turns one rule into its compiled form and lists everything wrong
// with it.
func compileRule(r Rule, index int) (compiled, []Problem) {
	name := ruleName(r, index)
	var problems []Problem
	// at is which condition the next report is about, counting from 1, and 0
	// while the action is checked.
	at := 0
	report := func(format string, args ...any) {
		problems = append(problems, Problem{
			Index: index, Rule: name, Condition: at, Message: fmt.Sprintf(format, args...),
		})
	}
	c := compiled{index: index, name: name, act: snapshot(r.Action), wantsGroups: readsGroups(r.Action)}
	for j, raw := range r.Conditions {
		at = j + 1
		cc, err := compileCondition(raw)
		if err != nil {
			report("condition %d (%s): %v", j+1, raw.Field, err)
			continue
		}
		c.conds = append(c.conds, cc)
	}
	at = 0
	for _, msg := range actionProblems(r.Action, c.conds) {
		report("%s", msg)
	}
	return c, problems
}

// snapshot copies the action's pointer values, so editing the settings struct
// in place cannot change a rule that is already compiled.
func snapshot(a Action) Action {
	if a.Priority != nil {
		v := *a.Priority
		a.Priority = &v
	}
	if a.AutoExtract != nil {
		v := *a.AutoExtract
		a.AutoExtract = &v
	}
	if a.Chunks != nil {
		v := *a.Chunks
		a.Chunks = &v
	}
	return a
}

// ruleName is what a problem, an Effect and a rejection call the rule: its
// name, or its position when it has none.
func ruleName(r Rule, index int) string {
	if n := strings.TrimSpace(r.Name); n != "" {
		return n
	}
	return fmt.Sprintf("rule %d", index+1)
}

// Empty reports whether the matcher holds no usable rule, so a caller can skip
// building a Candidate for every link when nothing is configured.
func (m *Matcher) Empty() bool { return len(m.rules) == 0 }

// Apply runs the Packagizer flavour. Every matching rule contributes in order
// and a later rule wins per field, unless the set stops at the first match.
//
// Only the winning template per field is expanded, after the loop. Expanding
// inside it would make <jd:append> count values a later rule overwrites, so
// two rules writing the same field would hand the first link "_2".
func (m *Matcher) Apply(c Candidate) Effect {
	c = c.filled()
	var e Effect
	// Each winning template keeps the capture groups of the rule that set it,
	// since <jd:match:...> reads that rule's own pattern.
	var pkg, dir, name, comment, unpackDir tpl
	m.walk(c, func(r compiled, g groups) bool {
		e.Matched = append(e.Matched, r.name)
		a := r.act
		if a.PackageName != "" {
			pkg = tpl{a.PackageName, g}
		}
		if a.DownloadDir != "" {
			dir = tpl{a.DownloadDir, g}
		}
		if a.ExtractDir != "" {
			unpackDir = tpl{a.ExtractDir, g}
		}
		if a.Filename != "" {
			name = tpl{a.Filename, g}
		}
		if a.Comment != "" {
			comment = tpl{a.Comment, g}
		}
		// Headers and Category are not templates; see Action.
		if a.Headers != "" {
			e.Headers = a.Headers
		}
		if a.Category != "" {
			e.Category = a.Category
		}
		// Values are copied so a caller writing through the Effect cannot
		// change the compiled rule.
		if a.Priority != nil {
			v := *a.Priority
			e.Priority = &v
		}
		if a.AutoExtract != nil {
			v := *a.AutoExtract
			e.AutoExtract = &v
		}
		if a.Chunks != nil {
			v := *a.Chunks
			e.Chunks = &v
		}
		return true
	})
	if pkg.text != "" {
		e.Package = m.expand(pkg.text, string(FieldPackage), c, pkg.groups)
	}
	if dir.text != "" {
		// Whether the folder is absolute or writable is left to the caller:
		// filepath.IsAbs answers differently on Windows and Linux, and a rule
		// written on one would be rejected on the other.
		e.Dir = m.expand(dir.text, "dir", c, dir.groups)
	}
	if unpackDir.text != "" {
		// Keyed apart from the download folder, so <jd:append> counts the two
		// independently.
		e.ExtractDir = m.expand(unpackDir.text, "extractdir", c, unpackDir.groups)
	}
	if name.text != "" {
		// Cut to one segment after expanding, so "../../x" cannot move the
		// download out of its folder and the append counter is not disturbed.
		e.Filename = segment(m.expand(name.text, string(FieldFilename), c, name.groups), "file")
	}
	if comment.text != "" {
		e.Comment = m.expand(comment.text, "comment", c, comment.groups)
	}
	return e
}

// tpl is a template that has won its field, with the capture groups of the
// rule it came from.
type tpl struct {
	text   string
	groups groups
}

// walk visits every rule that matches, in order, and stops when visit returns
// false or the set stops at the first match. Apply, Check and Preview all go
// through it, so a dry run cannot report an order staging never takes.
func (m *Matcher) walk(c Candidate, visit func(r compiled, g groups) bool) {
	for _, r := range m.rules {
		ok, g := r.evaluate(c)
		if !ok {
			continue
		}
		if !visit(r, g) || m.stopAfterMatch {
			return
		}
	}
}

// Check runs the filter flavour. The first matching rule decides: a reject
// ends it, and an explicit accept ends it too when the set stops at the first
// match, which is how a narrow "keep this" above a broad "drop that" works. A
// link no rule matched is accepted, so an empty or broken filter takes
// everything.
func (m *Matcher) Check(c Candidate) Verdict {
	c = c.filled()
	var v Verdict
	m.walk(c, func(r compiled, g groups) bool {
		if r.act.Reject {
			v = m.rejection(r, c, g)
			return false
		}
		if m.stopAfterMatch {
			v = Verdict{Rule: r.name}
		}
		return true
	})
	return v
}

// rejection is the verdict of a rule that refuses. Its reason is never empty:
// a rule without a reason still has a name.
func (m *Matcher) rejection(r compiled, c Candidate, g groups) Verdict {
	v := Verdict{Rejected: true, Rule: r.name}
	if r.act.Reason != "" {
		if out := strings.TrimSpace(m.expand(r.act.Reason, "reason", c, g)); out != "" {
			v.Reason = out
			return v
		}
	}
	v.Reason = fmt.Sprintf("rejected by link filter rule %q", r.name)
	v.Code, v.Params = CodeFilterRule, map[string]string{"rule": r.name}
	return v
}

// ResetAppend clears the <jd:append> counter, so a caller that treats each
// paste as a fresh batch can start its numbering over.
func (m *Matcher) ResetAppend() {
	m.mu.Lock()
	m.seen = nil
	m.mu.Unlock()
}

// evaluate reports whether every condition holds and returns the capture
// groups a rule that reads them needs. Where two "matches" conditions test the
// same field, the first one's groups are used, since the rule reads top to
// bottom.
func (r compiled) evaluate(c Candidate) (bool, groups) {
	var g groups
	for _, cd := range r.conds {
		if !r.wantsGroups || cd.op != OpMatches {
			if !cd.match(c) {
				return false, nil
			}
			continue
		}
		sub := cd.re.FindStringSubmatch(fieldValue(c, cd.field))
		if sub == nil {
			return false, nil
		}
		if _, taken := g[cd.field]; !taken {
			if g == nil {
				g = groups{}
			}
			g[cd.field] = sub
		}
	}
	return true, g
}

func (cd cond) match(c Candidate) bool {
	if cd.field == FieldFilesize {
		return cd.matchSize(c.Filesize)
	}
	s := fieldValue(c, cd.field)
	switch cd.op {
	case OpMatches:
		return cd.re.MatchString(s)
	case OpContains:
		return strings.Contains(strings.ToLower(s), cd.value)
	case OpContainsNot:
		return !strings.Contains(strings.ToLower(s), cd.value)
	case OpEquals:
		return strings.ToLower(s) == cd.value
	case OpEqualsNot:
		return strings.ToLower(s) != cd.value
	}
	return false
}

func (cd cond) matchSize(n int64) bool {
	switch cd.op {
	case OpBetween:
		return n >= cd.min && (!cd.hasMax || n <= cd.max)
	case OpEquals:
		return n == cd.num
	case OpEqualsNot:
		return n != cd.num
	}
	return false
}

func fieldValue(c Candidate, f Field) string {
	switch f {
	case FieldFilename:
		return c.Filename
	case FieldURL:
		return c.URL
	case FieldHoster:
		return c.Hoster
	case FieldSource:
		return c.Source
	case FieldFiletype:
		return c.Filetype
	case FieldPackage:
		return c.Package
	}
	return ""
}

// compileCondition turns one condition into its compiled form, or says why it
// cannot be used.
func compileCondition(c Condition) (cond, error) {
	switch c.Field {
	case FieldFilename, FieldURL, FieldHoster, FieldSource, FieldFiletype, FieldFilesize, FieldPackage:
	default:
		return cond{}, fmt.Errorf("unknown field %q", c.Field)
	}
	switch c.Op {
	case OpContains, OpEquals, OpContainsNot, OpEqualsNot, OpMatches, OpBetween:
	default:
		return cond{}, fmt.Errorf("unknown operator %q", c.Op)
	}
	out := cond{field: c.Field, op: c.Op}
	if c.Field == FieldFilesize {
		return compileSize(c, out)
	}
	if c.Op == OpBetween {
		return cond{}, fmt.Errorf("is-between compares numbers, so it only works on %s", FieldFilesize)
	}
	// An empty value would make "contains" match every link, and a filter rule
	// reject the entire paste.
	if strings.TrimSpace(c.Value) == "" {
		return cond{}, fmt.Errorf("%s has no value; an empty test would match every link", c.Op)
	}
	if c.Op == OpMatches {
		// The length is checked before compiling. The pattern is used
		// untrimmed, since trailing whitespace can be part of the match.
		if len(c.Value) > maxPattern {
			return cond{}, fmt.Errorf("the pattern is %d characters, the limit is %d", len(c.Value), maxPattern)
		}
		re, err := regexp.Compile(c.Value)
		if err != nil {
			return cond{}, fmt.Errorf("invalid regular expression: %v", err)
		}
		out.re = re
		return out, nil
	}
	value := strings.TrimSpace(c.Value)
	// ".mkv" means the same as "mkv"; otherwise the rule would silently never
	// fire.
	if c.Field == FieldFiletype {
		value = strings.TrimPrefix(value, ".")
	}
	out.value = strings.ToLower(value)
	return out, nil
}

// compileSize handles the file-size field, where the operators mean numbers.
func compileSize(c Condition, out cond) (cond, error) {
	switch c.Op {
	case OpBetween:
		if c.Min < 0 || c.Max < 0 {
			return cond{}, errors.New("is-between: a negative size can never match")
		}
		// Both bounds empty means any size, which would match every link. A
		// real range sets at least one; "exactly nothing" is equals 0.
		if c.Min == 0 && c.Max == 0 {
			return cond{}, errors.New("is-between has no bounds; an empty range would match every link")
		}
		if c.Max > 0 && c.Min > c.Max {
			return cond{}, fmt.Errorf("is-between: the lower bound %d is above the upper bound %d, so nothing can match", c.Min, c.Max)
		}
		out.min, out.max, out.hasMax = c.Min, c.Max, c.Max > 0
		return out, nil
	case OpEquals, OpEqualsNot:
		n, err := strconv.ParseInt(strings.TrimSpace(c.Value), 10, 64)
		if err != nil {
			return cond{}, fmt.Errorf("%s: %q is not a size in bytes", c.Op, c.Value)
		}
		out.num = n
		return out, nil
	}
	// "filesize contains 100" would match 1000, 2100 and 100000.
	return cond{}, fmt.Errorf("%s cannot compare a file size; use is-between, equals or equals-not", c.Op)
}

// templates lists an action's template fields with the label a message uses
// for each, so a problem names the box to fix.
func (a Action) templates() []struct{ Label, Text string } {
	return []struct{ Label, Text string }{
		{"package name", a.PackageName},
		{"download folder", a.DownloadDir},
		{"unpack folder", a.ExtractDir},
		{"file name", a.Filename},
		{"comment", a.Comment},
		{"reason", a.Reason},
	}
}

// readsGroups reports whether any of an action's templates asks for a capture
// group.
func readsGroups(a Action) bool {
	for _, f := range a.templates() {
		if matchTag.MatchString(f.Text) {
			return true
		}
	}
	return false
}

// actionProblems lists everything wrong with an action, including where it
// disagrees with the conditions of its rule.
func actionProblems(a Action, conds []cond) []string {
	msgs := matchTagProblems(a, conds)
	if a.Priority != nil && (*a.Priority < PriorityMin || *a.Priority > PriorityMax) {
		msgs = append(msgs, fmt.Sprintf("priority %d is outside %d..%d", *a.Priority, PriorityMin, PriorityMax))
	}
	// Zero chunks would be a download with no connection, not the default.
	if a.Chunks != nil && (*a.Chunks < 1 || *a.Chunks > MaxChunks) {
		msgs = append(msgs, fmt.Sprintf("chunk count %d is outside 1..%d", *a.Chunks, MaxChunks))
	}
	for _, f := range a.templates() {
		if unterminated(f.Text) {
			msgs = append(msgs, fmt.Sprintf("the %s opens a <jd:...> it never closes", f.Label))
		}
	}
	if msg := headerProfileProblem(a.Headers); msg != "" {
		msgs = append(msgs, msg)
	}
	if msg := categoryProblem(a.Category); msg != "" {
		msgs = append(msgs, msg)
	}
	return msgs
}

// matchTagProblems refuses a <jd:match:FIELD:N> that can never resolve. Unlike
// other unresolved placeholders it would not be spotted: people copy it from
// JDownloader templates, and every download would land under a literal
// "<jd:match:source:1>" folder.
func matchTagProblems(a Action, conds []cond) []string {
	var msgs []string
	for _, f := range a.templates() {
		for _, m := range matchTag.FindAllStringSubmatch(f.Text, -1) {
			field := Field(strings.ToLower(m[1]))
			n, err := strconv.Atoi(m[2])
			if err != nil {
				continue // the pattern only matches digits
			}
			re := patternFor(conds, field)
			switch {
			case re == nil:
				msgs = append(msgs, fmt.Sprintf(
					"the %s reads capture group %d of %s, but no condition in this rule matches %s against a pattern",
					f.Label, n, field, field))
			case n > re.NumSubexp():
				msgs = append(msgs, fmt.Sprintf(
					"the %s reads capture group %d of %s, but that pattern has %d",
					f.Label, n, field, re.NumSubexp()))
			}
		}
	}
	return msgs
}

// patternFor is the regular expression a rule tests one field with, or nil.
// The first one wins, as in evaluate.
func patternFor(conds []cond, f Field) *regexp.Regexp {
	for _, cd := range conds {
		if cd.field == f && cd.op == OpMatches {
			return cd.re
		}
	}
	return nil
}

// unterminated reports a template that opens a placeholder it never closes.
// pathvars leaves such a template untouched, which would put the raw tag into
// a folder or file name.
func unterminated(s string) bool {
	low := strings.ToLower(s)
	for {
		i := strings.Index(low, openTag)
		if i < 0 {
			return false
		}
		j := strings.IndexByte(low[i:], '>')
		if j < 0 {
			return true
		}
		low = low[i+j+1:]
	}
}
