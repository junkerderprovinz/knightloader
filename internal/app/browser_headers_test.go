package app

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/crawler"
	"github.com/junkerderprovinz/knightloader/internal/resolver"
)

const browserCookie = "session=s3cr3t-value"

func TestBrowserHeadersAcceptOnlyCookieRefererAndUserAgent(t *testing.T) {
	set, err := BrowserHeaders("https://files.example/a.zip", map[string]string{
		"cookie":     browserCookie,
		"referer":    "https://files.example/",
		"user-agent": "Mozilla/5.0",
	})
	if err != nil {
		t.Fatal(err)
	}
	if set.Origin != "https://files.example:443" || len(set.Headers) != 3 {
		t.Fatalf("got %v, want three headers scoped to https://files.example:443", set)
	}

	for name, value := range map[string]string{
		"Authorization": "Bearer " + browserCookie,
		"Host":          "elsewhere.example",
		"Cookie":        browserCookie + "\r\nX-Injected: 1",
	} {
		_, err := BrowserHeaders("https://files.example/a.zip", map[string]string{name: value})
		if err == nil {
			t.Errorf("%s was accepted", name)
			continue
		}
		if strings.Contains(err.Error(), browserCookie) {
			t.Errorf("the refusal of %s quotes the value: %v", name, err)
		}
	}
}

func TestBrowserHeadersStayOffOtherOrigins(t *testing.T) {
	var seenElsewhere http.Header
	elsewhere := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenElsewhere = r.Header.Clone()
		_, _ = w.Write([]byte("x"))
	}))
	t.Cleanup(elsewhere.Close)
	var seenHome http.Header
	home := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenHome = r.Header.Clone()
		if r.URL.Path == "/moved.zip" {
			http.Redirect(w, r, elsewhere.URL+"/cdn.zip", http.StatusFound)
			return
		}
		_, _ = w.Write([]byte("x"))
	}))
	t.Cleanup(home.Close)

	a := newQueueApp(t)
	resolve := func(link string) resolver.Result {
		t.Helper()
		set, err := BrowserHeaders(link, map[string]string{"Cookie": browserCookie})
		if err != nil {
			t.Fatal(err)
		}
		task := &core.Task{ID: "handed-over", URL: link, Status: core.StatusQueued, Enabled: true}
		a.mu.Lock()
		a.tasks[task.ID] = task
		a.mu.Unlock()
		a.keepBrowserHeaders(set, []string{task.ID})
		a.mu.Lock()
		defer a.mu.Unlock()
		got, err := a.resolveLocked(resolver.Direct{}, task)
		if err != nil {
			t.Fatal(err)
		}
		return got
	}

	got := resolve(home.URL + "/file.zip")
	if seenHome.Get("Cookie") != browserCookie {
		t.Error("the preflight went out without the browser's cookie")
	}
	if got.Headers["Cookie"] != browserCookie {
		t.Error("the download is not handed the browser's cookie for its own origin")
	}

	got = resolve(home.URL + "/moved.zip")
	if seenElsewhere == nil {
		t.Fatal("the redirect was not followed")
	}
	if seenElsewhere.Get("Cookie") != "" {
		t.Error("the cookie followed a redirect to another origin")
	}
	if got.DirectURL != elsewhere.URL+"/cdn.zip" || len(got.Headers) != 0 {
		t.Errorf("download target %s with %d headers, want the other origin's address with none", got.DirectURL, len(got.Headers))
	}
}

func TestBrowserHeadersGoWhenTheDownloadIsDone(t *testing.T) {
	a := newQueueApp(t)
	set, err := BrowserHeaders("https://files.example/a.zip", map[string]string{"Cookie": browserCookie})
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"finishes", "removed"} {
		a.mu.Lock()
		a.tasks[id] = &core.Task{ID: id, URL: "https://files.example/a.zip", Status: core.StatusRunning, Enabled: true}
		a.mu.Unlock()
	}
	a.keepBrowserHeaders(set, []string{"finishes", "removed"})

	a.onUpdate("finishes", core.Update{Status: core.StatusDone})
	if a.browserHeadersFor("finishes", "https://files.example/a.zip") != nil {
		t.Error("the cookie outlives the finished download")
	}
	a.Remove("removed", false)
	if a.browserHeadersFor("removed", "https://files.example/a.zip") != nil {
		t.Error("the cookie outlives the removed task")
	}
}

func TestAHandedOverLinkIsStagedAsItIs(t *testing.T) {
	a := newCrawlApp(t, true)
	fc := &fakeCrawler{yield: []crawler.Result{{URL: "https://files.example/login-help.pdf"}}}
	a.Crawler = fc
	const link = "https://files.example/download?id=7"
	set, err := BrowserHeaders(link, map[string]string{"Cookie": browserCookie})
	if err != nil {
		t.Fatal(err)
	}

	created, err := a.AddLinksWithOptions([]string{link}, "", OriginCnL, LinkBatchOptions{
		Headers: set, Source: "https://files.example/thread/7", KeepCollected: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(fc.seen) != 0 {
		t.Errorf("the link was crawled without the browser's session: %v", fc.seen)
	}
	if len(created) != 1 || created[0].URL != link {
		t.Fatalf("staged %v, want the link itself", created)
	}
	if created[0].Source != "https://files.example/thread/7" {
		t.Errorf("source = %q, want the page it came from", created[0].Source)
	}
	if a.browserHeadersFor(created[0].ID, link)["Cookie"] != browserCookie {
		t.Error("the staged task does not keep the browser's cookie")
	}
}

func TestTheCollectorProbeCarriesTheBrowsersCookie(t *testing.T) {
	a := newCrawlApp(t, false)
	var mu sync.Mutex
	var probed http.Header
	a.Probe = probeFunc(func(req *http.Request) (*http.Response, error) {
		mu.Lock()
		probed = req.Header.Clone()
		mu.Unlock()
		return probeAnswer(req, http.StatusOK), nil
	})
	const link = "https://files.example/members/film.mkv"
	set, err := BrowserHeaders(link, map[string]string{"Cookie": browserCookie})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.AddLinksWithOptions([]string{link}, "", OriginCnL, LinkBatchOptions{Headers: set, KeepCollected: true}); err != nil {
		t.Fatal(err)
	}

	// The probe runs on its own goroutine.
	deadline := time.Now().Add(2 * time.Second)
	for {
		mu.Lock()
		got := probed.Get("Cookie")
		mu.Unlock()
		if got == browserCookie {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the collector's probe went out without the browser's cookie")
		}
		time.Sleep(10 * time.Millisecond)
	}
}
