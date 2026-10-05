package app

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/crawler"
	"github.com/junkerderprovinz/knightloader/internal/engine"
	"github.com/junkerderprovinz/knightloader/internal/httpx"
	"github.com/junkerderprovinz/knightloader/internal/resolver"
	"github.com/junkerderprovinz/knightloader/internal/resolver/ytdlp"
	"github.com/junkerderprovinz/knightloader/internal/settings"
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

func TestBrowserHeadersRefuseANameThatIsNoToken(t *testing.T) {
	for _, name := range []string{"Authorization:", "Cookie:", "X Y", "Referer\x00"} {
		if _, err := BrowserHeaders("https://files.example/a.zip", map[string]string{name: "Bearer x"}); err == nil {
			t.Errorf("%q was dropped instead of refused", name)
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
		_, later, err := a.resolveLocked(resolver.Direct{}, task)
		a.active[task.ID] = true
		seq := a.beginPreflightLocked(task.ID)
		a.mu.Unlock()
		if err != nil || later == nil {
			t.Fatalf("no preflight for a link with a browser's headers: %v", err)
		}
		job := engine.Job{TaskID: task.ID, URL: link}
		if !a.preflight(&job, later, seq) {
			t.Fatal("the start was dropped after its preflight")
		}
		return resolver.Result{DirectURL: job.URL, Headers: job.Headers}
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

func TestASlowPreflightDoesNotHoldTheAppLock(t *testing.T) {
	release := make(chan struct{})
	probed := make(chan struct{}, 1)
	slow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case probed <- struct{}{}:
		default:
		}
		select {
		case <-release:
		case <-time.After(3 * time.Second):
		}
		_, _ = w.Write([]byte("x"))
	}))
	t.Cleanup(slow.Close)
	t.Cleanup(func() { close(release) })

	a := newQueueApp(t)
	link := slow.URL + "/members/file.zip"
	set, err := BrowserHeaders(link, map[string]string{"Cookie": browserCookie})
	if err != nil {
		t.Fatal(err)
	}
	task := &core.Task{ID: "slow", URL: link, Status: core.StatusQueued, Enabled: true}
	a.mu.Lock()
	a.tasks[task.ID] = task
	a.mu.Unlock()
	a.keepBrowserHeaders(set, []string{task.ID})

	began := time.Now()
	a.mu.Lock()
	a.queue = append(a.queue, task.ID)
	a.dispatchLocked()
	a.mu.Unlock()
	if d := time.Since(began); d > time.Second {
		t.Fatalf("starting the task held the app lock for %v while its server was slow", d)
	}
	select {
	case <-probed:
	case <-time.After(2 * time.Second):
		t.Fatal("the preflight never reached the server")
	}
	began = time.Now()
	_ = a.Tasks()
	if d := time.Since(began); d > time.Second {
		t.Errorf("listing the tasks waited %v for the preflight", d)
	}
}

func TestATaskPausedDuringItsPreflightDoesNotStart(t *testing.T) {
	release := make(chan struct{})
	probed := make(chan struct{}, 1)
	var mu sync.Mutex
	fetched := 0
	slow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		fetched++
		mu.Unlock()
		select {
		case probed <- struct{}{}:
		default:
		}
		<-release
		_, _ = w.Write([]byte("x"))
	}))
	t.Cleanup(slow.Close)

	a := newQueueApp(t)
	link := slow.URL + "/members/file.zip"
	set, err := BrowserHeaders(link, map[string]string{"Cookie": browserCookie})
	if err != nil {
		t.Fatal(err)
	}
	task := &core.Task{ID: "paused", URL: link, Status: core.StatusQueued, Enabled: true}
	a.mu.Lock()
	a.tasks[task.ID] = task
	a.mu.Unlock()
	a.keepBrowserHeaders(set, []string{task.ID})
	a.mu.Lock()
	a.queue = append(a.queue, task.ID)
	a.dispatchLocked()
	a.mu.Unlock()
	<-probed

	a.Pause(task.ID)
	close(release)
	a.mu.Lock()
	a.awaitHandoversLocked([]string{task.ID})
	a.mu.Unlock()
	time.Sleep(100 * time.Millisecond)
	mu.Lock()
	defer mu.Unlock()
	if fetched != 1 {
		t.Errorf("the server was asked %d times, want the preflight alone", fetched)
	}
	if got := snapshot(t, a, task.ID).Status; got != core.StatusPaused {
		t.Errorf("status %s, want the task still paused", got)
	}
}

func TestATaskResumedAfterItsPreflightWasCutShortStarts(t *testing.T) {
	// The second preflight is held as well, so the test ends on a pause and
	// no download runs past it.
	gates := make(chan chan struct{}, 2)
	asked := make(chan chan struct{}, 2)
	for range 2 {
		gates <- make(chan struct{})
	}
	slow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gate := <-gates
		asked <- gate
		<-gate
		_, _ = w.Write([]byte("x"))
	}))
	t.Cleanup(slow.Close)

	a := newQueueApp(t)
	link := slow.URL + "/members/file.zip"
	set, err := BrowserHeaders(link, map[string]string{"Cookie": browserCookie})
	if err != nil {
		t.Fatal(err)
	}
	task := &core.Task{ID: "resumed", URL: link, Status: core.StatusQueued, Enabled: true}
	a.mu.Lock()
	a.tasks[task.ID] = task
	a.mu.Unlock()
	a.keepBrowserHeaders(set, []string{task.ID})
	a.mu.Lock()
	a.queue = append(a.queue, task.ID)
	a.dispatchLocked()
	a.mu.Unlock()
	pauseDuring := func(gate chan struct{}) {
		a.Pause(task.ID)
		close(gate)
		a.mu.Lock()
		a.awaitHandoversLocked([]string{task.ID})
		a.mu.Unlock()
	}
	pauseDuring(<-asked)

	a.Resume(task.ID)
	select {
	case gate := <-asked:
		pauseDuring(gate)
	case <-time.After(3 * time.Second):
		t.Fatalf("the resumed task never reached its server again, status %s", snapshot(t, a, task.ID).Status)
	}
}

func TestTheCollectorProbeKeepsTheBrowsersHeadersOffAnotherOrigin(t *testing.T) {
	var seenElsewhere http.Header
	elsewhere := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenElsewhere = r.Header.Clone()
	}))
	t.Cleanup(elsewhere.Close)
	home := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, elsewhere.URL+"/cdn.zip", http.StatusFound)
	}))
	t.Cleanup(home.Close)

	a := newQueueApp(t)
	a.Probe = httpx.New(httpx.Options{Timeout: probeTimeout})
	link := home.URL + "/get?token=LINKTOKEN"
	set, err := BrowserHeaders(link, map[string]string{
		"Cookie":     browserCookie,
		"Referer":    home.URL + "/thread?token=REFTOKEN",
		"User-Agent": "Mozilla/5.0 (Browser)",
	})
	if err != nil {
		t.Fatal(err)
	}
	a.mu.Lock()
	a.tasks["handed-over"] = &core.Task{ID: "handed-over", URL: link, Status: core.StatusCollected, Enabled: true}
	a.mu.Unlock()
	a.keepBrowserHeaders(set, []string{"handed-over"})

	a.analyze("handed-over", link)
	if seenElsewhere == nil {
		t.Fatal("the redirect was not followed")
	}
	for _, name := range []string{"Cookie", "Referer"} {
		if v := seenElsewhere.Get(name); v != "" {
			t.Errorf("%s followed the redirect to another origin: %q", name, v)
		}
	}
	if ua := seenElsewhere.Get("User-Agent"); ua == "Mozilla/5.0 (Browser)" {
		t.Error("the browser's user agent followed the redirect to another origin")
	}
}

// sentProber is a yt-dlp backend that reports the headers each title probe
// was given.
type sentProber struct{ sent chan map[string]string }

func (sentProber) Download(string, string, map[string]string, int) {}
func (sentProber) Pause(string)                                    {}
func (sentProber) Resume(string)                                   {}
func (sentProber) Remove(string, bool)                             {}

func (p sentProber) ProbeTitle(_ context.Context, _ string, sent map[string]string) (ytdlp.ProbeResult, error) {
	p.sent <- sent
	return ytdlp.ProbeResult{Title: "Live"}, nil
}

func TestTheTitleProbeOfAHandedOverStreamCarriesTheBrowsersCookie(t *testing.T) {
	a, _ := newRuleApp(t, func(*settings.Settings, string) {})
	p := sentProber{sent: make(chan map[string]string, 8)}
	wireYtdlp(a, p)
	const link = "https://media.example/live/index.m3u8"
	set, err := BrowserHeaders(link, map[string]string{"Cookie": browserCookie, "Referer": "https://site.example/watch/7"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.AddLinksWithOptions([]string{link}, "", OriginCnL, LinkBatchOptions{Headers: set, KeepCollected: true}); err != nil {
		t.Fatal(err)
	}
	select {
	case sent := <-p.sent:
		if sent["Cookie"] != browserCookie || sent["Referer"] != "https://site.example/watch/7" {
			t.Errorf("the probe went out with %v, want the browser's cookie and Referer", sent)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("the link was never probed")
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

func TestAFileTheBrowserWasDownloadingStaysOffYtdlp(t *testing.T) {
	a := newCrawlApp(t, false)
	a.Probe = probeFunc(func(req *http.Request) (*http.Response, error) { return probeAnswer(req, http.StatusOK), nil })
	fake, _ := newFakeYtdlp()
	wireYtdlp(a, fake)
	const link = "https://files.example/download?id=7"
	set, err := BrowserHeaders(link, map[string]string{"Cookie": browserCookie})
	if err != nil {
		t.Fatal(err)
	}

	created, err := a.AddLinksWithOptions([]string{link}, "", OriginCnL, LinkBatchOptions{
		Headers: set, File: true, FileName: "report.pdf", KeepCollected: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(created) != 1 {
		t.Fatalf("staged %d rows, want the one file", len(created))
	}
	if created[0].Resolver != "http" || created[0].Name != "report.pdf" {
		t.Errorf("staged on %q as %q, want the plain download under the browser's name", created[0].Resolver, created[0].Name)
	}
	a.mu.Lock()
	res := a.resolverForTaskLocked(a.tasks[created[0].ID])
	a.mu.Unlock()
	if res == nil || res.Info().ID != "http" {
		t.Errorf("the start would go to %v, want the plain download that carries the browser's headers", res)
	}
}

func TestABrowsersFileNameMustBeOneFileName(t *testing.T) {
	a, _ := newRuleApp(t, func(*settings.Settings, string) {})
	for _, opts := range []LinkBatchOptions{
		{File: true, FileName: "../escape.pdf"},
		{File: true, FileName: `..\escape.pdf`},
		{File: true, FileName: ".."},
		{FileName: "report.pdf"},
	} {
		opts.KeepCollected = true
		link := "https://files.example/nocd?name=" + url.QueryEscape(opts.FileName)
		if created, err := a.AddLinksWithOptions([]string{link}, "", OriginCnL, opts); err == nil {
			t.Errorf("file %v with name %q was staged as %v", opts.File, opts.FileName, created)
		}
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
