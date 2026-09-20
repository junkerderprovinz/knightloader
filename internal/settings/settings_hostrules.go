package settings

// The two exception tables: what one host may differ in, and what one kind of
// failure may differ in.
//
// Every value here overrides a number that exists a level above it, and every
// zero means "no opinion, use the level above", the convention Chunks and
// Task.Chunks carry. An empty table therefore changes nothing.

import (
	"strings"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/rules"
)

// The built-in backoff an install with an empty retry table gets: fifteen
// seconds, doubling, stopping at ten minutes. They live here because this is
// the file that resolves a zero into something, and app.retryDelay reads them
// rather than keeping a second copy.
const (
	DefaultRetryDelay = 15 * time.Second
	DefaultRetryMax   = 10 * time.Minute
)

// maxRetryWait bounds a configured delay, in seconds. A day is past any hoster
// cool-down, and it means a typed 999999999 arms a timer somebody can still see
// fire rather than one that lands after a hundred reboots.
const maxRetryWait = 24 * 60 * 60

// RetryRule is one entry of the retry policy.
//
// A single doubling backoff cannot describe what hosters do. Fifteen seconds
// doubling to ten minutes asks a host with a one-hour block six times inside
// those ten minutes and then gives up: the six attempts spend the queue's
// slots, they are all refused, and the one that would have worked is never
// made.
//
// Delay and Max are seconds, like every other duration in this struct, because
// that is what settings.json holds and nobody types nanoseconds. Tries is
// attempts, counted the way MaxRetries is.
type RetryRule struct {
	// Delay is the wait before the first retry. Zero takes the level above.
	Delay int `json:"delay,omitempty"`
	// Max is where the doubling stops. Zero takes the level above.
	Max int `json:"max,omitempty"`
	// Tries is how many attempts this failure gets at all. Zero takes
	// MaxRetries, the global count.
	Tries int `json:"tries,omitempty"`
	// Never settles the task without arming any retry, as its own end state:
	// not "failed after three attempts" but "this will not be tried again".
	// The two read differently on a list, see core.Task.GaveUp: raising
	// MaxRetries mends the first and does nothing for the second.
	Never bool `json:"never,omitempty"`
}

// merge fills this rule's zeroes from the one below it in the chain and returns
// the result. Per field rather than per rule: a host entry saying nothing but
// "wait an hour" must not also silence the attempt count a reason entry set,
// which is what "the most specific whole rule wins" would do.
func (r RetryRule) merge(under RetryRule) RetryRule {
	if r.Delay <= 0 {
		r.Delay = under.Delay
	}
	if r.Max <= 0 {
		r.Max = under.Max
	}
	if r.Tries <= 0 {
		r.Tries = under.Tries
	}
	// Never is an or and not a fallback: either level saying "do not try this
	// again" is an instruction, and a host rule with Never unset is a host
	// nobody has said that about rather than one overruling the reason table.
	r.Never = r.Never || under.Never
	return r
}

// RetryPolicy is the whole retry configuration: the instance-wide backoff and
// the per-reason table.
//
// ByReason is keyed by core.Reason's string form ("limit", "network",
// "gone", ...), the taxonomy internal/core publishes rather than a second
// vocabulary. An unknown key is inert rather than an error: the taxonomy grows,
// and a key that names nothing never matches a failure. A string key rather
// than a typed one, because a map key in JSON is a string whatever Go calls it,
// and because this package does not import internal/core for one constant.
type RetryPolicy struct {
	// Delay and Max are the instance-wide backoff, in seconds. Zero on either
	// keeps the built-in pair above.
	Delay int `json:"delay"`
	Max   int `json:"max"`
	// ByReason is the per-failure table. Empty, the default, gives every
	// failure the same backoff.
	ByReason map[string]RetryRule `json:"byReason"`
}

// HostRule is everything one host pattern may differ in.
//
// One table rather than three keyed by the same host: connections, chunk count
// and retry policy all answer what a given hoster tolerates, and somebody who
// has just discovered that a host allows two connections and blocks for an hour
// writes that down in one place.
type HostRule struct {
	// MaxPerHost is this host's own simultaneous-download ceiling. Zero takes
	// the global MaxPerHost.
	MaxPerHost int `json:"maxPerHost,omitempty"`
	// Chunks is how many connections one download from this host opens. Zero
	// takes the global Chunks, and the built-in default behind that.
	//
	// An override and not a ceiling, which is where this table differs from
	// what a resolver reports (see app.connsFor). A resolver's figure is a
	// report about the host and may only lower the number; this is a person
	// writing down what they want, so it can say eight on an instance whose
	// global is four. It still passes through every ceiling afterwards, so a
	// resolver that knows the host permits two wins over a hopeful eight.
	Chunks int `json:"chunks,omitempty"`
	// Retry is this host's own backoff, layered over the per-reason table. See
	// RetryRule.merge for how the two combine.
	Retry RetryRule `json:"retry,omitzero"`
}

// RetryPlan is the resolved answer for one failure: what the app is actually
// about to do. Durations rather than seconds, because nothing past this point
// deals in settings.json's units.
type RetryPlan struct {
	// Delay is the wait before the first retry, Max where the doubling stops.
	// RetryFor resolves the zeroes, so both are always set and a caller never
	// has to know the built-in pair.
	Delay time.Duration
	Max   time.Duration
	// Tries is how many attempts this failure gets in total.
	Tries int
	// Never is "do not try this again", the end state that is not "failed".
	Never bool
}

// RetryFor is the retry policy for one failure: reason is core.Reason's string
// form, host the file host the link is on.
//
// The chain is host rule, then reason rule, then the instance-wide numbers,
// then the built-in pair, resolved field by field so that an install which has
// configured one thing changes one thing. With both tables empty it returns the
// fifteen-seconds-to-ten-minutes backoff and MaxRetries.
func (s Settings) RetryFor(reason, host string) RetryPlan {
	rule := s.HostRuleFor(host).Retry.
		merge(s.Retry.ByReason[strings.TrimSpace(reason)]).
		merge(RetryRule{Delay: s.Retry.Delay, Max: s.Retry.Max, Tries: s.MaxRetries})
	plan := RetryPlan{
		Delay: time.Duration(rule.Delay) * time.Second,
		Max:   time.Duration(rule.Max) * time.Second,
		Tries: rule.Tries,
		Never: rule.Never,
	}
	if plan.Delay <= 0 {
		plan.Delay = DefaultRetryDelay
	}
	if plan.Max <= 0 {
		plan.Max = DefaultRetryMax
	}
	if plan.Max < plan.Delay {
		// A ceiling below the first wait makes that wait unreachable, so the
		// number somebody typed as the delay would never be the delay. Usually
		// this is the built-in ten-minute cap meeting a hand-written one-hour
		// delay, and the hand-written one is what was meant.
		plan.Max = plan.Delay
	}
	return plan
}

// HostRuleFor is the table entry that applies to host, or the zero rule when
// the table has nothing to say about it.
//
// A pattern matches the host itself and any subdomain of it, on a dot boundary:
// "rapidgator.net" covers rg.rapidgator.net and does not cover
// notrapidgator.net. "*.rapidgator.net" is accepted as the same pattern spelled
// out, so a line pasted from somewhere else does not match nothing.
//
// The longest matching pattern wins: an entry for "dl3.example.com" beats one
// for "example.com" on a link from that server. Two patterns of equal length
// are settled by comparing them as text, which is arbitrary but fixed. Go's map
// iteration is not, and a table answering differently on alternate passes would
// be a queue that behaves differently every time it is looked at.
func (s Settings) HostRuleFor(host string) HostRule {
	host = strings.ToLower(strings.TrimSpace(host))
	if host == "" || len(s.HostRules) == 0 {
		return HostRule{}
	}
	best, bestRaw := "", ""
	var out HostRule
	for raw, rule := range s.HostRules {
		p := normalizeHostPattern(raw)
		if p == "" || !hostMatchesPattern(p, host) {
			continue
		}
		if len(p) > len(best) || (len(p) == len(best) && (bestRaw == "" || raw < bestRaw)) {
			best, bestRaw, out = p, raw, rule
		}
	}
	return out
}

// normalizeHostPattern folds the spellings of one pattern into one: case, the
// stray whitespace of a pasted line, a leading "*." and the trailing dot of a
// fully qualified name.
func normalizeHostPattern(p string) string {
	p = strings.ToLower(strings.TrimSpace(p))
	p = strings.TrimPrefix(p, "*.")
	return strings.Trim(p, ".")
}

// hostMatchesPattern is the dot-boundary suffix match HostRuleFor documents.
func hostMatchesPattern(pattern, host string) bool {
	return host == pattern || strings.HasSuffix(host, "."+pattern)
}

// sanitizeHostRules bounds both tables and drops the entries that could never
// match anything.
//
// The map is rebuilt rather than edited in place: the caller still holds the
// same map, and a settings document that changes underneath whoever submitted
// it is a bug people find months later.
func sanitizeHostRules(n Settings) Settings {
	n.Retry.Delay = clampSeconds(n.Retry.Delay)
	n.Retry.Max = clampSeconds(n.Retry.Max)
	n.Retry.ByReason = sanitizeRetryTable(n.Retry.ByReason)
	if len(n.HostRules) == 0 {
		return n
	}
	out := make(map[string]HostRule, len(n.HostRules))
	for raw, rule := range n.HostRules {
		if normalizeHostPattern(raw) == "" {
			// A blank pattern matches nothing, so keeping it would put a row on
			// the page that can never fire.
			continue
		}
		if rule.MaxPerHost < 0 {
			rule.MaxPerHost = 0
		}
		if rule.MaxPerHost > maxConcurrentCeiling {
			rule.MaxPerHost = maxConcurrentCeiling
		}
		if rule.Chunks < 0 {
			rule.Chunks = 0
		}
		if rule.Chunks > rules.MaxChunks {
			// The engine will not honour more, and app.connsFor cuts it anyway.
			// Cutting it where it is saved is what makes the page show what
			// will happen.
			rule.Chunks = rules.MaxChunks
		}
		rule.Retry = sanitizeRetryRule(rule.Retry)
		out[raw] = rule
	}
	n.HostRules = out
	return n
}

// maxConcurrentCeiling is the ceiling sanitizeQueue puts on MaxConcurrent,
// applied to a per-host override for the same reason: this counts live
// transfers, and a four-digit one is a typo rather than a wish.
const maxConcurrentCeiling = 64

func sanitizeRetryTable(in map[string]RetryRule) map[string]RetryRule {
	if len(in) == 0 {
		return in
	}
	out := make(map[string]RetryRule, len(in))
	for reason, rule := range in {
		key := strings.TrimSpace(reason)
		if key == "" {
			continue
		}
		out[key] = sanitizeRetryRule(rule)
	}
	return out
}

func sanitizeRetryRule(r RetryRule) RetryRule {
	r.Delay = clampSeconds(r.Delay)
	r.Max = clampSeconds(r.Max)
	if r.Tries < 0 {
		r.Tries = 0
	}
	if r.Tries > maxRetryTries {
		r.Tries = maxRetryTries
	}
	return r
}

// maxRetryTries is the ceiling sanitizeQueue puts on MaxRetries, applied to a
// per-rule override so one table entry cannot ask for a hundred attempts that
// the global count is not allowed to ask for.
const maxRetryTries = 20

func clampSeconds(v int) int {
	if v < 0 {
		return 0
	}
	if v > maxRetryWait {
		return maxRetryWait
	}
	return v
}
