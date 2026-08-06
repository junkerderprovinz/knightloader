package rules

import (
	"encoding/json"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

func ptr[T any](v T) *T { return &v }

// testCandidate is one link with every field populated differently, so a
// condition reading the wrong field fails the test instead of accidentally
// matching the right value.
func testCandidate() Candidate {
	return Candidate{
		Filename: "The.Show.S01E02.1080p.mkv",
		URL:      "https://www.example.org/files/abc123/The.Show.S01E02.1080p.mkv",
		Source:   "https://tracker.example.net/tv/season-1/index.html",
		Filesize: 1_400_000_000,
		Package:  "The Show",
		Added:    time.Date(2026, 3, 7, 9, 5, 4, 0, time.UTC),
	}
}

// TestCompileRejectsUnusableRules is the whole reason Compile exists as a
// separate step. Every case here is a rule a web form can produce, and every
// one of them would otherwise either crash on the first link, match nothing
// with no explanation, or - worst of all in a filter - match everything.
func TestCompileRejectsUnusableRules(t *testing.T) {
	long := strings.Repeat("a", maxPattern+1)
	cases := []struct {
		name string
		rule Rule
		want string // substring of the message shown to the user
	}{
		{"unknown field", Rule{Conditions: []Condition{{Field: "colour", Op: OpContains, Value: "red"}}}, "unknown field"},
		{"unknown operator", Rule{Conditions: []Condition{{Field: FieldFilename, Op: "starts-with", Value: "x"}}}, "unknown operator"},
		{"empty value", Rule{Conditions: []Condition{{Field: FieldFilename, Op: OpContains}}}, "no value"},
		{"blank value", Rule{Conditions: []Condition{{Field: FieldFilename, Op: OpEquals, Value: "   "}}}, "no value"},
		{"invalid regex", Rule{Conditions: []Condition{{Field: FieldFilename, Op: OpMatches, Value: "(unclosed"}}}, "invalid regular expression"},
		{"regex too long", Rule{Conditions: []Condition{{Field: FieldFilename, Op: OpMatches, Value: long}}}, "the limit is"},
		{"between on text", Rule{Conditions: []Condition{{Field: FieldFilename, Op: OpBetween, Min: 1, Max: 2}}}, "only works on filesize"},
		{"contains on size", Rule{Conditions: []Condition{{Field: FieldFilesize, Op: OpContains, Value: "100"}}}, "cannot compare a file size"},
		{"regex on size", Rule{Conditions: []Condition{{Field: FieldFilesize, Op: OpMatches, Value: "^1"}}}, "cannot compare a file size"},
		{"size is not a number", Rule{Conditions: []Condition{{Field: FieldFilesize, Op: OpEquals, Value: "700 MB"}}}, "not a size in bytes"},
		{"negative bound", Rule{Conditions: []Condition{{Field: FieldFilesize, Op: OpBetween, Min: -1, Max: 10}}}, "negative size"},
		// The numeric spelling of the unfinished form: a size condition with both
		// boxes empty holds for every link, so a filter built on it takes out the
		// whole paste. The string operators have always refused this shape.
		{"range with no bounds", Rule{Conditions: []Condition{{Field: FieldFilesize, Op: OpBetween}}}, "no bounds"},
		{"bounds swapped", Rule{Conditions: []Condition{{Field: FieldFilesize, Op: OpBetween, Min: 100, Max: 10}}}, "above the upper bound"},
		{"priority too high", Rule{Action: Action{Priority: ptr(9)}}, "outside -2..2"},
		{"priority too low", Rule{Action: Action{Priority: ptr(-9)}}, "outside -2..2"},
		{"no chunks", Rule{Action: Action{Chunks: ptr(0)}}, "outside 1..16"},
		{"too many chunks", Rule{Action: Action{Chunks: ptr(99)}}, "outside 1..16"},
		{"unterminated folder", Rule{Action: Action{DownloadDir: "/dl/<jd:packagename"}}, "never closes"},
		{"unterminated second tag", Rule{Action: Action{DownloadDir: "/dl/<jd:packagename>/<jd:hoster"}}, "never closes"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m, problems := Compile(Set{Rules: []Rule{c.rule}})
			if len(problems) != 1 {
				t.Fatalf("Compile reported %d problems, want 1: %v", len(problems), problems)
			}
			if !strings.Contains(problems[0].Message, c.want) {
				t.Errorf("message = %q, want it to contain %q", problems[0].Message, c.want)
			}
			// The rule is dropped whole. A half-applied rule does something the
			// user never wrote down.
			if !m.Empty() {
				t.Errorf("the broken rule was kept in the matcher")
			}
		})
	}
}

// TestCompileAcceptsTheEdgesOfEveryBound pins the values that are legal, so a
// tightened check cannot start refusing rules that were fine yesterday.
func TestCompileAcceptsTheEdgesOfEveryBound(t *testing.T) {
	cases := []struct {
		name string
		rule Rule
	}{
		{"no conditions at all", Rule{Action: Action{PackageName: "everything"}}},
		{"pattern at the length limit", Rule{Conditions: []Condition{{Field: FieldURL, Op: OpMatches, Value: strings.Repeat("a", maxPattern)}}}},
		{"open ended range", Rule{Conditions: []Condition{{Field: FieldFilesize, Op: OpBetween, Min: 500}}}},
		{"range with only a ceiling", Rule{Conditions: []Condition{{Field: FieldFilesize, Op: OpBetween, Max: 500}}}},
		{"size of zero", Rule{Conditions: []Condition{{Field: FieldFilesize, Op: OpEquals, Value: "0"}}}},
		{"lowest priority", Rule{Action: Action{Priority: ptr(PriorityMin)}}},
		{"highest priority", Rule{Action: Action{Priority: ptr(PriorityMax)}}},
		{"one chunk", Rule{Action: Action{Chunks: ptr(1)}}},
		{"most chunks", Rule{Action: Action{Chunks: ptr(MaxChunks)}}},
		{"auto extract off", Rule{Action: Action{AutoExtract: ptr(false)}}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m, problems := Compile(Set{Rules: []Rule{c.rule}})
			if len(problems) != 0 {
				t.Fatalf("Compile refused a usable rule: %v", problems)
			}
			if m.Empty() {
				t.Errorf("the rule compiled without problems but is not in the matcher")
			}
		})
	}
}

// TestBrokenRuleNeverEatsLinks is the failure the whole package is built
// around. A filter rule whose regex does not compile must not turn into a rule
// that rejects nothing quietly, and above all must not take the rest of the
// filter down with it.
func TestBrokenRuleNeverEatsLinks(t *testing.T) {
	m, problems := Compile(Set{
		StopAfterMatch: true,
		Rules: []Rule{
			{Name: "broken", Conditions: []Condition{{Field: FieldFilename, Op: OpMatches, Value: "*.mkv"}}, Action: Action{Reject: true}},
			{Name: "no samples", Conditions: []Condition{{Field: FieldFilename, Op: OpContains, Value: "sample"}}, Action: Action{Reject: true, Reason: "samples are noise"}},
		},
	})
	if len(problems) != 1 || problems[0].Rule != "broken" {
		t.Fatalf("problems = %v, want exactly the broken rule reported", problems)
	}
	// The link the broken rule was aimed at survives: a kept link can still be
	// deleted by hand, a silently dropped one cannot be recovered at all.
	if v := m.Check(testCandidate()); v.Rejected {
		t.Errorf("a link was rejected by a rule that failed to compile: %+v", v)
	}
	// The healthy rule below it still works.
	c := testCandidate()
	c.Filename = "The.Show.S01E02-sample.mkv"
	v := m.Check(c)
	if !v.Rejected || v.Rule != "no samples" || v.Reason != "samples are noise" {
		t.Errorf("Check = %+v, want a rejection naming \"no samples\"", v)
	}
}

// TestCompileNeverReturnsNil covers the caller that ignores the problems: it
// must still get a matcher it can call, not a nil pointer that panics on the
// first link of the first paste.
func TestCompileNeverReturnsNil(t *testing.T) {
	for _, s := range []Set{
		{},
		{Rules: []Rule{}},
		{Rules: []Rule{{Conditions: []Condition{{Field: "nonsense", Op: OpContains, Value: "x"}}}}},
	} {
		m, _ := Compile(s)
		if m == nil {
			t.Fatalf("Compile(%+v) returned a nil matcher", s)
		}
		if !m.Empty() {
			t.Errorf("Compile(%+v) produced usable rules, want none", s)
		}
		if e := m.Apply(testCandidate()); len(e.Matched) != 0 {
			t.Errorf("Apply on an empty matcher matched %v", e.Matched)
		}
		if v := m.Check(testCandidate()); v.Rejected {
			t.Errorf("Check on an empty matcher rejected the link")
		}
	}
}

// TestZeroValueRuleIsEnabled pins the flag's polarity through the JSON, which
// is how rules actually arrive. A client that omits the field means to add a
// working rule; the opposite spelling would store it switched off with nothing
// on screen to explain why it never fires.
func TestZeroValueRuleIsEnabled(t *testing.T) {
	var s Set
	if err := json.Unmarshal([]byte(`{"rules":[{"name":"keep","action":{"packageName":"x"}}]}`), &s); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	m, problems := Compile(s)
	if len(problems) != 0 {
		t.Fatalf("problems = %v", problems)
	}
	if m.Empty() {
		t.Fatalf("a rule posted without \"disabled\" was compiled as disabled")
	}
	// The round trip must not start writing the default back out, or every
	// stored rule grows a field the user never set.
	b, err := json.Marshal(s)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if strings.Contains(string(b), "disabled") {
		t.Errorf("marshalled set carries the default flag: %s", b)
	}
}

func TestDisabledRuleIsSkipped(t *testing.T) {
	m, problems := Compile(Set{Rules: []Rule{
		// Disabled and broken: a rule that is switched off is not the user's
		// problem right now, so it must not fill the interface with errors.
		{Name: "off", Disabled: true, Conditions: []Condition{{Field: FieldFilename, Op: OpMatches, Value: "("}}, Action: Action{Reject: true}},
	}})
	if len(problems) != 0 {
		t.Errorf("a disabled rule was validated anyway: %v", problems)
	}
	if v := m.Check(testCandidate()); v.Rejected {
		t.Errorf("a disabled rule rejected a link")
	}
}

// TestConditionMatching walks every field and operator against one link.
func TestConditionMatching(t *testing.T) {
	cases := []struct {
		name string
		cond Condition
		want bool
	}{
		{"filename contains", Condition{Field: FieldFilename, Op: OpContains, Value: "S01E02"}, true},
		// The case fold is the point: a user typing lower case into a form does
		// not mean to let the capitalised spelling through.
		{"filename contains folds case", Condition{Field: FieldFilename, Op: OpContains, Value: "s01e02"}, true},
		{"filename contains misses", Condition{Field: FieldFilename, Op: OpContains, Value: "S02"}, false},
		{"filename contains-not", Condition{Field: FieldFilename, Op: OpContainsNot, Value: "sample"}, true},
		{"filename contains-not misses", Condition{Field: FieldFilename, Op: OpContainsNot, Value: "1080P"}, false},
		{"filename equals folds case", Condition{Field: FieldFilename, Op: OpEquals, Value: "the.show.s01e02.1080p.mkv"}, true},
		{"filename equals is not a substring test", Condition{Field: FieldFilename, Op: OpEquals, Value: "The.Show"}, false},
		{"filename equals-not", Condition{Field: FieldFilename, Op: OpEqualsNot, Value: "other.mkv"}, true},

		{"url contains", Condition{Field: FieldURL, Op: OpContains, Value: "/files/"}, true},

		// The hoster is derived from the URL, and "www." is stripped, the same
		// way internal/app does it.
		{"hoster equals without www", Condition{Field: FieldHoster, Op: OpEquals, Value: "example.org"}, true},
		{"hoster equals with www", Condition{Field: FieldHoster, Op: OpEquals, Value: "www.example.org"}, false},

		{"source contains", Condition{Field: FieldSource, Op: OpContains, Value: "tracker.example.net"}, true},
		{"source is not the url", Condition{Field: FieldSource, Op: OpContains, Value: "example.org"}, false},

		// The extension is derived from the file name, and a value typed with
		// the dot means the same thing as one without.
		{"filetype equals", Condition{Field: FieldFiletype, Op: OpEquals, Value: "mkv"}, true},
		{"filetype equals with a dot", Condition{Field: FieldFiletype, Op: OpEquals, Value: ".mkv"}, true},
		{"filetype equals-not", Condition{Field: FieldFiletype, Op: OpEqualsNot, Value: "rar"}, true},

		{"package contains", Condition{Field: FieldPackage, Op: OpContains, Value: "show"}, true},

		{"size in range", Condition{Field: FieldFilesize, Op: OpBetween, Min: 1_000_000_000, Max: 2_000_000_000}, true},
		{"size below range", Condition{Field: FieldFilesize, Op: OpBetween, Min: 2_000_000_000, Max: 3_000_000_000}, false},
		{"size on the lower bound", Condition{Field: FieldFilesize, Op: OpBetween, Min: 1_400_000_000, Max: 2_000_000_000}, true},
		{"size on the upper bound", Condition{Field: FieldFilesize, Op: OpBetween, Min: 1, Max: 1_400_000_000}, true},
		// An empty upper box means "no ceiling", not "up to zero bytes".
		{"size with no ceiling", Condition{Field: FieldFilesize, Op: OpBetween, Min: 1_000_000_000}, true},
		{"size with no ceiling below floor", Condition{Field: FieldFilesize, Op: OpBetween, Min: 9_000_000_000}, false},
		{"size equals", Condition{Field: FieldFilesize, Op: OpEquals, Value: "1400000000"}, true},
		{"size equals-not", Condition{Field: FieldFilesize, Op: OpEqualsNot, Value: "0"}, true},

		// The user's flags win on a regex, so a bare pattern stays case
		// sensitive and (?i) is honoured.
		{"regex is case sensitive", Condition{Field: FieldFilename, Op: OpMatches, Value: `the\.show`}, false},
		{"regex honours the flag", Condition{Field: FieldFilename, Op: OpMatches, Value: `(?i)the\.show`}, true},
		// Unanchored, so the anchors the user writes are the only ones there.
		{"regex is unanchored", Condition{Field: FieldFilename, Op: OpMatches, Value: `S01E\d\d`}, true},
		{"regex anchored by the user", Condition{Field: FieldFilename, Op: OpMatches, Value: `^S01E\d\d$`}, false},
		{"regex end anchor", Condition{Field: FieldFilename, Op: OpMatches, Value: `\.mkv$`}, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m, problems := Compile(Set{Rules: []Rule{{Name: "r", Conditions: []Condition{c.cond}, Action: Action{Reject: true}}}})
			if len(problems) != 0 {
				t.Fatalf("Compile: %v", problems)
			}
			if got := m.Check(testCandidate()).Rejected; got != c.want {
				t.Errorf("matched = %v, want %v", got, c.want)
			}
		})
	}
}

func TestRuleConditionsAreAnded(t *testing.T) {
	both := []Condition{
		{Field: FieldFiletype, Op: OpEquals, Value: "mkv"},
		{Field: FieldFilesize, Op: OpBetween, Min: 9_000_000_000},
	}
	m, problems := Compile(Set{Rules: []Rule{{Name: "r", Conditions: both, Action: Action{Reject: true}}}})
	if len(problems) != 0 {
		t.Fatalf("Compile: %v", problems)
	}
	if m.Check(testCandidate()).Rejected {
		t.Errorf("a rule fired with only one of its two conditions holding")
	}
}

// TestRuleWithoutConditionsMatchesEverything pins the deliberate catch-all,
// which is how a default folder or a closing blanket reject is written.
func TestRuleWithoutConditionsMatchesEverything(t *testing.T) {
	m, problems := Compile(Set{Rules: []Rule{{Name: "catch all", Action: Action{PackageName: "misc"}}}})
	if len(problems) != 0 {
		t.Fatalf("Compile: %v", problems)
	}
	for _, c := range []Candidate{{}, testCandidate(), {URL: "magnet:?xt=urn:btih:abc"}} {
		if e := m.Apply(c); e.Package != "misc" {
			t.Errorf("Apply(%+v).Package = %q, want misc", c, e.Package)
		}
	}
}

// TestApplyLaterRuleWinsPerField is the Packagizer's ordering rule: every
// matching rule contributes, and a rule that says nothing about a field must
// not clear what an earlier one put there.
func TestApplyLaterRuleWinsPerField(t *testing.T) {
	m, problems := Compile(Set{Rules: []Rule{
		{Name: "first", Action: Action{PackageName: "first pkg", DownloadDir: "/dl/first", Priority: ptr(1)}},
		{Name: "second", Action: Action{PackageName: "second pkg", Chunks: ptr(2)}},
	}})
	if len(problems) != 0 {
		t.Fatalf("Compile: %v", problems)
	}
	e := m.Apply(testCandidate())
	if e.Package != "second pkg" {
		t.Errorf("Package = %q, want the later rule's value", e.Package)
	}
	if e.Dir != "/dl/first" {
		t.Errorf("Dir = %q, want the earlier rule's value to survive", e.Dir)
	}
	if e.Priority == nil || *e.Priority != 1 {
		t.Errorf("Priority = %v, want the earlier rule's value to survive", e.Priority)
	}
	if e.Chunks == nil || *e.Chunks != 2 {
		t.Errorf("Chunks = %v, want 2", e.Chunks)
	}
	want := []string{"first", "second"}
	if len(e.Matched) != 2 || e.Matched[0] != want[0] || e.Matched[1] != want[1] {
		t.Errorf("Matched = %v, want %v", e.Matched, want)
	}
}

func TestApplyStopAfterMatch(t *testing.T) {
	set := Set{StopAfterMatch: true, Rules: []Rule{
		{Name: "first", Action: Action{PackageName: "first pkg"}},
		{Name: "second", Action: Action{PackageName: "second pkg", DownloadDir: "/dl/second"}},
	}}
	m, problems := Compile(set)
	if len(problems) != 0 {
		t.Fatalf("Compile: %v", problems)
	}
	e := m.Apply(testCandidate())
	if e.Package != "first pkg" || e.Dir != "" {
		t.Errorf("Apply = %+v, want only the first rule applied", e)
	}
	if len(e.Matched) != 1 {
		t.Errorf("Matched = %v, want one rule", e.Matched)
	}
}

// TestApplyDoesNotAliasRuleValues catches the copy that is easy to leave out:
// handing the rule's own pointer to the caller would let anything writing
// through the Effect rewrite the compiled rule, and every later link would then
// be packaged by a rule nobody edited.
func TestApplyDoesNotAliasRuleValues(t *testing.T) {
	prio, chunks, extract := 2, 8, true
	m, problems := Compile(Set{Rules: []Rule{
		{Name: "r", Action: Action{Priority: &prio, Chunks: &chunks, AutoExtract: &extract}},
	}})
	if len(problems) != 0 {
		t.Fatalf("Compile: %v", problems)
	}
	first := m.Apply(testCandidate())
	*first.Priority = -2
	*first.Chunks = 1
	*first.AutoExtract = false

	second := m.Apply(testCandidate())
	if *second.Priority != 2 || *second.Chunks != 8 || !*second.AutoExtract {
		t.Errorf("the rule was rewritten through the effect: priority %d, chunks %d, extract %v",
			*second.Priority, *second.Chunks, *second.AutoExtract)
	}
	if prio != 2 || chunks != 8 || !extract {
		t.Errorf("the caller's own values were rewritten: %d, %d, %v", prio, chunks, extract)
	}
}

// TestCheckAlwaysExplainsARejection is the promise the package makes. However
// sparse the rule is, a rejected link comes back with something the user can
// act on.
func TestCheckAlwaysExplainsARejection(t *testing.T) {
	cases := []struct {
		name       string
		rule       Rule
		wantRule   string
		wantReason string
	}{
		{
			name:       "reason given",
			rule:       Rule{Name: "no samples", Action: Action{Reject: true, Reason: "samples are noise"}},
			wantRule:   "no samples",
			wantReason: "samples are noise",
		},
		{
			name:       "no reason given",
			rule:       Rule{Name: "no samples", Action: Action{Reject: true}},
			wantRule:   "no samples",
			wantReason: `blocked by filter rule "no samples"`,
		},
		{
			// An unnamed rule is still findable by its position, which is the
			// only handle the user has on it.
			name:       "no name given",
			rule:       Rule{Action: Action{Reject: true}},
			wantRule:   "rule 1",
			wantReason: `blocked by filter rule "rule 1"`,
		},
		{
			name:       "reason is whitespace",
			rule:       Rule{Name: "x", Action: Action{Reject: true, Reason: "   "}},
			wantRule:   "x",
			wantReason: `blocked by filter rule "x"`,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m, problems := Compile(Set{Rules: []Rule{c.rule}})
			if len(problems) != 0 {
				t.Fatalf("Compile: %v", problems)
			}
			v := m.Check(testCandidate())
			if !v.Rejected {
				t.Fatalf("Check did not reject")
			}
			if v.Rule != c.wantRule {
				t.Errorf("Rule = %q, want %q", v.Rule, c.wantRule)
			}
			if v.Reason != c.wantReason {
				t.Errorf("Reason = %q, want %q", v.Reason, c.wantReason)
			}
		})
	}
}

// TestCheckOrdering pins what the stop flag buys a filter: with it on, an
// accept placed above a broad reject actually protects the link; with it off,
// the reject still wins.
func TestCheckOrdering(t *testing.T) {
	rules := []Rule{
		{Name: "keep mkv", Conditions: []Condition{{Field: FieldFiletype, Op: OpEquals, Value: "mkv"}}},
		{Name: "drop everything", Action: Action{Reject: true, Reason: "not on the list"}},
	}
	stop, problems := Compile(Set{StopAfterMatch: true, Rules: rules})
	if len(problems) != 0 {
		t.Fatalf("Compile: %v", problems)
	}
	if v := stop.Check(testCandidate()); v.Rejected || v.Rule != "keep mkv" {
		t.Errorf("Check = %+v, want an accept naming \"keep mkv\"", v)
	}
	// A link the accept does not cover still hits the blanket reject.
	other := testCandidate()
	other.Filename = "The.Show.S01E02.1080p.rar"
	other.Filetype = ""
	if v := stop.Check(other); !v.Rejected || v.Rule != "drop everything" {
		t.Errorf("Check = %+v, want the blanket reject", v)
	}

	run, problems := Compile(Set{Rules: rules})
	if len(problems) != 0 {
		t.Fatalf("Compile: %v", problems)
	}
	if v := run.Check(testCandidate()); !v.Rejected || v.Rule != "drop everything" {
		t.Errorf("Check = %+v, want the reject to win when evaluation does not stop", v)
	}
}

func TestCheckAcceptsWhatNoRuleMatched(t *testing.T) {
	m, problems := Compile(Set{Rules: []Rule{
		{Name: "no samples", Conditions: []Condition{{Field: FieldFilename, Op: OpContains, Value: "sample"}}, Action: Action{Reject: true}},
	}})
	if len(problems) != 0 {
		t.Fatalf("Compile: %v", problems)
	}
	v := m.Check(testCandidate())
	if v.Rejected || v.Rule != "" || v.Reason != "" {
		t.Errorf("Check = %+v, want a plain accept", v)
	}
}

// TestProblemNamesTheRule keeps the error text usable: a message with no rule
// and no position in it is not something anyone can act on.
func TestProblemNamesTheRule(t *testing.T) {
	_, problems := Compile(Set{Rules: []Rule{
		{Name: "ok"},
		{Name: "bad regex", Conditions: []Condition{{Field: FieldURL, Op: OpMatches, Value: "("}}},
	}})
	if len(problems) != 1 {
		t.Fatalf("problems = %v, want 1", problems)
	}
	p := problems[0]
	if p.Index != 1 || p.Rule != "bad regex" {
		t.Errorf("problem = %+v, want index 1 and the rule's name", p)
	}
	if !strings.Contains(p.Error(), "bad regex") || !strings.Contains(p.Error(), "condition 1") {
		t.Errorf("Error() = %q, want the rule and the condition in it", p.Error())
	}
}

// TestCandidateDerivesWhatTheCallerOmits means app.go can hand over the two
// fields it actually has and still write rules against the hoster and the
// extension.
func TestCandidateDerivesWhatTheCallerOmits(t *testing.T) {
	cases := []struct {
		name             string
		in               Candidate
		hoster, filetype string
	}{
		{"from url and name", Candidate{URL: "https://WWW.Example.ORG/a/b.mkv", Filename: "b.mkv"}, "example.org", "mkv"},
		{"explicit values win", Candidate{URL: "https://example.org/a", Filename: "b.mkv", Hoster: "cdn.example.org", Filetype: "iso"}, "cdn.example.org", "iso"},
		{"no extension", Candidate{URL: "https://example.org/a", Filename: "README"}, "example.org", ""},
		// A magnet link has no host at all; the raw string is the only honest
		// answer and must not come back empty.
		{"magnet keeps the raw link", Candidate{URL: "magnet:?xt=urn:btih:abc"}, "magnet:?xt=urn:btih:abc", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := c.in.filled()
			if got.Hoster != c.hoster {
				t.Errorf("Hoster = %q, want %q", got.Hoster, c.hoster)
			}
			if got.Filetype != c.filetype {
				t.Errorf("Filetype = %q, want %q", got.Filetype, c.filetype)
			}
		})
	}
}

// TestPatternIsCompiledOnce pins the promise Compile exists to keep. A paste is
// several thousand links and every one of them is run past every condition, so a
// regexp rebuilt inside the match loop is the difference between a paste that
// lands and one that hangs the server. Allocation count is the only handle on it
// from outside: regexp.Compile allocates dozens of objects, matching an already
// compiled pattern allocates none.
func TestPatternIsCompiledOnce(t *testing.T) {
	m, problems := Compile(Set{Rules: []Rule{{
		Name:       "big pattern",
		Conditions: []Condition{{Field: FieldFilename, Op: OpMatches, Value: `(?i)^(the|a)\.[a-z.]+\.s\d\d(e\d\d)+\.(720|1080|2160)p\.(web|bluray)`}},
		Action:     Action{Reject: true},
	}}})
	if len(problems) != 0 {
		t.Fatalf("Compile: %v", problems)
	}
	// A candidate that does not match, so the measurement is the matching work
	// alone and not the rejection message built afterwards.
	c := Candidate{Filename: "unrelated.file.txt", Hoster: "example.org", Filetype: "txt"}
	if v := m.Check(c); v.Rejected {
		t.Fatalf("the probe candidate matched: %+v", v)
	}
	if n := testing.AllocsPerRun(100, func() { m.Check(c) }); n > 4 {
		t.Errorf("Check allocates %v objects per link, which is a pattern being rebuilt per link rather than at compile time", n)
	}
}

// TestCompiledRuleDoesNotFollowTheCallersSet: Compile is the moment the caller
// hands its data over. The optional action values arrive as pointers, and while
// the matcher kept them, editing the stored settings in place would re-aim rules
// that are already packaging links with nothing on screen to show for it.
func TestCompiledRuleDoesNotFollowTheCallersSet(t *testing.T) {
	prio, chunks, extract := 2, 8, true
	m, problems := Compile(Set{Rules: []Rule{
		{Name: "r", Action: Action{Priority: &prio, Chunks: &chunks, AutoExtract: &extract}},
	}})
	if len(problems) != 0 {
		t.Fatalf("Compile: %v", problems)
	}
	prio, chunks, extract = -2, 1, false

	e := m.Apply(testCandidate())
	if *e.Priority != 2 || *e.Chunks != 8 || !*e.AutoExtract {
		t.Errorf("the compiled rule followed the caller's edit: priority %d, chunks %d, extract %v",
			*e.Priority, *e.Chunks, *e.AutoExtract)
	}
}

// TestMatcherIsSafeForConcurrentUse backs the claim in Matcher's documentation.
// Links are staged from several goroutines against the one matcher a rule set
// compiles to, and the append counter is shared state behind all of them. The
// runtime throws on a concurrent map write whether or not the race detector is
// on, so this catches a dropped lock even in a build without cgo.
func TestMatcherIsSafeForConcurrentUse(t *testing.T) {
	m, problems := Compile(Set{Rules: []Rule{
		{Name: "pack", Conditions: []Condition{{Field: FieldFiletype, Op: OpEquals, Value: "mkv"}}, Action: Action{PackageName: "<jd:hoster><jd:append>"}},
		{Name: "drop", Conditions: []Condition{{Field: FieldFilename, Op: OpMatches, Value: `(?i)sample`}}, Action: Action{Reject: true}},
	}})
	if len(problems) != 0 {
		t.Fatalf("Compile: %v", problems)
	}
	var wg sync.WaitGroup
	for i := range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			c := testCandidate()
			c.Package = "pkg" + strconv.Itoa(i)
			for range 250 {
				m.Apply(c)
				m.Check(c)
			}
		}()
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		for range 250 {
			m.ResetAppend()
		}
	}()
	wg.Wait()
}

// TestApplyFilenameIsOneSegment: the folder is the only field allowed to spell
// out levels. A rename that carries a separator is not a name, it is a way out
// of the folder the caller picked, and the caller has no reason to expect one
// back from a field called Filename.
func TestApplyFilenameIsOneSegment(t *testing.T) {
	m, problems := Compile(Set{Rules: []Rule{
		{Name: "rename", Action: Action{Filename: `../../etc/<jd:orgfilename>`}},
	}})
	if len(problems) != 0 {
		t.Fatalf("Compile: %v", problems)
	}
	got := m.Apply(testCandidate()).Filename
	if strings.ContainsAny(got, `/\`) {
		t.Errorf("Filename = %q, which is a path and not a name", got)
	}
	if !strings.Contains(got, "The.Show") {
		t.Errorf("Filename = %q, want the expanded name still in it", got)
	}
}

func TestUnterminatedPlaceholder(t *testing.T) {
	cases := map[string]bool{
		"":                                 false,
		"/downloads":                       false,
		"/downloads/<jd:packagename>":      false,
		"/downloads/<jd:packagename>/<x>":  false,
		"/downloads/<jd:packagename":       true,
		"/dl/<jd:packagename>/<jd:hoster":  true,
		"/dl/<JD:PackageName>/<JD:Hoster":  true,
		"/dl/<jd:hoster>/<jd:packagename>": false,
	}
	for in, want := range cases {
		if got := unterminated(in); got != want {
			t.Errorf("unterminated(%q) = %v, want %v", in, got, want)
		}
	}
}
