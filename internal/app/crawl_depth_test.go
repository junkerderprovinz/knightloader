package app

// The wiring between the crawl settings block, the crawler's own walk options,
// and the stop button on the status strip. What a walk DOES with those options
// is internal/crawler's business and is pinned there (walk_test.go); what is
// tested here is that the numbers a person typed are the numbers that run, and
// that the run can be called off.

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/crawler"
	"github.com/junkerderprovinz/knightloader/internal/settings"
)

// deepFakeCrawler records the options it was handed. It satisfies
// crawler.DeepCrawler, unlike fakeCrawler (crawl_test.go) which satisfies only
// crawler.Crawler - the pair is what proves the fallback below is real.
type deepFakeCrawler struct {
	mu   sync.Mutex
	opt  crawler.Options
	seen int
	// block, when set, holds Crawl until the context is cancelled. It is how a
	// test gets to look at a crawl that is still running.
	block bool
	yield []crawler.Result
}

func (d *deepFakeCrawler) Info() crawler.Info { return crawler.Info{ID: "deepfake"} }
func (d *deepFakeCrawler) Match(string) bool  { return true }

func (d *deepFakeCrawler) Crawl(ctx context.Context, u string) ([]crawler.Result, error) {
	return d.CrawlDeep(ctx, u, crawler.Options{})
}

func (d *deepFakeCrawler) CrawlDeep(ctx context.Context, _ string, opt crawler.Options) ([]crawler.Result, error) {
	d.mu.Lock()
	d.opt = opt
	d.seen++
	blocking := d.block
	d.mu.Unlock()
	if blocking {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	return d.yield, nil
}

func (d *deepFakeCrawler) options() crawler.Options {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.opt
}

func (d *deepFakeCrawler) calls() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.seen
}

// crawlSettingsApp is newCrawlApp with the crawl block filled in.
func crawlSettingsApp(t *testing.T, cfg settings.Settings) *App {
	t.Helper()
	a, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { a.Close() })
	cfg.MaxConcurrent, cfg.MaxPerHost, cfg.DownloadDir, cfg.Crawl = 2, 1, t.TempDir(), true
	if _, err := a.ApplySettings(cfg); err != nil {
		t.Fatal(err)
	}
	return a
}

// TestCrawlPassesTheSettingsBlockThrough is the seam between the settings page
// and the walk. Every one of these is a control somebody set for a reason, and
// a field dropped on the way here is a setting that saves, reloads, reads back
// correctly and does nothing at all.
func TestCrawlPassesTheSettingsBlockThrough(t *testing.T) {
	a := crawlSettingsApp(t, settings.Settings{
		CrawlDepth:    3,
		CrawlMaxPages: 42,
		CrawlSameHost: true,
		CrawlInclude:  []string{`\.mkv$`},
		CrawlExclude:  []string{"sample"},
	})
	dc := &deepFakeCrawler{yield: []crawler.Result{{URL: "https://host.example/one.bin"}}}
	a.Crawler = dc

	a.AddLinks([]string{"https://host.example/thread"}, "")

	got := dc.options()
	if got.Depth != 3 || got.MaxPages != 42 || !got.SameHost {
		t.Errorf("crawl ran with %+v, want depth 3, 42 pages, same host on", got)
	}
	if len(got.Include) != 1 || got.Include[0] != `\.mkv$` {
		t.Errorf("include reached the crawler as %v", got.Include)
	}
	if len(got.Exclude) != 1 || got.Exclude[0] != "sample" {
		t.Errorf("exclude reached the crawler as %v", got.Exclude)
	}
}

// TestCrawlDefaultsToOnePageAndNoFilters is the promise made to every existing
// install: an update must not turn a paste into a three-level crawl of somebody
// else's forum. A fresh instance that never opens the settings block gets
// exactly the crawl it always got.
func TestCrawlDefaultsToOnePageAndNoFilters(t *testing.T) {
	a := crawlSettingsApp(t, settings.Defaults())
	dc := &deepFakeCrawler{yield: []crawler.Result{{URL: "https://host.example/one.bin"}}}
	a.Crawler = dc

	a.AddLinks([]string{"https://host.example/thread"}, "")

	got := dc.options()
	if got.Depth != 1 {
		t.Errorf("a default install crawls at depth %d, want 1", got.Depth)
	}
	if len(got.Include) != 0 || len(got.Exclude) != 0 {
		t.Errorf("a default install crawls with filters %v / %v, want none", got.Include, got.Exclude)
	}
}

// TestCrawlFallsBackToThePlainInterface pins that the options are OFFERED, not
// required. A site-specific crawler knows its own site and has no depth to be
// told about, and every stand-in a test has written implements the three
// methods of crawler.Crawler and nothing else - if the deep path were mandatory
// they would all stop being crawlers at all.
func TestCrawlFallsBackToThePlainInterface(t *testing.T) {
	a := crawlSettingsApp(t, settings.Settings{CrawlDepth: 3})
	plain := &fakeCrawler{yield: []crawler.Result{{URL: "https://host.example/one.bin", Name: "one.bin"}}}
	a.Crawler = plain

	created := a.AddLinks([]string{"https://host.example/thread"}, "")
	if len(plain.seen) != 1 {
		t.Fatalf("a Crawler-only implementation saw %v, want the one page", plain.seen)
	}
	if len(created) != 1 {
		t.Fatalf("staged %d tasks, want the file the page pointed at", len(created))
	}
}

// TestAbortActivityStopsARunningCrawl is the stop button end to end: a crawl
// that is going nowhere is called off through the same activity stream it is
// published on, and the paste finishes instead of holding the request open
// until the deadline.
func TestAbortActivityStopsARunningCrawl(t *testing.T) {
	a := crawlSettingsApp(t, settings.Settings{CrawlDepth: 3})
	dc := &deepFakeCrawler{block: true}
	a.Crawler = dc

	done := make(chan struct{})
	go func() {
		defer close(done)
		a.AddLinks([]string{"https://host.example/thread"}, "")
	}()

	// Wait for the run to be visible AS cancellable, which is the state the
	// strip draws a button from: a test that only waited for Active>0 would
	// pass with the handle never registered at all.
	waitForCancellableCrawl(t, a, 1)

	if n := a.AbortActivity(ActivityCrawl); n != 1 {
		t.Fatalf("AbortActivity called off %d runs, want 1", n)
	}
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("the paste never returned; the abort did not reach the crawl")
	}

	// And the handle is gone again, so the strip stops offering a button for
	// work that is over.
	for _, s := range a.ActivitySnapshot() {
		if s.Kind == ActivityCrawl && s.Cancellable != 0 {
			t.Errorf("crawl still reports %d cancellable runs after the abort", s.Cancellable)
		}
	}
}

// TestAbortActivityWithNothingRunningIsZeroNotAnError pins the ordinary race
// for a control that only exists while work is in flight: the run can finish
// between the strip drawing the button and somebody pressing it.
func TestAbortActivityWithNothingRunningIsZeroNotAnError(t *testing.T) {
	a := crawlSettingsApp(t, settings.Defaults())
	if n := a.AbortActivity(ActivityCrawl); n != 0 {
		t.Errorf("AbortActivity on an idle instance = %d, want 0", n)
	}
}

// TestKnownActivityKind pins the guard the abort route stands on. A free-text
// kind would make a typo look exactly like "nothing was running", which is a
// client bug that goes on being pressed forever.
func TestKnownActivityKind(t *testing.T) {
	for _, s := range []string{"crawl", "CRAWL", " linkcheck ", "captcha", "autoconfirm", "container"} {
		if _, ok := KnownActivityKind(s); !ok {
			t.Errorf("KnownActivityKind(%q) = false, want it recognised", s)
		}
	}
	for _, s := range []string{"", "crawls", "download", "crawl "} {
		if k, ok := KnownActivityKind(s); ok && string(k) != "crawl" {
			t.Errorf("KnownActivityKind(%q) = %q, want it refused", s, k)
		}
	}
	if _, ok := KnownActivityKind("download"); ok {
		t.Error("KnownActivityKind accepted a kind that does not exist")
	}
}

// waitForCancellableCrawl blocks until at least n crawl runs report a stop
// handle. Polled rather than read once: AddLinks runs on its own goroutine
// here, so reading the snapshot immediately would be racing it.
func waitForCancellableCrawl(t *testing.T, a *App, n int) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		for _, s := range a.ActivitySnapshot() {
			if s.Kind == ActivityCrawl && s.Cancellable >= n {
				return
			}
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatalf("no crawl reported %d cancellable run(s) within the deadline", n)
}
