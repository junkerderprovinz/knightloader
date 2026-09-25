package engine

import (
	"bytes"
	cryptorand "crypto/rand"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/core"
)

// slowOrigin serves one file with ranges at a pace a test can act in the
// middle of, and records the Range of every request it answers.
type slowOrigin struct {
	srv  *httptest.Server
	data []byte

	mu     sync.Mutex
	ranges []string
}

func newSlowOrigin(t *testing.T, size int) *slowOrigin {
	t.Helper()
	o := &slowOrigin{data: make([]byte, size)}
	_, _ = cryptorand.Read(o.data)
	o.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		o.mu.Lock()
		o.ranges = append(o.ranges, r.Header.Get("Range"))
		o.mu.Unlock()
		lo, hi := 0, len(o.data)-1
		if rg, ok := strings.CutPrefix(r.Header.Get("Range"), "bytes="); ok {
			from, to, _ := strings.Cut(rg, "-")
			lo, _ = strconv.Atoi(from)
			if to != "" {
				hi, _ = strconv.Atoi(to)
			}
			w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", lo, hi, len(o.data)))
		}
		w.Header().Set("Accept-Ranges", "bytes")
		w.Header().Set("Content-Length", strconv.Itoa(hi-lo+1))
		if r.Header.Get("Range") != "" {
			w.WriteHeader(http.StatusPartialContent)
		}
		for off := lo; off <= hi; off += 32 << 10 {
			if _, err := w.Write(o.data[off:min(off+32<<10, hi+1)]); err != nil {
				return
			}
			w.(http.Flusher).Flush()
			time.Sleep(20 * time.Millisecond)
		}
	}))
	t.Cleanup(o.srv.Close)
	return o
}

func (o *slowOrigin) requests() int {
	o.mu.Lock()
	defer o.mu.Unlock()
	return len(o.ranges)
}

// updates keeps everything the engine reported for one task.
type updates struct {
	mu   sync.Mutex
	list []core.Update
}

func (u *updates) add(_ string, up core.Update) {
	u.mu.Lock()
	u.list = append(u.list, up)
	u.mu.Unlock()
}

func (u *updates) last() core.Update {
	u.mu.Lock()
	defer u.mu.Unlock()
	if len(u.list) == 0 {
		return core.Update{}
	}
	return u.list[len(u.list)-1]
}

func (u *updates) loaded() int64 {
	u.mu.Lock()
	defer u.mu.Unlock()
	var n int64
	for _, up := range u.list {
		n = max(n, up.Loaded)
	}
	return n
}

func (u *updates) saw(s core.Status) bool {
	u.mu.Lock()
	defer u.mu.Unlock()
	for _, up := range u.list {
		if up.Status == s {
			return true
		}
	}
	return false
}

func waitUntil(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("%s never happened", what)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// startSlow starts a download of a fresh slowOrigin and waits for its first
// megabyte.
func startSlow(t *testing.T) (*Engine, *slowOrigin, *updates, string) {
	t.Helper()
	if raceEnabled {
		// gopeed v1.9.3 writes a running task's status, progress and timer on
		// its own goroutines while it hands the same task to its listener and
		// clones it to JSON, all without a lock, so every real HTTP transfer
		// trips -race inside the library. The run without -race covers this.
		t.Skip("gopeed v1.9.3 has internal data races in every real HTTP transfer; see comment")
	}
	o := newSlowOrigin(t, 16<<20)
	u := &updates{}
	dir := t.TempDir()
	e, err := New(dir, u.add)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = e.Close() })
	e.Start(Job{TaskID: "t1", URL: o.srv.URL + "/f.bin", Conns: 4, Dir: dir})
	waitUntil(t, "the first megabyte", func() bool { return u.loaded() >= 1<<20 })
	return e, o, u, dir
}

// A reconnect asks for the rest of the file on new connections, keeps what was
// written, and does not tell the app about the pause it is made of.
func TestReconnectKeepsTheBytesAndReportsNoPause(t *testing.T) {
	e, o, u, dir := startSlow(t)
	before := o.requests()
	if !e.Reconnectable("t1") {
		t.Fatal("a running HTTP download is not reconnectable")
	}
	if !e.Reconnect("t1") {
		t.Fatal("Reconnect found nothing to reconnect")
	}
	waitUntil(t, "the download finishing", func() bool { return u.last().Status == core.StatusDone })

	if u.saw(core.StatusPaused) {
		t.Error("the app was told the task paused")
	}
	o.mu.Lock()
	after := o.ranges[before:]
	o.mu.Unlock()
	if len(after) == 0 {
		t.Error("no new request after the reconnect")
	}
	for _, rg := range after {
		if rg == "" || strings.HasPrefix(rg, "bytes=0-") {
			t.Errorf("after the reconnect the file was asked for from the start (Range %q)", rg)
		}
	}
	got, err := os.ReadFile(filepath.Join(dir, "f.bin"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, o.data) {
		t.Errorf("the finished file (%d bytes) is not what was served (%d bytes)", len(got), len(o.data))
	}
}

// A pause that comes in while a reconnect is under way is the last word: the
// transfer stops and stays stopped, and the app hears of the pause.
func TestAPauseDuringAReconnectStaysPaused(t *testing.T) {
	e, _, u, _ := startSlow(t)
	done := make(chan struct{})
	go func() {
		e.Reconnect("t1")
		close(done)
	}()
	e.Pause("t1")
	<-done

	waitUntil(t, "the pause reaching the app", func() bool { return u.saw(core.StatusPaused) })
	time.Sleep(time.Second)
	held := u.loaded()
	time.Sleep(2 * time.Second)
	if moved := u.loaded() - held; moved > 0 {
		t.Errorf("%d bytes moved after the pause; the reconnect resumed a paused task", moved)
	}
	if u.last().Status == core.StatusDone {
		t.Error("a paused task ran to the end")
	}
}

// gate records what the engine reports and, once armed, holds the next
// progress report until it is opened. The library reports progress with the
// task's status lock held, so while a report is held every pause and resume
// of the task waits at that lock.
type gate struct {
	updates
	armed              atomic.Bool
	held, shut         chan struct{}
	heldOnce, openOnce sync.Once
}

func newGate() *gate {
	return &gate{held: make(chan struct{}), shut: make(chan struct{})}
}

func (g *gate) report(id string, up core.Update) {
	g.add(id, up)
	if !g.armed.Load() || up.Status != core.StatusRunning {
		return
	}
	g.heldOnce.Do(func() { close(g.held) })
	<-g.shut
}

func (g *gate) open() { g.openOnce.Do(func() { close(g.shut) }) }

// A pause that has already looked for a reconnect and found none is not
// undone by one that starts right after, whichever of the two reaches the
// library first.
func TestAPauseUnderWayIsNotUndoneByAReconnect(t *testing.T) {
	if raceEnabled {
		t.Skip("gopeed v1.9.3 has internal data races in every real HTTP transfer; see startSlow")
	}
	o := newSlowOrigin(t, 16<<20)
	g := newGate()
	dir := t.TempDir()
	e, err := New(dir, g.report)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = e.Close() })
	t.Cleanup(g.open)
	e.Start(Job{TaskID: "t1", URL: o.srv.URL + "/f.bin", Conns: 4, Dir: dir})
	waitUntil(t, "the first megabyte", func() bool { return g.loaded() >= 1<<20 })

	g.armed.Store(true)
	select {
	case <-g.held:
	case <-time.After(5 * time.Second):
		t.Fatal("no progress report to hold")
	}
	paused := make(chan struct{})
	go func() {
		e.Pause("t1")
		close(paused)
	}()
	// Long enough for the pause to look and reach the library's lock.
	time.Sleep(200 * time.Millisecond)
	reconnected := make(chan struct{})
	go func() {
		e.Reconnect("t1")
		close(reconnected)
	}()
	time.Sleep(200 * time.Millisecond)
	g.open()
	<-paused
	<-reconnected

	time.Sleep(time.Second)
	held := g.loaded()
	time.Sleep(2 * time.Second)
	if moved := g.loaded() - held; moved > 0 {
		t.Errorf("%d bytes moved after the pause; the reconnect resumed a paused task", moved)
	}
	if g.last().Status == core.StatusDone {
		t.Error("a paused task ran to the end")
	}
}

// Nothing the engine does not fetch over HTTP is reconnected.
func TestReconnectLeavesWhatItDoesNotFetch(t *testing.T) {
	e, err := New(t.TempDir(), func(string, core.Update) {})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = e.Close() })
	if e.Reconnectable("unknown") || e.Reconnect("unknown") {
		t.Error("a task the engine never started was reconnected")
	}
	e.mu.Lock()
	e.toGopeed["t1"], e.toKL["g1"], e.torrents["t1"] = "g1", "t1", true
	e.mu.Unlock()
	if e.Reconnectable("t1") || e.Reconnect("t1") {
		t.Error("a torrent was reconnected, which throws its peers away")
	}
}
