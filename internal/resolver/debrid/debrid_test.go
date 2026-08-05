package debrid

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/core"
)

// fakeEngine captures the direct URL a backend hands off.
type fakeEngine struct{ got chan string }

func (f *fakeEngine) Download(_, url string, _ map[string]string, _ int) { f.got <- url }
func (f *fakeEngine) Pause(string)                                       {}
func (f *fakeEngine) Resume(string)                                      {}
func (f *fakeEngine) Remove(string, bool)                                {}

// TestAllDebrid drives the AllDebrid client against payloads shaped like the
// live API (verified against api.alldebrid.com/v4).
func TestAllDebrid(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v4/hosts", func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("agent"); got != "knightloader" {
			t.Errorf("agent = %q, want knightloader", got)
		}
		_, _ = w.Write([]byte(`{"status":"success","data":{"hosts":{
			"1fichier":{"name":"1fichier","domains":["1fichier.com","dl4free.com"]},
			"rapidgator":{"name":"Rapidgator","domains":["rapidgator.net","RG.TO"]}}}}`))
	})
	mux.HandleFunc("/v4/link/unlock", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if q.Get("apikey") != "AD-KEY" || q.Get("link") != "https://rapidgator.net/file/x" {
			t.Errorf("unlock params = %v", q)
		}
		_, _ = w.Write([]byte(`{"status":"success","data":{"link":"https://cdn.alldebrid.com/dl/movie.mkv",
			"filename":"movie.mkv","filesize":4096}}`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	ad := NewAllDebrid("AD-KEY")
	ad.base = srv.URL + "/v4"

	hosts, err := ad.Hosts(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"1fichier.com", "dl4free.com", "rapidgator.net", "rg.to"} {
		if !hosts[want] {
			t.Errorf("host set missing %q (got %v)", want, hosts)
		}
	}

	d, err := ad.Unlock(context.Background(), "https://rapidgator.net/file/x")
	if err != nil {
		t.Fatal(err)
	}
	if d.URL != "https://cdn.alldebrid.com/dl/movie.mkv" || d.Name != "movie.mkv" || d.Size != 4096 {
		t.Fatalf("unlock = %+v", d)
	}

	// The error envelope must surface as a Go error, not a silent empty result.
	mux.HandleFunc("/v4/link/unlock/bad", func(w http.ResponseWriter, r *http.Request) {})
	adErr := NewAllDebrid("")
	adErr.base = srv.URL + "/v4"
	errSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"status":"error","error":{"code":"AUTH_BAD_APIKEY","message":"The auth apikey is invalid"}}`))
	}))
	defer errSrv.Close()
	adErr.base = errSrv.URL
	if _, err := adErr.Unlock(context.Background(), "https://x.example/f"); err == nil {
		t.Fatal("bad apikey did not produce an error")
	}
}

// TestRealDebrid drives the Real-Debrid client against payloads shaped like the
// live REST 1.0 API (verified against api.real-debrid.com).
func TestRealDebrid(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/hosts/domains", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`["1fichier.com","rapidgator.net","Uploaded.net"]`))
	})
	mux.HandleFunc("/unrestrict/link", func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer RD-TOKEN" {
			t.Errorf("auth = %q", got)
		}
		_ = r.ParseForm()
		if got := r.FormValue("link"); got != "https://rapidgator.net/file/y" {
			t.Errorf("link = %q", got)
		}
		_, _ = w.Write([]byte(`{"id":"ABC","filename":"show.mkv","filesize":2048,
			"host":"rapidgator.net","download":"https://cdn.real-debrid.com/d/ABC/show.mkv"}`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	rd := NewRealDebrid("RD-TOKEN")
	rd.base = srv.URL

	hosts, err := rd.Hosts(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !hosts["rapidgator.net"] || !hosts["uploaded.net"] {
		t.Fatalf("host set = %v", hosts)
	}

	d, err := rd.Unlock(context.Background(), "https://rapidgator.net/file/y")
	if err != nil {
		t.Fatal(err)
	}
	if d.URL != "https://cdn.real-debrid.com/d/ABC/show.mkv" || d.Name != "show.mkv" || d.Size != 2048 {
		t.Fatalf("unlock = %+v", d)
	}
}

// TestBackendHandsOffToEngine pins the shared backend: unlock, then the engine
// gets the direct URL and the task carries the resolved name/size.
func TestBackendHandsOffToEngine(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"id":"1","filename":"f.bin","filesize":99,"download":"https://cdn.example/f.bin"}`))
	}))
	defer srv.Close()
	rd := NewRealDebrid("T")
	rd.base = srv.URL

	fe := &fakeEngine{got: make(chan string, 1)}
	names := make(chan string, 8)
	b := NewBackend(rd, fe, func(_ string, u core.Update) {
		if u.Name != "" {
			names <- u.Name
		}
	})
	b.Download("t1", "https://rapidgator.net/file/z", nil, 0)

	select {
	case url := <-fe.got:
		if url != "https://cdn.example/f.bin" {
			t.Fatalf("engine got %q", url)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("engine.Download never called")
	}
	var sawName bool
	for len(names) > 0 {
		if <-names == "f.bin" {
			sawName = true
		}
	}
	if !sawName {
		t.Error("resolved filename was never mirrored to the task")
	}
}

func TestHostInSet(t *testing.T) {
	set := map[string]bool{"rapidgator.net": true}
	for _, h := range []string{"rapidgator.net", "www.rapidgator.net", "dl5.rapidgator.net"} {
		if !HostInSet(h, set) {
			t.Errorf("HostInSet(%q) = false, want true", h)
		}
	}
	if HostInSet("example.com", set) {
		t.Error("unrelated host matched")
	}
}
