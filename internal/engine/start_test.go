package engine

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/collide"
	"github.com/junkerderprovinz/knightloader/internal/core"
)

// gatedOrigin serves body, but holds every request until open is called, so
// a start stays in its resolve for as long as a test needs.
type gatedOrigin struct {
	url    string
	gate   chan struct{}
	once   sync.Once
	served atomic.Int32
}

func newGatedOrigin(t *testing.T, body []byte) *gatedOrigin {
	t.Helper()
	o := &gatedOrigin{gate: make(chan struct{})}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-o.gate
		http.ServeContent(w, r, "f.bin", time.Time{}, bytes.NewReader(body))
		o.served.Add(1)
	}))
	t.Cleanup(srv.Close)
	t.Cleanup(o.open)
	o.url = srv.URL + "/f.bin"
	return o
}

func (o *gatedOrigin) open() { o.once.Do(func() { close(o.gate) }) }

// startResolving starts t1 against a gated origin and returns once the start
// is waiting on it.
func startResolving(t *testing.T, body []byte) (*Engine, *gatedOrigin, *updates, string) {
	t.Helper()
	o := newGatedOrigin(t, body)
	u := &updates{}
	dir := t.TempDir()
	e, err := New(dir, u.add)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { e.Close() })
	e.Start(Job{TaskID: "t1", URL: o.url, Conns: 1})
	waitUntil(t, "the start resolving", func() bool {
		e.mu.Lock()
		defer e.mu.Unlock()
		return e.starting["t1"] != nil
	})
	return e, o, u, dir
}

// startOver waits until the engine has nothing left of t1's start.
func startOver(t *testing.T, e *Engine) {
	t.Helper()
	waitUntil(t, "the start ending", func() bool {
		e.mu.Lock()
		defer e.mu.Unlock()
		return e.starting["t1"] == nil
	})
}

// A task removed while its link resolves never reaches the library, so no
// transfer runs on for a row that is gone.
func TestATaskRemovedWhileResolvingIsNeverStarted(t *testing.T) {
	e, o, u, dir := startResolving(t, bytes.Repeat([]byte("x"), 4096))

	e.Remove("t1", true)
	o.open()
	startOver(t, e)

	if n := len(e.d.GetTasks()); n != 0 {
		t.Errorf("the library holds %d task(s) for a removed start", n)
	}
	if _, err := os.Stat(filepath.Join(dir, "f.bin")); err == nil {
		t.Error("a removed start wrote its file")
	}
	if u.saw(core.StatusError) {
		t.Error("a removed start reported a failure")
	}
}

// A start begun again after a Remove takes the task's place, and the removed
// one still resolving does not take the new one's for its own.
func TestARemovedStartStaysRemovedWhenTheTaskStartsAgain(t *testing.T) {
	e, o, _, _ := startResolving(t, bytes.Repeat([]byte("x"), 4096))

	e.Remove("t1", true)
	again := newGatedOrigin(t, []byte("again"))
	e.Start(Job{TaskID: "t1", URL: again.url, Conns: 1})
	o.open()
	waitUntil(t, "the removed start's resolve being answered", func() bool { return o.served.Load() > 0 })
	time.Sleep(300 * time.Millisecond)

	if n := len(e.d.GetTasks()); n != 0 {
		t.Errorf("the library holds %d task(s) while the only live start still resolves", n)
	}

	// Ended here rather than by the cleanup, which would open the second
	// origin first and let a transfer write into the folder being removed.
	e.Remove("t1", true)
	again.open()
	startOver(t, e)
}

// A task paused while its link resolves stays out of the library until it is
// resumed, and then downloads.
func TestATaskPausedWhileResolvingWaitsForResume(t *testing.T) {
	if raceEnabled {
		t.Skip("gopeed v1.9.3 has internal data races in every real HTTP transfer; see startSlow")
	}
	body := bytes.Repeat([]byte("paused "), 1000)
	e, o, u, dir := startResolving(t, body)

	e.Pause("t1")
	o.open()
	startOver(t, e)

	if n := len(e.d.GetTasks()); n != 0 {
		t.Fatalf("the library holds %d task(s) for a paused start", n)
	}

	e.Resume("t1")
	waitUntil(t, "the resumed download finishing", func() bool {
		return u.last().Status == core.StatusDone
	})
	got, err := os.ReadFile(filepath.Join(dir, "f.bin"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, body) {
		t.Errorf("the resumed download wrote %d bytes, want %d", len(got), len(body))
	}
}

// endlessOrigin answers every request with a file too large to finish, sent
// slowly, and reports when the client hangs up.
type endlessOrigin struct {
	url  string
	gone chan struct{}
	once sync.Once
}

func newEndlessOrigin(t *testing.T) *endlessOrigin {
	t.Helper()
	o := &endlessOrigin{gone: make(chan struct{})}
	stop := make(chan struct{})
	const size = 1 << 30
	chunk := make([]byte, 32<<10)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Accept-Ranges", "bytes")
		w.Header().Set("Content-Length", strconv.Itoa(size))
		if r.Header.Get("Range") != "" {
			w.Header().Set("Content-Range", fmt.Sprintf("bytes 0-%d/%d", size-1, size))
			w.WriteHeader(http.StatusPartialContent)
		}
		defer o.once.Do(func() { close(o.gone) })
		for {
			if _, err := w.Write(chunk); err != nil {
				return
			}
			w.(http.Flusher).Flush()
			select {
			case <-r.Context().Done():
				return
			case <-stop:
				return
			case <-time.After(5 * time.Millisecond):
			}
		}
	}))
	t.Cleanup(srv.Close)
	t.Cleanup(func() { close(stop) })
	o.url = srv.URL + "/f.bin"
	return o
}

// A start that ends before its task is created, or is paused while it waits
// to hear of further sources, stops reading the answer to its resolve, which
// the library would otherwise copy to the temp folder to the end of the file.
func TestAStartThatNeverCreatesItsTaskStopsFetchingTheFile(t *testing.T) {
	for _, how := range []string{"pause", "remove", "skip"} {
		t.Run(how, func(t *testing.T) {
			smallMultiSource(t)
			tmp := t.TempDir()
			t.Setenv("TMP", tmp)
			t.Setenv("TEMP", tmp)
			t.Setenv("TMPDIR", tmp)
			o := newEndlessOrigin(t)
			dir := t.TempDir()
			e, err := New(dir, nil)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = e.Close() })

			asked, release := make(chan struct{}), make(chan struct{})
			defer close(release)
			j := Job{TaskID: "t1", URL: o.url, Conns: 1, Sources: func(ctx context.Context) []string {
				close(asked)
				select {
				case <-release:
				case <-ctx.Done():
				}
				return nil
			}}
			if how == "skip" {
				if err := os.WriteFile(filepath.Join(dir, "f.bin"), []byte("mine"), 0o600); err != nil {
					t.Fatal(err)
				}
				j.Collision = collide.Skip
			}
			e.Start(j)
			switch how {
			case "pause":
				<-asked
				e.Pause("t1")
			case "remove":
				<-asked
				e.Remove("t1", true)
			}

			select {
			case <-o.gone:
			case <-time.After(10 * time.Second):
				t.Fatal("the origin is still sending the file after the start ended")
			}
			startOver(t, e)
			waitUntil(t, "the temp folder emptying", func() bool {
				left, _ := os.ReadDir(tmp)
				return len(left) == 0
			})
		})
	}
}

// A start paused and resumed while it waits to hear of further sources
// resolves its link again, since the pause closed the first resolve, and
// downloads the file.
func TestAStartPausedAndResumedWhileAskingForSourcesDownloads(t *testing.T) {
	if raceEnabled {
		t.Skip("gopeed v1.9.3 has internal data races in every real HTTP transfer; see startSlow")
	}
	smallMultiSource(t)
	body := bytes.Repeat([]byte("resumed "), 256<<10)
	cut := make(chan struct{})
	var cutOnce sync.Once
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.ServeContent(pacedWriter{w, r}, r, "f.bin", time.Time{}, bytes.NewReader(body))
		if r.Context().Err() != nil {
			cutOnce.Do(func() { close(cut) })
		}
	}))
	t.Cleanup(srv.Close)
	u := &updates{}
	dir := t.TempDir()
	e, err := New(dir, u.add)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = e.Close() })

	// An unlock that answers late whatever happens to the start.
	asked, release := make(chan struct{}), make(chan struct{})
	var askedOnce sync.Once
	e.Start(Job{TaskID: "t1", URL: srv.URL + "/f.bin", Conns: 1, Sources: func(context.Context) []string {
		askedOnce.Do(func() { close(asked) })
		<-release
		return nil
	}})
	<-asked
	e.Pause("t1")
	select {
	case <-cut:
	case <-time.After(10 * time.Second):
		close(release)
		t.Fatal("the pause left the resolve reading the file")
	}
	e.Resume("t1")
	close(release)

	waitUntil(t, "the resumed start ending", func() bool {
		st := u.last().Status
		return st == core.StatusDone || st == core.StatusError
	})
	if last := u.last(); last.Status != core.StatusDone {
		t.Fatalf("the resumed start ended with %+v", last)
	}
	got, err := os.ReadFile(filepath.Join(dir, "f.bin"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, body) {
		t.Errorf("the resumed download wrote %d bytes, want %d", len(got), len(body))
	}
}
