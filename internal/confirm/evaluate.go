package confirm

import (
	"fmt"
	"strings"
)

// Item is one collected link, described by the two facts OnDupes and
// OnOffline act on.
type Item struct {
	ID string
	// Duplicate is true when the same URL, or a mirror already tied to a
	// task, is in the list.
	Duplicate bool
	// Offline is true only when a check has settled that the link is gone.
	// An unknown or uncheckable link must be false, or one hoster refusing a
	// probe would drop a whole package.
	Offline bool
}

// Reason is which fact produced a non-Include outcome.
type Reason string

const (
	ReasonDuplicate Reason = "duplicate"
	ReasonOffline   Reason = "offline"
)

// severity orders the outcomes, least exclusionary first. Ask ranks above
// ExcludeAndRemove, so a link one fact wants a person to look at is never
// deleted on the other fact's account before the question is answered.
var severity = map[Policy]int{
	Include:          0,
	Exclude:          1,
	ExcludeAndRemove: 2,
	Ask:              3,
}

// reasonTally is how summarize groups a count: items held back for a reason
// under a verdict.
type reasonTally struct {
	reason  Reason
	verdict Policy
}

// Outcome is the settled answer for one item.
type Outcome struct {
	ID string
	// Verdict is the strictest policy among the facts that applied.
	Verdict Policy
	// Reasons lists the facts whose policy produced Verdict, duplicate before
	// offline. A fact that was outranked is left out.
	Reasons []Reason
}

// Result is the answer for a whole batch.
type Result struct {
	Outcomes []Outcome
	// Start holds every id whose verdict was Include.
	Start []string
	// Remove holds every id to delete from the list.
	Remove []string
	// Ask holds every id waiting on a person. It is empty unless the trigger
	// was interactive.
	Ask []string
	// Summary is an English sentence about every excluded or removed link
	// ("3 offline and 2 duplicate links were not started."), or "". It is
	// for logs and non-browser clients; a browser should build its own
	// localised sentence from Outcomes.
	Summary string
}

// Evaluate applies a resolved Config (see ResolveConfig) to a batch. A link
// that is both a duplicate and offline is settled once, under the stricter of
// its two policies.
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
		// Only the facts whose policy equals the verdict are to blame; one that
		// applied but was outranked decided nothing.
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
			// Stays in the collector.
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

func reasonNoun(r Reason) string {
	switch r {
	case ReasonDuplicate:
		return "duplicate"
	case ReasonOffline:
		return "offline"
	}
	return string(r)
}

// summarize builds Result.Summary from the tally: Exclude before
// ExcludeAndRemove, and offline before duplicate within each.
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
