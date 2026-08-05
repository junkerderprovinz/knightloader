package app

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/junkerderprovinz/knightloader/internal/crawler"
	"github.com/junkerderprovinz/knightloader/internal/settings"
)

// fakeCrawler stands in for the HTML crawler so the wiring can be tested
// without a network round trip. It records what it was asked to look at, which
// is how the "did not crawl" cases below are proved rather than assumed.
type fakeCrawler struct {
	seen  []string
	yield []crawler.Result
}

func (f *fakeCrawler) Info() crawler.Info { return crawler.Info{ID: "fake"} }
func (f *fakeCrawler) Match(string) bool  { return true }
func (f *fakeCrawler) Crawl(_ context.Context, u string) ([]crawler.Result, error) {
	f.seen = append(f.seen, u)
	return f.yield, nil
}

// TestAddLinksCrawlsAPage is the whole point of the crawl seam: one pasted page
// becomes the files it points at, not one unusable task for the page.
func TestAddLinksCrawlsAPage(t *testing.T) {
	a := newCrawlApp(t, true)
	fc := &fakeCrawler{yield: []crawler.Result{
		{URL: "https://host.example/one.bin", Name: "one.bin"},
		{URL: "https://host.example/two.bin", Name: "two.bin"},
	}}
	a.Crawler = fc

	created := a.AddLinks([]string{"https://host.example/gallery"}, "Batch")
	if len(created) != 2 {
		t.Fatalf("staged %d tasks, want the 2 files the page pointed at", len(created))
	}
	if len(fc.seen) != 1 || fc.seen[0] != "https://host.example/gallery" {
		t.Fatalf("crawler saw %v", fc.seen)
	}
	for _, task := range created {
		if task.Package != "Batch" {
			t.Errorf("%s landed in package %q, want the batch's", task.Name, task.Package)
		}
		if task.URL == "https://host.example/gallery" {
			t.Error("the page itself was staged alongside its files")
		}
	}
}

// TestCrawlSkipsRealFileLinks stops the crawler from firing a request at every
// plain download link, which would double the traffic and slow every paste.
func TestCrawlSkipsRealFileLinks(t *testing.T) {
	a := newCrawlApp(t, true)
	fc := &fakeCrawler{yield: []crawler.Result{{URL: "https://host.example/nope.bin"}}}
	a.Crawler = fc

	created := a.AddLinks([]string{"https://host.example/movie.mkv"}, "")
	if len(fc.seen) != 0 {
		t.Errorf("crawled %v; a file link is already a download", fc.seen)
	}
	if len(created) != 1 || created[0].URL != "https://host.example/movie.mkv" {
		t.Fatalf("staged %d tasks, want the file itself", len(created))
	}
}

// TestCrawlOffStagesThePage keeps the setting meaningful: with crawling off the
// old behaviour has to come back exactly.
func TestCrawlOffStagesThePage(t *testing.T) {
	a := newCrawlApp(t, false)
	fc := &fakeCrawler{yield: []crawler.Result{{URL: "https://host.example/one.bin"}}}
	a.Crawler = fc

	created := a.AddLinks([]string{"https://host.example/gallery"}, "")
	if len(fc.seen) != 0 {
		t.Errorf("crawled %v with crawling disabled", fc.seen)
	}
	if len(created) != 1 || created[0].URL != "https://host.example/gallery" {
		t.Fatalf("staged %d tasks, want the page itself", len(created))
	}
}

// TestCrawlDoesNotDuplicate covers the case where a page links to something the
// same paste also named directly.
func TestCrawlDoesNotDuplicate(t *testing.T) {
	a := newCrawlApp(t, true)
	a.Crawler = &fakeCrawler{yield: []crawler.Result{
		{URL: "https://host.example/one.bin"},
		{URL: "https://host.example/one.bin"},
	}}

	created := a.AddLinks([]string{"https://host.example/gallery"}, "")
	if len(created) != 1 {
		t.Fatalf("staged %d tasks, want the duplicate collapsed", len(created))
	}
}

// TestPageThatYieldsNothingIsStillStaged is the "never drop a link" rule
// applied to the crawler: a page with no files must not vanish.
func TestPageThatYieldsNothingIsStillStaged(t *testing.T) {
	a := newCrawlApp(t, true)
	a.Crawler = &fakeCrawler{}

	created := a.AddLinks([]string{"https://host.example/empty"}, "")
	if len(created) != 1 {
		t.Fatalf("staged %d tasks, want the page kept", len(created))
	}
}

// TestCrawlAgainstARealPage exercises the shipped HTML crawler through the app,
// so the two are known to fit together and not merely to compile.
func TestCrawlAgainstARealPage(t *testing.T) {
	var base string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(`<html><body>
			<a href="/files/first.bin">first</a>
			<a href="` + base + `/files/second.iso">second</a>
			<a href="/about.html">not a file</a>
			<a href="mailto:someone@example.com">nor this</a>
		</body></html>`))
	}))
	defer srv.Close()
	base = srv.URL

	a := newCrawlApp(t, true)
	created := a.AddLinks([]string{srv.URL + "/index"}, "Real")
	if len(created) != 2 {
		names := make([]string, 0, len(created))
		for _, c := range created {
			names = append(names, c.URL)
		}
		t.Fatalf("staged %v, want exactly the two file links", names)
	}
}

func newCrawlApp(t *testing.T, crawl bool) *App {
	t.Helper()
	a, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { a.Close() })
	if _, err := a.ApplySettings(settings.Settings{
		MaxConcurrent: 2, MaxPerHost: 1, DownloadDir: t.TempDir(), Crawl: crawl,
	}); err != nil {
		t.Fatal(err)
	}
	return a
}
