package app

import (
	"context"
	"strings"
	"testing"

	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/crawler"
	"github.com/junkerderprovinz/knightloader/internal/watch"
)

// pagedCrawler answers a different link list per page. A crawler that returns
// the same thing for every URL cannot tell one package per page from one
// package for everything.
type pagedCrawler struct {
	pages map[string][]crawler.Result
}

func (pagedCrawler) Info() crawler.Info { return crawler.Info{ID: "paged"} }
func (pagedCrawler) Match(string) bool  { return true }
func (p pagedCrawler) Crawl(_ context.Context, u string) ([]crawler.Result, error) {
	return p.pages[u], nil
}

// Every way into the app names itself on the tasks it stages, the plain
// AddLinks included, since that is the call a new entrance reaches for first.
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
		// Through AddLinksCnL, the method the listener's Adder interface names.
		// Passing OriginCnL by hand to the call underneath would prove only that
		// the parameter is honoured, not that the listener supplies it.
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

// Naming runs per crawled page, so pasting two galleries in one call does not
// file the second page's files under the first page's title.
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

// A batch no name can be derived from lands in the catch-all rather than in an
// empty package, which would be no grouping at all.
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

// The catch-all pass runs last and over everything, so a name from a rule, a
// page title or the user has to stop it.
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

// packageOf reads a task's package out of the live list. Naming happens through
// SetPackage after AddLinks has handed its structs back, so those copies are
// the wrong ones to read.
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
