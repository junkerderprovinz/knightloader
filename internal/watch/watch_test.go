package watch

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// sink collects the jobs a watcher hands over. OnJob runs on the polling
// goroutine, so the slice needs a lock.
type sink struct {
	mu   sync.Mutex
	jobs []Job
}

func (s *sink) add(j Job) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.jobs = append(s.jobs, j)
}

func (s *sink) all() []Job {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]Job(nil), s.jobs...)
}

func (s *sink) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.jobs)
}

// newWatcher builds a watcher over a fresh temp dir. Tests drive poll() by hand
// so nothing depends on wall-clock timing.
func newWatcher(t *testing.T, del bool) (*Watcher, string, *sink) {
	t.Helper()
	dir := t.TempDir()
	rec := &sink{}
	w, err := New(Options{Dir: dir, Interval: time.Hour, OnJob: rec.add, Delete: del})
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	return w, dir, rec
}

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func appendTo(t *testing.T, path, content string) {
	t.Helper()
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString(content); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
}

// If this fails, files written by JDownloader's folder watch lose their
// destination, password or auto-start flag on the way in.
func TestParseCrawljobReadsEveryField(t *testing.T) {
	const body = "# dropped by some tool\n" +
		`text=https://example.com/a\r\nhttps://example.com/b` + "\n" +
		"packageName=My Package\n" +
		"downloadFolder=/mnt/user/downloads/movies\n" +
		"autoStart=TRUE\n" +
		`extractPasswords=["secret","other"]` + "\n" +
		"chunks=4\n"

	job, err := Parse("drop.crawljob", strings.NewReader(body))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(job.URLs) != 2 || job.URLs[0] != "https://example.com/a" || job.URLs[1] != "https://example.com/b" {
		t.Fatalf("URLs = %v, want both links from the escaped text= value", job.URLs)
	}
	if job.Package != "My Package" {
		t.Fatalf("Package = %q, want %q", job.Package, "My Package")
	}
	if job.Dir != "/mnt/user/downloads/movies" {
		t.Fatalf("Dir = %q, want the downloadFolder value", job.Dir)
	}
	if job.Password != "secret" {
		t.Fatalf("Password = %q, want the first extractPasswords entry", job.Password)
	}
	if !job.AutoStart {
		t.Fatal("AutoStart = false, want true for autoStart=TRUE")
	}
}

// If this fails, a hand-written link list with comments or spacing in it is
// either rejected or turns comment lines into downloads.
func TestParseTextSkipsBlanksAndComments(t *testing.T) {
	const body = "\n" +
		"# season one\n" +
		"https://example.com/one\n" +
		"\n" +
		"// disabled for now\n" +
		"   https://example.com/two   \n" +
		"magnet:?xt=urn:btih:0123456789abcdef0123456789abcdef01234567\n" +
		"not a link at all\n"

	job, err := Parse("Season 1.txt", strings.NewReader(body))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	want := []string{
		"https://example.com/one",
		"https://example.com/two",
		"magnet:?xt=urn:btih:0123456789abcdef0123456789abcdef01234567",
	}
	if len(job.URLs) != len(want) {
		t.Fatalf("URLs = %v, want %v", job.URLs, want)
	}
	for i := range want {
		if job.URLs[i] != want[i] {
			t.Fatalf("URLs[%d] = %q, want %q", i, job.URLs[i], want[i])
		}
	}
	// A .txt carries no package name, so the file name has to supply one.
	if job.Package != "Season 1" {
		t.Fatalf("Package = %q, want the file's base name", job.Package)
	}
}

// If this fails, junk in the drop folder is turned into empty downloads instead
// of being reported as unusable.
func TestParseRejectsUnusableFiles(t *testing.T) {
	tests := []struct {
		name string
		file string
		body string
	}{
		{"unknown extension", "notes.md", "https://example.com/a\n"},
		{"empty list", "links.txt", "\n# nothing here\n"},
		{"text without links", "links.txt", "just some prose\n"},
		{"crawljob without text", "drop.crawljob", "packageName=Empty\nautoStart=TRUE\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := Parse(tt.file, strings.NewReader(tt.body)); err == nil {
				t.Fatal("Parse succeeded, want an error")
			}
		})
	}
}

// If this fails, a file still being copied onto the share is consumed
// half-written and the links that had not landed yet are lost for good.
func TestPollWaitsUntilFileStopsGrowing(t *testing.T) {
	w, dir, rec := newWatcher(t, false)
	path := filepath.Join(dir, "links.txt")
	write(t, path, "https://example.com/one\n")

	w.poll() // first sighting, nothing is known about it yet
	if n := rec.count(); n != 0 {
		t.Fatalf("consumed %d jobs on first sight, want 0", n)
	}

	appendTo(t, path, "https://example.com/two\n")
	w.poll() // grew since the last poll: still being written
	if n := rec.count(); n != 0 {
		t.Fatalf("consumed %d jobs while the file was still growing, want 0", n)
	}

	w.poll() // unchanged since the last poll: settled
	jobs := rec.all()
	if len(jobs) != 1 {
		t.Fatalf("consumed %d jobs after the file settled, want 1", len(jobs))
	}
	if len(jobs[0].URLs) != 2 {
		t.Fatalf("URLs = %v, want both lines", jobs[0].URLs)
	}
}

// If this fails, every poll re-adds the same links until someone notices the
// duplicate downloads.
func TestConsumedFileIsRenamedAndNotOfferedTwice(t *testing.T) {
	w, dir, rec := newWatcher(t, false)
	path := filepath.Join(dir, "links.txt")
	write(t, path, "https://example.com/one\n")

	w.poll()
	w.poll()
	if n := rec.count(); n != 1 {
		t.Fatalf("consumed %d jobs, want 1", n)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("stat %s: err = %v, want the original to be gone", path, err)
	}
	if _, err := os.Stat(path + ".done"); err != nil {
		t.Fatalf("stat %s.done: %v, want the file renamed", path, err)
	}

	// The .done file must never be picked up again, however often we look.
	w.poll()
	w.poll()
	if n := rec.count(); n != 1 {
		t.Fatalf("consumed %d jobs after retiring the file, want 1", n)
	}
}

// If this fails, Delete: true still leaves .done files behind and the drop
// folder fills up on a box that asked for it not to.
func TestDeleteRemovesConsumedFile(t *testing.T) {
	w, dir, rec := newWatcher(t, true)
	path := filepath.Join(dir, "drop.crawljob")
	write(t, path, "text=https://example.com/a\npackageName=Gone\n")

	w.poll()
	w.poll()
	if n := rec.count(); n != 1 {
		t.Fatalf("consumed %d jobs, want 1", n)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("stat %s: err = %v, want it deleted", path, err)
	}
	if _, err := os.Stat(path + ".done"); !os.IsNotExist(err) {
		t.Fatalf("stat %s.done: err = %v, want no .done file with Delete set", path, err)
	}
}

// If this fails, one bad file in the drop folder either stops later files from
// being picked up or gets renamed away where the user cannot find it.
func TestUnparseableFileIsLeftAloneAndPollingContinues(t *testing.T) {
	w, dir, rec := newWatcher(t, false)
	bad := filepath.Join(dir, "garbage.txt")
	write(t, bad, "this file contains no links whatsoever\n")

	w.poll()
	w.poll()
	if n := rec.count(); n != 0 {
		t.Fatalf("consumed %d jobs from an unparseable file, want 0", n)
	}
	if _, err := os.Stat(bad); err != nil {
		t.Fatalf("stat %s: %v, want the bad file left where the user put it", bad, err)
	}
	if _, err := os.Stat(bad + ".done"); !os.IsNotExist(err) {
		t.Fatalf("the unparseable file was retired, want it untouched")
	}

	// A good file dropped afterwards still has to be picked up.
	good := filepath.Join(dir, "links.txt")
	write(t, good, "https://example.com/ok\n")
	w.poll()
	w.poll()
	if n := rec.count(); n != 1 {
		t.Fatalf("consumed %d jobs after the bad file, want 1", n)
	}
	if _, err := os.Stat(bad); err != nil {
		t.Fatalf("stat %s: %v, want the bad file still there", bad, err)
	}
}

// If this fails, shutdown hangs on the poll interval or a job lands after the
// app has already torn its download engine down.
func TestCloseStopsPollingPromptly(t *testing.T) {
	dir := t.TempDir()
	rec := &sink{}
	got := make(chan struct{}, 4)
	w, err := New(Options{
		Dir:      dir,
		Interval: 10 * time.Millisecond,
		OnJob: func(j Job) {
			rec.add(j)
			got <- struct{}{}
		},
	})
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	write(t, filepath.Join(dir, "first.txt"), "https://example.com/first\n")
	w.Start()

	select {
	case <-got:
	case <-time.After(5 * time.Second):
		t.Fatal("the watcher never picked the file up")
	}

	start := time.Now()
	if err := w.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	if d := time.Since(start); d > 2*time.Second {
		t.Fatalf("Close took %v, want it to return promptly", d)
	}

	// Nothing may be consumed once Close has returned.
	write(t, filepath.Join(dir, "second.txt"), "https://example.com/second\n")
	time.Sleep(100 * time.Millisecond)
	if n := rec.count(); n != 1 {
		t.Fatalf("consumed %d jobs, want polling to have stopped after Close", n)
	}
	if _, err := os.Stat(filepath.Join(dir, "second.txt")); err != nil {
		t.Fatalf("a file dropped after Close was touched: %v", err)
	}

	// Close is idempotent; a second call must not panic on the closed channel.
	if err := w.Close(); err != nil {
		t.Fatalf("second close: %v", err)
	}
}

// If this fails, a misconfigured watcher silently eats intake files instead of
// refusing to start.
func TestNewRejectsUnusableOptions(t *testing.T) {
	if _, err := New(Options{Dir: "  ", OnJob: func(Job) {}}); err == nil {
		t.Fatal("New with a blank Dir succeeded, want an error")
	}
	if _, err := New(Options{Dir: t.TempDir()}); err == nil {
		t.Fatal("New without OnJob succeeded, want an error")
	}
	// The drop folder is created rather than demanded, so a fresh install works.
	dir := filepath.Join(t.TempDir(), "watch")
	w, err := New(Options{Dir: dir, OnJob: func(Job) {}})
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	defer w.Close()
	if fi, err := os.Stat(dir); err != nil || !fi.IsDir() {
		t.Fatalf("stat %s: fi = %v, err = %v, want a created directory", dir, fi, err)
	}
	if w.interval != defaultInterval {
		t.Fatalf("interval = %v, want the default %v", w.interval, defaultInterval)
	}
}
