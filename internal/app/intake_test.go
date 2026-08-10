package app

import (
	"context"
	"strings"
	"testing"

	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/crawler"
	"github.com/junkerderprovinz/knightloader/internal/watch"
)

// pagedCrawler answers a different link list per page, which is what the
// package-bucket tests need: one crawler that returns the same thing for every
// URL cannot tell "each page got its own package" from "everything got the first
// page's package".
type pagedCrawler struct {
	pages map[string][]crawler.Result
}

func (pagedCrawler) Info() crawler.Info { return crawler.Info{ID: "paged"} }
func (pagedCrawler) Match(string) bool  { return true }
func (p pagedCrawler) Crawl(_ context.Context, u string) ([]crawler.Result, error) {
	return p.pages[u], nil
}

// TestEveryEntranceRecordsWhereALinkCameFrom is the whole of source tracking.
//
// The column existed for a whole wave with nothing writing to it, which is worse
// than not having it: "why is this here" reads as answerable and comes back
// blank on every row. There are five ways into this app and every one of them
// has to say which it is — including the plain AddLinks, because that is the
// call a sixth entrance written in a later wave will reach for by default, and a
// default of "" is how the column goes quiet again.
func TestEveryEntranceRecordsWhereALinkCameFrom(t *testing.T) {
	t.Run("the paste box", func(t *testing.T) {
		a := newCrawlApp(t, false)
		created := a.AddLinks([]string{"https://host.example/one.bin"}, "")
		mustOrigin(t, created, OriginPaste)
	})

	t.Run("a link a crawl found, with the page beside it", func(t *testing.T) {
		a := newCrawlApp(t, true)
		a.Crawler = &fakeCrawler{yield: []crawler.Result{
			{URL: "https://host.example/one.bin", Name: "one.bin"},
			{URL: "https://host.example/two.bin", Name: "two.bin"},
		}}
		created := a.AddLinks([]string{"https://pages.example/gallery"}, "")
		mustOrigin(t, created, OriginCrawl)
		for _, task := range created {
			if task.Source != "https://pages.example/gallery" {
				t.Errorf("%s came off %q, want the page that was crawled", task.Name, task.Source)
			}
		}
	})

	t.Run("Click'n'Load", func(t *testing.T) {
		a := newCrawlApp(t, false)
		// Through AddLinksCnL, which is the method the listener's Adder interface
		// names, rather than through the one underneath it. The entrance is now a
		// parameter down there so a bridge can name it, and a test that passed
		// OriginCnL in by hand would prove only that the parameter is honoured —
		// not that the listener still supplies it.
		a.AddLinksCnL([]string{"https://host.example/one.bin"}, "", nil)
		mustOrigin(t, a.Tasks(), OriginCnL)
	})

	t.Run("the watched folder", func(t *testing.T) {
		a := newCrawlApp(t, false)
		a.stageWatchJob(watch.Job{URLs: []string{"https://host.example/one.bin"}})
		waitFor(t, "the dropped job reaching the list", func() bool {
			a.mu.Lock()
			defer a.mu.Unlock()
			return len(a.tasks) == 1
		})
		a.mu.Lock()
		defer a.mu.Unlock()
		for _, task := range a.tasks {
			if task.Origin != OriginWatch {
				t.Errorf("origin = %q, want %q", task.Origin, OriginWatch)
			}
		}
	})

	t.Run("a container", func(t *testing.T) {
		a := newCrawlApp(t, false)
		created := a.AddLinksFrom([]string{"https://host.example/one.bin"}, "", OriginContainer)
		mustOrigin(t, created, OriginContainer)
	})
}

func mustOrigin(t *testing.T, created []*core.Task, want core.Origin) {
	t.Helper()
	if len(created) == 0 {
		t.Fatal("nothing was staged")
	}
	for _, task := range created {
		if task.Origin != want {
			t.Errorf("%s arrived by %q, want %q", task.URL, task.Origin, want)
		}
	}
}

// TestEachCrawledPageGetsItsOwnPackage is the bug that made package buckets
// worth building. Naming ran once over everything one call staged, so pasting
// two galleries at once named both after whichever happened to be first — and
// the second page's files sat under a title that was never about them.
func TestEachCrawledPageGetsItsOwnPackage(t *testing.T) {
	a := newCrawlApp(t, true)
	a.Crawler = pagedCrawler{pages: map[string][]crawler.Result{
		"https://pages.example/first": {
			{URL: "https://host.example/a1.bin", Name: "a1.bin", Title: "First Gallery"},
			{URL: "https://host.example/a2.bin", Name: "a2.bin", Title: "First Gallery"},
		},
		"https://pages.example/second": {
			{URL: "https://host.example/b1.bin", Name: "b1.bin", Title: "Second Gallery"},
			{URL: "https://host.example/b2.bin", Name: "b2.bin", Title: "Second Gallery"},
		},
	}}

	created := a.AddLinks([]string{
		"https://pages.example/first",
		"https://pages.example/second",
	}, "")
	if len(created) != 4 {
		t.Fatalf("staged %d tasks, want the 4 files the two pages pointed at", len(created))
	}

	got := map[string]string{}
	for _, task := range created {
		got[task.Name] = packageOf(t, a, task.ID)
	}
	for name, want := range map[string]string{
		"a1.bin": "First Gallery", "a2.bin": "First Gallery",
		"b1.bin": "Second Gallery", "b2.bin": "Second Gallery",
	} {
		if got[name] != want {
			t.Errorf("%s landed in %q, want %q", name, got[name], want)
		}
	}
}

// TestNamelessLinksLandInTheCatchAll pins the other half of the bucket rule.
// A batch nothing can be derived from used to keep the empty package, and an
// empty package is not a group: it is the flat list of unrelated links this app
// set out to replace.
func TestNamelessLinksLandInTheCatchAll(t *testing.T) {
	a := newCrawlApp(t, false)
	// Two different hosts and no shared stem, so every derivation gives up:
	// there is genuinely nothing these two have in common but the paste.
	created := a.AddLinks([]string{
		"https://one.example/alpha.bin",
		"https://two.example/omega.iso",
	}, "")
	if len(created) != 2 {
		t.Fatalf("staged %d tasks", len(created))
	}
	for _, task := range created {
		if pkg := packageOf(t, a, task.ID); pkg != catchAllPackage {
			t.Errorf("%s landed in %q, want the catch-all %q", task.Name, pkg, catchAllPackage)
		}
	}
}

// TestTheCatchAllDoesNotOverwriteAName guards the order of the two passes. The
// catch-all runs last and over everything, so a name a rule, a page title or the
// user supplied has to be the thing that stops it.
func TestTheCatchAllDoesNotOverwriteAName(t *testing.T) {
	a := newCrawlApp(t, false)
	created := a.AddLinks([]string{"https://host.example/alpha.bin"}, "Chosen By Hand")
	if len(created) != 1 {
		t.Fatalf("staged %d tasks", len(created))
	}
	if pkg := packageOf(t, a, created[0].ID); pkg != "Chosen By Hand" {
		t.Errorf("package = %q, want the name the user typed", pkg)
	}
}

// packageOf reads a task's package back out of the live list. The tasks AddLinks
// returns are the ones it built, and naming happens through SetPackage after
// they were handed back — so a test that trusts the returned struct is testing
// the wrong copy.
func packageOf(t *testing.T, a *App, id string) string {
	t.Helper()
	a.mu.Lock()
	defer a.mu.Unlock()
	task := a.tasks[id]
	if task == nil {
		t.Fatalf("task %s is not in the list", id)
	}
	return strings.TrimSpace(task.Package)
}
