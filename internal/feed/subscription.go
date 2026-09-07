package feed

// One configured subscription: what a user is allowed to say about a feed, what
// is refused, and what is quietly corrected.
//
// The split between the two is the same one internal/settings already draws
// (settings_rules.go against settings_captcha.go). A value nobody could have
// meant is corrected in Sanitize, because costing somebody the rest of the page
// over a number in a spinner is the worse failure. A value that would change
// what the subscription DOES is refused by Validate instead: a title filter that
// will not compile must never be dropped so that the subscription runs
// unfiltered, because that is a filter the user goes on believing in while every
// entry of the feed lands in the collector.

import (
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/rules"
)

// The bounds on how often a feed is fetched.
const (
	// DefaultIntervalMinutes is the period for a subscription that names none.
	// A feed is a human-speed input and every poll is a request to somebody
	// else's server, so a quarter of an hour is already far faster than any
	// publisher updates.
	DefaultIntervalMinutes = 15
	// MinIntervalMinutes is the floor. Below a minute this stops being a
	// subscription and becomes a load generator pointed at a stranger's host,
	// which is how an instance gets its address blocked.
	MinIntervalMinutes = 1
	// MaxIntervalMinutes is a week. Higher is not a longer wait, it is arithmetic
	// nobody checked: the value becomes a timer duration, and a number large
	// enough to overflow one produces a timer that fires immediately and forever.
	MaxIntervalMinutes = 7 * 24 * 60
)

// Subscription is one feed as the user configured it.
//
// Every field but URL has a zero value that means "no opinion", so a
// subscription that names nothing but an address behaves the way somebody who
// pasted an address expects: poll it, stage what is new, put it wherever
// downloads normally go. Priority is a pointer for exactly the reason
// watch.Job.Priority is: zero is a real priority somebody may have meant, so a
// plain int could not tell "normal" from "the user said nothing".
type Subscription struct {
	// URL is the feed document's address. It is also the identity of the
	// subscription: the runner keys its live pollers on it, and the memory of
	// which entries have already been handed over is stored under it. Changing it
	// is therefore not an edit but a different subscription, which is correct,
	// because a different address is a different feed.
	URL string `json:"url"`

	// IntervalMinutes is how often the feed is fetched. Zero means
	// DefaultIntervalMinutes. Minutes rather than a time.Duration because a
	// duration encodes into JSON as a nanosecond count, and 900000000000 in a
	// settings file is a number nobody can read or safely edit by hand.
	IntervalMinutes int `json:"intervalMinutes"`

	// TitleFilter is a regular expression an entry's title has to match before it
	// is staged. Empty takes everything.
	//
	// It decides what is STAGED and never what is REMEMBERED, see
	// poller.remember. A filter is a statement about what the user wants
	// downloaded now, not a claim that the entries it rejected never existed, and
	// remembering only the matches would mean that relaxing the filter next month
	// dumps the feed's whole current window into the collector at once.
	TitleFilter string `json:"titleFilter,omitempty"`

	// Dir is where this subscription's downloads land, empty for the app's own
	// download folder. It is the same override a dropped crawljob's
	// downloadFolder is, and it is applied the same way, after staging.
	Dir string `json:"dir,omitempty"`

	// Priority is one of the seven values the queue orders by, nil when the
	// subscription named none.
	Priority *int `json:"priority,omitempty"`
}

// Every is the poll period this subscription asks for. It is a method rather
// than a field so that "zero means the default" is answered in one place: a
// second reader that forgot the fallback would poll a whole subscription with a
// zero duration, which is a timer that fires as fast as the network answers.
func (s Subscription) Every() time.Duration {
	n := s.IntervalMinutes
	if n <= 0 {
		n = DefaultIntervalMinutes
	}
	return time.Duration(n) * time.Minute
}

// Validate reports why this subscription cannot be polled, or nil.
//
// It is what the API refuses a save with and what the runner refuses one
// subscription with, so a row that survived a hand-edited settings file is
// still never polled on a guess. Both callers report the sentence rather than
// dropping the row, so the user can see the row they got wrong instead of
// watching it disappear.
func (s Subscription) Validate() error {
	raw := strings.TrimSpace(s.URL)
	if raw == "" {
		return fmt.Errorf("feed: a subscription needs an address")
	}
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("feed: %s is not an address: %w", raw, err)
	}
	switch strings.ToLower(u.Scheme) {
	case "http", "https":
	default:
		// Only these two, and deliberately narrower than what the entries inside
		// the feed may carry (see resolve, which passes any scheme through). This
		// is an address this process fetches with an HTTP client; a file:// feed
		// would be a way to read any file on the box through a settings field, and
		// an ftp:// one would simply never be fetched at all.
		return fmt.Errorf("feed: %s is not an http or https address", raw)
	}
	if u.Host == "" {
		return fmt.Errorf("feed: %s names no host", raw)
	}
	if s.TitleFilter != "" {
		if _, err := regexp.Compile(s.TitleFilter); err != nil {
			return fmt.Errorf("feed: the title filter of %s is not a usable pattern: %w", raw, err)
		}
	}
	return nil
}

// filter compiles the title pattern, or returns nil for a subscription that
// takes everything. Callers have already been through Validate, so a pattern
// that will not compile here cannot happen; it is treated as "match nothing"
// rather than "match everything" if it ever does, because a broken filter that
// silently stages the whole feed is the failure this package works hardest to
// avoid.
func (s Subscription) filter() *regexp.Regexp {
	if s.TitleFilter == "" {
		return nil
	}
	re, err := regexp.Compile(s.TitleFilter)
	if err != nil {
		return regexp.MustCompile(`$^`)
	}
	return re
}

// wants reports whether an entry's title passes this subscription's filter.
func (s Subscription) wants(re *regexp.Regexp, title string) bool {
	return re == nil || re.MatchString(title)
}

// Sanitize cleans a stored list of subscriptions.
//
// It corrects and it collapses; it does not delete a row somebody can still see
// and fix. The one exception is a row naming no address at all, which is what an
// interface's "add a row" button produces before anything is typed into it: it
// has no identity, nothing can be stored under it, and two of them would be
// indistinguishable.
//
// Two rows naming one address are collapsed for the same reason internal/watch
// collapses two rows naming one directory: the memory of what has already been
// handed over is keyed on the address, so a second row would either share the
// first one's memory and stage nothing, or overwrite it and stage everything
// twice. The first row wins, so the collapse is stable across saves.
func Sanitize(subs []Subscription) []Subscription {
	if len(subs) == 0 {
		// nil rather than an empty non-nil slice, so an empty list reads the same
		// way on disk however it got there.
		return nil
	}
	seen := make(map[string]bool, len(subs))
	out := make([]Subscription, 0, len(subs))
	for _, s := range subs {
		s.URL = strings.TrimSpace(s.URL)
		if s.URL == "" || seen[s.URL] {
			continue
		}
		seen[s.URL] = true
		s.TitleFilter = strings.TrimSpace(s.TitleFilter)
		s.Dir = strings.TrimSpace(s.Dir)
		// Zero stays zero: that is "use the default period", not "poll as fast as
		// possible". Anything else is pulled into the bounds rather than refused,
		// because a number in a spinner should never cost somebody the save.
		if s.IntervalMinutes != 0 {
			if s.IntervalMinutes < MinIntervalMinutes {
				s.IntervalMinutes = MinIntervalMinutes
			}
			if s.IntervalMinutes > MaxIntervalMinutes {
				s.IntervalMinutes = MaxIntervalMinutes
			}
		}
		if s.Priority != nil {
			// Clamped to the queue's own range, and copied rather than kept: the
			// pointer belongs to whoever decoded the settings, and handing the same
			// one to every task this subscription creates is how two downloads end
			// up sharing a priority.
			p := *s.Priority
			if p < rules.PriorityMin {
				p = rules.PriorityMin
			}
			if p > rules.PriorityMax {
				p = rules.PriorityMax
			}
			s.Priority = &p
		}
		out = append(out, s)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
