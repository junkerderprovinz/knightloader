package engine

import (
	"bytes"
	"context"
	cryptorand "crypto/rand"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/GopeedLab/gopeed/pkg/base"
	"github.com/junkerderprovinz/knightloader/internal/core"
)

// mirrorSource serves data with ranges. From the request numbered failFrom
// on, counting ranged requests from 1, it answers 410 and calls onFail; zero
// never fails.
type mirrorSource struct {
	srv      *httptest.Server
	data     []byte
	failFrom int32
	onFail   func()
	etag     string

	ranged atomic.Int32
}

func newMirrorSource(t *testing.T, data []byte) *mirrorSource {
	t.Helper()
	m := &mirrorSource{data: data}
	m.srv = httptest.NewServer(http.HandlerFunc(m.serve))
	t.Cleanup(m.srv.Close)
	return m
}

func (m *mirrorSource) serve(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("Range") != "" {
		if n := m.ranged.Add(1); m.failFrom > 0 && n >= m.failFrom {
			if m.onFail != nil {
				m.onFail()
			}
			w.WriteHeader(http.StatusGone)
			return
		}
	}
	if m.etag != "" {
		w.Header().Set("ETag", m.etag)
	}
	http.ServeContent(w, r, "f.bin", time.Time{}, bytes.NewReader(m.data))
}

func (m *mirrorSource) url() string { return m.srv.URL + "/f.bin" }

func randomBytes(t *testing.T, n int) []byte {
	t.Helper()
	b := make([]byte, n)
	if _, err := cryptorand.Read(b); err != nil {
		t.Fatal(err)
	}
	return b
}

// smallMultiSource lets files of a few megabytes take more than one source.
func smallMultiSource(t *testing.T) {
	was := multiSourceMin
	multiSourceMin = 1 << 20
	t.Cleanup(func() { multiSourceMin = was })
}

func ranged(size int) *base.Resource {
	return &base.Resource{Size: int64(size), Range: true, Files: []*base.FileInfo{{Name: "f.bin"}}}
}

func vetEngine(t *testing.T) *Engine {
	t.Helper()
	e, err := New(t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = e.Close() })
	return e
}

func offering(urls ...string) func(context.Context) []string {
	return func(context.Context) []string { return urls }
}

func TestALinkToTheSameBytesIsTakenAsASource(t *testing.T) {
	smallMultiSource(t)
	data := randomBytes(t, 4<<20)
	own, other := newMirrorSource(t, data), newMirrorSource(t, data)
	e := vetEngine(t)

	got := e.vetSources(Job{TaskID: "t1", URL: own.url(), Sources: offering(other.url())}, ranged(len(data)))

	if len(got) != 1 || got[0] != other.url() {
		t.Fatalf("sources = %v, want the other link", got)
	}
}

func TestALinkToAFileOfAnotherSizeIsRefused(t *testing.T) {
	smallMultiSource(t)
	data := randomBytes(t, 4<<20)
	own := newMirrorSource(t, data)
	longer := newMirrorSource(t, append(append([]byte(nil), data...), 0))
	e := vetEngine(t)

	if got := e.vetSources(Job{TaskID: "t1", URL: own.url(), Sources: offering(longer.url())}, ranged(len(data))); len(got) != 0 {
		t.Fatalf("sources = %v, want none", got)
	}
}

// The volumes of split archives mostly share one size, so equal sizes alone
// would splice one volume into another.
func TestALinkOfTheSameSizeWithOtherBytesIsRefused(t *testing.T) {
	smallMultiSource(t)
	data := randomBytes(t, 4<<20)
	own, other := newMirrorSource(t, data), newMirrorSource(t, randomBytes(t, len(data)))
	e := vetEngine(t)

	if got := e.vetSources(Job{TaskID: "t1", URL: own.url(), Sources: offering(other.url())}, ranged(len(data))); len(got) != 0 {
		t.Fatalf("sources = %v, want none", got)
	}
}

func TestALinkThatCannotSendRangesIsRefused(t *testing.T) {
	smallMultiSource(t)
	data := randomBytes(t, 4<<20)
	own := newMirrorSource(t, data)
	whole := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(data)
	}))
	t.Cleanup(whole.Close)
	e := vetEngine(t)

	if got := e.vetSources(Job{TaskID: "t1", URL: own.url(), Sources: offering(whole.URL + "/f.bin")}, ranged(len(data))); len(got) != 0 {
		t.Fatalf("sources = %v, want none", got)
	}
}

// One host naming its copies apart is saying they are different files.
func TestADifferentETagOnTheSameHostIsRefused(t *testing.T) {
	smallMultiSource(t)
	data := randomBytes(t, 4<<20)
	own := newMirrorSource(t, data)
	own.etag = `"a"`
	e := vetEngine(t)

	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("ETag", `"b"`)
		http.ServeContent(w, r, "f.bin", time.Time{}, bytes.NewReader(data))
	}))
	t.Cleanup(other.Close)

	if got := e.vetSources(Job{TaskID: "t1", URL: own.url(), Sources: offering(other.URL + "/f.bin")}, ranged(len(data))); len(got) != 0 {
		t.Fatalf("sources = %v, want none", got)
	}
}

// A login in the job's headers is for its own link and goes nowhere else.
func TestAJobWithHeadersAsksForNoFurtherSources(t *testing.T) {
	smallMultiSource(t)
	data := randomBytes(t, 4<<20)
	own, other := newMirrorSource(t, data), newMirrorSource(t, data)
	e := vetEngine(t)
	asked := false
	job := Job{TaskID: "t1", URL: own.url(), Headers: map[string]string{"Authorization": "Basic x"},
		Sources: func(context.Context) []string { asked = true; return []string{other.url()} }}

	if got := e.vetSources(job, ranged(len(data))); len(got) != 0 || asked {
		t.Fatalf("sources = %v, asked = %v; want none and no asking", got, asked)
	}
}

func TestASmallFileAsksForNoFurtherSources(t *testing.T) {
	data := randomBytes(t, 1<<20)
	own, other := newMirrorSource(t, data), newMirrorSource(t, data)
	e := vetEngine(t)

	if got := e.vetSources(Job{TaskID: "t1", URL: own.url(), Sources: offering(other.url())}, ranged(len(data))); len(got) != 0 {
		t.Fatalf("sources = %v, want none below %d bytes", got, multiSourceMin)
	}
}

// runToEnd runs job to the end and returns the last update and the file.
func runToEnd(t *testing.T, job Job) (core.Update, []byte) {
	t.Helper()
	if raceEnabled {
		t.Skip("gopeed v1.9.3 races on a task's status when a real transfer starts; see settle")
	}
	u := &updates{}
	dir := t.TempDir()
	e, err := New(dir, u.add)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = e.Close() })
	job.TaskID, job.Dir, job.Conns = "t1", dir, 2
	e.Start(job)
	waitUntil(t, "the download settling", func() bool { _, ok := u.settled(); return ok })
	last, _ := u.settled()
	b, _ := os.ReadFile(filepath.Join(dir, "f.bin"))
	return last, b
}

func TestAFileComesFromBothSources(t *testing.T) {
	smallMultiSource(t)
	data := randomBytes(t, 16<<20)
	own, other := newMirrorSource(t, data), newMirrorSource(t, data)

	last, got := runToEnd(t, Job{URL: own.url(), Sources: offering(other.url())})

	if last.Status != core.StatusDone {
		t.Fatalf("the download ended %q: %s", last.Status, last.Err)
	}
	if !bytes.Equal(got, data) {
		t.Fatal("the file is not the one the sources have")
	}
	// Two of the further source's ranged requests are the check.
	if other.ranged.Load() <= 2 {
		t.Errorf("the further source answered %d ranged requests, want some beyond the check", other.ranged.Load())
	}
}

// A source that stops part way leaves the rest to the others.
func TestASourceFailingMidwayLeavesTheRestToTheOther(t *testing.T) {
	smallMultiSource(t)
	data := randomBytes(t, 16<<20)
	own, other := newMirrorSource(t, data), newMirrorSource(t, data)
	// The check and one range, then 410.
	other.failFrom = 4

	last, got := runToEnd(t, Job{URL: own.url(), Sources: offering(other.url())})

	if last.Status != core.StatusDone {
		t.Fatalf("the download ended %q: %s", last.Status, last.Err)
	}
	if !bytes.Equal(got, data) {
		t.Fatal("the file is not the one the source has")
	}
}

// The job's connection count is the ceiling its own hoster agreed to, so the
// connections of further sources that die must not move onto its link.
func TestDeadSourcesLeaveTheOwnLinkItsConnectionCount(t *testing.T) {
	smallMultiSource(t)
	data := randomBytes(t, 16<<20)
	const conns = 2
	var refused atomic.Bool
	// over is how long the own link served more ranges at once than the job
	// may open. A request the client dropped ends here a moment later than
	// there, so a few milliseconds are noise; moved connections stay for
	// seconds.
	var (
		mu        sync.Mutex
		inFlight  int
		over      time.Duration
		overSince time.Time
	)
	own := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Range") != "" && refused.Load() {
			mu.Lock()
			if inFlight++; inFlight == conns+1 {
				overSince = time.Now()
			}
			mu.Unlock()
			defer func() {
				mu.Lock()
				if inFlight == conns+1 {
					over += time.Since(overSince)
				}
				inFlight--
				mu.Unlock()
			}()
		}
		http.ServeContent(pacedWriter{w, r}, r, "f.bin", time.Time{}, bytes.NewReader(data))
	}))
	t.Cleanup(own.Close)
	var others []string
	for range 3 {
		m := newMirrorSource(t, data)
		// The check, then 410.
		m.failFrom = 3
		m.onFail = func() { refused.Store(true) }
		others = append(others, m.url())
	}

	last, got := runToEnd(t, Job{URL: own.URL + "/f.bin", Sources: offering(others...)})

	if last.Status != core.StatusDone || !bytes.Equal(got, data) {
		t.Fatalf("the download ended %q (%s) with %d bytes, want the file", last.Status, last.Err, len(got))
	}
	mu.Lock()
	defer mu.Unlock()
	if over > 200*time.Millisecond {
		t.Fatalf("the own link served more than %d ranges at once for %v", conns, over)
	}
}

// A paused download's record keeps none of its further sources, so after a
// restart the transfer has only its own link, which must not take the
// connections that were dealt out over all of them.
func TestAfterARestartTheOwnLinkKeepsToTheJobsConnectionCount(t *testing.T) {
	if raceEnabled {
		t.Skip("gopeed v1.9.3 has internal data races in every real HTTP transfer; see startSlow")
	}
	smallMultiSource(t)
	data := randomBytes(t, 16<<20)
	const conns = 4
	var (
		counting atomic.Bool
		mu       sync.Mutex
		inFlight int
		peak     int
	)
	own := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Range") != "" && counting.Load() {
			mu.Lock()
			inFlight++
			peak = max(peak, inFlight)
			mu.Unlock()
			defer func() {
				mu.Lock()
				inFlight--
				mu.Unlock()
			}()
		}
		http.ServeContent(pacedWriter{w, r}, r, "f.bin", time.Time{}, bytes.NewReader(data))
	}))
	t.Cleanup(own.Close)
	var others []string
	for range 2 {
		m := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.ServeContent(pacedWriter{w, r}, r, "f.bin", time.Time{}, bytes.NewReader(data))
		}))
		t.Cleanup(m.Close)
		others = append(others, m.URL+"/f.bin")
	}
	dir, state := t.TempDir(), t.TempDir()
	j := Job{TaskID: "t1", URL: own.URL + "/f.bin", Dir: dir, Conns: conns, Sources: offering(others...)}
	u := &updates{}
	e, err := Open(dir, state, u.add)
	if err != nil {
		t.Fatal(err)
	}
	e.Start(j)
	waitUntil(t, "the first megabyte", func() bool { return u.loaded() >= 1<<20 })
	e.Pause(j.TaskID)
	waitUntil(t, "the pause", func() bool { return u.last().Status == core.StatusPaused })
	if err := e.Close(); err != nil {
		t.Fatal(err)
	}

	counting.Store(true)
	e, u = restarted(t, dir, state)
	e.Start(j)
	waitUntil(t, "the download settling", func() bool { _, ok := u.settled(); return ok })

	if last, _ := u.settled(); last.Status != core.StatusDone {
		t.Fatalf("the download ended %q: %s", last.Status, last.Err)
	}
	if got, _ := os.ReadFile(filepath.Join(dir, "f.bin")); !bytes.Equal(got, data) {
		t.Fatal("the file is not the one the sources have")
	}
	mu.Lock()
	defer mu.Unlock()
	if peak > conns {
		t.Fatalf("after the restart the own link served %d ranges at once, want at most %d", peak, conns)
	}
}

// pacedWriter slows a response down, so the requests to one source overlap,
// and stops it once the client is gone.
type pacedWriter struct {
	http.ResponseWriter
	r *http.Request
}

func (w pacedWriter) Write(p []byte) (int, error) {
	select {
	case <-w.r.Context().Done():
		return 0, w.r.Context().Err()
	case <-time.After(25 * time.Millisecond):
	}
	return w.ResponseWriter.Write(p)
}

func TestAFileFromAFurtherSourceOfAnotherSizeComesFromItsOwnLinkAlone(t *testing.T) {
	smallMultiSource(t)
	data := randomBytes(t, 8<<20)
	own := newMirrorSource(t, data)
	other := newMirrorSource(t, randomBytes(t, len(data)+4096))

	last, got := runToEnd(t, Job{URL: own.url(), Sources: offering(other.url())})

	if last.Status != core.StatusDone || !bytes.Equal(got, data) {
		t.Fatalf("the download ended %q (%s) with %d bytes, want the own link's file", last.Status, last.Err, len(got))
	}
	if n := other.ranged.Load(); n > 2 {
		t.Errorf("the refused source answered %d ranged requests, want only the check", n)
	}
}

// A 403 from the own link may only mean it takes no more connections, so
// the ones it turns away wait for the further source rather than leave
// their part of the file undone.
func TestAnOwnLinkForbiddingRangesLeavesTheFileToTheFurtherSource(t *testing.T) {
	smallMultiSource(t)
	data := randomBytes(t, 16<<20)
	var ranges atomic.Int32
	own := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// The check, then 403.
		if r.Header.Get("Range") != "" && ranges.Add(1) > 2 {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		http.ServeContent(pacedWriter{w, r}, r, "f.bin", time.Time{}, bytes.NewReader(data))
	}))
	t.Cleanup(own.Close)
	other := newMirrorSource(t, data)

	last, got := runToEnd(t, Job{URL: own.URL + "/f.bin", Sources: offering(other.url())})

	if last.Status != core.StatusDone || !bytes.Equal(got, data) {
		t.Fatalf("the download ended %q (%s) with %d bytes, want the file", last.Status, last.Err, len(got))
	}
}

// lockedBuffer is a log destination the vetting goroutines may share.
type lockedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func captureLog(t *testing.T) *lockedBuffer {
	t.Helper()
	b := &lockedBuffer{}
	log.SetOutput(b)
	t.Cleanup(func() { log.SetOutput(os.Stderr) })
	return b
}

// A debrid link works for anybody who has it, so an unreachable one is
// reported by its host alone.
func TestAnUnreachableSourceIsLoggedWithoutItsLink(t *testing.T) {
	smallMultiSource(t)
	data := randomBytes(t, 4<<20)
	own := newMirrorSource(t, data)
	e := vetEngine(t)
	logged := captureLog(t)

	e.vetSources(Job{TaskID: "t1", URL: own.url(), Sources: offering("http://127.0.0.1:1/dl/FURTHERSECRET/f.bin")}, ranged(len(data)))
	e.vetSources(Job{TaskID: "t2", URL: "http://127.0.0.1:1/own/OWNSECRET/f.bin", Sources: offering(own.url())}, ranged(len(data)))

	out := logged.String()
	if !strings.Contains(out, "not used as a further source") || !strings.Contains(out, "its own link failed the check") {
		t.Fatalf("the log does not report both failed checks:\n%s", out)
	}
	if strings.Contains(out, "SECRET") {
		t.Fatalf("the log carries a link:\n%s", out)
	}
}
