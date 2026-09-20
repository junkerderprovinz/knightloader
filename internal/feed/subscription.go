package feed

// A value nobody could have meant is corrected in Sanitize, as
// internal/settings does. A value that changes what the subscription does is
// refused by Validate instead: a title filter that does not compile must not
// be dropped, or the user believes in a filter while the whole feed lands in
// the collector.

import (
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/rules"
)

const (
	// DefaultIntervalMinutes is the period for a subscription that names
	// none. Every poll is a request to somebody else's server.
	DefaultIntervalMinutes = 15
	// MinIntervalMinutes is the floor; faster polling gets an instance
	// blocked.
	MinIntervalMinutes = 1
	// MaxIntervalMinutes is a week. Much larger values overflow the timer
	// duration and fire continuously.
	MaxIntervalMinutes = 7 * 24 * 60
)

// Subscription is one feed as the user configured it. Every field but URL
// means "no opinion" at its zero value. Priority is a pointer because zero is
// a real priority.
type Subscription struct {
	// URL is the feed's address and the subscription's identity: pollers and
	// the memory of handed-over entries are keyed by it.
	URL string `json:"url"`

	// IntervalMinutes is how often the feed is fetched; zero means
	// DefaultIntervalMinutes. Minutes rather than a Duration, which would be
	// stored as unreadable nanoseconds.
	IntervalMinutes int `json:"intervalMinutes"`

	// TitleFilter is a regular expression an entry's title must match to be
	// staged; empty takes everything. It decides what is staged, never what
	// is remembered, so relaxing it later does not dump the current window.
	TitleFilter string `json:"titleFilter,omitempty"`

	// Dir is where this subscription's downloads land, empty for the default
	// folder, applied like a crawljob's downloadFolder.
	Dir string `json:"dir,omitempty"`

	// Priority is one of the queue's priority values, nil for none.
	Priority *int `json:"priority,omitempty"`
}

// Every is the poll period, with zero read as the default in one place.
func (s Subscription) Every() time.Duration {
	n := s.IntervalMinutes
	if n <= 0 {
		n = DefaultIntervalMinutes
	}
	return time.Duration(n) * time.Minute
}

// Validate reports why this subscription cannot be polled, or nil. The API
// refuses a save with it and the runner a subscription, so a hand-edited row
// is never polled on a guess.
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
		// Narrower than what entries may carry: a file:// feed would let a
		// settings field read any file on the box.
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

// filter compiles the title pattern, or returns nil for no filter. Callers
// have validated it; if it still fails, it matches nothing rather than
// everything.
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

// Sanitize cleans a stored list of subscriptions. It corrects values and
// collapses duplicates but keeps rows a person can still fix; only a row with
// no address, as an "add row" button leaves it, is dropped. A second row for
// the same address would share or overwrite the first one's memory, so the
// first one wins.
func Sanitize(subs []Subscription) []Subscription {
	if len(subs) == 0 {
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
		// Zero stays zero (the default); anything else is pulled into range.
		if s.IntervalMinutes != 0 {
			if s.IntervalMinutes < MinIntervalMinutes {
				s.IntervalMinutes = MinIntervalMinutes
			}
			if s.IntervalMinutes > MaxIntervalMinutes {
				s.IntervalMinutes = MaxIntervalMinutes
			}
		}
		if s.Priority != nil {
			// Clamped, and copied so no two tasks end up sharing the pointer.
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
