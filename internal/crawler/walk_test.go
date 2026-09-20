package crawler

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"
)

// site serves a fixed set of HTML pages and records which paths were
// requested. Most of what a walk must not do leaves no trace in the results,
// so the tests assert on the request log as well.
type site struct {
	mu   sync.Mutex
	hits []string
	srv  *httptest.Server
}

func newSite(t *testing.T, pages map[string]string) *site {
	t.Helper()
	s := &site{}
	s.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		s.hits = append(s.hits, r.URL.RequestURI())
		s.mu.Unlock()
		body, ok := pages[r.URL.Path]
		if !ok {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html")
		io.WriteString(w, body)
	}))
	t.Cleanup(s.srv.Close)
	return s
}

func (s *site) requested() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := append([]string(nil), s.hits...)
	return out
}

func (s *site) requestedPath(path string) bool {
	for _, h := range s.requested() {
		if h == path {
			return true
		}
	}
	return false
}

func urls(in []Result) []string {
	out := make([]string, 0, len(in))
	for _, r := range in {
		out = append(out, r.URL)
	}
	return out
}

func deepCrawl(t *testing.T, page string, opt Options) []Result {
	t.Helper()
	out, err := (HTML{}).CrawlDeep(context.Background(), page, opt)
	if err != nil {
		t.Fatalf("CrawlDeep(%q, %+v) = error %v, want success", page, opt, err)
	}
	return out
}

// TestWalkDepthReachesSubpages uses a table-of-contents page with no files
// of its own: depth 1 finds nothing and depth 2 finds the files one level
// down.
func TestWalkDepthReachesSubpages(t *testing.T) {
	s := newSite(t, map[string]string{
		"/thread": `<html><body>
			<a href="/part1">Part one</a>
			<a href="/part2">Part two</a>
		</body></html>`,
		"/part1": `<html><body><a href="/files/one.zip">one</a></body></html>`,
		"/part2": `<html><body><a href="/files/two.zip">two</a></body></html>`,
	})

	if got := deepCrawl(t, s.srv.URL+"/thread", Options{}); len(got) != 0 {
		t.Fatalf("the zero Options found %v, want nothing (it must stay the one-page crawl)", urls(got))
	}
	if s.requestedPath("/part1") {
		t.Error("a depth-1 crawl fetched a subpage; the default must follow nothing")
	}

	got := deepCrawl(t, s.srv.URL+"/thread", Options{Depth: 2})
	want := []string{s.srv.URL + "/files/one.zip", s.srv.URL + "/files/two.zip"}
	if strings.Join(urls(got), ",") != strings.Join(want, ",") {
		t.Errorf("depth 2 found %v, want %v", urls(got), want)
	}
}

func TestWalkThreeLevels(t *testing.T) {
	s := newSite(t, map[string]string{
		"/a": `<html><body><a href="/b">b</a></body></html>`,
		"/b": `<html><body><a href="/c">c</a></body></html>`,
		"/c": `<html><body><a href="/deep.zip">deep</a><a href="/d">d</a></body></html>`,
		"/d": `<html><body><a href="/toofar.zip">too far</a></body></html>`,
	})

	got := deepCrawl(t, s.srv.URL+"/a", Options{Depth: 3})
	if want := []string{s.srv.URL + "/deep.zip"}; strings.Join(urls(got), ",") != strings.Join(want, ",") {
		t.Errorf("depth 3 found %v, want %v", urls(got), want)
	}
	if s.requestedPath("/d") {
		t.Error("depth 3 fetched a fourth level")
	}

	if _, err := (HTML{}).CrawlDeep(context.Background(), s.srv.URL+"/a", Options{Depth: 99}); err != nil {
		t.Errorf("depth 99 = error %v, want it clamped to MaxDepth and run", err)
	}
}

// TestWalkStopsOnALoop checks that pages linking each other are fetched once
// each and the shared file is reported once.
func TestWalkStopsOnALoop(t *testing.T) {
	s := newSite(t, map[string]string{
		"/a": `<html><body><a href="/b">b</a><a href="/shared.zip">shared</a></body></html>`,
		"/b": `<html><body><a href="/a">a</a><a href="/shared.zip">shared again</a></body></html>`,
	})

	got := deepCrawl(t, s.srv.URL+"/a", Options{Depth: 3, MaxPages: 50})
	if want := []string{s.srv.URL + "/shared.zip"}; strings.Join(urls(got), ",") != strings.Join(want, ",") {
		t.Errorf("found %v, want the one file once; the loop was walked more than once", urls(got))
	}
	hits := s.requested()
	if len(hits) != 2 {
		t.Errorf("server saw %v, want exactly one request per page", hits)
	}
}

// TestWalkSameHost checks that pages on another host are not followed while
// files on any host are still kept.
func TestWalkSameHost(t *testing.T) {
	other := newSite(t, map[string]string{
		"/elsewhere": `<html><body><a href="/stranger.zip">stranger</a></body></html>`,
	})
	// httptest binds every server to 127.0.0.1 and the host rule ignores the
	// port, so the other server is addressed by a different name.
	elsewhere := strings.Replace(other.srv.URL, "127.0.0.1", "localhost", 1)

	s := newSite(t, map[string]string{
		"/index": fmt.Sprintf(`<html><body>
			<a href="%s/elsewhere">another host</a>
			<a href="https://cdn.example.net/far.zip">a file somewhere else entirely</a>
			<a href="/local">local page</a>
		</body></html>`, elsewhere),
		"/local": `<html><body><a href="/near.zip">near</a></body></html>`,
	})

	got := deepCrawl(t, s.srv.URL+"/index", Options{Depth: 2, SameHost: true})
	want := []string{"https://cdn.example.net/far.zip", s.srv.URL + "/near.zip"}
	sort.Strings(want)
	have := urls(got)
	sort.Strings(have)
	if strings.Join(have, ",") != strings.Join(want, ",") {
		t.Errorf("found %v, want %v (the host rule must bind pages, never results)", have, want)
	}
	if len(other.requested()) != 0 {
		t.Errorf("the other host saw %v, want nothing with SameHost on", other.requested())
	}

	got = deepCrawl(t, s.srv.URL+"/index", Options{Depth: 2, SameHost: false})
	if !strings.Contains(strings.Join(urls(got), ","), "/stranger.zip") {
		t.Errorf("with SameHost off the walk found %v, want the other host's file too", urls(got))
	}
}

// TestSameHostExcludesSubdomains covers what a test server cannot bind: a
// subdomain is a different host.
func TestSameHostExcludesSubdomains(t *testing.T) {
	w := &walk{host: "example.com", sameHost: true, visited: map[string]bool{}}
	for _, c := range []struct {
		raw  string
		want bool
	}{
		{"https://example.com/page", true},
		{"http://example.com/page", true},    // the scheme is not part of the rule
		{"https://example.com:8443/p", true}, // nor is the port
		{"https://EXAMPLE.com/page", true},   // hosts are case-insensitive
		{"https://news.example.com/p", false},
		{"https://www.example.com/p", false},
		{"https://example.com.evil.test/p", false}, // the prefix attack a suffix rule invites
	} {
		u, err := url.Parse(c.raw)
		if err != nil {
			t.Fatal(err)
		}
		if got := w.follow(u); got != c.want {
			t.Errorf("follow(%q) = %v, want %v", c.raw, got, c.want)
		}
	}
}

// TestWalkMaxPagesCountsFetchesNotLinks sets a page cap of three over
// subpages with three files each; counting requests yields six files.
func TestWalkMaxPagesCountsFetchesNotLinks(t *testing.T) {
	pages := map[string]string{
		"/index": `<html><body>
			<a href="/p1">1</a><a href="/p2">2</a><a href="/p3">3</a><a href="/p4">4</a>
		</body></html>`,
	}
	for i := 1; i <= 4; i++ {
		pages[fmt.Sprintf("/p%d", i)] = fmt.Sprintf(
			`<html><body><a href="/f%da.zip">a</a><a href="/f%db.zip">b</a><a href="/f%dc.zip">c</a></body></html>`, i, i, i)
	}
	s := newSite(t, pages)

	got := deepCrawl(t, s.srv.URL+"/index", Options{Depth: 2, MaxPages: 3})
	if len(s.requested()) != 3 {
		t.Errorf("server saw %d requests, want exactly the 3 the cap allows: %v", len(s.requested()), s.requested())
	}
	if len(got) != 6 {
		t.Errorf("found %d links, want the 6 that two crawled subpages hold: %v", len(got), urls(got))
	}
}

// TestWalkExcludeIsNotFetchedAtAll checks that an excluded page is never
// requested, not merely dropped from the results.
func TestWalkExcludeIsNotFetchedAtAll(t *testing.T) {
	s := newSite(t, map[string]string{
		"/index": `<html><body>
			<a href="/keep">keep</a>
			<a href="/private/secret">private</a>
		</body></html>`,
		"/keep":           `<html><body><a href="/good.zip">good</a><a href="/sample.zip">sample</a></body></html>`,
		"/private/secret": `<html><body><a href="/leak.zip">leak</a></body></html>`,
	})

	got := deepCrawl(t, s.srv.URL+"/index", Options{Depth: 2, Exclude: []string{"/private/", "sample"}})
	if want := []string{s.srv.URL + "/good.zip"}; strings.Join(urls(got), ",") != strings.Join(want, ",") {
		t.Errorf("found %v, want %v", urls(got), want)
	}
	if s.requestedPath("/private/secret") {
		t.Error("an excluded page was fetched; the exclude only filtered the results")
	}
}

// TestWalkIncludeNarrowsFilesWithoutCuttingTheWalk checks that an include
// pattern about files does not stop the walk at the first page.
func TestWalkIncludeNarrowsFilesWithoutCuttingTheWalk(t *testing.T) {
	s := newSite(t, map[string]string{
		"/index": `<html><body><a href="/season1">season one</a></body></html>`,
		"/season1": `<html><body>
			<a href="/e01.mkv">episode one</a>
			<a href="/e01.nfo">notes</a>
		</body></html>`,
	})

	got := deepCrawl(t, s.srv.URL+"/index", Options{Depth: 2, Include: []string{`\.mkv$`}})
	if !s.requestedPath("/season1") {
		t.Fatal("the include pattern stopped the walk from following a page; it must only narrow the files")
	}
	if want := []string{s.srv.URL + "/e01.mkv"}; strings.Join(urls(got), ",") != strings.Join(want, ",") {
		t.Errorf("found %v, want %v", urls(got), want)
	}
}

func TestWalkRefusesABadPatternBeforeFetching(t *testing.T) {
	s := newSite(t, map[string]string{"/index": `<html><body><a href="/f.zip">f</a></body></html>`})

	_, err := (HTML{}).CrawlDeep(context.Background(), s.srv.URL+"/index", Options{Exclude: []string{"("}})
	if err == nil {
		t.Fatal("a pattern that does not compile was accepted")
	}
	if !strings.Contains(err.Error(), "exclude") {
		t.Errorf("error %q does not name the list the bad pattern came from", err)
	}
	if len(s.requested()) != 0 {
		t.Errorf("server saw %v, want no request at all before the patterns were checked", s.requested())
	}
}

// TestWalkCancelKeepsNothing checks that a cancelled walk returns no links, so
// a partial list is not taken for a complete one.
func TestWalkCancelKeepsNothing(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	var served int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		served++
		if served > 1 {
			// Cancel on the second page, after the first has produced results.
			cancel()
			time.Sleep(20 * time.Millisecond)
		}
		w.Header().Set("Content-Type", "text/html")
		io.WriteString(w, `<html><body><a href="/a.zip">a</a><a href="/next">next</a></body></html>`)
	}))
	defer srv.Close()

	got, err := (HTML{}).CrawlDeep(ctx, srv.URL+"/start", Options{Depth: 3, MaxPages: 20})
	if err == nil {
		t.Fatal("a cancelled walk reported success")
	}
	if len(got) != 0 {
		t.Errorf("a cancelled walk returned %v, want nothing", urls(got))
	}
}

func TestWalkSkipsADeadSubpageButNotADeadSeed(t *testing.T) {
	s := newSite(t, map[string]string{
		"/index": `<html><body><a href="/gone">gone</a><a href="/alive">alive</a></body></html>`,
		"/alive": `<html><body><a href="/found.zip">found</a></body></html>`,
	})

	got := deepCrawl(t, s.srv.URL+"/index", Options{Depth: 2})
	if want := []string{s.srv.URL + "/found.zip"}; strings.Join(urls(got), ",") != strings.Join(want, ",") {
		t.Errorf("found %v, want %v; one 404 subpage must not take the others with it", urls(got), want)
	}

	if _, err := (HTML{}).CrawlDeep(context.Background(), s.srv.URL+"/missing", Options{Depth: 2}); err == nil {
		t.Error("a 404 on the pasted page reported success")
	}
}

func TestWalkLinkBudgetIsForTheWholeWalk(t *testing.T) {
	var index strings.Builder
	index.WriteString("<html><body>")
	pages := map[string]string{}
	for i := range 4 {
		fmt.Fprintf(&index, `<a href="/p%d">%d</a>`, i, i)
		var sub strings.Builder
		sub.WriteString("<html><body>")
		for j := range 5 {
			fmt.Fprintf(&sub, `<a href="/f%d-%d.zip">f</a>`, i, j)
		}
		sub.WriteString("</body></html>")
		pages[fmt.Sprintf("/p%d", i)] = sub.String()
	}
	index.WriteString("</body></html>")
	pages["/index"] = index.String()
	s := newSite(t, pages)

	out, err := (HTML{MaxLinks: 7}).CrawlDeep(context.Background(), s.srv.URL+"/index", Options{Depth: 2, MaxPages: 20})
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 7 {
		t.Errorf("collected %d links, want the whole walk capped at 7: %v", len(out), urls(out))
	}
}

// TestWalkFetchesAFileBehindAPageLink covers /download.php?id=7, which looks
// like a page and serves a file.
func TestWalkFetchesAFileBehindAPageLink(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/index" {
			w.Header().Set("Content-Type", "text/html")
			io.WriteString(w, `<html><body><a href="/download.php?id=7">get it</a></body></html>`)
			return
		}
		w.Header().Set("Content-Type", "application/zip")
		io.WriteString(w, "PK")
	}))
	defer srv.Close()

	got := deepCrawl(t, srv.URL+"/index", Options{Depth: 2})
	if want := []string{srv.URL + "/download.php?id=7"}; strings.Join(urls(got), ",") != strings.Join(want, ",") {
		t.Errorf("found %v, want %v", urls(got), want)
	}
}
