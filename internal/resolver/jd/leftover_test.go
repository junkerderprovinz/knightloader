package jd

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/core"
)

// fakeJDLeftover is a JD whose download list still holds a package an earlier
// attempt of task t1 left there, as JD keeps its list across restarts.
type fakeJDLeftover struct {
	t      *testing.T
	mu     sync.Mutex
	loaded int64 // what the leftover's one link has fetched
	gone   bool  // the leftover was removed
	calls  []string
}

func (f *fakeJDLeftover) log() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.calls)
}

func (f *fakeJDLeftover) handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		f.calls = append(f.calls, r.URL.Path)
		switch r.URL.Path {
		case "/downloadsV2/queryPackages":
			if f.gone {
				_, _ = w.Write([]byte(`{"data":[]}`))
				return
			}
			_, _ = w.Write([]byte(`{"data":[{"uuid":7,"name":"KL-t1"},{"uuid":8,"name":"his own package"}]}`))
		case "/downloadsV2/queryLinks":
			b, err := json.Marshal([]DownloadLink{{UUID: 100, Name: "a.rar", BytesTotal: 4096, BytesLoaded: f.loaded}})
			if err != nil {
				f.t.Error(err)
			}
			_, _ = w.Write([]byte(`{"data":` + string(b) + `}`))
		case "/downloadsV2/removeLinks":
			var params [][]int64
			decodeCallParams(f.t, r.URL.RawQuery, &params)
			if len(params) > 1 && slices.Contains(params[1], 7) {
				f.gone = true
			}
			if len(params) > 1 && slices.Contains(params[1], 8) {
				f.t.Error("the user's own package was removed from JD's download list")
			}
			_, _ = w.Write([]byte(`{"data":true}`))
		case "/linkgrabberv2/queryPackages":
			_, _ = w.Write([]byte(`{"data":[]}`))
		case "/linkgrabberv2/addLinks":
			_, _ = w.Write([]byte(`{"data":{"id":1}}`))
		default:
			_, _ = w.Write([]byte(`{"data":null}`))
		}
	})
}

// waitForCall polls the fake's log until path shows up.
func waitForCall(t *testing.T, f *fakeJDLeftover, path string) []string {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if log := f.log(); slices.Contains(log, path) {
			return log
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("JD was never asked for %s; calls: %v", path, f.log())
	return nil
}

// JD's duplicate check would hold a link that its own leftover package still
// carries, so the package goes before the link is added again.
func TestDownloadClearsALeftoverThatNeverLoadedAByte(t *testing.T) {
	f := &fakeJDLeftover{t: t}
	srv := httptest.NewServer(f.handler())
	defer srv.Close()

	b := NewBackend(srv.URL, func(string, core.Update) {})
	b.Download("t1", "https://rapidgator.example/file/aaa", nil, 0)
	defer b.Remove("t1", false)

	log := waitForCall(t, f, "/linkgrabberv2/addLinks")
	removed := slices.Index(log, "/downloadsV2/removeLinks")
	added := slices.Index(log, "/linkgrabberv2/addLinks")
	if removed < 0 || removed > added {
		t.Fatalf("calls %v: the leftover package was not removed before the link was added again", log)
	}
}

// A leftover that already fetched bytes is picked up where it stands, since
// removing it would throw away the partial file and adding the link again
// would fetch it twice.
func TestDownloadPicksUpALeftoverWithProgress(t *testing.T) {
	f := &fakeJDLeftover{t: t, loaded: 1024}
	srv := httptest.NewServer(f.handler())
	defer srv.Close()

	got := make(chan core.Update, 8)
	b := NewBackend(srv.URL, func(_ string, u core.Update) { got <- u })
	b.Download("t1", "https://rapidgator.example/file/aaa", nil, 0)

	select {
	case u := <-got:
		if u.Loaded != 1024 {
			t.Errorf("Loaded = %d, want the 1024 bytes the leftover holds", u.Loaded)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("nothing watches the picked-up package")
	}
	log := f.log()
	b.Remove("t1", false)
	if slices.Contains(log, "/linkgrabberv2/addLinks") {
		t.Errorf("calls %v: the link was added a second time beside a package that holds it", log)
	}
	if slices.Contains(log, "/downloadsV2/removeLinks") {
		t.Errorf("calls %v: a package with progress was removed", log)
	}
	if !slices.Contains(log, "/downloadsV2/setEnabled") {
		t.Errorf("calls %v: the picked-up package's links were not switched on", log)
	}
	// With no link added, addLinks' autostart never runs, and a JD that
	// restarted with every download paused keeps its controller stopped.
	if !slices.Contains(log, "/downloadcontroller/start") {
		t.Errorf("calls %v: JD's download controller was not started", log)
	}
}
