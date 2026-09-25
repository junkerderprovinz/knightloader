package torbox

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/resolver/debrid"
)

func TestTorBoxListsItsTorrentsWebAndUsenetDownloads(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if q := r.URL.Query(); q.Get("bypass_cache") != "true" || q.Get("limit") != "1000" || q.Get("id") != "" {
			t.Errorf("%s asked with %q", r.URL.Path, r.URL.RawQuery)
		}
		switch r.URL.Path {
		case "/api/torrents/mylist":
			writeEnv(w, `[{"id":7,"hash":"ABCDEF","name":"Show","size":730,"created_at":"2026-09-25T10:00:00Z"}]`)
		case "/api/webdl/mylist":
			writeEnv(w, `[{"id":8,"hash":"tb-own-hash","name":"file.zip","size":40,"created_at":"2026-09-25T11:00:00Z"}]`)
		case "/api/usenet/mylist":
			writeEnv(w, `[{"id":9,"hash":"tb-own-hash","name":"Film","size":900,"created_at":"2026-09-25T12:00:00Z"}]`)
		default:
			t.Errorf("unexpected %s", r.URL.Path)
		}
	}))
	defer srv.Close()
	c := NewClient("k")
	c.base = srv.URL

	got, complete, err := NewTorrents(c).List(context.Background())
	if err != nil || !complete {
		t.Fatalf("List = %v, %v", complete, err)
	}
	at := func(h int) time.Time { return time.Date(2026, 9, 25, h, 0, 0, 0, time.UTC) }
	want := []debrid.Listed{
		{ID: "7", Name: "Show", Size: 730, Hash: "abcdef", Added: at(10)},
		{ID: "web/8", Name: "file.zip", Size: 40, Added: at(11)},
		{ID: "usenet/9", Name: "Film", Size: 900, Added: at(12)},
	}
	if id := UsenetJob("9"); id != want[2].ID {
		t.Errorf("UsenetJob names the usenet download %q, the list %q, so the import cannot see it was sent from here", id, want[2].ID)
	}
	if !slices.EqualFunc(got, want, func(a, b debrid.Listed) bool {
		return a.ID == b.ID && a.Name == b.Name && a.Size == b.Size && a.Hash == b.Hash && a.Added.Equal(b.Added)
	}) {
		t.Errorf("List = %+v, want %+v", got, want)
	}
}

// importedRun fetches one imported TorBox download through a torrent backend
// and waits for the task to settle.
func importedRun(t *testing.T, c *Client, job string) (*engineStub, core.Update) {
	t.Helper()
	eng := &engineStub{}
	settled := make(chan core.Update, 1)
	b := debrid.NewTorrentBackend(NewTorrents(c), NewBackend(c, nil, nil), eng, debrid.NewRuns("torbox"), func(_ string, u core.Update) {
		if u.Status == core.StatusDone || u.Status == core.StatusError {
			settled <- u
		}
	})
	b.Download("t1", debrid.JobLink("torbox", job), nil, 1)
	select {
	case u := <-settled:
		return eng, u
	case <-time.After(30 * time.Second):
		t.Fatal("the download never settled")
	}
	return nil, core.Update{}
}

func TestAnImportedWebDownloadIsFetchedAndDeletedAsAWebDownload(t *testing.T) {
	deleted := make(chan map[string]any, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/webdl/mylist":
			if r.URL.Query().Get("id") != "8" {
				t.Errorf("mylist asked for %q", r.URL.RawQuery)
			}
			writeEnv(w, `{"id":8,"name":"file.zip","size":40,"download_present":true,"files":[{"id":3,"name":"file.zip","size":40}]}`)
		case "/api/webdl/requestdl":
			if q := r.URL.Query(); q.Get("web_id") != "8" || q.Get("file_id") != "3" {
				t.Errorf("requestdl got %q", r.URL.RawQuery)
			}
			writeEnv(w, `"https://cdn.torbox.example/web"`)
		case "/api/webdl/controlwebdownload":
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			deleted <- body
			writeEnv(w, `null`)
		default:
			t.Errorf("unexpected %s; an imported download is never added again", r.URL.Path)
		}
	}))
	defer srv.Close()
	c := NewClient("k")
	c.base = srv.URL

	eng, u := importedRun(t, c, "web/8")
	if u.Status != core.StatusDone {
		t.Fatalf("settled as %+v", u)
	}
	if got := eng.parts(); !slices.Equal(got, []string{"https://cdn.torbox.example/web -> file.zip"}) {
		t.Errorf("the engine got %v", got)
	}
	select {
	case body := <-deleted:
		if body["operation"] != "delete" || body["webdl_id"] != float64(8) {
			t.Errorf("controlwebdownload got %v", body)
		}
	case <-time.After(5 * time.Second):
		t.Error("the finished web download was not deleted on TorBox")
	}
}

func TestAnImportedUsenetDownloadIsFetchedAndDeletedAsAUsenetDownload(t *testing.T) {
	deleted := make(chan map[string]any, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/usenet/mylist":
			if r.URL.Query().Get("id") != "9" {
				t.Errorf("mylist asked for %q", r.URL.RawQuery)
			}
			writeEnv(w, `{"id":9,"name":"Film","size":900,"download_present":true,"files":[`+
				`{"id":0,"name":"Film/film.mkv","size":800},{"id":1,"name":"Film/film.nfo","size":100}]}`)
		case "/api/usenet/requestdl":
			q := r.URL.Query()
			if q.Get("token") != "k" || q.Get("usenet_id") != "9" {
				t.Errorf("requestdl got %q", r.URL.RawQuery)
			}
			writeEnv(w, fmt.Sprintf(`"https://cdn.torbox.example/nzb/%s"`, q.Get("file_id")))
		case "/api/usenet/controlusenetdownload":
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			deleted <- body
			writeEnv(w, `null`)
		default:
			t.Errorf("unexpected %s; an imported download is never added again", r.URL.Path)
		}
	}))
	defer srv.Close()
	c := NewClient("k")
	c.base = srv.URL

	eng, u := importedRun(t, c, "usenet/9")
	if u.Status != core.StatusDone {
		t.Fatalf("settled as %+v", u)
	}
	want := []string{"https://cdn.torbox.example/nzb/0 -> Film/film.mkv", "https://cdn.torbox.example/nzb/1 -> Film/film.nfo"}
	if got := eng.parts(); !slices.Equal(got, want) {
		t.Errorf("the engine got %v, want %v", got, want)
	}
	select {
	case body := <-deleted:
		if body["operation"] != "delete" || body["usenet_id"] != float64(9) {
			t.Errorf("controlusenetdownload got %v", body)
		}
	case <-time.After(5 * time.Second):
		t.Error("the finished usenet download was not deleted on TorBox")
	}
}

func TestAWebDownloadThisInstanceStartsIsReportedUnderItsJobID(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/webdl/createwebdownload", func(w http.ResponseWriter, r *http.Request) {
		writeEnv(w, `{"webdownload_id":42}`)
	})
	mux.HandleFunc("/api/webdl/mylist", func(w http.ResponseWriter, r *http.Request) {
		writeEnv(w, `{"id":42,"name":"movie.mkv","download_present":true,"files":[{"id":7,"name":"movie.mkv","size":1000}]}`)
	})
	mux.HandleFunc("/api/webdl/requestdl", func(w http.ResponseWriter, r *http.Request) {
		writeEnv(w, `"https://cdn.torbox.app/dl/movie.mkv"`)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	c := NewClient("k")
	c.base = srv.URL

	created := make(chan string, 1)
	b := NewBackend(c, &fakeEngine{got: make(chan string, 1)}, func(string, core.Update) {})
	b.Created = func(job string) { created <- job }
	b.Download("t1", "https://rapidgator.net/file/abc", nil, 0)
	select {
	case job := <-created:
		if job != "web/42" {
			t.Errorf("reported %q, want the id the import lists the download under", job)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("the web download was never reported")
	}
}

func TestAnImportedDownloadIsClaimedOnlyByItsOwnTorBoxAccount(t *testing.T) {
	link := debrid.JobLink("torbox#work", "web/8")
	if !(Resolver{Account: "work"}).Match(link) {
		t.Error("the account the download is on does not claim it")
	}
	if (Resolver{Torrents: true}).Match(link) {
		t.Error("the default account claims a download on another account")
	}
}
