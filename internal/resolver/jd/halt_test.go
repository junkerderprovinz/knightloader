package jd

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/core"
)

// fakeHaltJD is one package, KL-t1, with one link that goes on running for a
// few questions after it was disabled, the way JD finishes its current write
// before it lets go. It records every call by path.
type fakeHaltJD struct {
	mu      sync.Mutex
	calls   []string
	queries []string
	running bool
	// lingers is how many more link queries still find it running after the
	// disable.
	lingers int
}

func (f *fakeHaltJD) handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		f.calls = append(f.calls, r.URL.Path)
		switch r.URL.Path {
		case "/downloadsV2/queryPackages":
			_, _ = w.Write([]byte(`{"data":[{"uuid":9,"name":"KL-t1"}]}`))
		case "/downloadsV2/queryLinks":
			f.queries = append(f.queries, r.URL.RawQuery)
			running := f.running
			if !f.running && f.lingers > 0 {
				f.lingers--
				running = true
			}
			flag := ""
			if running {
				flag = `,"running":true`
			}
			_, _ = w.Write([]byte(`{"data":[{"uuid":1,"name":"a.bin","bytesTotal":100,"bytesLoaded":40` + flag + `}]}`))
		case "/downloadsV2/setEnabled":
			if strings.HasPrefix(r.URL.RawQuery, "false") {
				f.running = false
			}
			_, _ = w.Write([]byte(`{"data":""}`))
		default:
			_, _ = w.Write([]byte(`{"data":""}`))
		}
	})
}

func (f *fakeHaltJD) called(path string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, c := range f.calls {
		if c == path {
			n++
		}
	}
	return n
}

// watched puts a poller on t1, as a running download has.
func watched(t *testing.T, b *Backend) {
	t.Helper()
	b.Resume("t1")
	deadline := time.Now().Add(4 * time.Second)
	for time.Now().Before(deadline) {
		b.mu.Lock()
		_, ok := b.stop["t1"]
		b.mu.Unlock()
		if ok {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("no poller started")
}

// Halt returns only once JD has stopped writing, and the app hears of no
// pause.
func TestHaltWaitsUntilJDHasStoppedWriting(t *testing.T) {
	fake := &fakeHaltJD{running: true, lingers: 3}
	srv := httptest.NewServer(fake.handler())
	defer srv.Close()

	var mu sync.Mutex
	var paused bool
	b := NewBackend(srv.URL, func(_ string, u core.Update) {
		if u.Status == core.StatusPaused {
			mu.Lock()
			paused = true
			mu.Unlock()
		}
	})
	watched(t, b)
	defer b.Remove("t1", false)

	if !b.Halt("t1") {
		t.Fatal("Halt = false for a task with a poller")
	}
	fake.mu.Lock()
	lingers := fake.lingers
	fake.mu.Unlock()
	if lingers != 0 {
		t.Errorf("Halt returned with JD still writing (%d more running answers due)", lingers)
	}
	if fake.called("/downloadsV2/setEnabled") == 0 {
		t.Error("the links were never disabled")
	}
	b.mu.Lock()
	_, polling := b.stop["t1"]
	b.mu.Unlock()
	if polling {
		t.Error("the poller is still running after Halt")
	}
	mu.Lock()
	defer mu.Unlock()
	if paused {
		t.Error("Halt reported a pause; the app must not hear of one")
	}
}

// A package pinned to its folder while JD was already writing keeps the file
// in a folder of its own until JD has stopped and moved it into place, so
// Halt waits for that folder to go.
func TestHaltWaitsForJDToPutTheFileInPlace(t *testing.T) {
	dir := t.TempDir()
	own := filepath.Join(dir, "KL-t1")
	if err := os.MkdirAll(own, 0o755); err != nil {
		t.Fatal(err)
	}
	moved := time.AfterFunc(4*haltPoll, func() { _ = os.RemoveAll(own) })
	defer moved.Stop()
	fake := &fakeHaltJD{running: true}
	srv := httptest.NewServer(fake.handler())
	defer srv.Close()

	b := NewBackend(srv.URL, func(string, core.Update) {})
	b.Dir = func(string) string { return dir }
	watched(t, b)
	defer b.Remove("t1", false)

	if !b.Halt("t1") {
		t.Fatal("Halt = false for a task with a poller")
	}
	if _, err := os.Lstat(own); err == nil {
		t.Error("Halt returned while JD still had its own folder, before it put the file in place")
	}
}

// A task nobody follows has nothing for Halt to stop, and JD is not touched.
func TestHaltLeavesATaskNobodyFollows(t *testing.T) {
	fake := &fakeHaltJD{}
	srv := httptest.NewServer(fake.handler())
	defer srv.Close()

	b := NewBackend(srv.URL, func(string, core.Update) {})
	if b.Halt("t1") {
		t.Error("Halt = true for a task with no poller")
	}
	if n := fake.called("/downloadsV2/setEnabled"); n != 0 {
		t.Errorf("setEnabled called %d times for a task nobody follows", n)
	}
}

// MoveTo hands JD the new folder for the task's own package, so JD moves what
// it wrote there itself.
func TestMoveToPointsTheTasksPackageAtTheFolder(t *testing.T) {
	fake := &fakeFolderJD{adds: []string{"added"}}
	srv := httptest.NewServer(fake.handler())
	defer srv.Close()

	b := NewBackend(srv.URL, func(string, core.Update) {})
	if err := b.MoveTo("t1", "/data/download/Neuer Name"); err != nil {
		t.Fatalf("MoveTo: %v", err)
	}
	_, _, dirs := fake.snapshot()
	if len(dirs) != 1 {
		t.Fatalf("setDownloadDirectory calls = %d, want 1", len(dirs))
	}
	got := decoded(dirs[0])
	if !strings.Contains(got, "/data/download/Neuer Name") || !strings.Contains(got, "[9]") {
		t.Errorf("setDownloadDirectory sent %q, want the folder and the package 9", got)
	}
}

// JD stops its download controller once no enabled link is left, so enabling
// the links alone leaves them standing.
func TestResumeStartsJDsDownloadController(t *testing.T) {
	fake := &fakeHaltJD{}
	srv := httptest.NewServer(fake.handler())
	defer srv.Close()

	b := NewBackend(srv.URL, func(string, core.Update) {})
	b.Resume("t1")
	defer b.Remove("t1", false)
	if n := fake.called("/downloadcontroller/start"); n == 0 {
		t.Error("Resume did not start JD's download controller")
	}
}
