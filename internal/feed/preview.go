package feed

// A preview fetches one feed on request and reports what it carries, so a
// title filter can be tried against real titles before the first poll seeds
// the subscription. It shares the poller's fetch and nothing else: it gets no
// State and no sink, so it cannot stage a link or mark an entry as seen, both
// of which would be irreversible.

import (
	"context"
	"sync"
	"time"
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
	// Matched is how many of those the subscription takes; with no filter it
	// equals Total. A length only a probe could tell is probed for the listed
	// entries alone, so beyond them an unknown length counts as taken.
	Matched int `json:"matched"`
}

// PreviewEntry is one entry as a subscription would see it.
type PreviewEntry struct {
	// Title is what the filter matches and what the link would be filed
	// under.
	Title string `json:"title"`
	// Link is what would be staged, the enclosure where there is one.
	Link string `json:"link"`
	// Matches says whether the subscription takes this entry.
	Matches bool `json:"matches"`
	// Duration is the entry's length in seconds, 0 when unknown.
	Duration int `json:"duration"`
	// Reason is why the entry is not taken, absent when it is.
	Reason Reason `json:"reason,omitempty"`
}

// previewWindow is how many videos a test lists of a channel; a full listing
// would make the button wait on dozens of page requests.
const previewWindow = 30

// previewProbes is how many length probes a test runs side by side.
const previewProbes = 4

// Inspect fetches one subscription once and reports what it carries, without
// staging anything or recording anything as seen. The subscription is
// validated first, so a non-http address is never fetched and a broken
// pattern is reported rather than ignored.
func Inspect(ctx context.Context, src Sources, s Subscription) (Preview, error) {
	if err := s.Validate(); err != nil {
		return Preview{}, err
	}
	src = src.withDefaults()
	f, err := src.read(ctx, s, ListQuery{Limit: previewWindow})
	if err != nil {
		return Preview{}, err
	}

	m := s.matcher()
	out := Preview{
		Title:   f.Title,
		Entries: make([]PreviewEntry, 0, min(len(f.Items), maxPreview)),
		Total:   len(f.Items),
	}
	listed := f.Items[:min(len(f.Items), maxPreview)]
	judged := make([]Reason, len(listed))
	var wg sync.WaitGroup
	slots := make(chan struct{}, previewProbes)
	for i := range listed {
		wg.Add(1)
		slots <- struct{}{}
		go func() {
			defer func() { <-slots; wg.Done() }()
			listed[i], judged[i] = m.judge(ctx, src.Prober, listed[i])
		}()
	}
	wg.Wait()
	for i, it := range f.Items {
		var why Reason
		if i < len(listed) {
			it, why = listed[i], judged[i]
			out.Entries = append(out.Entries, PreviewEntry{
				Title: it.Title, Link: it.Link, Matches: why == "",
				Duration: int(it.Duration / time.Second), Reason: why,
			})
		} else if why = m.byTitle(it.Title); why == "" {
			why = m.byLength(it.Duration)
		}
		if why == "" {
			out.Matched++
		}
	}
	return out, nil
}
