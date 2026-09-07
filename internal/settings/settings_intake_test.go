package settings

import (
	"encoding/json"
	"testing"

	"github.com/junkerderprovinz/knightloader/internal/crawler"
)

// TestCrawlDefaultsAreTodaysBehaviour is the promise made to every install
// that already exists: one page, no filters. A default that crawled deeper
// would mean an update turning one paste into dozens of requests at somebody
// else's server, which is not a decision an update gets to make.
func TestCrawlDefaultsAreTodaysBehaviour(t *testing.T) {
	d := Defaults()
	if !d.Crawl {
		t.Error("Crawl defaults to false; pages have always been crawled")
	}
	if d.CrawlDepth != 1 {
		t.Errorf("CrawlDepth = %d, want 1 (the one page every install already gets)", d.CrawlDepth)
	}
	if len(d.CrawlInclude) != 0 || len(d.CrawlExclude) != 0 {
		t.Errorf("a fresh install starts with filters %v / %v, want none", d.CrawlInclude, d.CrawlExclude)
	}
	// The one default that is NOT the zero value, and only because at depth 1
	// it does nothing at all: the first person to raise the depth gets the safe
	// answer to "may it wander off this site" without being asked.
	if !d.CrawlSameHost {
		t.Error("CrawlSameHost defaults to false; a deeper crawl would then leave the site by default")
	}
}

// TestSettingsFileFromAnOlderBuildKeepsTheOnePageCrawl is the migration case
// with no migration code: these keys are simply absent from every settings.json
// written before they existed, so they decode as zero. CrawlDepth 0 has to come
// out of that as the single page, never as "unset, so use the ceiling".
func TestSettingsFileFromAnOlderBuildKeepsTheOnePageCrawl(t *testing.T) {
	var old Settings
	if err := json.Unmarshal([]byte(`{"crawl":true,"maxConcurrent":4}`), &old); err != nil {
		t.Fatal(err)
	}
	got := sanitize(old)
	if got.CrawlDepth != 1 {
		t.Errorf("an older settings file crawls at depth %d, want 1", got.CrawlDepth)
	}
	if got.CrawlMaxPages != crawler.DefaultMaxPages {
		t.Errorf("CrawlMaxPages = %d, want the package default %d", got.CrawlMaxPages, crawler.DefaultMaxPages)
	}
}

// TestSanitizeFoldsTheCrawlNumbersIntoRange pins that an impossible number is
// clamped rather than refused. These arrive from a file another build - or a
// hand edit - may have written, and the punishment for one bad integer must
// never be that pasting a page stops working.
func TestSanitizeFoldsTheCrawlNumbersIntoRange(t *testing.T) {
	for _, c := range []struct {
		name           string
		in             Settings
		depth, maxPage int
	}{
		{"above the ceiling", Settings{CrawlDepth: 99, CrawlMaxPages: 100000}, crawler.MaxDepth, crawler.MaxWalkPages},
		{"negative", Settings{CrawlDepth: -3, CrawlMaxPages: -1}, 1, crawler.DefaultMaxPages},
		{"in range is left alone", Settings{CrawlDepth: 2, CrawlMaxPages: 35}, 2, 35},
	} {
		t.Run(c.name, func(t *testing.T) {
			got := sanitize(c.in)
			if got.CrawlDepth != c.depth {
				t.Errorf("CrawlDepth = %d, want %d", got.CrawlDepth, c.depth)
			}
			if got.CrawlMaxPages != c.maxPage {
				t.Errorf("CrawlMaxPages = %d, want %d", got.CrawlMaxPages, c.maxPage)
			}
		})
	}
}

// TestSanitizeDropsBlankPatternsAndKeepsBrokenOnes pins the split between the
// two things a filter box can contain. A blank line is what a textarea leaves
// behind on every stray return and matches EVERYTHING - as an exclude that is
// "crawl nothing", silently - so it goes. A pattern that does not compile stays
// exactly as typed: the crawler refuses the run and names the box it came from,
// whereas deleting it would leave somebody looking at a filter they believe is
// filtering.
func TestSanitizeDropsBlankPatternsAndKeepsBrokenOnes(t *testing.T) {
	got := sanitize(Settings{
		CrawlExclude: []string{"sample", "", "   ", "("},
		CrawlInclude: []string{"", `\.mkv$`},
	})
	if len(got.CrawlExclude) != 2 || got.CrawlExclude[0] != "sample" || got.CrawlExclude[1] != "(" {
		t.Errorf("CrawlExclude = %v, want the two real entries with the blanks gone", got.CrawlExclude)
	}
	if len(got.CrawlInclude) != 1 || got.CrawlInclude[0] != `\.mkv$` {
		t.Errorf("CrawlInclude = %v, want the one real entry", got.CrawlInclude)
	}
}

// TestSanitizeDoesNotEditTheCallersPatternList guards the classic in-place
// filter. Settings is copied by value, but a slice field is a view onto memory
// the caller still holds, so compacting into in[:0] would shorten somebody
// else's list from a function that promised to return a new one.
func TestSanitizeDoesNotEditTheCallersPatternList(t *testing.T) {
	mine := []string{"keep", "", "also"}
	sanitize(Settings{CrawlExclude: mine})
	if len(mine) != 3 || mine[1] != "" {
		t.Errorf("the caller's slice is now %v; sanitize edited it in place", mine)
	}
}
