package torbox

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/core"
)

// fakeEngine captures the direct URL the backend hands off for download, and
// the way back to a fresh one.
type fakeEngine struct {
	got    chan string
	relink func(context.Context) (string, error)
}

func (f *fakeEngine) Handover(_, url string, _ int, relink func(context.Context) (string, error)) {
	f.relink = relink
	f.got <- url
}
func (f *fakeEngine) Pause(string)        {}
func (f *fakeEngine) Resume(string)       {}
func (f *fakeEngine) Remove(string, bool) {}

func writeEnv(w http.ResponseWriter, data string) {
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(`{"success":true,"error":null,"detail":"ok","data":` + data + `}`))
}

// TestBackendUnlockFlow drives the full debrid path against a mock TorBox:
// createwebdownload -> mylist(present+files) -> requestdl -> CDN URL -> engine.
func TestBackendUnlockFlow(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/webdl/createwebdownload", func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer test-key" {
			t.Errorf("auth header = %q, want Bearer test-key", got)
		}
		_ = r.ParseForm()
		if got := r.FormValue("link"); got != "https://rapidgator.net/file/abc" {
			t.Errorf("link = %q, not forwarded", got)
		}
		writeEnv(w, `{"webdownload_id":42,"hash":"h"}`)
	})
	mux.HandleFunc("/api/webdl/mylist", func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("id"); got != "42" {
			t.Errorf("mylist id = %q, want 42 (query must be scoped)", got)
		}
		writeEnv(w, `{"id":42,"name":"movie.mkv","size":1000,"download_present":true,`+
			`"files":[{"id":7,"name":"movie.mkv","size":1000}]}`)
	})
	mux.HandleFunc("/api/webdl/requestdl", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if q.Get("token") != "test-key" || q.Get("web_id") != "42" || q.Get("file_id") != "7" {
			t.Errorf("requestdl params = %v, want token/web_id/file_id set", q)
		}
		writeEnv(w, `"https://cdn.torbox.app/dl/movie.mkv"`)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	c := NewClient("test-key")
	c.base = srv.URL

	fe := &fakeEngine{got: make(chan string, 1)}
	var mu sync.Mutex
	var names []string
	onUpdate := func(_ string, u core.Update) {
		if u.Name != "" {
			mu.Lock()
			names = append(names, u.Name)
			mu.Unlock()
		}
	}

	b := NewBackend(c, fe, onUpdate)
	b.Download("task1", "https://rapidgator.net/file/abc", nil, 0)

	select {
	case url := <-fe.got:
		if url != "https://cdn.torbox.app/dl/movie.mkv" {
			t.Fatalf("engine handed %q, want the CDN URL from requestdl", url)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("engine.Download was never called")
	}

	mu.Lock()
	defer mu.Unlock()
	var sawFile bool
	for _, n := range names {
		if n == "movie.mkv" {
			sawFile = true
		}
	}
	if !sawFile {
		t.Errorf("file name never mirrored into the task; names = %v", names)
	}
}

// TorBox's download link can expire before the file is complete. The engine
// gets a way back to TorBox, which hands out a fresh link to the same file of
// the same job.
func TestTheEngineCanAskTorBoxForAFreshLink(t *testing.T) {
	var mu sync.Mutex
	requests := 0
	mux := http.NewServeMux()
	mux.HandleFunc("/api/webdl/createwebdownload", func(w http.ResponseWriter, r *http.Request) {
		writeEnv(w, `{"webdownload_id":42}`)
	})
	mux.HandleFunc("/api/webdl/mylist", func(w http.ResponseWriter, r *http.Request) {
		writeEnv(w, `{"id":42,"name":"movie.mkv","size":1000,"download_present":true,"files":[{"id":7,"name":"movie.mkv","size":1000}]}`)
	})
	mux.HandleFunc("/api/webdl/requestdl", func(w http.ResponseWriter, r *http.Request) {
		if q := r.URL.Query(); q.Get("web_id") != "42" || q.Get("file_id") != "7" {
			t.Errorf("requestdl asked for %v, want the job's own file", q)
		}
		mu.Lock()
		requests++
		n := requests
		mu.Unlock()
		writeEnv(w, `"https://cdn.torbox.app/dl/`+strconv.Itoa(n)+`"`)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	c := NewClient("test-key")
	c.base = srv.URL
	fe := &fakeEngine{got: make(chan string, 1)}
	b := NewBackend(c, fe, func(string, core.Update) {})
	b.Download("t1", "https://rapidgator.net/file/abc", nil, 0)

	select {
	case <-fe.got:
	case <-time.After(10 * time.Second):
		t.Fatal("the link was never handed over")
	}
	if fe.relink == nil {
		t.Fatal("the engine was given no way to a fresh link")
	}
	url, err := fe.relink(context.Background())
	if err != nil || url != "https://cdn.torbox.app/dl/2" {
		t.Errorf("relink = %q, %v; want the second requestdl's link", url, err)
	}
}
