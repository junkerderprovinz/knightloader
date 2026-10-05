package debriddrive

import (
	"bytes"
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/resolver/debrid"
)

// fakeService is a debrid account holding a few downloads. Each unlock hands
// out a new address on the fake download server, so a test can tell one
// unlock from the next.
type fakeService struct {
	cdn string

	mu       sync.Mutex
	listed   []debrid.Listed
	jobs     map[string]debrid.TorrentJob
	lists    int
	statuses int
	unlocks  int
	refuse   error
}

func (s *fakeService) List(context.Context) ([]debrid.Listed, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.lists++
	return slices.Clone(s.listed), true, nil
}

func (s *fakeService) TorrentStatus(_ context.Context, id string) (debrid.TorrentJob, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.statuses++
	j, ok := s.jobs[id]
	if !ok {
		return debrid.TorrentJob{}, errors.New("no such download")
	}
	return j, nil
}

func (s *fakeService) FileURL(_ context.Context, _ string, f debrid.TorrentFile) (debrid.Direct, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.refuse != nil {
		return debrid.Direct{}, s.refuse
	}
	s.unlocks++
	return debrid.Direct{URL: fmt.Sprintf("%s/%s?unlock=%d", s.cdn, f.ID, s.unlocks), Size: f.Size}, nil
}

func (s *fakeService) counts() (lists, statuses, unlocks int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.lists, s.statuses, s.unlocks
}

// fakeCDN serves each file's bytes by id with range support, and refuses the
// addresses of unlocks it has been told are dead.
type fakeCDN struct {
	*httptest.Server
	files map[string][]byte

	mu   sync.Mutex
	dead map[string]bool
	// down answers every request with this status instead of the file.
	down   int
	ranges []string
}

func newCDN(t *testing.T, files map[string][]byte) *fakeCDN {
	c := &fakeCDN{files: files, dead: map[string]bool{}}
	c.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c.mu.Lock()
		c.ranges = append(c.ranges, r.Header.Get("Range"))
		dead, down := c.dead[r.URL.Query().Get("unlock")], c.down
		c.mu.Unlock()
		if down != 0 {
			http.Error(w, "busy", down)
			return
		}
		if dead {
			http.Error(w, "link expired", http.StatusForbidden)
			return
		}
		b, ok := c.files[strings.TrimPrefix(r.URL.Path, "/")]
		if !ok {
			http.NotFound(w, r)
			return
		}
		http.ServeContent(w, r, "", time.Time{}, bytes.NewReader(b))
	}))
	t.Cleanup(c.Close)
	return c
}

func (c *fakeCDN) kill(unlock string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.dead[unlock] = true
}

func (c *fakeCDN) lastRange() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.ranges) == 0 {
		return ""
	}
	return c.ranges[len(c.ranges)-1]
}

var (
	movie   = bytes.Repeat([]byte("0123456789"), 100)
	episode = []byte("an episode of something")
	added   = time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
)

// fixture is a drive over one TorBox account, served at /dav, with a clock
// the test moves.
type fixture struct {
	srv   *httptest.Server
	svc   *fakeService
	cdn   *fakeCDN
	drive *Drive
	clock *time.Time
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	cdn := newCDN(t, map[string][]byte{"m1": movie, "e1": episode})
	svc := &fakeService{
		cdn: cdn.URL,
		listed: []debrid.Listed{
			{ID: "1", Name: "Some Movie", Size: int64(len(movie)), Added: added},
			{ID: "2", Name: "Some Show", Size: int64(len(episode)), Added: added.Add(time.Hour)},
			{ID: "3", Name: "Some Movie", Size: 5, Added: added.Add(2 * time.Hour)},
		},
		jobs: map[string]debrid.TorrentJob{
			"1": {Name: "Some Movie", State: debrid.TorrentReady, Files: []debrid.TorrentFile{
				{ID: "m1", Path: "Some Movie/movie.mkv", Size: int64(len(movie)), Held: true},
				{ID: "x", Path: "Some Movie/sample.mkv", Size: 9},
			}},
			"2": {Name: "Some Show", State: debrid.TorrentFetching, Files: []debrid.TorrentFile{
				{ID: "e1", Path: "/Season 1/e1.mkv", Size: int64(len(episode)), Held: true},
			}},
		},
	}
	clock := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	d := New(
		func() []Account { return []Account{{Slot: "torbox", Name: "TorBox", Source: svc}} },
		func() time.Duration { return 5 * time.Minute },
		cdn.Client(),
	)
	d.now = func() time.Time { return clock }
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		d.Serve(w, r, "/dav")
	}))
	t.Cleanup(srv.Close)
	return &fixture{srv: srv, svc: svc, cdn: cdn, drive: d, clock: &clock}
}

func (f *fixture) do(t *testing.T, method, p string, header map[string]string) *http.Response {
	t.Helper()
	req, err := http.NewRequest(method, f.srv.URL+(&url.URL{Path: p}).EscapedPath(), nil)
	if err != nil {
		t.Fatal(err)
	}
	for k, v := range header {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { resp.Body.Close() })
	return resp
}

// list answers a Depth: 1 PROPFIND with the paths of the folder's entries,
// the folder itself left out.
func (f *fixture) list(t *testing.T, p string) []string {
	t.Helper()
	resp := f.do(t, "PROPFIND", p, map[string]string{"Depth": "1"})
	if resp.StatusCode != http.StatusMultiStatus {
		t.Fatalf("PROPFIND %s answered %s", p, resp.Status)
	}
	var ms struct {
		Responses []struct {
			Href string `xml:"href"`
		} `xml:"response"`
	}
	if err := xml.NewDecoder(resp.Body).Decode(&ms); err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, r := range ms.Responses {
		h, err := url.PathUnescape(r.Href)
		if err != nil {
			t.Fatal(err)
		}
		if strings.TrimSuffix(h, "/") != strings.TrimSuffix(p, "/") {
			out = append(out, h)
		}
	}
	slices.Sort(out)
	return out
}

func TestTheDriveListsAccountsDownloadsAndTheirFiles(t *testing.T) {
	f := newFixture(t)

	if got, want := f.list(t, "/dav/"), []string{"/dav/TorBox/"}; !slices.Equal(got, want) {
		t.Errorf("the root lists %q, want %q", got, want)
	}
	// The newer download of the same name has its id added, so the older
	// one's path does not move.
	want := []string{"/dav/TorBox/Some Movie [3]/", "/dav/TorBox/Some Movie/", "/dav/TorBox/Some Show/"}
	if got := f.list(t, "/dav/TorBox/"); !slices.Equal(got, want) {
		t.Errorf("the account lists %q, want %q", got, want)
	}
	// The torrent's own folder comes off, and a file the service does not
	// hold yet is left out.
	if got, want := f.list(t, "/dav/TorBox/Some Movie/"), []string{"/dav/TorBox/Some Movie/movie.mkv"}; !slices.Equal(got, want) {
		t.Errorf("the movie lists %q, want %q", got, want)
	}
	if got, want := f.list(t, "/dav/TorBox/Some Show/Season 1/"), []string{"/dav/TorBox/Some Show/Season 1/e1.mkv"}; !slices.Equal(got, want) {
		t.Errorf("the season lists %q, want %q", got, want)
	}
	if resp := f.do(t, "PROPFIND", "/dav/TorBox/Nothing/", map[string]string{"Depth": "0"}); resp.StatusCode != http.StatusNotFound {
		t.Errorf("a download that is not there answered %s, want 404", resp.Status)
	}
	if _, _, unlocks := f.svc.counts(); unlocks != 0 {
		t.Errorf("listing the folders unlocked %d files; only a read should", unlocks)
	}
}

func TestANameThatWouldLeaveItsFolderIsNotUsed(t *testing.T) {
	f := newFixture(t)
	f.svc.mu.Lock()
	f.svc.listed = []debrid.Listed{{ID: "7", Name: "..", Added: added}}
	f.svc.jobs["7"] = debrid.TorrentJob{Name: "..", State: debrid.TorrentReady, Files: []debrid.TorrentFile{
		{ID: "m1", Path: "../../etc/movie.mkv", Size: int64(len(movie)), Held: true},
	}}
	f.svc.mu.Unlock()

	if got, want := f.list(t, "/dav/TorBox/"), []string{"/dav/TorBox/7/"}; !slices.Equal(got, want) {
		t.Errorf("the account lists %q, want the download under its id", got)
	}
	if got, want := f.list(t, "/dav/TorBox/7/"), []string{"/dav/TorBox/7/etc/"}; !slices.Equal(got, want) {
		t.Errorf("the download lists %q, want the steps out of the folder dropped", got)
	}
}

func TestAnAccountNameWithASlashIsOneFolder(t *testing.T) {
	f := newFixture(t)
	f.drive.accounts = func() []Account {
		return []Account{
			{Slot: "torbox:dd/x", Name: "TorBox (dd/x)", Source: f.svc},
			{Slot: "torbox", Name: "TorBox", Source: f.svc},
		}
	}

	if got, want := f.list(t, "/dav/"), []string{"/dav/TorBox (dd%2Fx)/", "/dav/TorBox/"}; !slices.Equal(got, want) {
		t.Errorf("the root lists %q, want %q", got, want)
	}
	if got := f.list(t, "/dav/TorBox (dd%2Fx)/"); len(got) != 3 {
		t.Errorf("the account with the slash lists %q, want its three downloads", got)
	}
	if got, want := f.drive.Folders(), []string{"TorBox", "TorBox (dd%2Fx)"}; !slices.Equal(got, want) {
		t.Errorf("Folders() = %q, want %q", got, want)
	}
}

func TestAnAccountFolderNamesItsAccountAndKeepsItWhenAnotherComesOrGoes(t *testing.T) {
	f := newFixture(t)
	all := []Account{
		{Slot: "torbox:dd_x", Name: "TorBox (dd_x)", Source: f.svc},
		{Slot: "torbox:dd/x", Name: "TorBox (dd/x)", Source: f.svc},
		{Slot: `torbox:dd\x`, Name: `TorBox (dd\x)`, Source: f.svc},
		{Slot: "torbox:dd%2Fx", Name: "TorBox (dd%2Fx)", Source: f.svc},
	}
	want := map[string]string{
		"TorBox (dd_x)":     "torbox:dd_x",
		"TorBox (dd%2Fx)":   "torbox:dd/x",
		"TorBox (dd%5Cx)":   `torbox:dd\x`,
		"TorBox (dd%252Fx)": "torbox:dd%2Fx",
	}

	for _, gone := range []string{"", "torbox:dd/x", "torbox:dd_x"} {
		f.drive.accounts = func() []Account {
			return slices.DeleteFunc(slices.Clone(all), func(a Account) bool { return a.Slot == gone })
		}
		var folders []string
		for name, slot := range want {
			if slot != gone {
				folders = append(folders, name)
			}
		}
		slices.Sort(folders)
		if got := f.drive.Folders(); !slices.Equal(got, folders) {
			t.Errorf("with %q gone the folders are %q, want %q", gone, got, folders)
		}
		for name, slot := range want {
			if slot == gone {
				continue
			}
			s, err := f.drive.find(context.Background(), name)
			if err != nil || s.acct.Slot != slot {
				t.Errorf("with %q gone the folder %q serves %q (%v), want %q", gone, name, s.acct.Slot, err, slot)
			}
		}
	}
}

func TestAListingIsReadAgainOnlyAfterTheRefreshInterval(t *testing.T) {
	f := newFixture(t)

	f.list(t, "/dav/TorBox/Some Movie/")
	f.list(t, "/dav/TorBox/Some Show/")
	f.list(t, "/dav/TorBox/")
	if lists, statuses, _ := f.svc.counts(); lists != 1 || statuses != 2 {
		t.Fatalf("three listings read the account %d times and the downloads %d times, want 1 and 2", lists, statuses)
	}

	*f.clock = f.clock.Add(6 * time.Minute)
	f.list(t, "/dav/TorBox/Some Movie/")
	f.list(t, "/dav/TorBox/Some Show/")
	// The complete download is not read again; the one still on its way is.
	if lists, statuses, _ := f.svc.counts(); lists != 2 || statuses != 3 {
		t.Errorf("after the interval the account was read %d times and the downloads %d times, want 2 and 3", lists, statuses)
	}
}

func TestADownloadThatLeavesTheAccountLeavesTheDrive(t *testing.T) {
	f := newFixture(t)
	f.list(t, "/dav/TorBox/Some Movie/")
	f.list(t, "/dav/TorBox/Some Show/")

	f.svc.mu.Lock()
	f.svc.listed = slices.DeleteFunc(f.svc.listed, func(l debrid.Listed) bool { return l.ID == "2" })
	f.svc.mu.Unlock()
	*f.clock = f.clock.Add(6 * time.Minute)

	if resp := f.do(t, "PROPFIND", "/dav/TorBox/Some Show/", map[string]string{"Depth": "1"}); resp.StatusCode != http.StatusNotFound {
		t.Errorf("a download deleted on the account answered %s, want 404", resp.Status)
	}
	f.drive.mu.Lock()
	_, kept := f.drive.jobs["torbox\x001"]
	held := len(f.drive.jobs)
	f.drive.mu.Unlock()
	if !kept || held != 1 {
		t.Errorf("the drive holds the files of %d downloads, want only the one still on the account", held)
	}
}

func TestAServiceInTroubleIsAnErrorRatherThanAnEmptyFolder(t *testing.T) {
	f := newFixture(t)
	f.svc.mu.Lock()
	delete(f.svc.jobs, "2")
	f.svc.mu.Unlock()

	if resp := f.do(t, "PROPFIND", "/dav/TorBox/Some Show/", map[string]string{"Depth": "1"}); resp.StatusCode != http.StatusBadGateway {
		t.Errorf("a download the service could not read answered %s, want 502", resp.Status)
	}
}

func TestARangeIsFetchedFromTheDownloadServer(t *testing.T) {
	f := newFixture(t)

	resp := f.do(t, http.MethodGet, "/dav/TorBox/Some Movie/movie.mkv", map[string]string{"Range": "bytes=105-114"})
	if resp.StatusCode != http.StatusPartialContent {
		t.Fatalf("a range answered %s, want 206", resp.Status)
	}
	got, _ := io.ReadAll(resp.Body)
	if want := movie[105:115]; !bytes.Equal(got, want) {
		t.Errorf("the range read %q, want %q", got, want)
	}
	if r := f.cdn.lastRange(); r != "bytes=105-" {
		t.Errorf("the download server was asked for %q, want the file from the range's start", r)
	}

	resp = f.do(t, http.MethodGet, "/dav/TorBox/Some Movie/movie.mkv", map[string]string{"Range": "bytes=0-9,500-509"})
	_, params, err := mime.ParseMediaType(resp.Header.Get("Content-Type"))
	if resp.StatusCode != http.StatusPartialContent || err != nil {
		t.Fatalf("two ranges answered %s with type %q", resp.Status, resp.Header.Get("Content-Type"))
	}
	parts := multipart.NewReader(resp.Body, params["boundary"])
	for _, want := range [][]byte{movie[0:10], movie[500:510]} {
		part, err := parts.NextPart()
		if err != nil {
			t.Fatal(err)
		}
		if got, _ := io.ReadAll(part); !bytes.Equal(got, want) {
			t.Errorf("a part of two ranges read %q, want %q", got, want)
		}
	}

	resp = f.do(t, http.MethodGet, "/dav/TorBox/Some Movie/movie.mkv", nil)
	got, _ = io.ReadAll(resp.Body)
	if !bytes.Equal(got, movie) {
		t.Errorf("the whole file read %d bytes, want %d", len(got), len(movie))
	}
	if _, _, unlocks := f.svc.counts(); unlocks != 1 {
		t.Errorf("two reads unlocked the file %d times, want once", unlocks)
	}
}

func TestAFileOfNoKnownTypeIsNotReadToTellItsType(t *testing.T) {
	f := newFixture(t)
	f.svc.mu.Lock()
	f.svc.jobs["1"].Files[0].Path = "Some Movie/movie.klnotype"
	f.svc.mu.Unlock()
	p := "/dav/TorBox/Some Movie/movie.klnotype"

	resp := f.do(t, http.MethodHead, p, nil)
	if resp.StatusCode != http.StatusOK || resp.Header.Get("Content-Type") != "application/octet-stream" {
		t.Errorf("HEAD answered %s with type %q, want 200 application/octet-stream", resp.Status, resp.Header.Get("Content-Type"))
	}
	if _, _, unlocks := f.svc.counts(); unlocks != 0 {
		t.Errorf("HEAD unlocked the file %d times; only a read should", unlocks)
	}

	resp = f.do(t, http.MethodGet, p, map[string]string{"Range": "bytes=500-599"})
	got, _ := io.ReadAll(resp.Body)
	if !bytes.Equal(got, movie[500:600]) {
		t.Errorf("the range read %q", got)
	}
	f.cdn.mu.Lock()
	ranges := slices.Clone(f.cdn.ranges)
	f.cdn.mu.Unlock()
	if want := []string{"bytes=500-"}; !slices.Equal(ranges, want) {
		t.Errorf("the download server was asked for %q, want %q", ranges, want)
	}
}

func TestABrowserGetsADriveFileToSaveRatherThanAPageToRun(t *testing.T) {
	f := newFixture(t)
	f.svc.mu.Lock()
	page := f.svc.jobs["1"]
	page.Files = []debrid.TorrentFile{
		{ID: "m1", Path: "Some Movie/index.html", Size: int64(len(movie)), Held: true},
		{ID: "e1", Path: "Some Movie/pic.svg", Size: int64(len(episode)), Held: true},
		{ID: "e1", Path: "Some Movie/cover.png", Size: int64(len(episode)), Held: true},
	}
	f.svc.jobs["1"] = page
	f.svc.mu.Unlock()

	for _, p := range []string{"/dav/TorBox/Some Movie/index.html", "/dav/TorBox/Some Movie/pic.svg"} {
		for _, method := range []string{http.MethodHead, http.MethodGet} {
			resp := f.do(t, method, p, nil)
			h := resp.Header
			if resp.StatusCode != http.StatusOK {
				t.Errorf("%s %s answered %s", method, p, resp.Status)
			}
			if got := h.Get("Content-Type"); got != "application/octet-stream" {
				t.Errorf("%s %s answered the type %q, want application/octet-stream", method, p, got)
			}
			if got := h.Get("Content-Disposition"); got != "attachment" {
				t.Errorf("%s %s answered Content-Disposition %q, want attachment", method, p, got)
			}
			if got := h.Get("X-Content-Type-Options"); got != "nosniff" {
				t.Errorf("%s %s answered X-Content-Type-Options %q, want nosniff", method, p, got)
			}
			if got := h.Get("Content-Security-Policy"); !strings.Contains(got, "sandbox") || !strings.Contains(got, "default-src 'none'") {
				t.Errorf("%s %s answered Content-Security-Policy %q, want a sandbox that loads nothing", method, p, got)
			}
		}
	}

	// A picture keeps its type, which a player may go by.
	if got := f.do(t, http.MethodHead, "/dav/TorBox/Some Movie/cover.png", nil).Header.Get("Content-Type"); got != "image/png" {
		t.Errorf("a picture answered the type %q, want image/png", got)
	}
}

func TestVideoAndAudioHaveOneTypeOnEverySystem(t *testing.T) {
	f := newFixture(t)
	if got := f.do(t, http.MethodHead, "/dav/TorBox/Some Movie/movie.mkv", nil).Header.Get("Content-Type"); got != "video/x-matroska" {
		t.Errorf("HEAD answered movie.mkv with the type %q, want video/x-matroska", got)
	}
	for name, want := range map[string]string{
		"Show.S01E01.mkv": "video/x-matroska",
		"clip.avi":        "video/x-msvideo",
		"clip.m4v":        "video/x-m4v",
		"clip.ts":         "video/mp2t",
		"clip.mov":        "video/quicktime",
		"clip.wmv":        "video/x-ms-wmv",
		"CLIP.MKV":        "video/x-matroska",
		"track.flac":      "audio/flac",
		"track.m4a":       "audio/mp4",
	} {
		if got, _ := (info{&node{name: name}}).ContentType(context.Background()); got != want {
			t.Errorf("%s has the type %q, want %q", name, got, want)
		}
	}
}

func TestALinkIsUnlockedAgainWhenItRunsOut(t *testing.T) {
	f := newFixture(t)
	read := func() []byte {
		t.Helper()
		resp := f.do(t, http.MethodGet, "/dav/TorBox/Some Show/Season 1/e1.mkv", nil)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("the read answered %s", resp.Status)
		}
		b, _ := io.ReadAll(resp.Body)
		return b
	}

	read()
	// The download server stops taking the first link.
	f.cdn.kill("1")
	if got := read(); !bytes.Equal(got, episode) {
		t.Errorf("the read after the link died gave %q", got)
	}
	if _, _, unlocks := f.svc.counts(); unlocks != 2 {
		t.Errorf("the file was unlocked %d times, want again once its link was refused", unlocks)
	}

	// A link is used for an hour at most, refused or not.
	*f.clock = f.clock.Add(61 * time.Minute)
	read()
	if _, _, unlocks := f.svc.counts(); unlocks != 3 {
		t.Errorf("the file was unlocked %d times, want a new link after the hour", unlocks)
	}
}

func TestAFileTheServiceWillNotHandOutIsAnError(t *testing.T) {
	f := newFixture(t)
	f.svc.refuse = errors.New("the plan has run out")

	resp := f.do(t, http.MethodGet, "/dav/TorBox/Some Movie/movie.mkv", nil)
	if resp.StatusCode != http.StatusBadGateway {
		t.Errorf("a refused unlock answered %s, want 502", resp.Status)
	}
	b, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(b), "the plan has run out") {
		t.Errorf("the answer %q does not say why", b)
	}
}

func TestADownloadServerInTroubleIsAnErrorRatherThanAnEmptyFile(t *testing.T) {
	f := newFixture(t)
	var logged []string
	f.drive.logf = func(format string, args ...any) { logged = append(logged, fmt.Sprintf(format, args...)) }
	read := func(rng string) (*http.Response, string) {
		t.Helper()
		h := map[string]string{}
		if rng != "" {
			h["Range"] = rng
		}
		resp := f.do(t, http.MethodGet, "/dav/TorBox/Some Movie/movie.mkv", h)
		b, _ := io.ReadAll(resp.Body)
		return resp, string(b)
	}

	f.cdn.mu.Lock()
	f.cdn.down = http.StatusServiceUnavailable
	f.cdn.mu.Unlock()
	for _, rng := range []string{"", "bytes=100-", "bytes=0-9,500-509"} {
		resp, body := read(rng)
		if resp.StatusCode != http.StatusBadGateway {
			t.Errorf("a read with Range %q from a download server answering 503 answered %s, want 502", rng, resp.Status)
		}
		if resp.Header.Get("Content-Range") != "" || resp.Header.Get("ETag") != "" {
			t.Errorf("the 502 kept the headers of the file: %v", resp.Header)
		}
		if !strings.Contains(body, "503") {
			t.Errorf("the answer %q does not say why", body)
		}
	}

	// A download server that cannot start part way answers a range with the
	// whole file.
	f.cdn.mu.Lock()
	f.cdn.down = http.StatusOK
	f.cdn.mu.Unlock()
	if resp, body := read("bytes=100-"); resp.StatusCode != http.StatusBadGateway || !strings.Contains(body, "part way") {
		t.Errorf("a range the download server cannot start answered %s %q, want 502 saying so", resp.Status, body)
	}

	if len(logged) != 4 {
		t.Fatalf("the four failed reads logged %q", logged)
	}
	for _, line := range logged {
		if !strings.Contains(line, "Some Movie/movie.mkv") || strings.Contains(line, "unlock=") {
			t.Errorf("the log line %q should name the file and never the link", line)
		}
	}
}

func TestTheDriveOffersNoLock(t *testing.T) {
	f := newFixture(t)
	for _, p := range []string{"/dav/", "/dav/TorBox/Some Movie/"} {
		resp := f.do(t, "PROPFIND", p, map[string]string{"Depth": "1"})
		b, _ := io.ReadAll(resp.Body)
		if resp.StatusCode != http.StatusMultiStatus {
			t.Fatalf("PROPFIND %s answered %s", p, resp.Status)
		}
		if bytes.Contains(b, []byte("lockentry")) {
			t.Errorf("PROPFIND %s offers a lock on a share with nothing to lock:\n%s", p, b)
		}
		if !bytes.Contains(b, []byte("supportedlock")) {
			t.Errorf("PROPFIND %s lost the supportedlock property, which should be there and empty:\n%s", p, b)
		}
	}
	if dav := f.do(t, http.MethodOptions, "/dav/", nil).Header.Get("DAV"); dav != "1" {
		t.Errorf("OPTIONS says DAV: %q, want class 1", dav)
	}
}

func TestALockEntrySplitBetweenWritesIsStillTakenOut(t *testing.T) {
	body := `<D:prop><D:supportedlock>` + string(lockEntry) + `</D:supportedlock><D:x>&lt;D:lock</D:x></D:prop><D:lockent`
	rec := httptest.NewRecorder()
	w := &noLocks{ResponseWriter: rec}
	for i := range len(body) {
		w.Write([]byte{body[i]})
	}
	w.finish()
	want := `<D:prop><D:supportedlock></D:supportedlock><D:x>&lt;D:lock</D:x></D:prop><D:lockent`
	if got := rec.Body.String(); got != want {
		t.Errorf("one byte at a time gave\n%s\nwant\n%s", got, want)
	}
}

func TestTheDriveIsReadOnly(t *testing.T) {
	f := newFixture(t)
	for _, method := range []string{http.MethodPut, http.MethodDelete, "MOVE", "COPY", "PROPPATCH", "LOCK"} {
		if resp := f.do(t, method, "/dav/TorBox/Some Movie/movie.mkv", nil); resp.StatusCode != http.StatusMethodNotAllowed {
			t.Errorf("%s answered %s, want 405", method, resp.Status)
		}
	}
	if resp := f.do(t, "MKCOL", "/dav/TorBox/new folder/", nil); resp.StatusCode != http.StatusForbidden {
		t.Errorf("MKCOL answered %s, want 403", resp.Status)
	}
	for _, depth := range []string{"", "infinity"} {
		h := map[string]string{}
		if depth != "" {
			h["Depth"] = depth
		}
		if resp := f.do(t, "PROPFIND", "/dav/", h); resp.StatusCode != http.StatusForbidden {
			t.Errorf("PROPFIND with Depth %q answered %s, want 403", depth, resp.Status)
		}
	}
	if lists, _, _ := f.svc.counts(); lists != 0 {
		t.Errorf("a refused request read the account %d times", lists)
	}
}
