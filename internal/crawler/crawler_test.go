package crawler

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

// serve starts a test server that answers every request with body and type.
func serve(t *testing.T, contentType, body string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", contentType)
		io.WriteString(w, body)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func crawl(t *testing.T, page string, c HTML) []Result {
	t.Helper()
	out, err := c.Crawl(context.Background(), page)
	if err != nil {
		t.Fatalf("Crawl(%q) = error %v, want success", page, err)
	}
	return out
}

func wantResults(t *testing.T, got, want []Result) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("got %d results, want %d\ngot:  %+v\nwant: %+v", len(got), len(want), got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("result %d = %+v, want %+v", i, got[i], want[i])
		}
	}
}

// TestCrawlCollectsFileLinksInDocumentOrder is the core promise of the package:
// one page URL becomes the many file links it points at. If it fails, either
// something that is not a download leaked into the task list (an anchor, a
// mailto:, a link to the next page) or a relative link was queued unresolved
// and would 404 the moment the engine tried to fetch it.
func TestCrawlCollectsFileLinksInDocumentOrder(t *testing.T) {
	page := `<html><body>
		<a href="#top">back to top</a>
		<a href="mailto:me@example.com">mail me</a>
		<a href="javascript:void(0)">nothing</a>
		<a href="/files/one.zip">One archive</a>
		<a href="two.mkv">Two</a>
		<a href="https://cdn.example.net/three.iso">Three</a>
		<a href="index.html">next page</a>
		<a href="download.php?id=7">get it</a>
		<a href="ftp://example.com/four.zip">old school</a>
		<a href="/files/one.zip">the same archive again</a>
		<video src="/media/clip.mp4"></video>
		<img src="/img/pic.jpg">
	</body></html>`
	srv := serve(t, "text/html; charset=utf-8", page)

	got := crawl(t, srv.URL+"/gallery/index.html", HTML{})
	wantResults(t, got, []Result{
		{URL: srv.URL + "/files/one.zip", Name: "One archive"},
		{URL: srv.URL + "/gallery/two.mkv", Name: "Two"},
		{URL: "https://cdn.example.net/three.iso", Name: "Three"},
		{URL: srv.URL + "/media/clip.mp4", Name: "clip.mp4"},
		// No entry for the page's <img>: an ordinary page is full of logos and
		// tracking pixels, and none of them is what a link was pasted for.
	})
}

// TestCrawlIndexOfListing pins the shape this package was written for. An
// autoindex is a wall of anchors where the parent link and the subdirectories
// look exactly like the files; treating "../" or "sub/" as a download would
// queue directories as tasks.
func TestCrawlIndexOfListing(t *testing.T) {
	page := `<html><head><title>Index of /pub/</title></head><body>
<h1>Index of /pub/</h1><hr><pre><a href="../">../</a>
<a href="sub/">sub/</a>                        01-Jan-2026 00:00       -
<a href="debian-12.iso">debian-12.iso</a>      01-Jan-2026 00:00       4096
<a href="notes.txt">notes.txt</a>              01-Jan-2026 00:00       17
</pre><hr></body></html>`
	srv := serve(t, "text/html", page)

	got := crawl(t, srv.URL+"/pub/", HTML{})
	wantResults(t, got, []Result{
		{URL: srv.URL + "/pub/debian-12.iso", Name: "debian-12.iso"},
		{URL: srv.URL + "/pub/notes.txt", Name: "notes.txt"},
	})
}

// TestCrawlNonHTMLResponseIsOneUnparsedResult guards the case where the user
// pasted a file, not a page. The body here is valid HTML full of links, so a
// crawler that ignored the content type would explode one download into five.
func TestCrawlNonHTMLResponseIsOneUnparsedResult(t *testing.T) {
	body := `<html><body><a href="/a.zip">a</a><a href="/b.zip">b</a></body></html>`
	for _, ct := range []string{"application/octet-stream", "application/zip", ""} {
		t.Run(fmt.Sprintf("content-type=%q", ct), func(t *testing.T) {
			srv := serve(t, ct, body)
			got := crawl(t, srv.URL+"/archive.zip", HTML{})
			wantResults(t, got, []Result{{URL: srv.URL + "/archive.zip", Name: "archive.zip"}})
		})
	}
}

// TestCrawlMaxLinksTruncates pins the cap that keeps one paste from becoming an
// unmanageable task list. Truncation must keep document order, so the user gets
// the top of the page rather than an arbitrary subset of it.
func TestCrawlMaxLinksTruncates(t *testing.T) {
	var sb strings.Builder
	sb.WriteString("<html><body>")
	for i := range 50 {
		fmt.Fprintf(&sb, `<a href="/f%02d.bin">file %02d</a>`, i, i)
	}
	sb.WriteString("</body></html>")
	srv := serve(t, "text/html", sb.String())

	got := crawl(t, srv.URL+"/list", HTML{MaxLinks: 3})
	wantResults(t, got, []Result{
		{URL: srv.URL + "/f00.bin", Name: "file 00"},
		{URL: srv.URL + "/f01.bin", Name: "file 01"},
		{URL: srv.URL + "/f02.bin", Name: "file 02"},
	})

	// Zero has to mean "use the default", not "collect nothing".
	if n := len(crawl(t, srv.URL+"/list", HTML{})); n != 50 {
		t.Errorf("MaxLinks 0 collected %d links, want all 50 (zero must mean default)", n)
	}
}

// TestCrawlRefusesDeclaredOversizePage pins the cheap half of the size guard: a
// declared length over the cap is refused before any of the body is read.
func TestCrawlRefusesDeclaredOversizePage(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.Header().Set("Content-Length", fmt.Sprint(int64(4)<<30))
		io.WriteString(w, "<html><body>")
	}))
	// The handler deliberately sends less than it declared, which the server
	// reports; silence it so the test output stays readable.
	srv.Config.ErrorLog = log.New(io.Discard, "", 0)
	defer srv.Close()

	_, err := (HTML{}).Crawl(context.Background(), srv.URL+"/huge.html")
	if !errors.Is(err, ErrPageTooLarge) {
		t.Fatalf("Crawl of a 4 GB page = %v, want ErrPageTooLarge", err)
	}
}

// TestCrawlRefusesOversizeStreamWithoutDrainingIt pins the expensive half: a
// chunked response declares no length, so the read itself must stop at the cap.
// If it did not, this crawl would buffer 64 MB (and a real host could stream
// forever) — the failure mode is the crawler killing its own machine.
func TestCrawlRefusesOversizeStreamWithoutDrainingIt(t *testing.T) {
	const (
		chunk  = 256 << 10
		chunks = 256 // 64 MB in total, eight times the cap
	)
	var served atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		blob := make([]byte, chunk)
		for i := range blob {
			blob[i] = 'x'
		}
		for range chunks {
			n, err := w.Write(blob)
			served.Add(int64(n))
			if err != nil {
				return // the crawler hung up, which is the point
			}
			w.(http.Flusher).Flush()
		}
	}))
	srv.Config.ErrorLog = log.New(io.Discard, "", 0)
	defer srv.Close()

	_, err := (HTML{}).Crawl(context.Background(), srv.URL+"/endless.html")
	if !errors.Is(err, ErrPageTooLarge) {
		t.Fatalf("Crawl of a 64 MB page = %v, want ErrPageTooLarge", err)
	}
	// Socket buffering means the server gets a little ahead of the reader, so
	// this only asserts the stream was abandoned, not the exact byte it stopped at.
	if got := served.Load(); got >= chunk*chunks/2 {
		t.Errorf("server wrote %d bytes, want it cut off well before %d (body was drained, not refused)",
			got, chunk*chunks)
	}
}

// TestCrawlWithoutFileLinksIsEmptyNotAnError pins that a page of nothing but
// navigation is a legitimate answer. Returning an error here would make the UI
// show a failure for a page that simply has no downloads on it.
func TestCrawlWithoutFileLinksIsEmptyNotAnError(t *testing.T) {
	page := `<html><body>
		<a href="/about.html">about</a>
		<a href="#section">section</a>
		<a href="mailto:me@example.com">mail</a>
		<a href="/">home</a>
	</body></html>`
	srv := serve(t, "text/html", page)

	got, err := (HTML{}).Crawl(context.Background(), srv.URL+"/page.html")
	if err != nil {
		t.Fatalf("Crawl = error %v, want success", err)
	}
	if got == nil {
		t.Error("got nil, want an empty slice (callers range over the result)")
	}
	if len(got) != 0 {
		t.Errorf("got %+v, want no results", got)
	}
}

// TestCrawlResolvesLinksAgainstFinalURL pins that relative links resolve
// against where the page came from. Resolving against the requested URL instead
// would silently point every link at the wrong directory after a redirect.
func TestCrawlResolvesLinksAgainstFinalURL(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/old", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/new/list.html", http.StatusFound)
	})
	mux.HandleFunc("/new/list.html", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		io.WriteString(w, `<html><body><a href="moved.zip">Moved</a></body></html>`)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	got := crawl(t, srv.URL+"/old", HTML{})
	wantResults(t, got, []Result{{URL: srv.URL + "/new/moved.zip", Name: "Moved"}})
}

// TestCrawlStopsRedirectLoop pins the hop limit. Without it a page that
// redirects to itself keeps a crawl going until the client's own default gives
// up, which is twice as many round trips to the same hostile host.
func TestCrawlStopsRedirectLoop(t *testing.T) {
	var hits atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		http.Redirect(w, r, "/loop", http.StatusFound)
	}))
	defer srv.Close()

	if _, err := (HTML{}).Crawl(context.Background(), srv.URL+"/loop"); err == nil {
		t.Fatal("Crawl of a redirect loop succeeded, want an error")
	}
	if got := hits.Load(); got > maxRedirects+1 {
		t.Errorf("server saw %d requests, want at most %d (the hop limit did not hold)",
			got, maxRedirects+1)
	}
}

// TestCrawlRejectsNonHTTPStatus keeps a 404 page from being crawled for links.
// Error pages are full of navigation, and collecting it would turn a dead link
// into a handful of live but wrong ones.
func TestCrawlRejectsNonHTTPStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(http.StatusNotFound)
		io.WriteString(w, `<html><body><a href="/home.zip">home</a></body></html>`)
	}))
	defer srv.Close()

	if _, err := (HTML{}).Crawl(context.Background(), srv.URL+"/gone.html"); err == nil {
		t.Fatal("Crawl of a 404 succeeded, want an error")
	}
}

// TestMatch pins the scheme guard. Anything the engine cannot fetch over HTTP
// must be refused here, or the crawler claims a link and fails on it later
// instead of letting another backend have it.
func TestMatch(t *testing.T) {
	pages := []string{
		"http://example.com/",
		"https://example.com/gallery/42",
		"https://example.com/index.php?id=3",
	}
	others := []string{
		"ftp://example.com/pub/",
		"magnet:?xt=urn:btih:deadbeef",
		"mailto:me@example.com",
		"file:///c:/temp/list.html",
		"/local/path/index.html",
		"example.com/gallery",
		"",
	}
	for _, u := range pages {
		if !(HTML{}).Match(u) {
			t.Errorf("Match(%q) = false, want true (it is a fetchable page)", u)
		}
	}
	for _, u := range others {
		if (HTML{}).Match(u) {
			t.Errorf("Match(%q) = true, want false (it is not http(s))", u)
		}
	}
}

// TestCrawlRejectsNonHTTPURL pins that Match and Crawl agree: a URL Match turns
// down must not sneak through Crawl by another path.
func TestCrawlRejectsNonHTTPURL(t *testing.T) {
	if _, err := (HTML{}).Crawl(context.Background(), "ftp://example.com/pub/"); err == nil {
		t.Fatal("Crawl of an ftp URL succeeded, want an error")
	}
}

// TestHTMLSatisfiesCrawler fails at compile time if the generic crawler drifts
// away from the interface the registry will hold it by.
func TestHTMLSatisfiesCrawler(t *testing.T) {
	var c Crawler = HTML{}
	if c.Info().ID != "html" {
		t.Errorf("Info().ID = %q, want \"html\"", c.Info().ID)
	}
}

// TestOrdinaryPageYieldsNothing is the case that made <img> collection a bug
// rather than a feature: a hoster page is ordinary HTML full of furniture. If
// the crawler returns anything here, the app treats the page as "crawled" and
// the link the user actually pasted is dropped in favour of logos and pixels.
func TestOrdinaryPageYieldsNothing(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = io.WriteString(w, `<html><body>
			<img src="/assets/logo.png">
			<img src="https://ads.example/px.gif?id=9">
			<img src="/assets/sprite.svg">
			<a href="/terms.html">Terms</a>
			<a href="/login.php">Sign in</a>
		</body></html>`)
	}))
	defer srv.Close()

	got, err := (HTML{}).Crawl(context.Background(), srv.URL+"/f/abc123")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("collected %v from a page with no downloads", got)
	}
}
