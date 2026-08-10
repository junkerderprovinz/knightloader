package confirm

import (
	"fmt"
	"strings"
)

// Item is one collected link, described in exactly the two facts OnDupes
// and OnOffline ever act on. Nothing else about a task is any of this
// package's business.
type Item struct {
	// ID is the caller's handle, carried through untouched into the result.
	ID string
	// Duplicate is true when the link already has a place in the list -
	// the exact same URL staged again, or a different one the mirror set
	// has already tied to a task it is a second copy of. Anything else is
	// not a duplicate as far as OnDupes is concerned.
	Duplicate bool
	// Offline is true only for a link a check has actually settled as
	// gone. An unknown or an uncheckable link must never be reported here
	// as true - OnOffline may only ever act on a definite no, or one
	// hoster declining a probe silently drops a whole package. See the
	// package comment.
	Offline bool
}

// Reason is which fact produced a non-Include outcome, so a combined report
// can name it without re-deriving it from the two input facts.
type Reason string

const (
	ReasonDuplicate Reason = "duplicate"
	ReasonOffline   Reason = "offline"
)

// severity orders the four outcomes Evaluate can settle a single item on,
// least exclusionary first, with Ask pulled past ExcludeAndRemove on
// purpose: a link either fact wants a person to look at is held for that
// answer before anything gets deleted on its account, so a policy that
// would otherwise remove it silently is never allowed to pre-empt the
// question the other fact is asking. UseGlobal never appears here - it does
// not survive Resolve.
var severity = map[Policy]int{
	Include:          0,
	Exclude:          1,
	ExcludeAndRemove: 2,
	Ask:              3,
}

// reasonTally is the key summarize groups a count by: how many items were
// held back for a given reason, under a given verdict. Named at package
// level, once, so Evaluate (which fills it in) and summarize (which reads it
// back) are working with the identical type rather than two structurally
// equal but distinct anonymous ones - Go does not consider those assignable
// to each other.
type reasonTally struct {
	reason  Reason
	verdict Policy
}

// Outcome is the settled answer for one item.
type Outcome struct {
	ID string
	// Verdict is the one policy that decided this item's fate: the
	// strictest (see severity) of the (up to two) policies whose fact
	// actually applied to it.
	Verdict Policy
	// Reasons is which fact or facts share the blame for Verdict, in a
	// fixed order (duplicate, then offline). A fact that applied but whose
	// own policy was milder than the other one is not listed - it did not
	// actually decide anything.
	Reasons []Reason
}

// Result is the answer for a whole batch: who starts, who is asked about,
// who is dropped, and one sentence that reports the rest.
type Result struct {
	Outcomes []Outcome
	// Start is every id whose verdict was Include - hand this to whatever
	// actually begins the download.
	Start []string
	// Remove is every id ExcludeAndRemove settled on: gone from the list
	// entirely, not merely left uncollected.
	Remove []string
	// Ask is every id still waiting on a person - only ever populated when
	// the trigger passed to Evaluate (by way of ResolveConfig) was
	// interactive, because a non-interactive Ask never survives Resolve.
	Ask []string
	// Summary is a ready-made English sentence describing every excluded or
	// removed link ("3 offline and 2 duplicate links were not started."),
	// or "" when nothing was held back. It exists for a caller with nowhere
	// better to put the news - a server log, a test assertion, an API
	// client that is not a browser - and it is deliberately not the only
	// way to say this: Outcomes carries the same information as data (per
	// item, with the reason and the verdict that produced it), which is
	// what a browser should compose its own, correctly localised sentence
	// from instead of showing this one verbatim. See docs/build-plan.md
	// package 1's ruling on stateKey/stateArgs, which this mirrors: the
	// server does not know which of the shipped locales is looking at this
	// response, and two clients of one instance routinely differ.
	Summary string
}

// Evaluate applies a resolved Config to a batch in one pass: a link that is
// both a duplicate and reported offline is settled once, under whichever
// fact wants to do more about it (see severity), rather than being asked
// about or excluded twice under two facts that happen to agree.
//
// cfg must already be resolved - see Resolve and ResolveConfig - so neither
// field is ever UseGlobal here, and Ask means exactly what it says:
// somebody is going to be asked about this batch.
func Evaluate(items []Item, cfg Config) Result {
	var res Result
	counts := map[reasonTally]int{}

	for _, it := range items {
		axis := map[Reason]Policy{}
		if it.Duplicate {
			axis[ReasonDuplicate] = cfg.OnDupes
		}
		if it.Offline {
			axis[ReasonOffline] = cfg.OnOffline
		}
		final := Include
		for _, p := range axis {
			if severity[p] > severity[final] {
				final = p
			}
		}
		// Only worth attributing when something was actually held back: a
		// fact that applied but whose own policy was milder than the other
		// one (typically Include, when this item's Verdict is not) did not
		// decide anything and does not belong in the blame list - axis[r]
		// for a fact that never applied at all reads as the Policy zero
		// value "", which cannot equal final either way, so the guard is
		// only needed for the "applied but outvoted" case.
		var reasons []Reason
		if final != Include {
			for _, r := range [...]Reason{ReasonDuplicate, ReasonOffline} {
				if axis[r] == final {
					reasons = append(reasons, r)
				}
			}
		}
		res.Outcomes = append(res.Outcomes, Outcome{ID: it.ID, Verdict: final, Reasons: reasons})

		switch final {
		case ExcludeAndRemove:
			res.Remove = append(res.Remove, it.ID)
		case Ask:
			res.Ask = append(res.Ask, it.ID)
		case Exclude:
			// Stays in the collector. It is exactly as confirmable a moment
			// later as it was a moment before - see Policy.Exclude.
		default: // Include
			res.Start = append(res.Start, it.ID)
		}
		if final == Exclude || final == ExcludeAndRemove {
			for _, r := range reasons {
				counts[reasonTally{r, final}]++
			}
		}
	}
	res.Summary = summarize(counts)
	return res
}

// reasonNoun is the plain-English word a count is built around in Summary.
// Not translated - see Result.Summary's own doc comment for why a sentence
// is produced here at all, alongside the structured Outcomes a locale-aware
// caller should prefer.
func reasonNoun(r Reason) string {
	switch r {
	case ReasonDuplicate:
		return "duplicate"
	case ReasonOffline:
		return "offline"
	}
	return string(r)
}

// summarize turns the per-(reason,verdict) tally into the sentence
// Result.Summary carries. Exclude is reported before ExcludeAndRemove - the
// milder outcome read first - and within each, offline before duplicate,
// matching the worked example in this package's own design brief ("3
// offline and 2 duplicate links were not started").
func summarize(counts map[reasonTally]int) string {
	byVerdict := map[Policy]map[Reason]int{}
	for k, n := range counts {
		if n <= 0 {
			continue
		}
		if byVerdict[k.verdict] == nil {
			byVerdict[k.verdict] = map[Reason]int{}
		}
		byVerdict[k.verdict][k.reason] = n
	}

	var clauses []string
	for _, verdict := range [...]Policy{Exclude, ExcludeAndRemove} {
		byReason := byVerdict[verdict]
		if len(byReason) == 0 {
			continue
		}
		var words []string
		multi := len(byReason) > 1
		singular := !multi
		for _, r := range [...]Reason{ReasonOffline, ReasonDuplicate} {
			n := byReason[r]
			if n == 0 {
				continue
			}
			if n != 1 {
				singular = false
			}
			words = append(words, fmt.Sprintf("%d %s", n, reasonNoun(r)))
		}
		noun, verb := "link", "was not started"
		if !singular {
			noun = "links"
			verb = "were not started"
		}
		if verdict == ExcludeAndRemove {
			verb = "was removed"
			if !singular {
				verb = "were removed"
			}
		}
		clauses = append(clauses, fmt.Sprintf("%s %s %s", strings.Join(words, " and "), noun, verb))
	}
	if len(clauses) == 0 {
		return ""
	}
	out := strings.Join(clauses, "; ") + "."
	return strings.ToUpper(out[:1]) + out[1:]
}
