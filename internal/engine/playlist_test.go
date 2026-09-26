package engine

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/GopeedLab/gopeed/pkg/download"
	"github.com/junkerderprovinz/knightloader/internal/core"
)

// hlsMaster is the master playlist playmate.to serves as master.txt, with
// application/vnd.apple.mpegurl as its type.
const hlsMaster = "#EXTM3U\n#EXT-X-VERSION:6\n" +
	"#EXT-X-STREAM-INF:BANDWIDTH=2712197,AVERAGE-BANDWIDTH=2440977,CODECS=\"avc1.640028,mp4a.40.2\",RESOLUTION=1920x1080,FRAME-RATE=30.000\n" +
	"index_avc_1080p.txt\n"

func TestAPlaylistIsRecognisedByItsFirstBytes(t *testing.T) {
	dir := t.TempDir()
	for name, body := range map[string]string{
		"master.txt":  hlsMaster,
		"bom.m3u8":    "\xef\xbb\xbf#EXTM3U\n#EXTINF:10,\nseg0.ts\n",
		"padded.txt":  "\r\n  #EXTM3U\n",
		"stream.xml":  "<?xml version=\"1.0\" encoding=\"UTF-8\"?>\n<!-- made by a packager -->\n<MPD xmlns=\"urn:mpeg:dash:schema:mpd:2011\" type=\"static\">",
		"bare.mpd":    "<MPD type=\"dynamic\"></MPD>",
		"notes.txt":   "a text file that mentions #EXTM3U further in",
		"page.html":   "<html><body><MPD-like tag></body></html>",
		"movie.mkv":   "\x1aE\xdf\xa3 binary",
		"empty.bin":   "",
		"listing.txt": "https://a.example/1.zip\nhttps://a.example/2.zip\n",
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	playlists := map[string]bool{"master.txt": true, "bom.m3u8": true, "padded.txt": true, "stream.xml": true, "bare.mpd": true}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if got := streamPlaylist(fileHead(filepath.Join(dir, e.Name()))); got != playlists[e.Name()] {
			t.Errorf("streamPlaylist(%s) = %v, want %v", e.Name(), got, playlists[e.Name()])
		}
	}
	if streamPlaylist(fileHead(filepath.Join(dir, "missing.txt"))) {
		t.Error("a file that is not there was taken for a playlist")
	}
}

func TestAWebPageIsRecognisedByItsFirstBytes(t *testing.T) {
	pages := []string{
		"<!DOCTYPE html><html><head><title>1000267652</title>",
		"\xef\xbb\xbf\n<!doctype HTML>\n<meta charset=utf-8>",
		"<html lang=\"en\"><body></body></html>",
		"<!-- served by nginx -->\n<HTML>",
		"<?xml version=\"1.0\"?>\n<!DOCTYPE html PUBLIC \"-//W3C//DTD XHTML 1.0 Strict//EN\" \"x.dtd\">\n<html>",
	}
	others := []string{
		"<?xml version=\"1.0\"?><!DOCTYPE svg><svg></svg>",
		"<MPD type=\"static\"></MPD>",
		"#EXTM3U\n",
		"PK\x03\x04 a zip",
		"a text file that mentions <html> further in",
		"",
	}
	for _, c := range []struct {
		bodies []string
		want   bool
	}{{pages, true}, {others, false}} {
		for _, body := range c.bodies {
			p := filepath.Join(t.TempDir(), "download")
			if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
				t.Fatal(err)
			}
			if got := webPage(fileHead(p)); got != c.want {
				t.Errorf("webPage(%q) = %v, want %v", body, got, c.want)
			}
		}
	}
}

// doneWith sends a done event for task g1 once body has been written where
// the library would have put it, with job as the engine's record of t1.
func doneWith(t *testing.T, job Job, name, body string) []core.Update {
	t.Helper()
	e, got := eventEngine(t)
	e.jobs["t1"] = job
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	e.onEvent(&download.Event{Key: download.EventKeyDone, Task: libraryTask(t, dir, name, name)})
	return *got
}

func TestAPlaylistTakenForAFileIsHandedOn(t *testing.T) {
	got := doneWith(t, Job{PassOnPlaylists: true}, "master.txt", hlsMaster)
	if len(got) != 1 {
		t.Fatalf("reported %+v, want one update", got)
	}
	u := got[0]
	if u.Status != core.StatusError || !u.Unsupported {
		t.Fatalf("reported %+v, want an unsupported failure the app hands to the next backend", u)
	}
	if u.File == "" {
		t.Error("the failure does not name the playlist, so nothing removes it")
	}
}

// A job the resolver picked on purpose, such as a debrid unlock, keeps what
// it fetched.
func TestAPlaylistIsKeptWhenTheJobDoesNotPassItOn(t *testing.T) {
	got := doneWith(t, Job{}, "master.txt", hlsMaster)
	if len(got) != 1 || got[0].Status != core.StatusDone {
		t.Fatalf("reported %+v, want the download done", got)
	}
}

func TestAnOrdinaryFileTakenByItsLookFinishes(t *testing.T) {
	got := doneWith(t, Job{PassOnPlaylists: true}, "notes.txt", "shopping list\n")
	if len(got) != 1 || got[0].Status != core.StatusDone {
		t.Fatalf("reported %+v, want the download done", got)
	}
}

// embedPage is what the HTTP fallback fetches for a playmate.to embed link.
const embedPage = "<!DOCTYPE html><html><head><meta charset=\"utf-8\">\n<title>1000267652</title>\n" +
	"<script src='/assets/jw8/jwplayer.js'></script></head><body></body></html>"

func TestAPageTheFallbackFetchedFailsAsAnUnsupportedPlayer(t *testing.T) {
	got := doneWith(t, Job{PassOnPlaylists: true, RefusePages: true}, "FrBxuaYCIKvsh", embedPage)
	if len(got) != 1 {
		t.Fatalf("reported %+v, want one update", got)
	}
	u := got[0]
	if u.Status != core.StatusError || u.Reason != core.ReasonUnsupportedPlayer {
		t.Fatalf("reported %+v, want a failure naming the unsupported player", u)
	}
	if u.Unsupported {
		t.Error("the page was handed on, and nothing after the fallback takes it")
	}
}

// The direct download claims a link by a file extension, so what it fetches
// is the file, whatever it holds.
func TestAPageIsKeptByAJobThatDoesNotRefuseIt(t *testing.T) {
	got := doneWith(t, Job{PassOnPlaylists: true}, "notes.html", embedPage)
	if len(got) != 1 || got[0].Status != core.StatusDone {
		t.Fatalf("reported %+v, want the download done", got)
	}
}

// End to end against a local server: the page is deleted with its task.
func TestAPageTheFallbackFetchedIsNotLeftBehind(t *testing.T) {
	dir := t.TempDir()
	got := settle(t, dir, "FrBxuaYCIKvsh", []byte(embedPage), Job{PassOnPlaylists: true, RefusePages: true})
	last := got[len(got)-1]
	if last.Status != core.StatusError || last.Reason != core.ReasonUnsupportedPlayer {
		t.Fatalf("the download ended as %+v, want a failure naming the unsupported player", last)
	}
	left, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range left {
		t.Errorf("%s was left in the download folder", e.Name())
	}
}

// End to end against a local server: the playlist arrives under a .txt name
// and the job ends unsupported rather than done.
func TestADirectDownloadOfAPlaylistEndsUnsupported(t *testing.T) {
	got := settle(t, t.TempDir(), "master.txt", []byte(hlsMaster), Job{PassOnPlaylists: true})
	last := got[len(got)-1]
	if last.Status != core.StatusError || !last.Unsupported {
		t.Fatalf("the download ended as %+v, want an unsupported failure", last)
	}
	for _, u := range got {
		if u.Status == core.StatusDone {
			t.Fatalf("the playlist was reported done: %+v", u)
		}
	}
}
