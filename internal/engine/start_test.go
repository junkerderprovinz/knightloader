package engine

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

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
