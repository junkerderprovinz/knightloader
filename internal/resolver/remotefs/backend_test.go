package remotefs

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/core"
)

// recorder collects what the backend reported and hands back the terminal
// update.
type recorder struct {
	mu   sync.Mutex
	all  []core.Update
	done chan core.Update
}

func newRecorder() *recorder { return &recorder{done: make(chan core.Update, 4)} }

func (r *recorder) update(_ string, u core.Update) {
	r.mu.Lock()
	r.all = append(r.all, u)
	r.mu.Unlock()
	if u.Status == core.StatusDone || u.Status == core.StatusError {
		select {
		case r.done <- u:
		default:
		}
	}
}

func (r *recorder) wait(t *testing.T) core.Update {
	t.Helper()
	select {
	case u := <-r.done:
		return u
	case <-time.After(15 * time.Second):
		t.Fatal("the download never settled")
		return core.Update{}
	}
}

// stubEngine stands in for the embedded download engine and records what it
// was handed.
type stubEngine struct {
	mu      sync.Mutex
	gotURL  string
	gotAuth string
	called  chan struct{}
}

func newStubEngine() *stubEngine { return &stubEngine{called: make(chan struct{}, 1)} }

func (e *stubEngine) Download(_, url string, headers map[string]string, _ int) {
	e.mu.Lock()
	e.gotURL, e.gotAuth = url, headers["Authorization"]
	e.mu.Unlock()
	select {
	case e.called <- struct{}{}:
	default:
	}
}
func (e *stubEngine) Pause(string)        {}
func (e *stubEngine) Resume(string)       {}
func (e *stubEngine) Remove(string, bool) {}

func TestBackendDownloadsOverFTPAndLeavesNoPartFileBehind(t *testing.T) {
	s := newFakeFTP(t, "alice", "secret", ftpTree())
	dir := t.TempDir()
	rec := newRecorder()
	b := NewBackend(Logins{s.host(): {Username: "alice", Password: "secret"}}, Dialer{}, newStubEngine(), dir, rec.update)

	b.Download("t1", LinkOf(s.target("/pub/film.mkv")), nil, 4)
	if u := rec.wait(t); u.Status != core.StatusDone {
		t.Fatalf("update = %+v, want a finished download", u)
	}

	got, err := os.ReadFile(filepath.Join(dir, "film.mkv"))
	if err != nil {
		t.Fatalf("the file is not where the app expects it: %v", err)
	}
	if len(got) != 4096 || strings.Trim(string(got), "A") != "" {
		t.Errorf("the file holds %d bytes of %q, want 4096 of A", len(got), firstRune(got))
	}
	if _, err := os.Stat(filepath.Join(dir, "film.mkv"+partSuffix)); !os.IsNotExist(err) {
		t.Error("the part file survived a finished download")
	}
}

func TestBackendResumesFromThePartFileInsteadOfStartingAgain(t *testing.T) {
	s := newFakeFTP(t, "alice", "secret", ftpTree())
	dir := t.TempDir()
	// Three bytes of "hello" left by a paused download.
	part := filepath.Join(dir, "notes.txt"+partSuffix)
	if err := os.WriteFile(part, []byte("hel"), 0o644); err != nil {
		t.Fatal(err)
	}
	rec := newRecorder()
	b := NewBackend(Logins{s.host(): {Username: "alice", Password: "secret"}}, Dialer{}, newStubEngine(), dir, rec.update)

	b.Download("t1", LinkOf(s.target("/pub/notes.txt")), nil, 1)
	if u := rec.wait(t); u.Status != core.StatusDone {
		t.Fatalf("update = %+v, want a finished download", u)
	}
	got, err := os.ReadFile(filepath.Join(dir, "notes.txt"))
	if err != nil {
		t.Fatal(err)
	}
	// "hello", not "helhello".
	if string(got) != "hello" {
		t.Errorf("the finished file holds %q, want %q", got, "hello")
	}
	select {
	case off := <-s.restarts:
		if off != 3 {
			t.Errorf("the server was asked to restart at %d, want 3", off)
		}
	case <-time.After(2 * time.Second):
		t.Error("no restart reached the server, so the part file was refetched from the start")
	}
}

func TestBackendRefusesToCallATruncatedTransferFinished(t *testing.T) {
	// The stream ends early without an error.
	tree := ftpTree()
	s := newFakeFTP(t, "alice", "secret", tree)
	s.shortBy = 100
	dir := t.TempDir()
	rec := newRecorder()
	b := NewBackend(Logins{s.host(): {Username: "alice", Password: "secret"}}, Dialer{}, newStubEngine(), dir, rec.update)

	b.Download("t1", LinkOf(s.target("/pub/film.mkv")), nil, 1)
	u := rec.wait(t)
	if u.Status != core.StatusError {
		t.Fatalf("update = %+v, want a failure", u)
	}
	if !strings.Contains(u.Err, "of 4096 bytes") {
		t.Errorf("the error does not say how much arrived: %q", u.Err)
	}
	if _, err := os.Stat(filepath.Join(dir, "film.mkv")); !os.IsNotExist(err) {
		t.Error("a truncated download was renamed to its final name")
	}
}

func TestBackendHandsAWebDAVLinkToTheEngineRatherThanFetchingIt(t *testing.T) {
	eng := newStubEngine()
	b := NewBackend(Logins{}, Dialer{}, eng, t.TempDir(), func(string, core.Update) {})
	b.Download("t1", "https://cloud.example.com/dav/film.mkv", map[string]string{"Authorization": "Basic xyz"}, 6)

	select {
	case <-eng.called:
	case <-time.After(5 * time.Second):
		t.Fatal("the engine was never asked to fetch the link")
	}
	eng.mu.Lock()
	defer eng.mu.Unlock()
	if eng.gotURL != "https://cloud.example.com/dav/film.mkv" {
		t.Errorf("the engine got %q", eng.gotURL)
	}
	if eng.gotAuth != "Basic xyz" {
		t.Errorf("the credential did not travel with it: %q", eng.gotAuth)
	}
}

func TestBackendReportsAMissingFileWithTheServersOwnReason(t *testing.T) {
	s := newFakeFTP(t, "alice", "secret", ftpTree())
	rec := newRecorder()
	b := NewBackend(Logins{s.host(): {Username: "alice", Password: "secret"}}, Dialer{}, newStubEngine(), t.TempDir(), rec.update)

	b.Download("t1", LinkOf(s.target("/pub/gone.mkv")), nil, 1)
	u := rec.wait(t)
	if u.Status != core.StatusError || !strings.Contains(u.Err, "does not exist") {
		t.Fatalf("update = %+v, want it to say the path is gone", u)
	}
}

func firstRune(b []byte) string {
	if len(b) == 0 {
		return ""
	}
	return string(b[:1])
}

// Halt stops a transfer without a failure reaching the app and returns once
// the part file is closed, so it can be moved. Resume goes on from it in the
// folder Dir names by then.
func TestBackendHaltsQuietlyAndResumesInTheNewFolder(t *testing.T) {
	tree := ftpTree()
	big := bytes.Repeat([]byte("B"), 1<<20)
	tree["/pub/big.bin"] = fakeNode{data: big}
	s := newFakeFTP(t, "alice", "secret", tree)
	first, second := t.TempDir(), t.TempDir()
	var mu sync.Mutex
	dir := first
	var limit atomic.Int64
	limit.Store(64 << 10)
	rec := newRecorder()
	b := NewBackend(Logins{s.host(): {Username: "alice", Password: "secret"}}, Dialer{}, newStubEngine(), first, rec.update)
	b.Dir = func(string) string {
		mu.Lock()
		defer mu.Unlock()
		return dir
	}
	b.RateLimit = limit.Load

	b.Download("t1", LinkOf(s.target("/pub/big.bin")), nil, 1)
	part := filepath.Join(first, "big.bin"+partSuffix)
	deadline := time.Now().Add(15 * time.Second)
	for {
		if fi, err := os.Stat(part); err == nil && fi.Size() >= 256<<10 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the transfer never got going")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !b.Halt("t1") {
		t.Fatal("Halt found no transfer running")
	}
	b.mu.Lock()
	_, running := b.runs["t1"]
	b.mu.Unlock()
	if running {
		t.Error("Halt returned while the transfer was still going")
	}
	rec.mu.Lock()
	for _, u := range rec.all {
		if u.Status == core.StatusError {
			t.Errorf("the halt reached the app as a failure: %s", u.Err)
		}
	}
	rec.mu.Unlock()
	if err := os.Rename(part, filepath.Join(second, "big.bin"+partSuffix)); err != nil {
		t.Fatalf("the part file could not be moved after the halt: %v", err)
	}

	mu.Lock()
	dir = second
	mu.Unlock()
	limit.Store(0)
	for len(s.restarts) > 0 {
		<-s.restarts
	}
	b.Resume("t1")
	if u := rec.wait(t); u.Status != core.StatusDone {
		t.Fatalf("update = %+v, want a finished download", u)
	}
	got, err := os.ReadFile(filepath.Join(second, "big.bin"))
	if err != nil || !bytes.Equal(got, big) {
		t.Fatalf("the finished file holds %d bytes, %v; want the %d served", len(got), err, len(big))
	}
	select {
	case off := <-s.restarts:
		if off < 256<<10 {
			t.Errorf("the resume asked the server to restart at %d, before what the part file held", off)
		}
	case <-time.After(2 * time.Second):
		t.Error("no restart reached the server, so the part file was fetched again from the start")
	}
}
