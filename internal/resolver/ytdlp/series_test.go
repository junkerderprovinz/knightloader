package ytdlp

import (
	"encoding/xml"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/core"
)

func TestBuildArgsFilesASeriesRowUnderItsShowAndSeason(t *testing.T) {
	args := buildArgs("d", Options{Series: &Series{Show: "Kurzgesagt"}})
	got, _ := valueAfter(args, "-o")
	want := filepath.Join("d", "Kurzgesagt/Season %(upload_date>%Y|0000)s/"+
		"Kurzgesagt - S%(upload_date>%Y|0000)sE%(upload_date>%m%d|0000)s - %(title)s.%(ext)s")
	if got != want {
		t.Errorf("-o = %q, want %q", got, want)
	}
	if !hasArg(args, "--write-info-json") {
		t.Errorf("the episode NFO is built from the info json, which is not asked for: %v", args)
	}
	if strings.Contains(strings.Join(args, " "), plannedPrefix) {
		t.Errorf("an episode carries no quality in its name, yet the frame is asked for: %v", args)
	}
}

// A show name is the user's or the site's text, and yt-dlp would read a % in
// it as a field and a slash as another folder.
func TestASeriesShowNameStaysOneLiteralFolder(t *testing.T) {
	got, _ := valueAfter(buildArgs("d", Options{Series: &Series{Show: "100% Wissen/AT"}}), "-o")
	show, _, _ := strings.Cut(strings.TrimPrefix(got, "d"+string(filepath.Separator)), "Season ")
	show = strings.TrimRight(show, `/\`)
	if strings.ContainsAny(show, `/\`) || !strings.Contains(show, "100%% Wissen") {
		t.Errorf("show folder = %q, want one segment with the %% doubled", show)
	}
}

func TestASeriesWithoutAShowNameTakesTheChannels(t *testing.T) {
	got, _ := valueAfter(buildArgs("d", Options{Series: &Series{}}), "-o")
	if !strings.HasPrefix(got, filepath.Join("d", "%(channel,uploader,extractor)s/Season ")) {
		t.Errorf("-o = %q, want the channel's name as the show folder", got)
	}
}

// artServer plays the site's image host: a PNG avatar and a JPEG banner.
func artServer(t *testing.T) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		switch r.URL.Path {
		case "/avatar":
			_, _ = w.Write([]byte("\x89PNG\r\n\x1a\n avatar"))
		case "/banner":
			_, _ = w.Write([]byte("\xff\xd8\xff\xe0 banner"))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv, &hits
}

// runEpisode runs one series row against the helper, the site's art served by
// srv, into dir.
func runEpisode(t *testing.T, mode, dir string, srv *httptest.Server, s Series) *recorder {
	t.Helper()
	t.Setenv(runHelperEnv, mode)
	t.Setenv(artHelperEnv, srv.URL)
	rec := &recorder{}
	b := NewBackend(os.Args[0], dir, rec.add)
	b.SetFFmpeg(os.Args[0], os.Args[0])
	b.Client = srv.Client()
	b.Options = func(string) Options { return Options{Series: &s} }
	b.run("task-1", "https://example.invalid/watch?v=x")
	return rec
}

func readXML(t *testing.T, path string, into any) {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(b), xml.Header) {
		t.Errorf("%s does not start with the XML header", path)
	}
	if err := xml.Unmarshal(b, into); err != nil {
		t.Fatalf("%s: %v", path, err)
	}
}

type episodeDoc struct {
	XMLName   xml.Name `xml:"episodedetails"`
	Title     string   `xml:"title"`
	ShowTitle string   `xml:"showtitle"`
	Season    int      `xml:"season"`
	Episode   int      `xml:"episode"`
	Aired     string   `xml:"aired"`
	Plot      string   `xml:"plot"`
	UniqueID  uniqueID `xml:"uniqueid"`
}

type showDoc struct {
	XMLName  xml.Name `xml:"tvshow"`
	Title    string   `xml:"title"`
	Plot     string   `xml:"plot"`
	UniqueID uniqueID `xml:"uniqueid"`
}

func TestRunFilesAnEpisodeWithItsNFOAndTheShowsOnce(t *testing.T) {
	srv, hits := artServer(t)
	dir := t.TempDir()
	rec := runEpisode(t, "episode:full", dir, srv, Series{Show: "Some Show"})

	show := filepath.Join(dir, "Some Show")
	episode := filepath.Join(show, "Season 2026", "Some Show - S2026E090701 - A Video.mkv")
	got := rec.last()
	if got.Status != core.StatusDone || got.File != episode {
		t.Fatalf("last update = %+v, want Done with %q", got, episode)
	}
	if got.Name != "Some Show - S2026E090701 - A Video" {
		t.Errorf("the row is named %q, want the episode's name", got.Name)
	}

	var ep episodeDoc
	readXML(t, filepath.Join(show, "Season 2026", "Some Show - S2026E090701 - A Video.nfo"), &ep)
	want := episodeDoc{
		XMLName: ep.XMLName, Title: "A Video", ShowTitle: "Some Show", Season: 2026, Episode: 90701,
		Aired: "2026-09-07", Plot: "Line one.\nLine two.",
		UniqueID: uniqueID{Type: "youtube", Default: true, Value: "dQw4w9WgXcQ"},
	}
	if ep != want {
		t.Errorf("episode NFO = %+v, want %+v", ep, want)
	}

	var sh showDoc
	readXML(t, filepath.Join(show, "tvshow.nfo"), &sh)
	if sh.Title != "Some Show" || sh.Plot != "About the channel." ||
		sh.UniqueID != (uniqueID{Type: "youtube", Default: true, Value: "UCsomechannel"}) {
		t.Errorf("tvshow.nfo = %+v", sh)
	}
	if b, _ := os.ReadFile(filepath.Join(show, "poster.png")); !strings.HasSuffix(string(b), "avatar") {
		t.Errorf("poster.png = %q, want the channel's avatar as the site served it", b)
	}
	if b, _ := os.ReadFile(filepath.Join(show, "fanart.jpg")); !strings.HasSuffix(string(b), "banner") {
		t.Errorf("fanart.jpg = %q, want the channel's banner", b)
	}
	if n := hits.Load(); n != 2 {
		t.Errorf("the art was asked for %d times, want 2", n)
	}
	entries, _ := os.ReadDir(filepath.Join(show, "Season 2026"))
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	if wantNames := []string{"Some Show - S2026E090701 - A Video.mkv", "Some Show - S2026E090701 - A Video.nfo"}; !slices.Equal(names, wantNames) {
		t.Errorf("the season holds %q, want %q", names, wantNames)
	}
}

func TestASecondUploadOfTheDayIsTheNextEpisode(t *testing.T) {
	srv, _ := artServer(t)
	dir := t.TempDir()
	season := filepath.Join(dir, "Some Show", "Season 2026")
	putEpisode(t, season, "Some Show - S2026E090701 - Earlier", "earlier1234")

	got := runEpisode(t, "episode:full", dir, srv, Series{Show: "Some Show"}).last()
	if want := filepath.Join(season, "Some Show - S2026E090702 - A Video.mkv"); got.File != want {
		t.Errorf("File = %q, want %q", got.File, want)
	}
}

// Fetched again, a video keeps its episode number rather than taking the
// next one and showing up twice.
func TestAnUploadFetchedAgainKeepsItsEpisodeNumber(t *testing.T) {
	srv, _ := artServer(t)
	dir := t.TempDir()
	season := filepath.Join(dir, "Some Show", "Season 2026")
	putEpisode(t, season, "Some Show - S2026E090701 - Earlier", "earlier1234")
	putEpisode(t, season, "Some Show - S2026E090702 - A Video", "dQw4w9WgXcQ")

	got := runEpisode(t, "episode:full", dir, srv, Series{Show: "Some Show"}).last()
	if want := filepath.Join(season, "Some Show - S2026E090702 - A Video.mkv"); got.File != want {
		t.Errorf("File = %q, want %q", got.File, want)
	}
}

func putEpisode(t *testing.T, season, stem, id string) {
	t.Helper()
	if err := os.MkdirAll(season, 0o755); err != nil {
		t.Fatal(err)
	}
	nfo := xml.Header + `<episodedetails><title>x</title><uniqueid type="youtube" default="true">` + id + `</uniqueid></episodedetails>`
	if err := os.WriteFile(filepath.Join(season, stem+".nfo"), []byte(nfo), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(season, stem+".mkv"), []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestTheShowsNFOAndArtAreNeverWrittenTwice(t *testing.T) {
	srv, hits := artServer(t)
	dir := t.TempDir()
	show := filepath.Join(dir, "Some Show")
	if err := os.MkdirAll(show, 0o755); err != nil {
		t.Fatal(err)
	}
	mine := xml.Header + "<tvshow><title>My own words</title></tvshow>\n"
	if err := os.WriteFile(filepath.Join(show, "tvshow.nfo"), []byte(mine), 0o644); err != nil {
		t.Fatal(err)
	}

	if got := runEpisode(t, "episode:full", dir, srv, Series{Show: "Some Show"}).last(); got.Status != core.StatusDone {
		t.Fatalf("last update = %+v, want Done", got)
	}
	if b, _ := os.ReadFile(filepath.Join(show, "tvshow.nfo")); string(b) != mine {
		t.Errorf("tvshow.nfo was rewritten:\n%s", b)
	}
	if n := hits.Load(); n != 0 {
		t.Errorf("the art was fetched %d times for a show that has its NFO", n)
	}
}

func TestAShowWithoutArtStillGetsItsNFO(t *testing.T) {
	srv, hits := artServer(t)
	dir := t.TempDir()
	if got := runEpisode(t, "episodebare:full", dir, srv, Series{}).last(); got.Status != core.StatusDone {
		t.Fatalf("last update = %+v, want Done", got)
	}
	show := filepath.Join(dir, "Some Channel")
	var sh showDoc
	readXML(t, filepath.Join(show, "tvshow.nfo"), &sh)
	if sh.Title != "Some Channel" {
		t.Errorf("tvshow.nfo names the show %q, want the channel's name", sh.Title)
	}
	for _, art := range []string{"poster.jpg", "poster.png", "fanart.jpg", "fanart.png"} {
		if _, err := os.Stat(filepath.Join(show, art)); err == nil {
			t.Errorf("%s was written for a site that has none", art)
		}
	}
	if n := hits.Load(); n != 0 {
		t.Errorf("the image host was asked %d times", n)
	}
}

// An upload without a date is filed under the day it was fetched, not in a
// season 0 Jellyfin would show as specials.
func TestAnUndatedUploadIsFiledUnderTheDayItArrived(t *testing.T) {
	srv, _ := artServer(t)
	dir := t.TempDir()
	got := runEpisode(t, "episodeundated:full", dir, srv, Series{Show: "Some Show"}).last()

	today := time.Now()
	want := filepath.Join(dir, "Some Show", "Season "+today.Format("2006"),
		"Some Show - S"+today.Format("2006")+"E"+today.Format("0102")+"01 - A Video.mkv")
	if got.File != want {
		t.Errorf("File = %q, want %q", got.File, want)
	}
	if _, err := os.Stat(filepath.Join(dir, "Some Show", "Season 0000")); err == nil {
		t.Error("the empty Season 0000 folder yt-dlp wrote into is still there")
	}
}
