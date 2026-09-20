package settings

// What happens to a link on the way in and to a file on the way out: how far a
// pasted page is followed, when two URLs are the same download, and what to do
// when the destination is taken.

import (
	"strings"

	"github.com/junkerderprovinz/knightloader/internal/collide"
	"github.com/junkerderprovinz/knightloader/internal/crawler"
	"github.com/junkerderprovinz/knightloader/internal/dedupe"
)

func sanitizeIntake(n Settings) Settings {
	// The crawl numbers are folded into the range the crawler can honour rather
	// than refused, the trade the two policies below make: they arrive from a
	// settings file another build may have written, and one impossible integer
	// must not stop a pasted page from working.
	//
	// Zero reads as "not set" rather than being clamped up, because an install
	// that predates these fields decodes them as 0 and CrawlDepth 0 has to keep
	// meaning the one page it has always been.
	if n.CrawlDepth < 1 {
		n.CrawlDepth = 1
	}
	if n.CrawlDepth > crawler.MaxDepth {
		n.CrawlDepth = crawler.MaxDepth
	}
	if n.CrawlMaxPages < 1 {
		n.CrawlMaxPages = crawler.DefaultMaxPages
	}
	if n.CrawlMaxPages > crawler.MaxWalkPages {
		n.CrawlMaxPages = crawler.MaxWalkPages
	}
	// Blank lines are dropped, invalid patterns are not. A textarea produces an
	// empty entry every time somebody hits return, and an empty pattern matches
	// everything, which as an exclude means crawling nothing. A pattern that
	// does not compile is left as typed: the crawler refuses the run and says
	// which box it came from, whereas deleting it would leave somebody looking
	// at a filter box they believe is filtering.
	n.CrawlInclude = nonBlank(n.CrawlInclude)
	n.CrawlExclude = nonBlank(n.CrawlExclude)

	// Both policies fold an unknown value onto their package's default instead
	// of failing, so a settings file written by another build, or hand-edited
	// with a typo, can never stop links from being added.
	n.MirrorPolicy = string(dedupe.ParsePolicy(n.MirrorPolicy))
	n.CollisionPolicy = string(collide.ParsePolicy(n.CollisionPolicy))
	// Zero stays zero, meaning the package's own cap. A number above it is not a
	// bigger allowance but the runaway guard switched off, which is how a watch
	// folder re-reading one list fills a directory nobody can open.
	if n.CollisionMaxAttempts < 0 {
		n.CollisionMaxAttempts = 0
	}
	if n.CollisionMaxAttempts > collide.DefaultMaxAttempts {
		n.CollisionMaxAttempts = collide.DefaultMaxAttempts
	}
	return n
}

// nonBlank drops the empty entries a multi-line box leaves behind.
//
// It builds a new slice rather than filtering in place. Settings is copied by
// value, but a slice field is a view onto memory the caller still holds, so
// compacting into in[:0] would shorten the list the caller passed in.
func nonBlank(in []string) []string {
	if len(in) == 0 {
		return in
	}
	out := make([]string, 0, len(in))
	for _, s := range in {
		if strings.TrimSpace(s) != "" {
			out = append(out, s)
		}
	}
	return out
}
