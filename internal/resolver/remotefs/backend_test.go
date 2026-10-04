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

	"github.com/junkerderprovinz/knightloader/internal/collide"
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
	if left := partFiles(t, dir); len(left) > 0 {
		t.Error("the part file survived a finished download")
	}
}

func TestBackendResumesFromThePartFileInsteadOfStartingAgain(t *testing.T) {
	s := newFakeFTP(t, "alice", "secret", ftpTree())
	dir := t.TempDir()
	// Three bytes of "hello" left by a paused download.
	part := partPath(dir, "notes.txt", "t1")
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

// arriving waits until a slowed transfer has written its first burst into
// part, so a Halt finds it running.
func arriving(t *testing.T, part string) {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for {
		if fi, err := os.Stat(part); err == nil && fi.Size() >= 256<<10 {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("the transfer never got going")
		}
		time.Sleep(10 * time.Millisecond)
	}
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
	part := partPath(first, "big.bin", "t1")
	arriving(t, part)
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
	if err := os.Rename(part, partPath(second, "big.bin", "t1")); err != nil {
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

// byTask hands each download's updates to a recorder of its own.
type byTask map[string]*recorder

func (r byTask) update(id string, u core.Update) { r[id].update(id, u) }

// sameNameTree serves a second, different film.mkv beside ftpTree's, as a
// folder listing does for two subfolders that hold one name.
func sameNameTree(size int) map[string]fakeNode {
	tree := ftpTree()
	tree["/pub/other"] = fakeNode{dir: true}
	tree["/pub/other/film.mkv"] = fakeNode{data: bytes.Repeat([]byte("B"), size)}
	return tree
}

// holds reports whether the file at path is exactly want.
func holds(path string, want []byte) bool {
	got, err := os.ReadFile(path)
	return err == nil && bytes.Equal(got, want)
}

func TestRemovingADownloadSavedUnderANumberedNameDeletesThatFileAndNotTheNamedOne(t *testing.T) {
	tree := sameNameTree(64)
	s := newFakeFTP(t, "alice", "secret", tree)
	dir := t.TempDir()
	rec := byTask{"a": newRecorder(), "b": newRecorder()}
	b := NewBackend(Logins{s.host(): {Username: "alice", Password: "secret"}}, Dialer{}, newStubEngine(), dir, rec.update)

	b.Download("a", LinkOf(s.target("/pub/film.mkv")), nil, 1)
	first := rec["a"].wait(t)
	b.Download("b", LinkOf(s.target("/pub/other/film.mkv")), nil, 1)
	second := rec["b"].wait(t)

	named := filepath.Join(dir, "film.mkv")
	if first.Status != core.StatusDone || first.File != named {
		t.Fatalf("first download = %+v, want it done at %s", first, named)
	}
	if second.Status != core.StatusDone || second.File == "" || second.File == named || filepath.Base(second.File) != second.Name {
		t.Fatalf("second download = %+v, want it done under a numbered name it reports as its file", second)
	}
	if !holds(second.File, tree["/pub/other/film.mkv"].data) {
		t.Fatalf("%s does not hold the second download", second.File)
	}

	b.Remove("b", true)

	if !holds(named, tree["/pub/film.mkv"].data) {
		t.Error("removing the second download took the first one's file")
	}
	if _, err := os.Stat(second.File); !os.IsNotExist(err) {
		t.Error("the removed download's own file is still there")
	}
}

func TestRemovingAFailedDownloadSparesAFinishedOneOfTheSameName(t *testing.T) {
	s := newFakeFTP(t, "alice", "secret", ftpTree())
	dir := t.TempDir()
	rec := byTask{"a": newRecorder(), "b": newRecorder()}
	b := NewBackend(Logins{s.host(): {Username: "alice", Password: "secret"}}, Dialer{}, newStubEngine(), dir, rec.update)

	b.Download("a", LinkOf(s.target("/pub/film.mkv")), nil, 1)
	if u := rec["a"].wait(t); u.Status != core.StatusDone {
		t.Fatalf("first download = %+v, want it done", u)
	}
	b.Download("b", LinkOf(s.target("/pub/gone/film.mkv")), nil, 1)
	if u := rec["b"].wait(t); u.Status != core.StatusError {
		t.Fatalf("second download = %+v, want it failed", u)
	}

	// What a restart of the failed download does first.
	b.Remove("b", true)

	if _, err := os.Stat(filepath.Join(dir, "film.mkv")); err != nil {
		t.Errorf("removing the failed download took the finished one's file: %v", err)
	}
}

func TestRemovingAFinishedDownloadSparesThePartFileOfAnotherOfTheSameName(t *testing.T) {
	tree := sameNameTree(1 << 20)
	s := newFakeFTP(t, "alice", "secret", tree)
	dir := t.TempDir()
	var limit atomic.Int64
	rec := byTask{"a": newRecorder(), "b": newRecorder()}
	b := NewBackend(Logins{s.host(): {Username: "alice", Password: "secret"}}, Dialer{}, newStubEngine(), dir, rec.update)
	b.RateLimit = limit.Load

	b.Download("a", LinkOf(s.target("/pub/film.mkv")), nil, 1)
	if u := rec["a"].wait(t); u.Status != core.StatusDone {
		t.Fatalf("first download = %+v, want it done", u)
	}

	limit.Store(64 << 10)
	b.Download("b", LinkOf(s.target("/pub/other/film.mkv")), nil, 1)
	part := partPath(dir, "film.mkv", "b")
	arriving(t, part)
	if !b.Halt("b") {
		t.Fatal("Halt found no transfer running")
	}

	b.Remove("a", true)

	if _, err := os.Stat(part); err != nil {
		t.Fatalf("removing the finished download took the part file of the one still arriving: %v", err)
	}
	limit.Store(0)
	b.Resume("b")
	second := rec["b"].wait(t)
	if second.Status != core.StatusDone || !holds(second.File, tree["/pub/other/film.mkv"].data) {
		t.Errorf("second download = %+v, want its own bytes in its file", second)
	}
}

func TestTwoDownloadsOfTheSameNameAtOnceKeepTheirBytesApart(t *testing.T) {
	tree := sameNameTree(1 << 20)
	tree["/pub/film.mkv"] = fakeNode{data: bytes.Repeat([]byte("A"), 1<<20)}
	s := newFakeFTP(t, "alice", "secret", tree)
	dir := t.TempDir()
	rec := byTask{"a": newRecorder(), "b": newRecorder()}
	b := NewBackend(Logins{s.host(): {Username: "alice", Password: "secret"}}, Dialer{}, newStubEngine(), dir, rec.update)
	// Slow enough that both are still arriving when the other starts.
	b.RateLimit = func() int64 { return 1 << 20 }

	b.Download("a", LinkOf(s.target("/pub/film.mkv")), nil, 1)
	b.Download("b", LinkOf(s.target("/pub/other/film.mkv")), nil, 1)
	first, second := rec["a"].wait(t), rec["b"].wait(t)

	if first.Status != core.StatusDone || !holds(first.File, tree["/pub/film.mkv"].data) {
		t.Errorf("first download = %+v, want its own bytes in its file", first)
	}
	if second.Status != core.StatusDone || !holds(second.File, tree["/pub/other/film.mkv"].data) {
		t.Errorf("second download = %+v, want its own bytes in its file", second)
	}
}

// A package rename moves a halted download's part file without the backend
// hearing of it, so the path it has on record points where the file was.
func TestADownloadOfTheSameNameLeavesAHaltedOnesPartFileAloneAfterItsFolderMoved(t *testing.T) {
	tree := sameNameTree(1 << 20)
	tree["/pub/film.mkv"] = fakeNode{data: bytes.Repeat([]byte("A"), 1<<20)}
	s := newFakeFTP(t, "alice", "secret", tree)
	first, second := t.TempDir(), t.TempDir()
	var mu sync.Mutex
	dir := first
	var limit atomic.Int64
	limit.Store(64 << 10)
	rec := byTask{"a": newRecorder(), "b": newRecorder()}
	b := NewBackend(Logins{s.host(): {Username: "alice", Password: "secret"}}, Dialer{}, newStubEngine(), first, rec.update)
	b.Dir = func(string) string {
		mu.Lock()
		defer mu.Unlock()
		return dir
	}
	b.RateLimit = limit.Load

	b.Download("a", LinkOf(s.target("/pub/film.mkv")), nil, 1)
	part := partPath(first, "film.mkv", "a")
	arriving(t, part)
	if !b.Halt("a") {
		t.Fatal("Halt found no transfer running")
	}
	if err := os.Rename(part, partPath(second, "film.mkv", "a")); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	dir = second
	mu.Unlock()
	limit.Store(0)

	b.Download("b", LinkOf(s.target("/pub/other/film.mkv")), nil, 1)
	if u := rec["b"].wait(t); u.Status != core.StatusDone || !holds(u.File, tree["/pub/other/film.mkv"].data) {
		t.Errorf("second download = %+v, want its own bytes in its file", u)
	}
	b.Resume("a")
	if u := rec["a"].wait(t); u.Status != core.StatusDone || !holds(u.File, tree["/pub/film.mkv"].data) {
		t.Errorf("first download = %+v, want its own bytes in its file", u)
	}
}

func TestDownloadsFinishingAtOnceUnderOneNameEachKeepTheirFile(t *testing.T) {
	b := NewBackend(Logins{}, Dialer{}, newStubEngine(), t.TempDir(), func(string, core.Update) {})
	for round := range 20 {
		dir := t.TempDir()
		target := filepath.Join(dir, "film.mkv")
		const n = 6
		var wg sync.WaitGroup
		saved := make([]string, n)
		errs := make([]error, n)
		for i := range n {
			part := filepath.Join(dir, collide.Counted("film.mkv", i+2)+partSuffix)
			if err := os.WriteFile(part, []byte{byte('a' + i)}, 0o644); err != nil {
				t.Fatal(err)
			}
			wg.Add(1)
			go func() {
				defer wg.Done()
				saved[i], errs[i] = b.finish(part, target)
			}()
		}
		wg.Wait()
		seen := map[string]bool{}
		for i := range n {
			if errs[i] != nil {
				t.Fatalf("round %d: download %d did not finish: %v", round, i, errs[i])
			}
			if seen[saved[i]] {
				t.Fatalf("round %d: two downloads were saved as %s", round, saved[i])
			}
			seen[saved[i]] = true
			if !holds(saved[i], []byte{byte('a' + i)}) {
				t.Fatalf("round %d: %s does not hold download %d", round, saved[i], i)
			}
		}
	}
}

// partFiles lists the part files in dir.
func partFiles(t *testing.T, dir string) []string {
	t.Helper()
	got, err := filepath.Glob(filepath.Join(dir, "*"+partSuffix))
	if err != nil {
		t.Fatal(err)
	}
	return got
}

// A restart empties the backend, and the download of the same name that comes
// back first must neither take the other one's bytes for its own nor start
// again from nothing.
func TestPausedDownloadsOfOneNameEachResumeFromTheirOwnPartFileAfterARestart(t *testing.T) {
	tree := sameNameTree(1 << 20)
	tree["/pub/film.mkv"] = fakeNode{data: bytes.Repeat([]byte("A"), 1<<20)}
	s := newFakeFTP(t, "alice", "secret", tree)
	dir := t.TempDir()
	logins := Logins{s.host(): {Username: "alice", Password: "secret"}}
	paths := map[string]string{"a": "/pub/film.mkv", "b": "/pub/other/film.mkv"}

	before := NewBackend(logins, Dialer{}, newStubEngine(), dir, func(string, core.Update) {})
	before.RateLimit = func() int64 { return 64 << 10 }
	for _, id := range []string{"a", "b"} {
		before.Download(id, LinkOf(s.target(paths[id])), nil, 1)
		arriving(t, partPath(dir, "film.mkv", id))
		if !before.Halt(id) {
			t.Fatalf("Halt found no transfer of %s running", id)
		}
	}

	rec := byTask{"a": newRecorder(), "b": newRecorder()}
	after := NewBackend(logins, Dialer{}, newStubEngine(), dir, rec.update)
	for _, id := range []string{"b", "a"} {
		for len(s.restarts) > 0 {
			<-s.restarts
		}
		after.Download(id, LinkOf(s.target(paths[id])), nil, 1)
		want := tree[paths[id]].data
		if u := rec[id].wait(t); u.Status != core.StatusDone || !holds(u.File, want) {
			t.Errorf("download %s = %+v, want its own bytes in its file", id, u)
		}
		select {
		case off := <-s.restarts:
			if off < 256<<10 {
				t.Errorf("download %s asked the server to restart at %d, before what its part file held", id, off)
			}
		case <-time.After(2 * time.Second):
			t.Errorf("download %s did not resume from its part file", id)
		}
	}
	if left := partFiles(t, dir); len(left) > 0 {
		t.Errorf("part files left behind: %v", left)
	}
}

// A plain remove is what an undo can take back, and the row it brings back
// resumes from its part file, as an engine download resumes from its partial.
func TestAPlainRemoveKeepsThePartFileForTheDownloadToResumeFrom(t *testing.T) {
	tree := sameNameTree(1 << 20)
	tree["/pub/film.mkv"] = fakeNode{data: bytes.Repeat([]byte("A"), 1<<20)}
	s := newFakeFTP(t, "alice", "secret", tree)
	dir := t.TempDir()
	var limit atomic.Int64
	limit.Store(64 << 10)
	rec := newRecorder()
	b := NewBackend(Logins{s.host(): {Username: "alice", Password: "secret"}}, Dialer{}, newStubEngine(), dir, rec.update)
	b.RateLimit = limit.Load
	link := LinkOf(s.target("/pub/film.mkv"))

	b.Download("a", link, nil, 1)
	part := partPath(dir, "film.mkv", "a")
	arriving(t, part)
	if !b.Halt("a") {
		t.Fatal("Halt found no transfer running")
	}

	b.Remove("a", false)

	if _, err := os.Stat(part); err != nil {
		t.Fatalf("a remove without files deleted the part file: %v", err)
	}
	for len(s.restarts) > 0 {
		<-s.restarts
	}
	limit.Store(0)
	b.Download("a", link, nil, 1)
	if u := rec.wait(t); u.Status != core.StatusDone || !holds(u.File, tree["/pub/film.mkv"].data) {
		t.Fatalf("download = %+v, want it done with its own bytes", u)
	}
	select {
	case off := <-s.restarts:
		if off < 256<<10 {
			t.Errorf("the download asked the server to restart at %d, before what its part file held", off)
		}
	case <-time.After(2 * time.Second):
		t.Error("the download started again from nothing")
	}
}

func TestRemovingWithFilesDeletesThePartFileOfAHaltedDownload(t *testing.T) {
	s := newFakeFTP(t, "alice", "secret", sameNameTree(1<<20))
	dir := t.TempDir()
	b := NewBackend(Logins{s.host(): {Username: "alice", Password: "secret"}}, Dialer{}, newStubEngine(), dir, func(string, core.Update) {})
	b.RateLimit = func() int64 { return 64 << 10 }

	b.Download("a", LinkOf(s.target("/pub/other/film.mkv")), nil, 1)
	part := partPath(dir, "film.mkv", "a")
	arriving(t, part)
	if !b.Halt("a") {
		t.Fatal("Halt found no transfer running")
	}

	b.Remove("a", true)

	if _, err := os.Stat(part); !os.IsNotExist(err) {
		t.Errorf("the part file is still there: %v", err)
	}
}
