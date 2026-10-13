package torbox

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"
	"time"

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
	if !slices.EqualFunc(got, want, func(a, b debrid.Listed) bool {
		return a.ID == b.ID && a.Name == b.Name && a.Size == b.Size && a.Hash == b.Hash && a.Added.Equal(b.Added)
	}) {
		t.Errorf("List = %+v, want %+v", got, want)
	}
}

// The debrid drive reads a web and a usenet download the way it reads a
// torrent, by the id List gave it.
func TestAWebAndAUsenetDownloadReadLikeATorrent(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		switch r.URL.Path {
		case "/api/webdl/mylist":
			if q.Get("id") != "8" {
				t.Errorf("mylist asked for %q", r.URL.RawQuery)
			}
			writeEnv(w, `{"id":8,"name":"file.zip","size":40,"download_present":true,"files":[{"id":3,"name":"file.zip","size":40}]}`)
		case "/api/webdl/requestdl":
			if q.Get("web_id") != "8" || q.Get("file_id") != "3" {
				t.Errorf("requestdl got %q", r.URL.RawQuery)
			}
			writeEnv(w, `"https://cdn.torbox.example/web"`)
		case "/api/usenet/mylist":
			if q.Get("id") != "9" {
				t.Errorf("mylist asked for %q", r.URL.RawQuery)
			}
			writeEnv(w, `{"id":9,"name":"Film","size":900,"download_present":true,"files":[{"id":0,"name":"Film/film.mkv","size":800}]}`)
		case "/api/usenet/requestdl":
			if q.Get("token") != "k" || q.Get("usenet_id") != "9" {
				t.Errorf("requestdl got %q", r.URL.RawQuery)
			}
			writeEnv(w, fmt.Sprintf(`"https://cdn.torbox.example/nzb/%s"`, q.Get("file_id")))
		default:
			t.Errorf("unexpected %s", r.URL.Path)
		}
	}))
	defer srv.Close()
	c := NewClient("k")
	c.base = srv.URL
	tb := NewTorrents(c)
	ctx := context.Background()

	for _, want := range []struct{ job, file, url string }{
		{"web/8", "file.zip", "https://cdn.torbox.example/web"},
		{"usenet/9", "Film/film.mkv", "https://cdn.torbox.example/nzb/0"},
	} {
		job, err := tb.TorrentStatus(ctx, want.job)
		if err != nil || job.State != debrid.TorrentReady || len(job.Files) != 1 || job.Files[0].Path != want.file {
			t.Errorf("TorrentStatus(%q) = %+v, %v", want.job, job, err)
			continue
		}
		d, err := tb.FileURL(ctx, want.job, job.Files[0])
		if err != nil || d.URL != want.url {
			t.Errorf("FileURL(%q) = %+v, %v; want %s", want.job, d, err, want.url)
		}
	}
}
