package rules

// The dry run behind the test box.
//
// A rule list is the one part of a downloader whose mistakes are invisible. A
// filter that drops too much drops it silently — that is what dropping means —
// and a Packagizer folder template that resolves to the wrong thing is only
// found by looking at the disk afterwards. Compile catches what cannot run at
// all; this catches what runs and does the wrong thing, which is the larger half.
//
// Preview is deliberately the only door in. Explaining a candidate needs a
// Matcher that exists for the dry run and nothing else, because <jd:append>
// counts: run against the live Matcher, a preview of three links would hand the
// next real download "_4", and the numbering would be off by however often
// somebody pressed the button.

import "strings"

// Report is what a whole dry run answers with.
type Report struct {
	// Problems is every rule Compile could not use, keyed to its position in the
	// set so the editor can put the message on the rule instead of in a list at
	// the bottom of the page that nobody connects to anything.
	Problems []Problem `json:"problems"`
	// Rules mirrors the set one entry per rule, in order, including the ones that
	// are switched off or broken. A rule missing from the answer would look like a
	// rendering fault; one that is present and says why it did nothing does not.
	Rules []RuleReport `json:"rules"`
	// Links is one entry per sample, in the order they were given.
	Links []LinkReport `json:"links"`
	// Disabled reports that the set's master switch is off, so the editor can say
	// that nothing below is being applied. The rest of the report is still filled
	// in as though it were on: a set cannot be repaired while it is off if being
	// off also hides what is wrong with it.
	Disabled bool `json:"disabled,omitempty"`
}

// RuleReport is one rule's own verdict on the dry run.
type RuleReport struct {
	Index    int    `json:"index"`
	Name     string `json:"name"`
	Disabled bool   `json:"disabled,omitempty"`
	// Problems is this rule's share of Report.Problems. It is duplicated rather
	// than referenced because the editor renders it next to the rule, and a
	// client filtering a flat list by index is a filter every client has to write.
	Problems []Problem `json:"problems,omitempty"`
	// Matched is how many of the samples this rule fired on. Zero on a rule that
	// compiled cleanly is the answer somebody is usually looking for.
	Matched int `json:"matched"`
}

// LinkReport is what would happen to one sample link.
type LinkReport struct {
	URL      string `json:"url"`
	Filename string `json:"filename"`
	// Matched is the rules that fired, by index, in the order they fired.
	Matched []int `json:"matched"`
	// Effect and Verdict are exactly what the two engines would return, from the
	// same code that staging calls.
	Effect  Effect  `json:"effect"`
	Verdict Verdict `json:"verdict"`
	// Result is where the link would end up, with the fields no rule had an
	// opinion about filled in from the link itself. Effect leaves those empty and
	// means "unchanged", which is right for the caller applying it and useless to
	// somebody asking what the answer is.
	Result Outcome `json:"result"`
}

// Outcome is the two values a dry run can state outright. The folder is not one
// of them: when no rule names a folder the link lands in the one the settings
// say, and this package has no way to know which that is — a guess printed in a
// preview would be believed.
type Outcome struct {
	Package  string `json:"package"`
	Filename string `json:"filename"`
}

// Preview runs a set against a batch of sample links and says what each rule and
// each link did.
//
// The set is compiled with its master switch ignored, so a set that is switched
// off can still be worked on; Report.Disabled carries the real state. Disabled
// rules stay switched off, because a rule switched off is an edit somebody made
// on purpose and turning it back on in the preview would answer a question
// nobody asked.
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
	// Where each rule's report sits, so a match can be counted against it without
	// scanning the slice once per matching rule per link.
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
		// Apply before Check: Apply is what advances the append counter, and
		// running the two the other way round would still be correct but would put
		// the reason string's counter ahead of the folder's for no reason.
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

// problemsAt is one rule's problems. Compile reports them against the position
// in the set, which survives rules being dropped.
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
