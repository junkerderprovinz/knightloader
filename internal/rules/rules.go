// Package rules is KnightLoader's rule engine. It is one engine used twice:
// as the Packagizer, which rewrites a link's package, folder, name and download
// options before it is queued, and as the LinkGrabber filter, which decides
// whether a link is taken at all. The two differ only in what a matching rule
// does, so they share the types and the matching code.
//
// A Set is compiled once and the resulting Matcher is then asked about every
// candidate link. Compiling once is what keeps the per-link cost bounded — a
// paste can be several thousand links — and it is also the only moment at which
// a rule the user got wrong can still be reported to them. Compile therefore
// never fails: it returns a working Matcher plus everything it could not use,
// so a broken rule costs the user that rule and nothing else.
//
// The filter half exists because JDownloader's eats links in silence: something
// is filtered, nothing says what or why, and the link is simply not there. Check
// never returns a bare "no" — a rejection always names the rule that made it and
// carries a reason.
//
// # Variables
//
// Every string an action sets is a template. It goes through internal/pathvars
// first, which resolves <jd:packagename>, <jd:hoster>, <jd:filename> and the
// date placeholders, and then through this package, which resolves the ones the
// Packagizer adds on top:
//
//	<jd:orgfilename>            the link's file name as it arrived
//	<jd:orgfilenamewithoutext>  the same, with the extension cut off
//	<jd:orgfiletype>            the extension without its dot, empty when there is none
//	<jd:source:N>               the Nth path segment of the source page's URL, counting from 1
//	<jd:match:FIELD:N>          capture group N of this rule's "matches" pattern on FIELD
//	<jd:append>                 empty the first time a value is produced, "_2", "_3" ... after
//
// Unknown or out-of-range placeholders are left in the text rather than blanked,
// which is what internal/pathvars does and for the same reason: a typo that is
// visible in the folder name can be fixed, and one that quietly collapsed to
// nothing cannot be found at all.
//
// # <jd:source:N> does not mean what JDownloader means by it
//
// In a JDownloader Packagizer template, <jd:source:1> is capture group 1 of the
// regular expression the rule tests the source URL with. Here it is the first
// path segment of that URL, and it has been that since the package was written.
// The two agree often enough to be dangerous and disagree silently: a template
// copied out of a JD config builds a different folder, with no error and nothing
// to notice until the files are already somewhere else.
//
// The path-segment meaning is kept, because it is what is already stored in
// people's rules and because it is the only one of the two that works in a rule
// with no regular expression in it at all. JD's meaning is spelled
// <jd:match:FIELD:N>, which says out loud which pattern it reads — JD's version
// can only ever mean the source, and ours reads a capture group from any field
// the rule matched with. Compile refuses a rule whose <jd:match:...> names a
// field the rule has no "matches" condition on, so the JD habit of reaching for a
// group that is not there fails loudly at the moment the rule is saved rather
// than quietly in a folder name.
//
// Every variable resolves against the link as it arrived, so rules do not chain
// onto each other's output: <jd:filename> in the fourth rule is still the name
// the hoster gave, not whatever the second rule renamed it to. That is why
// <jd:filename> and <jd:orgfilename> are the same value here. It costs a little
// expressiveness and buys the property that matters in a list a user edits by
// hand — what a rule does can be read off that rule alone.
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
// because a user typing "sample" into a web form does not mean to let
// "Sample.mkv" through. OpMatches is left alone: the pattern carries its own
// flags and forcing (?i) onto it would overrule what the user wrote.
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
// size; Min and Max carry the range for OpBetween. Sizes are always plain
// bytes — turning "700 MB" into a number is the interface's job, and a parser
// hidden down here would disagree with it sooner or later.
type Condition struct {
	Field Field  `json:"field"`
	Op    Op     `json:"op"`
	Value string `json:"value,omitempty"`
	Min   int64  `json:"min,omitempty"`
	// Max of zero means "no upper bound". A half-filled range is the normal
	// shape of "at least 500 MB", and reading the empty box as zero would give
	// the user a rule that can never match anything.
	Max int64 `json:"max,omitempty"`
}

// Action is what a matching rule does. The Packagizer flavour uses the fields
// above Reject, the filter flavour uses Reject and Reason; a Set is free to use
// either, and each flavour ignores the other's fields.
//
// Every string field is a template and is expanded through internal/pathvars
// plus the Packagizer-only variables listed in the package documentation. An empty
// string means "leave this alone", never "clear it": a later rule that sets
// only the folder must not wipe the package name an earlier one chose. The
// three optional values are pointers for the same reason — priority 0, zero
// chunks and auto-extract off are all real settings, so "unset" needs to be
// something other than the zero value.
//
// DownloadDir is the only field allowed to spell out path levels. Filename is
// cut back to a single segment once it has been expanded, because a file name
// containing a separator is not a name, it is a way out of the folder the
// caller picked.
type Action struct {
	PackageName string `json:"packageName,omitempty"`
	DownloadDir string `json:"downloadDir,omitempty"`
	Filename    string `json:"filename,omitempty"`
	Comment     string `json:"comment,omitempty"`
	Priority    *int   `json:"priority,omitempty"`
	AutoExtract *bool  `json:"autoExtract,omitempty"`
	Chunks      *int   `json:"chunks,omitempty"`

	// Reject drops the link instead of taking it. Reason is shown to the user
	// alongside the rule's name; when it is empty Check writes one, because a
	// rejection nobody can explain is the behaviour this package exists to
	// avoid.
	Reject bool   `json:"reject,omitempty"`
	Reason string `json:"reason,omitempty"`
}

// Rule is one entry in a Set. All of its conditions must hold for it to match,
// so an "either/or" is written as two rules; that keeps the rule list something
// a person can read top to bottom without tracking precedence.
//
// A rule with no conditions matches every link. That is deliberate: it is how a
// catch-all default folder or a blanket reject at the end of a filter is
// written.
//
// The flag is Disabled rather than Enabled so the zero value is a live rule.
// A client that posts a rule without the field means to add a rule, and the
// other way round it would arrive switched off with nothing to explain why.
type Rule struct {
	Name       string      `json:"name,omitempty"`
	Disabled   bool        `json:"disabled,omitempty"`
	Conditions []Condition `json:"conditions,omitempty"`
	Action     Action      `json:"action"`
}

// Set is one ordered rule list, persisted as part of the settings.
type Set struct {
	Rules []Rule `json:"rules,omitempty"`
	// Disabled is the master switch for the whole list, so somebody can find out
	// whether a rule set is the reason for what they are seeing without deleting
	// it or switching off nine rules one at a time.
	//
	// It is spelled Disabled and not Enabled for the same reason Rule.Disabled is:
	// the zero value has to be a live set. An Enabled field arrives false in every
	// stored settings file written before it existed, which would switch both the
	// Packagizer and the link filter off on the first boot after the upgrade — and
	// the symptom reads as a matching bug, not as a settings bug, so it is looked
	// for in the wrong place.
	Disabled bool `json:"disabled,omitempty"`
	// StopAfterMatch ends evaluation at the first rule that matches. The
	// Packagizer wants it off, so every matching rule contributes and a later
	// rule wins per field; a filter usually wants it on, so an accept placed
	// above a broad reject actually protects the link.
	StopAfterMatch bool `json:"stopAfterMatch,omitempty"`
}

// Candidate is the link a rule set is asked about. Hoster and Filetype are
// derived from URL and Filename when left empty, so the ordinary caller fills
// in what it has and nothing more.
//
// Added is what the date variables format. It is an argument rather than a
// clock read inside the package, so a rule set's output is a pure function of
// its input and a test never has to wait for a second to pass.
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

// hostOf mirrors the helper of the same name in internal/app: a rule written
// against "example.org" has to match the same links the download folder's
// <jd:hoster> lands in, and two different ideas of what the hoster is would
// scatter one site's downloads across two folders.
func hostOf(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.Hostname() == "" {
		return raw
	}
	return strings.TrimPrefix(strings.ToLower(u.Hostname()), "www.")
}

// Effect is what the Packagizer decided for one link. Empty strings and nil
// pointers mean "no rule had an opinion", so the caller applies only what is
// set and leaves the rest of the task as it was.
type Effect struct {
	Package     string `json:"package,omitempty"`
	Dir         string `json:"dir,omitempty"`
	Filename    string `json:"filename,omitempty"`
	Comment     string `json:"comment,omitempty"`
	Priority    *int   `json:"priority,omitempty"`
	AutoExtract *bool  `json:"autoExtract,omitempty"`
	Chunks      *int   `json:"chunks,omitempty"`
	// Matched names the rules that fired, in the order they fired, so the
	// interface can answer "why did this land here" without re-running anything.
	Matched []string `json:"matched,omitempty"`
}

// Verdict is what the filter decided. Rule is set whenever a rule decided the
// outcome, including an explicit accept, so a link that survived a filter can
// say what let it through as well as what would have stopped it.
type Verdict struct {
	Rejected bool   `json:"rejected"`
	Rule     string `json:"rule,omitempty"`
	Reason   string `json:"reason,omitempty"`
}

// Problem is one rule Compile could not use, in words meant for the user.
type Problem struct {
	Index   int    `json:"index"` // position in Set.Rules, zero-based
	Rule    string `json:"rule"`  // the rule's name, or its position when unnamed
	Message string `json:"message"`
}

func (p Problem) Error() string { return fmt.Sprintf("%s: %s", p.Rule, p.Message) }

// Bounds a rule may not exceed. Priority matches the clamp in app.SetPriority
// so a rule cannot hand a task a priority the interface has no way to undo.
// MaxChunks is a guard rail rather than tuning: connections beyond a handful
// buy nothing on a hoster that rate-limits per file and are a reliable way to
// get an account flagged.
const (
	PriorityMin = -2
	PriorityMax = 2
	MaxChunks   = 16
)

// maxPattern caps the source length of a user's regular expression.
//
// Go's regexp is RE2, so no pattern can backtrack catastrophically the way a
// PCRE or Java one can — matching is linear in the subject either way. What a
// very long pattern still buys is a very large compiled program: the match cost
// grows with the pattern as well as with the link, and this runs once per
// condition per link on a paste of several thousand. 512 bytes is far more than
// any release-name or hoster pattern needs, and small enough that a blob of
// text pasted into the wrong box cannot become the engine's inner loop.
const maxPattern = 512

// cond is a compiled Condition. The comparison value is lower-cased here, once,
// rather than on every link.
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
	// index is the rule's position in the Set it came from. Compile drops rules,
	// so a compiled rule's position in the Matcher is not its position in the list
	// the user is looking at, and a dry run that pointed at the wrong row would be
	// worse than one that pointed at nothing.
	index int
	name  string
	conds []cond
	act   Action
	// wantsGroups is set when any of this rule's templates reads a capture group.
	// Collecting submatches costs an allocation per regular-expression condition
	// where MatchString costs none, and this runs once per condition per link on a
	// paste of several thousand — so the rules that do not ask do not pay.
	wantsGroups bool
}

// groups holds the submatches of a matching rule's regular-expression
// conditions, keyed by the field each pattern ran against. It backs
// <jd:match:FIELD:N> and nothing else.
type groups map[Field][]string

// Matcher is a compiled Set. It is safe for concurrent use, which matters
// because links are staged from several goroutines and there is exactly one
// Matcher per rule set.
type Matcher struct {
	stopAfterMatch bool
	rules          []compiled

	// seen backs <jd:append>. It is the only state a Matcher carries between
	// calls, which is unavoidable: a de-duplicating counter has to remember what
	// it already handed out.
	mu   sync.Mutex
	seen map[string]int
}

// maxAppendKeys caps that memory. A long-running server sees an unbounded
// number of distinct names, and a map that grew with every one of them would be
// a slow leak with no ceiling. Past the cap a name new to the counter simply
// gets no suffix, which is the same thing that happens to the first link
// carrying any name.
const maxAppendKeys = 4096

// Compile validates a rule set once and returns a Matcher plus every rule it
// had to leave out. It never returns nil: a caller that ignores the problems
// still gets a defined Matcher — one that does nothing — instead of a nil
// dereference on the first link.
//
// A rule with any problem at all is dropped whole rather than partly applied. A
// rule missing one of its conditions is not a stricter rule, it is a rule that
// does something the user never asked for, and for a filter that means links
// disappearing on a typo.
// A set switched off compiles to that same empty Matcher and reports nothing,
// exactly as a switched-off rule does. Off means off: leaving the problems
// standing would keep a page badge lit about rules that are not being applied,
// and there would then be no way to silence a rule set short of deleting it. The
// editor gets its problems from Preview, which validates the set as written
// whether or not it is switched on, so a set can still be fixed while it is off.
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
// with it. A rule with any problem at all is dropped whole by the caller rather
// than partly applied: a rule missing one of its conditions is not a stricter
// rule, it is a rule that does something the user never asked for, and for a
// filter that means links disappearing on a typo.
func compileRule(r Rule, index int) (compiled, []Problem) {
	name := ruleName(r, index)
	var problems []Problem
	report := func(format string, args ...any) {
		problems = append(problems, Problem{Index: index, Rule: name, Message: fmt.Sprintf(format, args...)})
	}
	c := compiled{index: index, name: name, act: snapshot(r.Action), wantsGroups: readsGroups(r.Action)}
	for j, raw := range r.Conditions {
		cc, err := compileCondition(raw)
		if err != nil {
			report("condition %d (%s): %v", j+1, raw.Field, err)
			continue
		}
		c.conds = append(c.conds, cc)
	}
	for _, msg := range actionProblems(r.Action, c.conds) {
		report("%s", msg)
	}
	return c, problems
}

// snapshot copies the action's pointer values, so a compiled rule stops
// referring to the Set it was built from. Compile is the one moment the caller
// hands its data over; while the pointers stayed shared, a settings struct
// edited in place would silently re-aim rules that are already packaging links,
// with nothing in the rule list on screen to show for it.
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

// ruleName is what a problem, an Effect and a rejection call the rule. An
// unnamed rule still has to be findable, and its position is the only handle
// the user has on it.
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
// and a later rule wins per field, unless the set asked to stop at the first
// match.
//
// Only the template that survived the whole list is expanded, and it is expanded
// after the loop rather than inside it. Expanding as we go would spend the work
// on values a later rule immediately overwrites, and — the reason this is a
// correctness question rather than a performance one — <jd:append> would count
// every one of those discarded values. Two rules both writing an <jd:append>
// package name would hand the very first link "_2", a de-duplication of nothing,
// and the suffix would then say how many rules touched the field rather than how
// often that name has been seen.
func (m *Matcher) Apply(c Candidate) Effect {
	c = c.filled()
	var e Effect
	// The winning template per field, still unexpanded, together with the
	// submatches of the rule that set it: <jd:match:...> in rule seven reads
	// rule seven's own pattern, so the groups travel with the template rather
	// than being collected globally.
	var pkg, dir, name, comment tpl
	m.walk(c, func(r compiled, g groups) bool {
		e.Matched = append(e.Matched, r.name)
		a := r.act
		// An empty string is "this rule has no opinion", never "clear it", so a
		// rule setting only the folder leaves an earlier package name standing.
		if a.PackageName != "" {
			pkg = tpl{a.PackageName, g}
		}
		if a.DownloadDir != "" {
			dir = tpl{a.DownloadDir, g}
		}
		if a.Filename != "" {
			name = tpl{a.Filename, g}
		}
		if a.Comment != "" {
			comment = tpl{a.Comment, g}
		}
		// The values are copied rather than the pointers. Handing the rule's own
		// pointer to the caller would let anything that writes through the Effect
		// rewrite the compiled rule, and every later link would then be packaged
		// by a rule the user never edited.
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
		// The folder is not checked for being absolute or writable here. That
		// check is filesystem work, and filepath.IsAbs answers differently on
		// Windows and Linux, so a rule written on one would be rejected by the
		// other. The caller validates the folder it is about to use.
		e.Dir = m.expand(dir.text, "dir", c, dir.groups)
	}
	if name.text != "" {
		// Cut to one segment after expanding, not before: the append counter
		// holds its place with a byte sanitising would eat. A rule renaming a
		// file to "../../x" is how a download lands outside the folder every
		// other part of this package works to keep it in.
		e.Filename = segment(m.expand(name.text, string(FieldFilename), c, name.groups), "file")
	}
	if comment.text != "" {
		e.Comment = m.expand(comment.text, "comment", c, comment.groups)
	}
	return e
}

// tpl is a template that has won its field, carried together with the capture
// groups of the rule it came from.
type tpl struct {
	text   string
	groups groups
}

// walk visits every rule that matches, in the order they are written, and stops
// when visit returns false or when the set stops at the first match.
//
// Apply, Check and Preview all evaluate through this one function. A dry run
// that walked the list its own way could report an order that staging never
// takes — and a test box that disagrees with the engine is worse than no test
// box, because it is believed.
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

// Check runs the filter flavour. The first matching rule decides: a reject ends
// it, and an explicit accept ends it too when the set stops at the first match,
// which is how a narrow "keep this" placed above a broad "drop that" survives.
// A link no rule matched is accepted, so an empty or broken filter takes
// everything rather than nothing.
func (m *Matcher) Check(c Candidate) Verdict {
	c = c.filled()
	var v Verdict
	m.walk(c, func(r compiled, g groups) bool {
		if r.act.Reject {
			v = Verdict{Rejected: true, Rule: r.name, Reason: m.reason(r, c, g)}
			return false
		}
		if m.stopAfterMatch {
			v = Verdict{Rule: r.name}
		}
		return true
	})
	return v
}

// reason is never empty. A rule that rejects without saying why still has a
// name, and "some rule dropped it" is exactly the answer this package refuses
// to give.
func (m *Matcher) reason(r compiled, c Candidate, g groups) string {
	if r.act.Reason != "" {
		if out := strings.TrimSpace(m.expand(r.act.Reason, "reason", c, g)); out != "" {
			return out
		}
	}
	return fmt.Sprintf("blocked by filter rule %q", r.name)
}

// ResetAppend clears the <jd:append> counter, so a caller that treats each
// paste as a fresh batch can start its numbering over instead of continuing
// from wherever the last batch left off.
func (m *Matcher) ResetAppend() {
	m.mu.Lock()
	m.seen = nil
	m.mu.Unlock()
}

// evaluate reports whether every condition holds, and hands back the capture
// groups a rule that reads them needs. A rule with no conditions matches, which
// is what makes a catch-all rule expressible.
//
// Where two "matches" conditions test the same field, the first one wins the
// group table. The rule is read top to bottom on screen, so the first pattern is
// the one somebody counting brackets is looking at.
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
	// Unreachable: Compile drops every rule holding an operator not listed
	// above. Not matching is still the right answer if it ever is reached — for
	// a filter it means the link is kept, and a kept link can be removed by
	// hand while a dropped one is simply gone.
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
	// An empty value is a form the user did not finish, not a test. Left alone
	// it would make "contains" match every link on earth, and in a filter that
	// is a rule that rejects the entire paste.
	if strings.TrimSpace(c.Value) == "" {
		return cond{}, fmt.Errorf("%s has no value; an empty test would match every link", c.Op)
	}
	if c.Op == OpMatches {
		// The length is checked before the compile, not after: the point is to
		// never hand regexp a pattern of unknown size in the first place. The
		// pattern is used untrimmed, because trailing whitespace can be part of
		// what the user meant to match.
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
	// A file type typed as ".mkv" is the same wish as "mkv". Without this the
	// rule silently never fires, and the user has no way to see the dot is the
	// reason.
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
		// Both boxes left empty is the numeric spelling of the unfinished form
		// the string operators already refuse: the range it describes is "any
		// size at all", so the condition holds for every link and a filter rule
		// built on it rejects the entire paste. A real range fills at least one
		// box — "up to 100 MB" sets Max, "at least 500 MB" sets Min, and "exactly
		// nothing" is what equals 0 is for.
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
	// The text operators are refused rather than run on the digits: "filesize
	// contains 100" would match 1000, 2100 and 100000, which is never what
	// anybody meant by it.
	return cond{}, fmt.Errorf("%s cannot compare a file size; use is-between, equals or equals-not", c.Op)
}

// templates lists an action's template fields with the word each one is called
// by in a message, so a problem can name the box the user has to go and fix.
func (a Action) templates() []struct{ Label, Text string } {
	return []struct{ Label, Text string }{
		{"package name", a.PackageName},
		{"download folder", a.DownloadDir},
		{"file name", a.Filename},
		{"comment", a.Comment},
		{"reason", a.Reason},
	}
}

// readsGroups reports whether any of an action's templates asks for a capture
// group, which is what decides whether matching pays for submatches.
func readsGroups(a Action) bool {
	for _, f := range a.templates() {
		if matchTag.MatchString(f.Text) {
			return true
		}
	}
	return false
}

// actionProblems lists everything wrong with an action, including the ways it
// disagrees with the conditions of the rule it belongs to.
func actionProblems(a Action, conds []cond) []string {
	msgs := matchTagProblems(a, conds)
	if a.Priority != nil && (*a.Priority < PriorityMin || *a.Priority > PriorityMax) {
		msgs = append(msgs, fmt.Sprintf("priority %d is outside %d..%d", *a.Priority, PriorityMin, PriorityMax))
	}
	// Zero chunks is not "the default", it is a download with no connection at
	// all; the caller has no way to tell that apart from a deliberate setting
	// once the pointer is non-nil.
	if a.Chunks != nil && (*a.Chunks < 1 || *a.Chunks > MaxChunks) {
		msgs = append(msgs, fmt.Sprintf("chunk count %d is outside 1..%d", *a.Chunks, MaxChunks))
	}
	for _, f := range a.templates() {
		if unterminated(f.Text) {
			msgs = append(msgs, fmt.Sprintf("the %s opens a <jd:...> it never closes", f.Label))
		}
	}
	return msgs
}

// matchTagProblems refuses a <jd:match:FIELD:N> that can never resolve.
//
// This is the one placeholder whose failure is invisible: an out-of-range date
// format or a misspelled variable stays in the text where somebody can see it,
// but a capture group is reached for by people copying JDownloader templates,
// where <jd:source:1> means a group and here it does not. Left to run, the tag
// survives into the folder name and every download in the set lands under a
// literal "<jd:match:source:1>". Refusing the rule at save time puts the message
// next to the box that is wrong.
func matchTagProblems(a Action, conds []cond) []string {
	var msgs []string
	for _, f := range a.templates() {
		for _, m := range matchTag.FindAllStringSubmatch(f.Text, -1) {
			field := Field(strings.ToLower(m[1]))
			n, err := strconv.Atoi(m[2])
			if err != nil {
				continue // the pattern only matches digits; unreachable
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

// patternFor is the regular expression a rule tests one field with, or nil when
// it tests it some other way or not at all. The first one wins, exactly as
// evaluate resolves it.
func patternFor(conds []cond, f Field) *regexp.Regexp {
	for _, cd := range conds {
		if cd.field == f && cd.op == OpMatches {
			return cd.re
		}
	}
	return nil
}

// unterminated reports a template that opens a placeholder it never closes.
// It is caught at compile time because pathvars deliberately leaves such a
// template untouched, which puts the raw tag into a folder or file name where
// nobody sees it until the download has already landed under it.
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
