package feed

// Which entries a subscription takes. The poller and the test button share
// this, so the test reports exactly what a poll would stage.

import (
	"context"
	"regexp"
	"time"
)

// Reason is why a subscription leaves an entry out; empty means it is taken.
type Reason string

const (
	ReasonInclude Reason = "include"
	ReasonExclude Reason = "exclude"
	ReasonShort   Reason = "short"
	ReasonLong    Reason = "long"
)

// Prober finds out how long an entry is when its feed does not say, so a
// length bound still applies. The app answers with yt-dlp; tests with a map.
type Prober interface {
	Duration(ctx context.Context, link string) (time.Duration, error)
}

// matcher is a subscription's filters, compiled once per configuration.
type matcher struct {
	include, exclude *regexp.Regexp
	min, max         time.Duration
}

// nothing matches no title at all. A pattern that fails to compile here was
// validated before, and matching nothing is the safe way to be wrong.
var nothing = regexp.MustCompile(`$^`)

func compile(pattern string) *regexp.Regexp {
	if pattern == "" {
		return nil
	}
	re, err := regexp.Compile(pattern)
	if err != nil {
		return nothing
	}
	return re
}

func (s Subscription) matcher() matcher {
	m := matcher{
		include: compile(s.TitleFilter),
		exclude: compile(s.ExcludeFilter),
		min:     time.Duration(s.MinMinutes) * time.Minute,
		max:     time.Duration(s.MaxMinutes) * time.Minute,
	}
	if m.exclude == nothing {
		// A broken exclude pattern must keep everything out, not let
		// everything in.
		m.include = nothing
	}
	return m
}

// byTitle judges what the title alone decides.
func (m matcher) byTitle(title string) Reason {
	if m.include != nil && !m.include.MatchString(title) {
		return ReasonInclude
	}
	if m.exclude != nil && m.exclude.MatchString(title) {
		return ReasonExclude
	}
	return ""
}

// bounded reports whether the length matters at all, which is when a probe
// is worth its cost.
func (m matcher) bounded() bool { return m.min > 0 || m.max > 0 }

// byLength judges a known length; zero is unknown and passes.
func (m matcher) byLength(d time.Duration) Reason {
	switch {
	case d <= 0:
		return ""
	case m.min > 0 && d < m.min:
		return ReasonShort
	case m.max > 0 && d > m.max:
		return ReasonLong
	}
	return ""
}

// judge decides one entry, asking the prober for a length the entry lacks
// only once the title has let it through. It returns the entry with the
// length it learned.
func (m matcher) judge(ctx context.Context, p Prober, it Item) (Item, Reason) {
	if r := m.byTitle(it.Title); r != "" {
		return it, r
	}
	if !m.bounded() {
		return it, ""
	}
	if it.Duration <= 0 && p != nil {
		if d, err := p.Duration(ctx, it.Link); err == nil {
			it.Duration = d
		}
	}
	return it, m.byLength(it.Duration)
}
