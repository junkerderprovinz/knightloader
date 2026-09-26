package jd

import (
	"fmt"
	"net/http"
	"net/http/httptest"
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
// with confirmed set, straight into its download list.
type fakeJDPage struct {
	names     []string
	confirmed bool

	mu            sync.Mutex
	added         bool
	grabberGone   bool
	downloadsGone bool
}

func (f *fakeJDPage) links() string {
	var out []string
	for i, n := range f.names {
		out = append(out, fmt.Sprintf(`{"uuid":%d,"packageUUID":7,"name":%q,"availability":"ONLINE","bytesTotal":100}`, 100+i, n))
	}
	return `{"data":[` + strings.Join(out, ",") + `]}`
}

func (f *fakeJDPage) handler() http.Handler {
	const ours = `{"data":[{"uuid":7,"name":"KL-t1"}]}`
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
