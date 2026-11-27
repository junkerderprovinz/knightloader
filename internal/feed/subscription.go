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
	"slices"
	"strings"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/resolver/ytdlp"
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

// Kind is where a subscription's entries come from.
type Kind string

const (
	// KindRSS is an RSS or Atom document, and what a row without a kind is.
	KindRSS Kind = "rss"
	// KindChannel is a channel or playlist yt-dlp lists, which reaches past
	// the handful of entries a site's own feed carries.
	KindChannel Kind = "channel"
	// KindMediathek is a saved search on MediathekViewWeb.
	KindMediathek Kind = "mediathek"
)

// Naming is how a subscription's downloads are filed.
type Naming string

// NamingChannel files every entry in a package and folder named after the
// channel or show, and the file after the entry.
const NamingChannel Naming = "channel"

// LibraryMovie and LibrarySeries are the library layouts a subscription can
// ask for; empty leaves it to the instance.
const (
	LibraryMovie  = "movie"
	LibrarySeries = "series"
)

// SponsorBlock names the SponsorBlock categories cut out of a video and those
// only marked as chapters.
type SponsorBlock struct {
	Remove []string `json:"remove"`
	Mark   []string `json:"mark"`
}

// sponsorCategories are yt-dlp's SponsorBlock categories. The markOnly ones
// describe a point or a chapter and cannot be cut.
var (
	sponsorCategories = []string{"sponsor", "intro", "outro", "selfpromo", "preview", "filler",
		"interaction", "music_offtopic", "poi_highlight", "chapter", "hook"}
	markOnly = []string{"poi_highlight", "chapter"}
)

// Backfill is what the first poll of a channel hands over, where otherwise it
// only remembers what is there.
type Backfill struct {
	Mode BackfillMode `json:"mode"`
	// N is the number of videos for BackfillCount and of days for
	// BackfillDays.
	N int `json:"n,omitempty"`
}

// BackfillMode says how far back a backfill reaches.
type BackfillMode string

const (
	BackfillAll   BackfillMode = "all"
	BackfillCount BackfillMode = "count"
	BackfillDays  BackfillMode = "days"
)

// Mediathek is a MediathekViewWeb search. Each field is matched as words in
// the result's field of the same name.
type Mediathek struct {
	Channel string `json:"channel"`
	Topic   string `json:"topic"`
	Title   string `json:"title"`
	// Quality is the video file to take, falling back to the next one down
	// where a result lacks it.
	Quality   Quality `json:"quality"`
	Subtitles bool    `json:"subtitles"`
}

// Quality is one of the files a Mediathek result offers.
type Quality string

const (
	QualityHD     Quality = "hd"
	QualityNormal Quality = "normal"
	QualityLow    Quality = "low"
)

// Subscription is one feed as the user configured it. Every field but URL
// means "no opinion" at its zero value, so a row written by an older version
// behaves as it did there. Priority is a pointer because zero is a real
// priority.
type Subscription struct {
	// URL is the feed's address and the subscription's identity: pollers and
	// the memory of handed-over entries are keyed by it.
	URL string `json:"url"`

	Kind Kind `json:"kind"`

	// Title is the name of the channel or show, for display and for
	// NamingChannel. Empty falls back to the feed's own title.
	Title string `json:"title"`

	// IntervalMinutes is how often the feed is fetched; zero means
	// DefaultIntervalMinutes. Minutes rather than a Duration, which would be
	// stored as unreadable nanoseconds.
	IntervalMinutes int `json:"intervalMinutes"`

	// TitleFilter is a regular expression an entry's title must match to be
	// staged; empty takes everything. It decides what is staged, never what
	// is remembered, so relaxing it later does not dump the current window.
	TitleFilter string `json:"titleFilter,omitempty"`

	// ExcludeFilter is a regular expression that skips an entry whose title
	// it matches, whatever TitleFilter says.
	ExcludeFilter string `json:"excludeFilter"`

	// MinMinutes and MaxMinutes bound an entry's length, zero for no bound.
	// An entry whose length nobody can tell is taken.
	MinMinutes int `json:"minMinutes"`
	MaxMinutes int `json:"maxMinutes"`

	// Dir is where this subscription's downloads land, empty for the default
	// folder, applied like a crawljob's downloadFolder.
	Dir string `json:"dir,omitempty"`

	// Priority is one of the queue's priority values, nil for none.
	Priority *int `json:"priority,omitempty"`

	// AutoStart confirms this subscription's entries as they arrive. Without
	// it only the instance's AutoConfirm does, since a feed is written by a
	// stranger.
	AutoStart bool `json:"autoStart"`

	// Media decides the rows a yt-dlp entry is staged with in place of its
	// host's preset; nil leaves it to the host preset.
	Media *ytdlp.HosterPreset `json:"media"`

	Naming Naming `json:"naming"`

	// Library is LibraryMovie, LibrarySeries or empty for the instance's.
	Library string `json:"library"`

	// SponsorBlock overrides the instance's SponsorBlock choice; nil keeps it.
	SponsorBlock *SponsorBlock `json:"sponsorBlock"`

	// Backfill is for KindChannel only.
	Backfill *Backfill `json:"backfill"`

	// Mediathek is the search of a KindMediathek row.
	Mediathek *Mediathek `json:"mediathek"`

	// AllowDownloaded lets an entry through that was downloaded before in
	// some other way, which is otherwise refused.
	AllowDownloaded bool `json:"allowDownloaded"`
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
	if s.ExcludeFilter != "" {
		if _, err := regexp.Compile(s.ExcludeFilter); err != nil {
			return fmt.Errorf("feed: the exclude filter of %s is not a usable pattern: %w", raw, err)
		}
	}
	if s.MinMinutes > 0 && s.MaxMinutes > 0 && s.MinMinutes > s.MaxMinutes {
		return fmt.Errorf("feed: %s asks for at least %d and at most %d minutes, which no entry can be", raw, s.MinMinutes, s.MaxMinutes)
	}
	switch s.Kind {
	case "", KindRSS, KindChannel:
	case KindMediathek:
		if m := s.Mediathek; m == nil || (m.Channel == "" && m.Topic == "" && m.Title == "") {
			return fmt.Errorf("feed: the Mediathek search of %s needs a channel, a topic or a title", raw)
		}
	default:
		return fmt.Errorf("feed: %s is of kind %q, which is none of rss, channel and mediathek", raw, s.Kind)
	}
	if b := s.Backfill; b != nil {
		switch b.Mode {
		case BackfillAll:
		case BackfillCount, BackfillDays:
			if b.N < 1 {
				return fmt.Errorf("feed: the backfill of %s needs a number of at least 1", raw)
			}
		default:
			return fmt.Errorf("feed: the backfill of %s is %q, which is none of all, count and days", raw, b.Mode)
		}
	}
	if s.Media != nil && len(s.Media.Variants) == 0 {
		return fmt.Errorf("feed: the media choice of %s picks no row at all, so nothing would be downloaded", raw)
	}
	switch s.Library {
	case "", LibraryMovie, LibrarySeries:
	default:
		return fmt.Errorf("feed: the library layout of %s is %q, which is neither movie nor series", raw, s.Library)
	}
	if sb := s.SponsorBlock; sb != nil {
		for _, c := range slices.Concat(sb.Remove, sb.Mark) {
			if !slices.Contains(sponsorCategories, c) {
				return fmt.Errorf("feed: %q in the SponsorBlock choice of %s is not a SponsorBlock category", c, raw)
			}
		}
		for _, c := range sb.Remove {
			if slices.Contains(markOnly, c) {
				return fmt.Errorf("feed: %s can only be marked in the SponsorBlock choice of %s, not cut out", c, raw)
			}
		}
	}
	return nil
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
		s.Kind = Kind(strings.ToLower(strings.TrimSpace(string(s.Kind))))
		if s.Kind == "" {
			s.Kind = KindRSS
		}
		if s.URL == "" && s.Kind == KindMediathek && s.Mediathek != nil {
			// A search typed into the form rather than pasted still needs an
			// identity, and its address on MediathekViewWeb is one.
			s.URL = MediathekURL(*s.Mediathek, s.MinMinutes, s.MaxMinutes)
		}
		if s.URL == "" || seen[s.URL] {
			continue
		}
		seen[s.URL] = true
		s.TitleFilter = strings.TrimSpace(s.TitleFilter)
		s.ExcludeFilter = strings.TrimSpace(s.ExcludeFilter)
		s.Title = text(s.Title)
		s.Dir = strings.TrimSpace(s.Dir)
		s.MinMinutes, s.MaxMinutes = max(s.MinMinutes, 0), max(s.MaxMinutes, 0)
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
		if s.Naming != NamingChannel {
			s.Naming = ""
		}
		s.Library = strings.ToLower(strings.TrimSpace(s.Library))
		if s.Media != nil {
			m := s.Media.Sanitize()
			s.Media = &m
		}
		if s.SponsorBlock != nil {
			s.SponsorBlock = &SponsorBlock{Remove: categories(s.SponsorBlock.Remove), Mark: categories(s.SponsorBlock.Mark)}
		}
		if s.Kind != KindChannel {
			s.Backfill = nil
		} else if s.Backfill != nil {
			b := *s.Backfill
			s.Backfill = &b
		}
		if s.Kind != KindMediathek {
			s.Mediathek = nil
		} else if s.Mediathek != nil {
			m := *s.Mediathek
			m.Channel, m.Topic, m.Title = text(m.Channel), text(m.Topic), text(m.Title)
			switch m.Quality {
			case QualityHD, QualityNormal, QualityLow:
			default:
				m.Quality = QualityHD
			}
			s.Mediathek = &m
		}
		out = append(out, s)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// categories lower-cases and de-duplicates a SponsorBlock list. Validate
// refuses whatever is left that yt-dlp does not know.
func categories(in []string) []string {
	out := []string{}
	for _, c := range in {
		c = strings.ToLower(strings.TrimSpace(c))
		if c != "" && !slices.Contains(out, c) {
			out = append(out, c)
		}
	}
	return out
}
