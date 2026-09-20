package feed

// A preview fetches one feed on request and reports what it carries, so a
// title filter can be tried against real titles before the first poll seeds
// the subscription. It shares the poller's fetch and nothing else: it gets no
// State and no sink, so it cannot stage a link or mark an entry as seen, both
// of which would be irreversible.

import (
	"context"
	"net/http"
	"strings"

	"github.com/junkerderprovinz/knightloader/internal/httpx"
)

// maxPreview is how many entries one preview lists. The counts in Preview
// cover the whole document, so a filter that matches nothing still shows.
const maxPreview = 10

// Preview is what one test fetch found.
type Preview struct {
	// Title is the feed's own name, empty when it publishes none; the
	// quickest sign that an address is a feed at all.
	Title string `json:"title"`
	// Entries are the first maxPreview entries in document order, usually
	// newest first. Never nil.
	Entries []PreviewEntry `json:"entries"`
	// Total is how many entries the document carried, capped by maxItems as
	// in a poll.
	Total int `json:"total"`
	// Matched is how many of those the title filter takes; with no filter it
	// equals Total.
	Matched int `json:"matched"`
}

// PreviewEntry is one entry as a subscription would see it.
type PreviewEntry struct {
	// Title is what the filter matches and what the link would be filed
	// under.
	Title string `json:"title"`
	// Link is what would be staged, the enclosure where there is one.
	Link string `json:"link"`
	// Matches says whether the title filter takes this entry.
	Matches bool `json:"matches"`
}

// Inspect fetches one feed once and reports what it carries, without staging
// anything or recording anything as seen. The subscription is validated
// first, so a non-http address is never fetched and a broken pattern is
// reported rather than ignored. A nil hc means the shared httpx policy.
func Inspect(ctx context.Context, hc *http.Client, s Subscription) (Preview, error) {
	if err := s.Validate(); err != nil {
		return Preview{}, err
	}
	if hc == nil {
		hc = httpx.New(httpx.Options{})
	}
	f, err := fetch(ctx, hc, strings.TrimSpace(s.URL))
	if err != nil {
		return Preview{}, err
	}

	filt := s.filter()
	out := Preview{
		Title:   f.Title,
		Entries: make([]PreviewEntry, 0, min(len(f.Items), maxPreview)),
		Total:   len(f.Items),
	}
	for _, it := range f.Items {
		ok := s.wants(filt, it.Title)
		if ok {
			out.Matched++
		}
		if len(out.Entries) < maxPreview {
			out.Entries = append(out.Entries, PreviewEntry{Title: it.Title, Link: it.Link, Matches: ok})
		}
	}
	return out, nil
}
