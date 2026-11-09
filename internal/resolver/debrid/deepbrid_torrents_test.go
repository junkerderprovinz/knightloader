package debrid

import (
	"bytes"
	"context"
	"errors"
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

// fakeDeepbrid answers the torrent calls with the bodies Deepbrid's API docs
// print. Its torrent links are short links on the API's own host that redirect
// to a download server elsewhere, which must never see the key.
type fakeDeepbrid struct {
	t   *testing.T
	srv *httptest.Server
	cdn *httptest.Server

	mu sync.Mutex
	// add answers /torrents/add and list the listing of every torrent.
	add  string
	list string
	// infos are the answers to /torrents/info?id= in turn, the last one
	// repeating. {srv} stands for the fake's own address.
	infos    []string
	reads    int
	magnet   string
	uploaded []byte
	deleted  []string
	// login sends the short links to the login page instead of the file.
	login bool
}

func newFakeDeepbrid(t *testing.T) *fakeDeepbrid {
	f := &fakeDeepbrid{
		t:    t,
		add:  `{"error":0,"message":"OK","id":"14648"}`,
		list: `{"error":1,"message":"No data"}`,
		infos: []string{`{"error":0,"message":"OK","id":"14648","filename":"Show","progress":100,"seeders":12,"speed":"0.00 MB/s",` +
			`"links":["{srv}/mytorrents?torrent=14648&file=Kx7mPq","{srv}/mytorrents?torrent=14648&file=Lm8nQr"],"status":"downloaded"}`},
	}
	f.srv = httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(f.srv.Close)
	f.cdn = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "" {
			t.Errorf("the download server got Authorization %q; the key stays with Deepbrid", got)
		}
		name := map[string]string{"/dl/a1": "e01.mkv", "/dl/b2": "e02.mkv"}[r.URL.Path]
		if name == "" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Disposition", `attachment; filename="`+name+`"`)
		w.Header().Set("Content-Length", "700")
	}))
	t.Cleanup(f.cdn.Close)
	return f
}

func (f *fakeDeepbrid) client() *Deepbrid {
	return deepbridAt(f.srv.URL + "/api/v1")
}

func (f *fakeDeepbrid) serve(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if got := r.Header.Get("Authorization"); got != "Bearer "+deepbridTestKey {
		f.t.Errorf("%s %s carried Authorization %q, want the key as bearer token", r.Method, r.URL.Path, got)
	}
	call := r.Method + " " + r.URL.Path
	switch {
	case call == "POST /api/v1/torrents/add":
		if file, _, err := r.FormFile("torrent_file"); err == nil {
			f.uploaded, _ = io.ReadAll(file)
		} else {
			f.magnet = r.FormValue("magnet")
		}
		fmt.Fprint(w, f.add)
	case call == "GET /api/v1/torrents/info" && r.URL.Query().Get("id") == "":
		fmt.Fprint(w, f.list)
	case call == "GET /api/v1/torrents/info":
		if id := r.URL.Query().Get("id"); id != "14648" {
			f.t.Errorf("read torrent %q", id)
		}
		fmt.Fprint(w, strings.ReplaceAll(f.infos[min(f.reads, len(f.infos)-1)], "{srv}", f.srv.URL))
		f.reads++
	case strings.HasPrefix(call, "DELETE /api/v1/torrents/delete/"):
		f.deleted = append(f.deleted, strings.TrimPrefix(r.URL.Path, "/api/v1/torrents/delete/"))
		fmt.Fprint(w, `{"error":0,"message":"OK","deleted":1,"ids":[14648],"not_found":[],"remote_failed":[]}`)
	case call == "GET /mytorrents":
		if f.login {
			http.Redirect(w, r, "/login", http.StatusFound)
			return
		}
		target := map[string]string{"Kx7mPq": "/dl/a1", "Lm8nQr": "/dl/b2", "Fresh9": "/dl/b2"}[r.URL.Query().Get("file")]
		// One hop on Deepbrid's own host first, as a short link that leads to
		// another of its pages would.
		http.Redirect(w, r, "/go?to="+target, http.StatusFound)
	case call == "GET /go":
		http.Redirect(w, r, f.cdn.URL+r.URL.Query().Get("to"), http.StatusFound)
	default:
		f.t.Errorf("unexpected %s", call)
		http.NotFound(w, r)
	}
}

func (f *fakeDeepbrid) deletes() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.deleted)
}

func TestDeepbridFetchesAMagnetFromAddToCleanup(t *testing.T) {
	db := newFakeDeepbrid(t)
	db.infos = append([]string{
		`{"error":0,"message":"OK","id":"14648","filename":"Show","progress":0,"seeders":0,"speed":"0.00 MB/s","links":[],"status":"magnet_conversion"}`,
		`{"error":0,"message":"OK","id":"14648","filename":"Show","progress":42,"seeders":3,"speed":"2.45 MB/s","links":[],"status":"downloading"}`,
	}, db.infos...)

	parts, _ := fetchTorrent(t, db.client(), testMagnet, nil, core.StatusDone)

	want := []string{db.cdn.URL + "/dl/a1 -> Show/e01.mkv", db.cdn.URL + "/dl/b2 -> Show/e02.mkv"}
	if got := partURLs(parts); !slices.Equal(got, want) {
		t.Errorf("the engine got %v, want %v", got, want)
	}
	if db.magnet != testMagnet {
		t.Errorf("magnet = %q", db.magnet)
	}
	waitFor(t, func() bool { return slices.Equal(db.deletes(), []string{"14648"}) }, "the torrent deleted on Deepbrid")
}

func TestDeepbridUploadsATorrentFile(t *testing.T) {
	db := newFakeDeepbrid(t)
	link, raw := builtTorrent(t, false)

	id, held, err := db.client().AddTorrent(context.Background(), mustSource(t, link))

	if err != nil || id != "14648" || held {
		t.Errorf("AddTorrent = %q, %v, %v; want the new torrent 14648", id, held, err)
	}
	if !bytes.Equal(db.uploaded, raw) {
		t.Error("the upload did not carry the .torrent")
	}
}

func TestDeepbridReadsTheIDOfAnAddWhereverItComes(t *testing.T) {
	for _, answer := range []string{
		`{"error":0,"id":14648}`,
		`{"error":0,"message":"OK","torrent":{"id":"14648"}}`,
	} {
		db := newFakeDeepbrid(t)
		db.add = answer

		id, _, err := db.client().AddTorrent(context.Background(), TorrentSource{Magnet: testMagnet})

		if err != nil || id != "14648" {
			t.Errorf("for %s AddTorrent = %q, %v; want 14648", answer, id, err)
		}
	}
}

func TestDeepbridAddWithoutAnIDIsAnError(t *testing.T) {
	db := newFakeDeepbrid(t)
	db.add = `{"error":0,"message":"OK"}`

	_, _, err := db.client().AddTorrent(context.Background(), TorrentSource{Magnet: testMagnet})

	if err == nil {
		t.Fatal("an add that names no torrent passed")
	}
}

func TestDeepbridTorrentTheAccountHadIsHeld(t *testing.T) {
	db := newFakeDeepbrid(t)
	db.list = `{"1":{"error":0,"id":"14648","filename":"Show","status":"downloaded","links":[]},` +
		`"2":{"error":0,"id":"14649","filename":"Other","status":"downloading","links":[]}}`

	fetchTorrent(t, db.client(), testMagnet, nil, core.StatusDone)

	if got := db.deletes(); len(got) != 0 {
		t.Errorf("deleted %v, which the account had before", got)
	}
}

func TestDeepbridPremiumOnlyRefusalHandsTheTorrentOn(t *testing.T) {
	db := newFakeDeepbrid(t)
	db.list = `{"error":2,"message":"Only premium users can use this feature."}`

	_, u := fetchTorrent(t, db.client(), testMagnet, nil, core.StatusError)

	if !u.Unsupported || !strings.Contains(u.Err, "premium") {
		t.Errorf("update = %+v, want the torrent handed on because the account is not premium", u)
	}
}

func TestDeepbridDecliningAnAddHandsTheTorrentOn(t *testing.T) {
	db := newFakeDeepbrid(t)
	db.add = `{"error":2,"message":"Only premium users can use this feature."}`

	_, _, err := db.client().AddTorrent(context.Background(), TorrentSource{Magnet: testMagnet})

	var no *Refusal
	if !errors.As(err, &no) {
		t.Errorf("err = %v, want a refusal", err)
	}
}

func TestDeepbridTorrentStates(t *testing.T) {
	tests := []struct {
		status string
		want   TorrentState
	}{
		{"queued", TorrentFetching},
		{"magnet_conversion", TorrentFetching},
		{"waiting_files_selection", TorrentFetching},
		{"downloading", TorrentFetching},
		{"compressing", TorrentFetching},
		{"uploading", TorrentFetching},
		{"downloaded", TorrentReady},
		{"error", TorrentFailed},
		{"dead", TorrentFailed},
		{"magnet_error", TorrentFailed},
		{"virus", TorrentFailed},
	}
	for _, tt := range tests {
		t.Run(tt.status, func(t *testing.T) {
			db := newFakeDeepbrid(t)
			db.infos = []string{`{"error":0,"message":"OK","id":"14648","filename":"Show","progress":10,` +
				`"links":["{srv}/mytorrents?torrent=14648&file=Kx7mPq"],"status":"` + tt.status + `"}`}

			job, err := db.client().TorrentStatus(context.Background(), "14648")

			if err != nil {
				t.Fatal(err)
			}
			if job.State != tt.want {
				t.Errorf("state = %v, want %v", job.State, tt.want)
			}
			if tt.want == TorrentFailed && job.Reason == "" {
				t.Error("a failed torrent names no reason")
			}
		})
	}
}

func TestDeepbridReadsTheStatusEvenWithAnErrorCode(t *testing.T) {
	db := newFakeDeepbrid(t)
	db.infos = []string{`{"error":1,"message":"Error docoding JSON","id":"14648","filename":"Show","status":"error"}`}

	job, err := db.client().TorrentStatus(context.Background(), "14648")

	if err != nil || job.State != TorrentFailed {
		t.Errorf("TorrentStatus = %+v, %v; want the torrent failed", job, err)
	}
}

func TestDeepbridShowsItsProgressSpeedAndSeeds(t *testing.T) {
	db := newFakeDeepbrid(t)
	db.infos = []string{`{"error":0,"message":"OK","id":"14648","filename":"Show","progress":42,"seeders":3,"speed":"2.45 MB/s","links":[],"status":"downloading"}`}

	job, err := db.client().TorrentStatus(context.Background(), "14648")

	if err != nil {
		t.Fatal(err)
	}
	if job.Name != "Show" || job.Progress != 0.42 || job.Seeds != 3 || job.Speed != 2569011 {
		t.Errorf("job = %+v, want Show at 42 percent, 3 seeds, 2.45 MB a second", job)
	}
}

func TestDeepbridListsAFileForEachLinkOnceDownloaded(t *testing.T) {
	db := newFakeDeepbrid(t)

	job, err := db.client().TorrentStatus(context.Background(), "14648")

	if err != nil {
		t.Fatal(err)
	}
	if job.State != TorrentReady || job.Progress != 1 || len(job.Files) != 2 {
		t.Fatalf("job = %+v, want ready with two files", job)
	}
	for _, f := range job.Files {
		if !f.Held || f.Path != "" {
			t.Errorf("file %+v; Deepbrid names no file, and every link is one it holds", f)
		}
	}
}

func TestDeepbridReadsAgainWhileADownloadedTorrentHasNoLinks(t *testing.T) {
	db := newFakeDeepbrid(t)
	db.infos = []string{`{"error":0,"message":"OK","id":"14648","filename":"Show","progress":100,"links":[],"status":"downloaded"}`}

	_, err := db.client().TorrentStatus(context.Background(), "14648")

	if err == nil {
		t.Error("a downloaded torrent without links counted as ready")
	}
}

func TestDeepbridTorrentWithoutDataIsGone(t *testing.T) {
	db := newFakeDeepbrid(t)
	db.infos = []string{`{"error":1,"message":"No data"}`}

	_, err := db.client().TorrentStatus(context.Background(), "14648")

	if !errors.Is(err, ErrJobGone) {
		t.Errorf("err = %v, want ErrJobGone", err)
	}
}

func TestDeepbridStatusOfAFreeAccountIsARefusal(t *testing.T) {
	db := newFakeDeepbrid(t)
	db.infos = []string{`{"error":2,"message":"Only premium users can use this feature."}`}

	_, err := db.client().TorrentStatus(context.Background(), "14648")

	var no *Refusal
	if !errors.As(err, &no) {
		t.Errorf("err = %v, want a refusal", err)
	}
}

func TestDeepbridFileURLAsksForTheLinkAfresh(t *testing.T) {
	db := newFakeDeepbrid(t)
	d := db.client()
	job, err := d.TorrentStatus(context.Background(), "14648")
	if err != nil {
		t.Fatal(err)
	}
	// The short link of the second file expired and Deepbrid issued another.
	db.infos = []string{`{"error":0,"message":"OK","id":"14648","filename":"Show","progress":100,` +
		`"links":["{srv}/mytorrents?torrent=14648&file=Kx7mPq","{srv}/mytorrents?torrent=14648&file=Fresh9"],"status":"downloaded"}`}
	db.reads = 0

	direct, err := d.FileURL(context.Background(), "14648", job.Files[1])

	if err != nil {
		t.Fatal(err)
	}
	if direct.URL != db.cdn.URL+"/dl/b2" || direct.Name != "e02.mkv" {
		t.Errorf("FileURL = %+v, want the download server's address and the name it serves", direct)
	}
	if db.reads != 1 {
		t.Errorf("read the torrent %d times, want once for the fresh link", db.reads)
	}
}

func TestDeepbridShortLinkToTheLoginPageIsARefusedKey(t *testing.T) {
	db := newFakeDeepbrid(t)
	db.login = true
	d := db.client()
	job, err := d.TorrentStatus(context.Background(), "14648")
	if err != nil {
		t.Fatal(err)
	}

	_, err = d.FileURL(context.Background(), "14648", job.Files[0])

	if err == nil || !strings.Contains(err.Error(), "key") {
		t.Errorf("err = %v, want the key named as refused", err)
	}
}

func TestDeepbridDeletesATorrentByItsID(t *testing.T) {
	db := newFakeDeepbrid(t)

	err := db.client().DeleteTorrent(context.Background(), "14648")

	if err != nil || !slices.Equal(db.deletes(), []string{"14648"}) {
		t.Errorf("DeleteTorrent = %v, deleted %v", err, db.deletes())
	}
}

func TestDeepbridDeletingATorrentThatIsGoneSucceeds(t *testing.T) {
	srv := deepbridServe(t, "/torrents/delete/14648", http.StatusOK, `{"error":1,"message":"Nothing matched","deleted":0,"ids":[],"not_found":[14648]}`)

	if err := deepbridAt(srv.URL).DeleteTorrent(context.Background(), "14648"); err != nil {
		t.Errorf("DeleteTorrent = %v; a torrent already gone is deleted", err)
	}
}
