package debrid

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/junkerderprovinz/knightloader/internal/core"
)

// fakeLinksnappy answers the torrent calls with the bodies pyLoad's and
// ResolveURL's clients read. Every call but AUTHENTICATE wants the session
// cookie AUTHENTICATE sets.
type fakeLinksnappy struct {
	t   *testing.T
	srv *httptest.Server

	mu     sync.Mutex
	seen   []string
	logins int
	// add is the answer to ADDMAGNET, and upload the one to the upload
	// script; upload also receives the .torrent sent.
	add      string
	upload   string
	uploaded []byte
	// pending is how many STARTs are answered that the magnet is still read.
	pending int
	starts  int
	// statuses are the returns of STATUS in turn, the last one repeating.
	statuses []string
	reads    int
	files    string
	cached   string
	deleted  []string
}

func newFakeLinksnappy(t *testing.T) *fakeLinksnappy {
	f := &fakeLinksnappy{
		t:        t,
		add:      `{"status":"OK","error":false,"return":[{"status":"OK","error":false,"torrentid":77}]}`,
		statuses: []string{`{"status":"FINISHED","name":"Show","percentDone":100,"getSize":"730 B"}`},
		files: `{"Show":{"e01.mkv":{"isVideo":"y","size":"700","downloadLink":"{srv}/torrents/9001/download"},` +
			`"extras":{"sample.mkv":{"isVideo":"n","size":30,"downloadLink":"{srv}/torrents/9002/download"}}}}`,
	}
	f.srv = httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeLinksnappy) client() *Linksnappy {
	l := NewLinksnappy("u", "p")
	l.base = f.srv.URL + "/api"
	return l
}

func (f *fakeLinksnappy) ok(w http.ResponseWriter, ret string) {
	fmt.Fprintf(w, `{"status":"OK","error":false,"return":%s}`, ret)
}

func (f *fakeLinksnappy) serve(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.seen = append(f.seen, r.URL.Path)
	q := r.URL.Query()
	if r.URL.Path == "/api/AUTHENTICATE" {
		if q.Get("username") != "u" || q.Get("password") != "p" {
			f.t.Errorf("logged in as %q", q.Get("username"))
		}
		f.logins++
		http.SetCookie(w, &http.Cookie{Name: "lslogin", Value: "s1", Path: "/"})
		f.ok(w, `"Logged in"`)
		return
	}
	// The engine fetches the file itself without the cookie.
	c, err := r.Cookie("lslogin")
	if !strings.HasPrefix(r.URL.Path, "/cdn/") && (err != nil || c.Value != "s1") {
		f.t.Errorf("%s went out without the session cookie", r.URL.Path)
		fmt.Fprint(w, `{"status":"ERROR","error":"Please login first.","return":false}`)
		return
	}
	switch r.URL.Path {
	case "/api/torrents/ADDMAGNET":
		if got := q.Get("magnetlinks"); got != testMagnet {
			f.t.Errorf("magnetlinks = %q", got)
		}
		fmt.Fprint(w, f.add)
	case "/includes/ajaxupload.php":
		file, _, err := r.FormFile("torrents[]")
		if err != nil {
			f.t.Errorf("the upload carries no torrents[]: %v", err)
			return
		}
		f.uploaded, _ = io.ReadAll(file)
		fmt.Fprint(w, f.upload)
	case "/api/torrents/START":
		f.starts++
		switch {
		case q.Get("tid") != "77":
			f.t.Errorf("started torrent %q", q.Get("tid"))
		case f.starts <= f.pending:
			fmt.Fprint(w, `{"status":"ERROR","error":"Magnet URI processing in progress. Please wait.","return":false}`)
		default:
			f.ok(w, `"Started"`)
		}
	case "/api/torrents/STATUS":
		if q.Get("tid") != "77" {
			f.t.Errorf("read torrent %q", q.Get("tid"))
		}
		f.ok(w, f.statuses[min(f.reads, len(f.statuses)-1)])
		f.reads++
	case "/api/torrents/FILES":
		if q.Get("id") != "77" {
			f.t.Errorf("listed the files of torrent %q", q.Get("id"))
		}
		f.ok(w, strings.ReplaceAll(f.files, "{srv}", f.srv.URL))
	case "/torrents/9001/download", "/torrents/9002/download":
		name := map[string]string{"9001": "e01.mkv", "9002": "sample.mkv"}[strings.Split(r.URL.Path, "/")[2]]
		http.Redirect(w, r, "/cdn/"+name, http.StatusFound)
	case "/cdn/e01.mkv", "/cdn/sample.mkv":
		w.Header().Set("Content-Length", "700")
	case "/api/torrents/DELETETORRENT":
		if q.Get("delFiles") != "1" {
			f.t.Errorf("deleted with delFiles=%q, which leaves the files on the account", q.Get("delFiles"))
		}
		f.deleted = append(f.deleted, q.Get("tid"))
		f.ok(w, `"Deleted"`)
	case "/api/torrents/HASHCHECK":
		if q.Get("hash") != "0123456789abcdef0123456789abcdef01234567" {
			f.t.Errorf("hash = %q", q.Get("hash"))
		}
		f.ok(w, f.cached)
	default:
		f.t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		w.WriteHeader(http.StatusNotFound)
	}
}

func (f *fakeLinksnappy) deletes() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.deleted)
}

func (f *fakeLinksnappy) count(p string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, s := range f.seen {
		if s == p {
			n++
		}
	}
	return n
}

func TestLinksnappyFetchesAMagnetFromAddToCleanup(t *testing.T) {
	ls := newFakeLinksnappy(t)
	ls.pending = 1
	ls.statuses = []string{
		`{"status":"PENDING","name":"Show","percentDone":0}`,
		`{"status":"DOWNLOADING","name":"Show","percentDone":"40","getSize":"730 B","downloadRate":"1 KB/s"}`,
		`{"status":"FINISHED","name":"Show","percentDone":100,"getSize":"730 B"}`,
	}

	parts, done := fetchTorrent(t, ls.client(), testMagnet, nil, core.StatusDone)

	want := []string{ls.srv.URL + "/cdn/e01.mkv -> Show/e01.mkv", ls.srv.URL + "/cdn/sample.mkv -> Show/extras/sample.mkv"}
	if got := partURLs(parts); !slices.Equal(got, want) {
		t.Errorf("the engine got %v, want %v", got, want)
	}
	if done.Loaded != 730 {
		t.Errorf("done with %d bytes", done.Loaded)
	}
	waitFor(t, func() bool { return slices.Equal(ls.deletes(), []string{"77"}) }, "the torrent deleted on Linksnappy")
	if n := ls.count("/api/torrents/START"); n != 2 {
		t.Errorf("START was sent %d times; once more after Linksnappy read the magnet, then never again", n)
	}
	if n := ls.count("/api/AUTHENTICATE"); n != 1 {
		t.Errorf("logged in %d times, want once for the whole torrent", n)
	}
}

func TestLinksnappyShowsItsProgressAndSpeed(t *testing.T) {
	ls := newFakeLinksnappy(t)
	ls.statuses = []string{`{"status":"DOWNLOADING","name":"Show","percentDone":"40","getSize":"730 B","downloadRate":"1 KB/s"}`}

	job, err := ls.client().TorrentStatus(context.Background(), "77")

	if err != nil {
		t.Fatal(err)
	}
	if job.State != TorrentFetching || job.Progress != 0.4 || job.Size != 730 || job.Speed != 1024 || job.Name != "Show" {
		t.Errorf("job = %+v, want Show fetching at 40 percent of 730 bytes, 1024 bytes a second", job)
	}
}

func TestLinksnappyUploadsATorrentFile(t *testing.T) {
	ls := newFakeLinksnappy(t)
	ls.upload = `{"Pack.torrent":{"error":false,"torrentid":"77"}}`
	ls.statuses = []string{`{"status":"FINISHED","name":"Pack","percentDone":100,"getSize":"128 B"}`}
	ls.files = `{"Pack":{"a.mkv":{"size":64,"downloadLink":"{srv}/torrents/9001/download"},"b.mkv":{"size":64,"downloadLink":"{srv}/torrents/9002/download"}}}`
	link, raw := builtTorrent(t, false)

	parts, _ := fetchTorrent(t, ls.client(), link, nil, core.StatusDone)

	if !bytes.Equal(ls.uploaded, raw) {
		t.Error("the upload did not carry the .torrent")
	}
	want := []string{ls.srv.URL + "/cdn/e01.mkv -> Pack/a.mkv", ls.srv.URL + "/cdn/sample.mkv -> Pack/b.mkv"}
	if got := partURLs(parts); !slices.Equal(got, want) {
		t.Errorf("the engine got %v, want %v", got, want)
	}
	waitFor(t, func() bool { return slices.Equal(ls.deletes(), []string{"77"}) }, "the torrent deleted on Linksnappy")
}

func TestLinksnappyNeverDeletesATorrentTheAccountHeld(t *testing.T) {
	ls := newFakeLinksnappy(t)
	ls.add = `{"status":"OK","error":false,"return":[{"status":"FAILED","error":"This torrent already exists in your account","torrentid":"77"}]}`

	fetchTorrent(t, ls.client(), testMagnet, nil, core.StatusDone)

	if got := ls.deletes(); len(got) != 0 {
		t.Errorf("deleted %v, which the account had before", got)
	}
}

func TestLinksnappyHeldUploadIsTheAccountsToo(t *testing.T) {
	ls := newFakeLinksnappy(t)
	ls.upload = `{"Pack.torrent":{"error":"This torrent already exists in your account","torrentid":77}}`
	link, _ := builtTorrent(t, false)

	id, held, err := ls.client().AddTorrent(context.Background(), mustSource(t, link))

	if err != nil || id != "77" || !held {
		t.Errorf("AddTorrent = %q, %v, %v; want the account's torrent 77, held", id, held, err)
	}
}

func mustSource(t *testing.T, link string) TorrentSource {
	t.Helper()
	src, _, _, err := sourceOf(link)
	if err != nil {
		t.Fatal(err)
	}
	return src
}

func TestLinksnappyDecliningAMagnetHandsItOn(t *testing.T) {
	ls := newFakeLinksnappy(t)
	ls.add = `{"status":"OK","error":false,"return":[{"status":"FAILED","error":"You have reached your torrent limit.","torrentid":false}]}`

	_, u := fetchTorrent(t, ls.client(), testMagnet, nil, core.StatusError)

	if !u.Unsupported || u.Err != "linksnappy: You have reached your torrent limit." {
		t.Errorf("got %+v, want the task handed on with Linksnappy's reason", u)
	}
}

func TestLinksnappyGivingUpOnATorrentHandsItOnAndDeletesIt(t *testing.T) {
	ls := newFakeLinksnappy(t)
	ls.statuses = []string{`{"status":"ERROR","error":"No seeders found.","name":"Show","percentDone":0}`}

	_, u := fetchTorrent(t, ls.client(), testMagnet, nil, core.StatusError)

	if !u.Unsupported || u.Err != "linksnappy: No seeders found." {
		t.Errorf("got %+v, want the task handed on with Linksnappy's reason", u)
	}
	waitFor(t, func() bool { return slices.Equal(ls.deletes(), []string{"77"}) }, "the torrent deleted on Linksnappy")
}

func TestLinksnappyReadsAgainWhileAFinishedTorrentHasNoLinks(t *testing.T) {
	ls := newFakeLinksnappy(t)
	ls.files = `{}`

	_, err := ls.client().TorrentStatus(context.Background(), "77")

	if err == nil {
		t.Error("a finished torrent without download links read as ready")
	}
}

// A file keyed by its id rather than its name takes the name it is served
// under.
func TestLinksnappyNamesAFileKeyedByIDAfterItsDownload(t *testing.T) {
	ls := newFakeLinksnappy(t)
	ls.files = `{"5512":{"size":700,"downloadLink":"{srv}/torrents/9001/download"}}`
	l := ls.client()

	job, err := l.TorrentStatus(context.Background(), "77")
	if err != nil {
		t.Fatal(err)
	}
	if len(job.Files) != 1 || job.Files[0].Path != "" || job.Files[0].Size != 700 {
		t.Fatalf("files = %+v, want one file of 700 bytes and no path", job.Files)
	}
	d, err := l.FileURL(context.Background(), "77", job.Files[0])
	if err != nil {
		t.Fatal(err)
	}
	if d.URL != ls.srv.URL+"/cdn/e01.mkv" || d.Name != "e01.mkv" {
		t.Errorf("FileURL = %+v, want the address the link leads to and its name", d)
	}
}

func TestLinksnappySaysWhetherItHasATorrentCached(t *testing.T) {
	const hash = "0123456789abcdef0123456789abcdef01234567"
	for answer, want := range map[string]bool{`"CACHED"`: true, `"NOT CACHED"`: false, `false`: false} {
		ls := newFakeLinksnappy(t)
		ls.cached = answer
		got, err := ls.client().Cached(context.Background(), hash)
		if err != nil || got != want {
			t.Errorf("Cached = %v, %v for %s; want %v", got, err, answer, want)
		}
	}
}

func TestLinksnappyCachedOnlyAsksBeforeAdding(t *testing.T) {
	ls := newFakeLinksnappy(t)
	ls.cached = `"NOT CACHED"`
	b, up := newTestBackend(t, ls.client(), &partRecorder{})
	b.CachedOnly = onlyCached

	b.Download("t1", testMagnet, nil, 1)
	u := up.until(t, core.StatusError)

	if !u.Unsupported || !strings.Contains(u.Err, "this one has not cached it") {
		t.Errorf("got %+v, want the task handed on as not cached", u)
	}
	if n := ls.count("/api/torrents/ADDMAGNET"); n != 0 {
		t.Errorf("the torrent was added %d times", n)
	}
}

func TestLinksnappyTorrentErrorsCarryNoPassword(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	base := srv.URL
	srv.Close()
	l := NewLinksnappy("alice", "hunter2-secret")
	l.base = base + "/api"

	if _, _, err := l.AddTorrent(context.Background(), TorrentSource{Magnet: testMagnet}); err == nil {
		t.Error("a torrent was added on a closed server")
	} else if strings.Contains(err.Error(), "hunter2-secret") {
		t.Errorf("AddTorrent error = %q, which gives the password away", err)
	}
}
