package rules

// The dry run behind the test box. Compile catches rules that cannot run;
// this shows what the others do, since a filter that drops too much does so
// silently and a wrong folder template is only noticed on disk.
//
// Preview compiles its own Matcher, because <jd:append> counts: run against
// the live Matcher, previewing three links would hand the next real download
// "_4".

import "strings"

// Report is what a whole dry run answers with.
type Report struct {
	// Problems is every rule Compile could not use, keyed to its position in
	// the set so the editor can show the message on the rule.
	Problems []Problem `json:"problems"`
	// Rules has one entry per rule, in order, including disabled and broken
	// ones, so every rule can say why it did nothing.
	Rules []RuleReport `json:"rules"`
	// Links is one entry per sample, in the order they were given.
	Links []LinkReport `json:"links"`
	// Disabled reports that the set's master switch is off. The rest of the
	// report is filled in as though it were on, so a disabled set can still
	// be repaired.
	Disabled bool `json:"disabled,omitempty"`
}

// RuleReport is one rule's own verdict on the dry run.
type RuleReport struct {
	Index    int    `json:"index"`
	Name     string `json:"name"`
	Disabled bool   `json:"disabled,omitempty"`
	// Problems is this rule's share of Report.Problems, repeated here so a
	// client does not have to filter the flat list by index.
	Problems []Problem `json:"problems,omitempty"`
	// Matched is how many of the samples this rule fired on.
	Matched int `json:"matched"`
}

// LinkReport is what would happen to one sample link.
type LinkReport struct {
	URL      string `json:"url"`
	Filename string `json:"filename"`
	// Matched is the rules that fired, by index, in the order they fired.
	Matched []int `json:"matched"`
	// Effect and Verdict are what the two engines return, from the same code
	// staging calls.
	Effect  Effect  `json:"effect"`
	Verdict Verdict `json:"verdict"`
	// Result is where the link would end up, with the fields no rule touched
	// filled in from the link itself, where Effect leaves them empty.
	Result Outcome `json:"result"`
}

// Outcome is the two values a dry run can state outright. The folder is not
// one of them: without a rule naming it, the settings decide, and this package
// cannot know them.
type Outcome struct {
	Package  string `json:"package"`
	Filename string `json:"filename"`
}

// Preview runs a set against a batch of sample links and says what each rule
// and each link did. The set's master switch is ignored so a disabled set can
// still be worked on, and Report.Disabled carries the real state. Disabled
// rules stay off.
func Preview(s Set, cands []Candidate) Report {
	live := s
	live.Disabled = false
	m, problems := Compile(live)

	rep := Report{
		Problems: problemsOrEmpty(problems),
		Rules:    make([]RuleReport, 0, len(s.Rules)),
		Links:    make([]LinkReport, 0, len(cands)),
		Disabled: s.Disabled,
	}
	// at maps a rule's index to its report, so matches are counted without a
	// scan per link.
	at := make(map[int]int, len(s.Rules))
	for i, r := range s.Rules {
		at[i] = len(rep.Rules)
		rep.Rules = append(rep.Rules, RuleReport{
			Index:    i,
			Name:     ruleName(r, i),
			Disabled: r.Disabled,
			Problems: problemsAt(problems, i),
		})
	}

	for _, c := range cands {
		c = c.filled()
		l := LinkReport{URL: c.URL, Filename: c.Filename, Matched: []int{}}
		m.walk(c, func(r compiled, _ groups) bool {
			l.Matched = append(l.Matched, r.index)
			if j, ok := at[r.index]; ok {
				rep.Rules[j].Matched++
			}
			return true
		})
		// Apply runs before Check, as in staging, so the append counter
		// advances in the same order.
		l.Effect = m.Apply(c)
		l.Verdict = m.Check(c)
		l.Result = Outcome{
			Package:  firstNonEmpty(l.Effect.Package, c.Package),
			Filename: firstNonEmpty(l.Effect.Filename, c.Filename),
		}
		rep.Links = append(rep.Links, l)
	}
	return rep
}

// problemsAt is one rule's problems, by its position in the set.
func problemsAt(all []Problem, index int) []Problem {
	var out []Problem
	for _, p := range all {
		if p.Index == index {
			out = append(out, p)
		}
	}
	return out
}

func problemsOrEmpty(in []Problem) []Problem {
	if in == nil {
		return []Problem{}
	}
	return in
}

func firstNonEmpty(a, b string) string {
	if strings.TrimSpace(a) != "" {
		return a
	}
	return b
}
