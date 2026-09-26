package ytdlp

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/junkerderprovinz/knightloader/internal/core"
)

// The embed link and the answer playmate.to gave for it, with the token cut.
const (
	playmateLink   = "http://playmate.to/embed/FrBxuaYCIKvsh"
	playmateCode   = "FrBxuaYCIKvsh"
	playmateMaster = "https://frv1.plauymito.live/hls/token/master.txt"
	playmateAnswer = `{"ax":"","cx":"FrBxuaYCIKvsh","ix":"https://frv1.plauymito.live/thumbnail/FrBxuaYCIKvsh.jpg",` +
		`"kx":null,"lx":"English","sx":"` + playmateMaster + `","tx":""}`
)

// fakePlaymate answers like playmate.to: the embed page, and the player API
// that refuses a client that does not introduce itself as a browser.
type fakePlaymate struct {
	title string
	// pageStatus, apiStatus and apiBody replace the real answers when set.
	pageStatus int
	apiStatus  int
	apiBody    string

	mu       sync.Mutex
	requests int
	sent     map[string]string
	referer  string
}

func (f *fakePlaymate) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.requests++
	switch {
	case r.Method == http.MethodGet && r.URL.Path == "/embed/"+playmateCode:
		if f.pageStatus != 0 {
			w.WriteHeader(f.pageStatus)
			return
		}
		io.WriteString(w, "<!DOCTYPE html><html><head><meta charset=\"utf-8\">\n    <title>"+f.title+"</title>\n"+
			"<script src='/assets/jw8/jwplayer.js'></script></head><body></body></html>")
	case r.Method == http.MethodPost && r.URL.Path == "/api/s":
		if !strings.HasPrefix(r.UserAgent(), "Mozilla/") || r.Header.Get("Content-Type") != "application/json" {
			w.WriteHeader(http.StatusForbidden)
			io.WriteString(w, `{"error":"forbidden"}`)
			return
		}
		_ = json.NewDecoder(r.Body).Decode(&f.sent)
		f.referer = r.Referer()
		switch {
		case f.apiStatus != 0:
			w.WriteHeader(f.apiStatus)
			io.WriteString(w, f.apiBody)
		case f.apiBody != "":
			io.WriteString(w, f.apiBody)
		default:
			w.Header().Set("Content-Type", "application/json")
			io.WriteString(w, playmateAnswer)
		}
	default:
		http.NotFound(w, r)
	}
}

// playmateSite starts f and returns a client that reaches it for every host,
// playmate.to included.
func playmateSite(t *testing.T, f *fakePlaymate) *http.Client {
	t.Helper()
	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)
	addr := srv.Listener.Addr().String()
	return &http.Client{Transport: &http.Transport{
		DialContext: func(ctx context.Context, network, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, network, addr)
		},
	}}
}

func TestAPlaymateLinkBecomesItsStream(t *testing.T) {
	f := &fakePlaymate{title: "1000267652"}
	s, ok, err := unwrap(context.Background(), playmateSite(t, f), playmateLink)
	if !ok || err != nil {
		t.Fatalf("unwrap = ok %v, err %v; want the stream", ok, err)
	}
	if s.url != playmateMaster {
		t.Errorf("stream = %q, want the master playlist %q", s.url, playmateMaster)
	}
	if s.title != "1000267652" {
		t.Errorf("title = %q, want the page's title", s.title)
	}
	if got := s.template(); got != "1000267652.%(ext)s" {
		t.Errorf("template = %q", got)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.sent["c"] != playmateCode || f.sent["d"] != "desktop" {
		t.Errorf("the API was sent %v, want the code and the device the player sends", f.sent)
	}
	if f.referer != playmateLink {
		t.Errorf("the API was sent the referer %q, want the embed page", f.referer)
	}
}

func TestATitleThatNamesNothingGivesWayToTheCode(t *testing.T) {
	for _, title := range []string{"", "   ", "Playmate", "playmate.to", playmateCode, "..."} {
		s, _, err := unwrap(context.Background(), playmateSite(t, &fakePlaymate{title: title}), playmateLink)
		if err != nil {
			t.Fatal(err)
		}
		if s.title != playmateCode {
			t.Errorf("page title %q gave the name %q, want the code", title, s.title)
		}
	}
}

func TestATitleIsMadeFitForAFileName(t *testing.T) {
	for title, want := range map[string]string{
		"Part 1: Intro / Outro?":  "Part 1 Intro Outro",
		"Tom &amp; Jerry":         "Tom & Jerry",
		"Say \"when\".":           "Say when",
		strings.Repeat("ab", 80):  strings.Repeat("ab", 50),
		"100% real\tfootage\n\n ": "100% real footage",
	} {
		s, _, err := unwrap(context.Background(), playmateSite(t, &fakePlaymate{title: title}), playmateLink)
		if err != nil {
			t.Fatal(err)
		}
		if s.title != want {
			t.Errorf("page title %q gave the name %q, want %q", title, s.title, want)
		}
	}
	s := embedded{title: "100% real"}
	if got := s.template(); got != "100%% real.%(ext)s" {
		t.Errorf("template = %q, want the percent sign escaped for yt-dlp", got)
	}
}

func TestAVideoPlaymateNoLongerHasIsGone(t *testing.T) {
	for name, f := range map[string]*fakePlaymate{
		"the page answers 404": {pageStatus: http.StatusNotFound},
		"the API says so":      {apiStatus: http.StatusNotFound, apiBody: `{"error":"Video not found"}`},
	} {
		_, ok, err := unwrap(context.Background(), playmateSite(t, f), playmateLink)
		if !ok || !errors.Is(err, errEmbedGone) {
			t.Errorf("%s: unwrap = ok %v, err %v; want the video reported gone", name, ok, err)
		}
		if err != nil && !strings.HasPrefix(err.Error(), "playmate.to: ") {
			t.Errorf("%s: %q does not name the site", name, err)
		}
	}
}

// Only the site saying so makes a video gone. Anything else it answers may be
// the site having changed, and a link reported gone gets deleted.
func TestAnUnexpectedAnswerIsAClearFailureButNotAGoneVideo(t *testing.T) {
	for name, f := range map[string]*fakePlaymate{
		"no stream in the answer":   {apiBody: `{"cx":"FrBxuaYCIKvsh","sx":""}`},
		"a stream that is no URL":   {apiBody: `{"sx":"/hls/master.txt"}`},
		"not JSON at all":           {apiBody: `<html>maintenance</html>`},
		"the API moved":             {apiStatus: http.StatusNotFound, apiBody: `<html>Not Found</html>`},
		"the API refuses":           {apiStatus: http.StatusForbidden, apiBody: `{"error":"forbidden"}`},
		"the page is not available": {pageStatus: http.StatusServiceUnavailable},
	} {
		_, ok, err := unwrap(context.Background(), playmateSite(t, f), playmateLink)
		if !ok || err == nil {
			t.Errorf("%s: unwrap = ok %v, err %v; want a failure", name, ok, err)
			continue
		}
		if errors.Is(err, errEmbedGone) {
			t.Errorf("%s: reported the video gone: %v", name, err)
		}
		// The app's classifier would file a failure carrying these words, a
		// 403 as a missing login among them, under a cause it is not.
		for _, word := range []string{"forbidden", "http", "status"} {
			if strings.Contains(strings.ToLower(err.Error()), word) {
				t.Errorf("%s: %q carries %q", name, err, word)
			}
		}
	}
}

func TestOtherLinksGoToYtdlpAsTheyAre(t *testing.T) {
	f := &fakePlaymate{title: "x"}
	c := playmateSite(t, f)
	for _, link := range []string{
		"https://www.youtube.com/watch?v=dQw4w9WgXcQ",
		"http://playmate.to/",
		"http://playmate.to/embed/",
		"http://playmate.to/v/FrBxuaYCIKvsh",
		"http://notplaymate.to/embed/FrBxuaYCIKvsh",
	} {
		if _, ok, err := unwrap(context.Background(), c, link); ok || err != nil {
			t.Errorf("unwrap(%q) = ok %v, err %v; want the link left alone", link, ok, err)
		}
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.requests != 0 {
		t.Errorf("%d requests went out for links that are no embed links", f.requests)
	}
}

// runEmbed drives one Backend.run for the playmate link against f, with the
// helper recording the argv it was started with.
func runEmbed(t *testing.T, f *fakePlaymate) (string, *recorder) {
	t.Helper()
	t.Setenv(runHelperEnv, "cookies:full")
	dir := t.TempDir()
	rec := &recorder{}
	b := NewBackend(os.Args[0], dir, rec.add)
	b.Client = playmateSite(t, f)
	b.run("task-1", playmateLink)
	return dir, rec
}

func TestRunFetchesTheStreamUnderThePageTitle(t *testing.T) {
	dir, rec := runEmbed(t, &fakePlaymate{title: "1000267652"})
	if got := rec.last(); got.Status == core.StatusError {
		t.Fatalf("the run failed: %+v", got)
	}
	raw, err := os.ReadFile(filepath.Join(dir, "argv.txt"))
	if err != nil {
		t.Fatalf("yt-dlp was not started: %v", err)
	}
	argv := strings.Split(strings.TrimSpace(string(raw)), "\n")
	if last := argv[len(argv)-1]; last != playmateMaster {
		t.Errorf("yt-dlp was given %q, want the stream %q", last, playmateMaster)
	}
	want := map[string]bool{
		"-o":                          false,
		"--add-header":                false,
		"Referer:http://playmate.to/": false,
	}
	for i, a := range argv {
		if a == "-o" && i+1 < len(argv) && strings.HasSuffix(argv[i+1], string(filepath.Separator)+"1000267652.%(ext)s") {
			want["-o"] = true
		}
		if _, ok := want[a]; ok && a != "-o" {
			want[a] = true
		}
	}
	for a, seen := range want {
		if !seen {
			t.Errorf("argv lacks %s as it should be: %q", a, argv)
		}
	}
}

func TestRunReportsAGoneVideoWithoutStartingYtdlp(t *testing.T) {
	dir, rec := runEmbed(t, &fakePlaymate{pageStatus: http.StatusNotFound})
	got := rec.last()
	if got.Status != core.StatusError || got.Reason != core.ReasonGone {
		t.Fatalf("last update = %+v, want a failure saying the video is gone", got)
	}
	if !strings.HasPrefix(got.Err, "playmate.to: ") {
		t.Errorf("Err = %q, want it to name the site", got.Err)
	}
	if got.Unsupported {
		t.Error("the failure was handed to the next backend, which would save the page's scripts")
	}
	if _, err := os.Stat(filepath.Join(dir, "argv.txt")); err == nil {
		t.Error("yt-dlp was started for a video that is gone")
	}
}

func TestTheProbeNamesTheStreamAfterThePage(t *testing.T) {
	b := fakeYtdlpBackend(t, "stream")
	b.Client = playmateSite(t, &fakePlaymate{title: "1000267652"})
	res, err := b.ProbeTitle(context.Background(), playmateLink)
	if err != nil {
		t.Fatal(err)
	}
	if res.Title != "1000267652" {
		t.Errorf("Title = %q, want the page's title rather than the playlist's name", res.Title)
	}
	if len(res.Formats) != 1 || res.Formats[0].FormatID != playmateMaster {
		t.Errorf("formats = %+v, want the one the stream %q offers", res.Formats, playmateMaster)
	}
}
