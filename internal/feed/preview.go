package feed

// One feed fetched once, on somebody's say-so, and reported back instead of
// staged.
//
// It answers a question the polling path cannot be asked. A title filter is a
// regular expression typed against titles nobody has seen: before this, the only
// way to find out what a pattern matched was to save the subscription, wait for
// a poll and look at what turned up, and by then the first poll has already
// written the whole feed down as known. The addresses have the same problem in
// smaller form, because a document that parses to zero entries and a document
// that was never a feed look identical from the outside.
//
// It shares the poller's fetch and NOTHING ELSE, and that is the whole design.
// The same client, the same Accept header, the same status check and the same
// parser, so what this reports is what a poll would really get. But no State and
// no sink anywhere in the call, so a preview cannot stage a link and cannot mark
// an entry as seen. Reusing poll() would have done both, and both are one-way:
// staging puts links somebody has to delete, and a memory that has been seeded
// cannot be un-seeded, so a preview that seeded would silently cost the user
// every entry the feed is carrying at that moment.

import (
	"context"
	"net/http"
	"strings"

	"github.com/junkerderprovinz/knightloader/internal/httpx"
)

// maxPreview is how many entries one preview reports.
//
// It is a sample rather than the document. What somebody needs is enough titles
// to recognise the feed and to see the shape their pattern is being matched
// against, and ten is that. The counts beside them are over the WHOLE document
// on purpose, so a filter that takes nothing at all is still visible when the
// ten shown happen to be ten misses.
const maxPreview = 10

// Preview is what one test fetch found.
type Preview struct {
	// Title is the feed's own name, empty for a document that publishes none.
	// It is the fastest way to tell an address that is a feed from an address
	// that answered with something else.
	Title string `json:"title"`
	// Entries are the first maxPreview entries in document order, which for
	// nearly every publisher is newest first.
	//
	// Never nil: a feed carrying nothing is an empty list, so a caller can walk
	// it without a null check and an empty feed is not mistaken for a failure.
	Entries []PreviewEntry `json:"entries"`
	// Total is how many entries the whole document carried, which is capped by
	// maxItems the same way a poll's is.
	Total int `json:"total"`
	// Matched is how many of those Total entries the title filter takes. With no
	// filter it equals Total, because no filter means everything is taken rather
	// than that nothing is: the empty pattern is "no opinion", never "off".
	Matched int `json:"matched"`
}

// PreviewEntry is one entry as it would be seen by a subscription.
type PreviewEntry struct {
	// Title is the entry's own name, which is both what the filter is matched
	// against and what the staged link would be filed under.
	Title string `json:"title"`
	// Link is what would be staged, so an entry whose real download is an
	// enclosure shows the enclosure and not the article beside it.
	Link string `json:"link"`
	// Matches says whether the title filter takes this entry. True for every
	// entry when there is no filter.
	Matches bool `json:"matches"`
}

// Inspect fetches one feed once and reports what it carries. Nothing is staged,
// and nothing is written down as seen: this function is handed no State and no
// sink, which is the guarantee rather than a promise it keeps by being careful.
//
// The subscription is validated first, so the two ways of getting the row wrong
// are refused here exactly as they are refused at save time and by the runner:
// an address that is not http or https is one this process must never fetch, and
// a pattern that will not compile has to be reported rather than quietly treated
// as no filter at all.
//
// hc nil means the app's shared outbound policy, the same fallback Options.HTTP
// documents, so a caller with no client of its own still carries the user agent
// and the ceilings a poll carries.
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
		// Counted over everything and shown for the first few, so the two numbers
		// answer "does this pattern work at all" while the list answers "what am I
		// writing a pattern against".
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
