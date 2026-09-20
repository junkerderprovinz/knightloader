package app

// The crawl settings reach the crawler's walk options, and the status strip's
// stop button can call a crawl off. What a walk does with the options is tested
// in internal/crawler (walk_test.go).

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/crawler"
	"github.com/junkerderprovinz/knightloader/internal/settings"
)

// deepFakeCrawler records the options it was handed. It is a
// crawler.DeepCrawler, unlike fakeCrawler (crawl_test.go), which is only a
// crawler.Crawler.
type deepFakeCrawler struct {
	mu   sync.Mutex
	opt  crawler.Options
	seen int
	// block holds Crawl until the context is cancelled, so a test can observe
	// a running crawl.
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

// Every crawl setting reaches the walk.
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

// The defaults crawl one page without filters.
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

// A crawler that only implements crawler.Crawler, such as a site-specific one,
// still works; the walk options are optional.
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

// The stop button calls a crawl off through the activity stream, and the paste
// returns instead of waiting for the deadline.
func TestAbortActivityStopsARunningCrawl(t *testing.T) {
	a := crawlSettingsApp(t, settings.Settings{CrawlDepth: 3})
	dc := &deepFakeCrawler{block: true}
	a.Crawler = dc

	done := make(chan struct{})
	go func() {
		defer close(done)
		a.AddLinks([]string{"https://host.example/thread"}, "")
	}()

	// Wait until the run is cancellable, which is what the strip draws the
	// button from.
	waitForCancellableCrawl(t, a, 1)

	if n := a.AbortActivity(ActivityCrawl); n != 1 {
		t.Fatalf("AbortActivity called off %d runs, want 1", n)
	}
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("the paste never returned; the abort did not reach the crawl")
	}

	// The handle is gone again.
	for _, s := range a.ActivitySnapshot() {
		if s.Kind == ActivityCrawl && s.Cancellable != 0 {
			t.Errorf("crawl still reports %d cancellable runs after the abort", s.Cancellable)
		}
	}
}

// The run may finish before the button is pressed, which is not an error.
func TestAbortActivityWithNothingRunningIsZeroNotAnError(t *testing.T) {
	a := crawlSettingsApp(t, settings.Defaults())
	if n := a.AbortActivity(ActivityCrawl); n != 0 {
		t.Errorf("AbortActivity on an idle instance = %d, want 0", n)
	}
}

// The abort route only accepts known kinds, so a typo is not mistaken for
// "nothing was running".
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
// handle. AddLinks runs on its own goroutine here, hence the polling.
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
