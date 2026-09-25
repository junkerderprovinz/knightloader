package app

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/crawler"
	"github.com/junkerderprovinz/knightloader/internal/settings"
)

// fakeCrawler stands in for the HTML crawler and records what it was asked to
// look at.
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

// A pasted page becomes the files it points at.
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

// A plain file link is not crawled, which would double every paste's traffic.
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

// With crawling off, the page itself is staged.
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

// A page linking the same file twice stages it once.
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

// A page with no files is staged itself rather than dropped.
func TestPageThatYieldsNothingIsStillStaged(t *testing.T) {
	a := newCrawlApp(t, true)
	a.Crawler = &fakeCrawler{}

	created := a.AddLinks([]string{"https://host.example/empty"}, "")
	if len(created) != 1 {
		t.Fatalf("staged %d tasks, want the page kept", len(created))
	}
}

// The shipped HTML crawler, through the app.
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

// yt-dlp claims every http link that is not a known hoster, so a page routed to
// it must still be crawled.
func TestCrawlRunsForYtdlpRoutedLinks(t *testing.T) {
	a := newCrawlApp(t, true)
	fc := &fakeCrawler{yield: []crawler.Result{{URL: "https://host.example/found.bin"}}}
	a.Crawler = fc

	// An extensionless page URL, routed to yt-dlp or the HTTP fallback.
	const page = "https://host.example/gallery/2026"
	created := a.AddLinks([]string{page}, "")
	if len(fc.seen) != 1 {
		t.Fatalf("the crawler saw %v; a page URL must reach it", fc.seen)
	}
	if len(created) != 1 || created[0].URL != "https://host.example/found.bin" {
		t.Fatalf("staged %d tasks; want the file the page pointed at", len(created))
	}
}

// A link a real backend claims is not crawled; fetching a hoster page would
// only collect its navigation.
func TestCrawlLeavesHosterLinksAlone(t *testing.T) {
	a := newCrawlApp(t, true)
	fc := &fakeCrawler{yield: []crawler.Result{{URL: "https://host.example/junk.bin"}}}
	a.Crawler = fc

	// A file link is claimed by the direct resolver, which is not in the set.
	a.AddLinks([]string{"https://host.example/archive.rar"}, "")
	if len(fc.seen) != 0 {
		t.Errorf("crawled %v; a link a real backend claims is already a download", fc.seen)
	}
}

// An availability probe finishing after the task was removed must not write it
// back, or it would reappear on the next restart.
func TestRemovedTaskIsNotResurrected(t *testing.T) {
	a := newCrawlApp(t, false)

	created := a.AddLinks([]string{"https://host.example/file.bin"}, "Race")
	if len(created) != 1 {
		t.Fatalf("staged %d tasks", len(created))
	}
	id := created[0].ID

	a.Remove(id, false)
	// Whatever the probe learns now arrives for a task that is gone.
	a.setAvailability(id, core.AvailOnline, "", core.ReasonUnknown)

	if len(a.Tasks()) != 0 {
		t.Fatal("the removed task came back in memory")
	}
	stored, err := a.Store.All()
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range stored {
		if s.ID == id {
			t.Fatal("the removed task was written back to the database")
		}
	}
}

// A copy of a task is taken under a.mu and written once the lock is let go,
// sometimes from a goroutine of its own, like the waiting reason a stopped queue
// gives a link it has just queued. A removal landing in between must win. The
// interleaving is written out by hand rather than raced for.
func TestACopyTakenBeforeARemovalDoesNotWriteTheTaskBack(t *testing.T) {
	a := newCrawlApp(t, false)
	// A magnet, which is resolved locally and starts no probe of its own.
	created := a.AddLinks([]string{"magnet:?xt=urn:btih:0123456789abcdef0123456789abcdef01234567"}, "Race")
	if len(created) != 1 {
		t.Fatalf("staged %d tasks", len(created))
	}
	id := created[0].ID

	a.mu.Lock()
	stale := a.copyLocked(a.tasks[id])
	a.mu.Unlock()
	a.Remove(id, false)
	a.publish(&stale)

	stored, err := a.Store.All()
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range stored {
		if s.ID == id {
			t.Fatal("the removed task was written back to the database and would reappear on the next start")
		}
	}
}

// Two copies of one task are each published after a.mu is let go, so they can
// land in either order. The newer one must win in the store and on screen, or
// the row shows an older state and keeps it after a restart. The interleaving
// is written out by hand rather than raced for.
func TestAnOlderCopyPublishedLateDoesNotReplaceANewerOne(t *testing.T) {
	a, fc, id := orderingApp(t)

	a.mu.Lock()
	older := a.copyLocked(a.tasks[id])
	a.tasks[id].Comment = "newer"
	newer := a.copyLocked(a.tasks[id])
	a.mu.Unlock()
	a.publish(&newer)
	a.publish(&older)

	if got := storedTask(t, a, id).Comment; got != "newer" {
		t.Errorf("the store holds comment %q, the older copy's", got)
	}
	if got := lastShown(t, a, fc, id).Comment; got != "newer" {
		t.Errorf("the last broadcast carries comment %q, the older copy's", got)
	}
}

// A torrent's live counts are only broadcast. A copy published after them was
// taken before them: it still has to be written, but it must not take the newer
// counts off the screen.
func TestAnOlderCopySavedAfterALiveUpdateLeavesTheScreenAlone(t *testing.T) {
	a, fc, id := orderingApp(t)

	a.mu.Lock()
	a.tasks[id].Comment = "saved"
	saved := a.copyLocked(a.tasks[id])
	a.tasks[id].Speed = 4096
	live := a.copyLocked(a.tasks[id])
	a.mu.Unlock()
	a.show(&live)
	a.publish(&saved)

	if got := storedTask(t, a, id).Comment; got != "saved" {
		t.Errorf("the store holds comment %q; the older copy was not written", got)
	}
	if got := lastShown(t, a, fc, id).Speed; got != 4096 {
		t.Errorf("the last broadcast shows speed %d, the older copy's", got)
	}
}

// orderingApp is an App with one magnet staged, whose broadcasts fc records.
func orderingApp(t *testing.T) (*App, *activityFakeConn, string) {
	t.Helper()
	a := newCrawlApp(t, false)
	fc := &activityFakeConn{}
	a.Hub.Add(fc)
	t.Cleanup(func() { a.Hub.Remove(fc) })
	created := a.AddLinks([]string{"magnet:?xt=urn:btih:0123456789abcdef0123456789abcdef01234567"}, "Race")
	if len(created) != 1 {
		t.Fatalf("staged %d tasks", len(created))
	}
	return a, fc, created[0].ID
}

// storedTask is the task as the next start would read it.
func storedTask(t *testing.T, a *App, id string) core.Task {
	t.Helper()
	stored, err := a.Store.All()
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range stored {
		if s.ID == id {
			return *s
		}
	}
	t.Fatalf("task %s is not in the store", id)
	return core.Task{}
}

// lastShown is the last broadcast of a task, read once everything broadcast so
// far has arrived.
func lastShown(t *testing.T, a *App, fc *activityFakeConn, id string) core.Task {
	t.Helper()
	a.Hub.Broadcast("test-sentinel", nil)
	waitForType(t, fc, "test-sentinel")
	var last core.Task
	for _, raw := range fc.snapshot() {
		var env struct {
			Type string    `json:"type"`
			Data core.Task `json:"data"`
		}
		if json.Unmarshal(raw, &env) == nil && env.Type == "task" && env.Data.ID == id {
			last = env.Data
		}
	}
	return last
}
