package jd

import (
	"bytes"
	"fmt"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/core"
)

// playerPage is what JD made of https://playmate.to/embed/FrBxuaYCIKvsh, a
// page whose player it has no plugin for.
var playerPage = []string{
	"crypto-js.min.js", "jwplayer.js", "jwplayer.css", "FrBxuaYCIKvsh.jpg",
	"on.js", "s.js", "147054", "provider.hlsjs.js", "jquery.min.js",
	"sweetalert.min.js", "logo.svg", "Inter.woff2",
}

func TestOnlyPagePartsKnowsAPageFromItsDownloads(t *testing.T) {
	for _, c := range []struct {
		names []string
		want  bool
	}{
		{playerPage, true},
		{[]string{"index.html", "app.js"}, true},
		{[]string{"style.css", "banner.png", "next"}, true},
		{[]string{"index.html", "about.html", "Inter.woff2", "logo.svg"}, true},
		{[]string{"Inter.woff2", "Inter-Bold.woff2", "hero.jpg"}, true},
		// A hoster plugin's one file is the download, whatever its type.
		{[]string{"library.js"}, false},
		{[]string{"cover.jpg"}, false},
		// Pictures alone are an album.
		{[]string{"01.jpg", "02.jpg", "03.png"}, false},
		// One real file among the scripts is what the page was pasted for.
		{append([]string{"movie.mp4"}, playerPage...), false},
		{[]string{"app.js", "setup.part1.rar"}, false},
		{[]string{"about", "contact"}, false},
		{nil, false},
	} {
		if got := onlyPageParts(c.names); got != c.want {
			t.Errorf("onlyPageParts(%q) = %v, want %v", c.names, got, c.want)
		}
	}
}

func TestOnlyALinkJDCanOnlyReadAsAPageIsWatched(t *testing.T) {
	t.Cleanup(func() { SetKnownHosts(nil) })
	SetKnownHosts([]string{"rapidgator.net"})
	for link, want := range map[string]bool{
		"https://playmate.to/embed/FrBxuaYCIKvsh": true,
		"https://blog.example/2026/09/a-post":     true,
		"https://blog.example/watch.php?v=1":      true,
		"https://files.example/cover.jpg":         false,
		"https://rapidgator.net/file/abc":         false,
		"not a link":                              false,
	} {
		if got := readsPage(link); got != want {
			t.Errorf("readsPage(%q) = %v, want %v", link, got, want)
		}
	}
}

// fakeJDPage is a JD that crawled a page into the task's grabber package, or,
// with confirmed set, straight into its download list, where the links named
// in finished are done at 100 bytes each in the folder saveTo.
type fakeJDPage struct {
	names     []string
	confirmed bool
	saveTo    string
	finished  map[string]bool

	mu            sync.Mutex
	added         bool
	grabberGone   bool
	downloadsGone bool
}

func (f *fakeJDPage) links() string {
	var out []string
	for i, n := range f.names {
		loaded := 0
		if f.finished[n] {
			loaded = 100
		}
		out = append(out, fmt.Sprintf(`{"uuid":%d,"packageUUID":7,"name":%q,"availability":"ONLINE","bytesTotal":100,"bytesLoaded":%d,"finished":%t}`,
			100+i, n, loaded, f.finished[n]))
	}
	return `{"data":[` + strings.Join(out, ",") + `]}`
}

func (f *fakeJDPage) handler() http.Handler {
	ours := fmt.Sprintf(`{"data":[{"uuid":7,"name":"KL-t1","saveTo":%q}]}`, f.saveTo)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		switch r.URL.Path {
		case "/linkgrabberv2/addLinks":
			f.added = true
			_, _ = w.Write([]byte(`{"data":{"id":1}}`))
		case "/linkgrabberv2/queryPackages":
			if !f.added || f.confirmed || f.grabberGone {
				_, _ = w.Write([]byte(`{"data":[]}`))
				return
			}
			_, _ = w.Write([]byte(ours))
		case "/linkgrabberv2/queryLinks":
			_, _ = w.Write([]byte(f.links()))
		case "/linkgrabberv2/isCollecting":
			_, _ = w.Write([]byte(`{"data":false}`))
		case "/linkgrabberv2/removeLinks":
			f.grabberGone = f.added
			_, _ = w.Write([]byte(`{"data":true}`))
		case "/downloadsV2/queryPackages":
			if !f.added || !f.confirmed || f.downloadsGone {
				_, _ = w.Write([]byte(`{"data":[]}`))
				return
			}
			_, _ = w.Write([]byte(ours))
		case "/downloadsV2/queryLinks":
			_, _ = w.Write([]byte(f.links()))
		case "/downloadsV2/removeLinks":
			f.downloadsGone = f.added
			_, _ = w.Write([]byte(`{"data":true}`))
		default:
			_, _ = w.Write([]byte(`{"data":null}`))
		}
	})
}

// downloadPage hands link to a backend over f and returns the first update it
// reports within wait, or nil.
func downloadPage(t *testing.T, f *fakeJDPage, link string, wait time.Duration) *core.Update {
	t.Helper()
	srv := httptest.NewServer(f.handler())
	t.Cleanup(srv.Close)
	got := make(chan core.Update, 8)
	b := NewBackend(srv.URL, func(_ string, u core.Update) { got <- u })
	b.Download("t1", link, nil, 0)
	t.Cleanup(func() { b.Remove("t1", false) })
	select {
	case u := <-got:
		return &u
	case <-time.After(wait):
		return nil
	}
}

func TestAPageOfNothingButItsOwnPartsFailsBeforeJDFetchesIt(t *testing.T) {
	f := &fakeJDPage{names: playerPage}
	u := downloadPage(t, f, "https://playmate.to/embed/FrBxuaYCIKvsh", 10*time.Second)
	if u == nil {
		t.Fatal("the page's scripts and images were left for JD to download")
	}
	if u.Status != core.StatusError || u.Reason != core.ReasonUnsupportedPlayer {
		t.Fatalf("update %+v, want a failure naming the unsupported player", *u)
	}
	if u.Unsupported {
		t.Error("the failure was handed on, and the HTTP fallback would save the page itself")
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if !f.grabberGone {
		t.Error("the crawl was left in JD's grabber, where autoconfirm would start it")
	}
}

func TestAPageJDHasAlreadyConfirmedIsTakenOffItsList(t *testing.T) {
	f := &fakeJDPage{names: playerPage, confirmed: true}
	u := downloadPage(t, f, "https://playmate.to/embed/FrBxuaYCIKvsh", 5*time.Second)
	if u == nil || u.Status != core.StatusError || u.Reason != core.ReasonUnsupportedPlayer {
		t.Fatalf("update %+v, want a failure naming the unsupported player", u)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if !f.downloadsGone {
		t.Error("the page's parts stayed in JD's download list")
	}
}

// Only what JD finished of the page goes, and only where it is the file JD
// wrote: a same-named file of another size, the names of parts still loading
// and the user's own files stay.
func TestThePagePartsJDAlreadyFetchedAreDeleted(t *testing.T) {
	dir := t.TempDir()
	put := func(name string, size int) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, bytes.Repeat([]byte("x"), size), 0o644); err != nil {
			t.Fatal(err)
		}
		return p
	}
	fetched := []string{put("crypto-js.min.js", 100), put("jwplayer.css", 100)}
	kept := []string{put("logo.svg", 7), put("jwplayer.js", 40), put("on.js", 0), put("holiday.mp4", 100)}
	f := &fakeJDPage{names: playerPage, confirmed: true, saveTo: dir, finished: map[string]bool{
		"crypto-js.min.js": true, "jwplayer.css": true, "logo.svg": true,
	}}
	u := downloadPage(t, f, "https://playmate.to/embed/FrBxuaYCIKvsh", 5*time.Second)
	if u == nil || u.Reason != core.ReasonUnsupportedPlayer {
		t.Fatalf("update %+v, want a failure naming the unsupported player", u)
	}
	for _, p := range fetched {
		if _, err := os.Stat(p); err == nil {
			t.Errorf("%s, which JD finished, is still there", filepath.Base(p))
		}
	}
	for _, p := range kept {
		if _, err := os.Stat(p); err != nil {
			t.Errorf("%s was deleted: %v", filepath.Base(p), err)
		}
	}
}

// A JD on another machine reports a folder this process does not have, and
// the log says what was left there.
func TestPagePartsOutOfReachAreNamedInTheLog(t *testing.T) {
	var buf bytes.Buffer
	out := log.Writer()
	log.SetOutput(&buf)
	t.Cleanup(func() { log.SetOutput(out) })

	dir := filepath.Join(t.TempDir(), "elsewhere")
	dropFinished(dir, []DownloadLink{{Name: "crypto-js.min.js", Finished: true, BytesLoaded: 100}})
	if !strings.Contains(buf.String(), dir) {
		t.Errorf("the log says %q, want the folder the file stays in", buf.String())
	}
}

// A host JD has a plugin for answers with the file the link stands for, and a
// page that also links a real file is a page worth crawling.
func TestACrawlWithADownloadInItIsLeftToJD(t *testing.T) {
	t.Cleanup(func() { SetKnownHosts(nil) })
	SetKnownHosts([]string{"hoster.example"})
	for name, c := range map[string]struct {
		link  string
		names []string
	}{
		"a known hoster":      {"https://hoster.example/file/abc", []string{"site.css", "app.js"}},
		"a page with a video": {"https://blog.example/post", append([]string{"clip.mp4"}, playerPage...)},
	} {
		t.Run(name, func(t *testing.T) {
			if u := downloadPage(t, &fakeJDPage{names: c.names}, c.link, 5*time.Second); u != nil {
				t.Fatalf("update %+v for a crawl JD should go on with", *u)
			}
		})
	}
}

// A page JD crawls to nothing fails as an unsupported player once the crawl
// has stayed empty for a while, instead of waiting out appearLimit.
func TestAPageJDFindsNothingOnFailsWithoutWaitingOutTheList(t *testing.T) {
	f := &fakeJDPage{}
	u := downloadPage(t, f, "https://example.org/", 25*time.Second)
	if u == nil || u.Status != core.StatusError || u.Reason != core.ReasonUnsupportedPlayer {
		t.Fatalf("update %+v, want a failure naming the unsupported player", u)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if !f.grabberGone {
		t.Error("the empty crawl was left in JD's grabber")
	}
}

// A link to a file is not a page, so an empty crawl is JD still working on
// it and nothing fails early.
func TestAnEmptyCrawlOfAFileLinkIsLeftToJD(t *testing.T) {
	f := &fakeJDPage{}
	if u := downloadPage(t, f, "https://host.example/film.mkv", 20*time.Second); u != nil {
		t.Fatalf("update %+v for a file link JD had not crawled yet", *u)
	}
}
