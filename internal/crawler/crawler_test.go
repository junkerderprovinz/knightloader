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
	"unicode/utf8"
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
	})
}

// TestCrawlIndexOfListing checks that an autoindex's parent and subdirectory
// links are not taken for files.
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
		{URL: srv.URL + "/pub/debian-12.iso", Name: "debian-12.iso", Title: "Index of /pub/"},
		{URL: srv.URL + "/pub/notes.txt", Name: "notes.txt", Title: "Index of /pub/"},
	})
}

func TestCrawlTitleIgnoresSVGAndCapsLength(t *testing.T) {
	t.Run("svg title is not the page title", func(t *testing.T) {
		page := `<html><head><title>Real page</title></head><body>
			<svg><title>Home icon</title></svg>
			<a href="/f.zip">f</a>
		</body></html>`
		srv := serve(t, "text/html", page)
		got := crawl(t, srv.URL+"/p", HTML{})
		wantResults(t, got, []Result{{URL: srv.URL + "/f.zip", Name: "f", Title: "Real page"}})
	})

	t.Run("no head title stays empty rather than borrowing the body's", func(t *testing.T) {
		page := `<html><body><svg><title>Home icon</title></svg><a href="/f.zip">f</a></body></html>`
		srv := serve(t, "text/html", page)
		got := crawl(t, srv.URL+"/p", HTML{})
		wantResults(t, got, []Result{{URL: srv.URL + "/f.zip", Name: "f"}})
	})

	t.Run("an overlong title is cut to the cap", func(t *testing.T) {
		// Multi-byte, so a cut by byte would leave invalid UTF-8.
		long := strings.Repeat("ä", maxTitleRunes+50)
		page := `<html><head><title>` + long + `</title></head><body><a href="/f.zip">f</a></body></html>`
		srv := serve(t, "text/html", page)
		got := crawl(t, srv.URL+"/p", HTML{})
		if n := len([]rune(got[0].Title)); n != maxTitleRunes {
			t.Errorf("title is %d runes, want it cut to %d", n, maxTitleRunes)
		}
		if !utf8.ValidString(got[0].Title) {
			t.Error("the cut title is not valid UTF-8; it was cut by byte, not by rune")
		}
	})
}

// TestCrawlNonHTMLResponseIsOneUnparsedResult serves HTML full of links under
// a non-HTML type; it must stay one download.
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

	if n := len(crawl(t, srv.URL+"/list", HTML{})); n != 50 {
		t.Errorf("MaxLinks 0 collected %d links, want all 50 (zero must mean default)", n)
	}
}

func TestCrawlRefusesDeclaredOversizePage(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.Header().Set("Content-Length", fmt.Sprint(int64(4)<<30))
		io.WriteString(w, "<html><body>")
	}))
	// The handler sends less than it declared, which the server logs.
	srv.Config.ErrorLog = log.New(io.Discard, "", 0)
	defer srv.Close()

	_, err := (HTML{}).Crawl(context.Background(), srv.URL+"/huge.html")
	if !errors.Is(err, ErrPageTooLarge) {
		t.Fatalf("Crawl of a 4 GB page = %v, want ErrPageTooLarge", err)
	}
}

// TestCrawlRefusesOversizeStreamWithoutDrainingIt checks that a chunked
// response with no declared length is cut off at the cap rather than read in
// full.
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
				return // the crawler hung up
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
	// Socket buffering lets the server get ahead of the reader, so only check
	// that the stream was abandoned early.
	if got := served.Load(); got >= chunk*chunks/2 {
		t.Errorf("server wrote %d bytes, want it cut off well before %d (body was drained, not refused)",
			got, chunk*chunks)
	}
}

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

// TestCrawlRejectsNonHTTPStatus keeps a 404 page's navigation from being
// collected as links.
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

func TestCrawlRejectsNonHTTPURL(t *testing.T) {
	if _, err := (HTML{}).Crawl(context.Background(), "ftp://example.com/pub/"); err == nil {
		t.Fatal("Crawl of an ftp URL succeeded, want an error")
	}
}

func TestHTMLSatisfiesCrawler(t *testing.T) {
	var c Crawler = HTML{}
	if c.Info().ID != "html" {
		t.Errorf("Info().ID = %q, want \"html\"", c.Info().ID)
	}
}

// TestOrdinaryPageYieldsNothing checks that a hoster page's images and
// navigation produce no results, so the pasted link is not replaced by logos
// and pixels.
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
