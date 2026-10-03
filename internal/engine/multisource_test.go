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
// on, counting ranged requests from 1, it answers 410; zero never fails.
type mirrorSource struct {
	srv      *httptest.Server
	data     []byte
	failFrom int32
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
