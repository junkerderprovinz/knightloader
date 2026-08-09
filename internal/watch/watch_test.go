package watch

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

// sink collects the jobs a watcher hands over. OnJob runs on a polling
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

// newPolled builds one folder's poller over a fresh temp dir. Tests drive poll()
// by hand so nothing depends on wall-clock timing.
func newPolled(t *testing.T, del bool) (*poller, string, *sink) {
	t.Helper()
	dir := t.TempDir()
	rec := &sink{}
	return newPoller(dir, del, time.Hour, rec.add), dir, rec
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

// pollAll drives every live folder once, without any of them running. Apply does
// not start a poller unless the watcher has been started, so a test that never
// calls Start owns the polling goroutine-free and can assert on exact counts.
func pollAll(w *Watcher) {
	w.mu.Lock()
	defer w.mu.Unlock()
	for _, p := range w.live {
		p.poll()
	}
}

func only(t *testing.T, jobs []Job) Job {
	t.Helper()
	if len(jobs) != 1 {
		t.Fatalf("parsed %d jobs, want 1", len(jobs))
	}
	return jobs[0]
}

// If this fails, files written by JDownloader's folder watch lose part of what
// they asked for on the way in.
func TestParseCrawljobReadsEveryFieldTheAppCanAct(t *testing.T) {
	const body = "# dropped by some tool\n" +
		`text=https://example.com/a\r\nhttps://example.com/b` + "\n" +
		"packageName=My Package\n" +
		"downloadFolder=/mnt/user/downloads/movies\n" +
		"autoStart=TRUE\n" +
		"forcedStart=TRUE\n" +
		"enabled=TRUE\n" +
		"priority=HIGHER\n" +
		"chunks=4\n" +
		"comment=from the shared drive\n" +
		"filename=episode.mkv\n" +
		"extractAfterDownload=FALSE\n" +
		`extractPasswords=["secret","other"]` + "\n" +
		"deepAnalyseEnabled=TRUE\n"

	job := only(t, mustParse(t, "drop.crawljob", body))
	if len(job.URLs) != 2 || job.URLs[0] != "https://example.com/a" || job.URLs[1] != "https://example.com/b" {
		t.Fatalf("URLs = %v, want both links from the escaped text= value", job.URLs)
	}
	if job.Package != "My Package" {
		t.Errorf("Package = %q, want %q", job.Package, "My Package")
	}
	if job.Dir != "/mnt/user/downloads/movies" {
		t.Errorf("Dir = %q, want the downloadFolder value", job.Dir)
	}
	if job.Password != "secret" {
		t.Errorf("Password = %q, want the first extractPasswords entry", job.Password)
	}
	if len(job.Passwords) != 2 || job.Passwords[1] != "other" {
		t.Errorf("Passwords = %v, want both entries kept", job.Passwords)
	}
	if !job.AutoStart {
		t.Error("AutoStart = false, want true for autoStart=TRUE")
	}
	if !job.Forced {
		t.Error("Forced = false, want true for forcedStart=TRUE")
	}
	if job.Disabled {
		t.Error("Disabled = true for enabled=TRUE")
	}
	if job.Priority == nil || *job.Priority != 2 {
		t.Errorf("Priority = %v, want HIGHER", job.Priority)
	}
	if job.Chunks != 4 {
		t.Errorf("Chunks = %d, want 4", job.Chunks)
	}
	if job.Comment != "from the shared drive" {
		t.Errorf("Comment = %q, want the comment value", job.Comment)
	}
	if job.Filename != "episode.mkv" {
		t.Errorf("Filename = %q, want the filename value", job.Filename)
	}
	if job.Extract == nil || *job.Extract {
		t.Errorf("Extract = %v, want an explicit false", job.Extract)
	}
}

// If this fails, a file that never mentions a key has that key's absence read as
// a decision: links arrive parked, at a priority nobody asked for, or with
// unpacking switched off against the global setting.
func TestParseLeavesUnmentionedKeysUndecided(t *testing.T) {
	job := only(t, mustParse(t, "drop.crawljob", "text=https://example.com/a\n"))
	if job.Disabled {
		t.Error("Disabled = true for a file that never mentions enabled")
	}
	if job.Priority != nil {
		t.Errorf("Priority = %v, want nil so a Packagizer rule is not overwritten", job.Priority)
	}
	if job.Extract != nil {
		t.Errorf("Extract = %v, want nil so the global switch decides", job.Extract)
	}
	if job.AutoStart || job.Forced || job.Chunks != 0 {
		t.Errorf("job = %+v, want every unmentioned field at its no-opinion value", job)
	}

	// UNSET is JD's own way of writing "no opinion" and must read the same way.
	unset := only(t, mustParse(t, "drop.crawljob", "text=https://example.com/a\nenabled=UNSET\nautoStart=UNSET\n"))
	if unset.Disabled || unset.AutoStart {
		t.Errorf("job = %+v, want UNSET to decide nothing", unset)
	}
}

// If this fails, enabled=FALSE is dropped and links the file asked to park start
// downloading.
func TestParseHonoursAnExplicitlyDisabledEntry(t *testing.T) {
	job := only(t, mustParse(t, "drop.crawljob", "text=https://example.com/a\nenabled=FALSE\n"))
	if !job.Disabled {
		t.Fatal("Disabled = false for enabled=FALSE")
	}
}

// If this fails, a file holding several jobs gives every link the last block's
// package, folder and password.
func TestParseCrawljobSplitsEntriesOnBlankLines(t *testing.T) {
	const body = "text=https://example.com/one\n" +
		"packageName=First\n" +
		"downloadFolder=/mnt/one\n" +
		"\n" +
		"text=https://example.com/two\n" +
		"packageName=Second\n" +
		"priority=LOWEST\n" +
		"\n" +
		"# a block with no link at all is not an error\n" +
		"packageName=Third\n"

	jobs := mustParse(t, "drop.crawljob", body)
	if len(jobs) != 2 {
		t.Fatalf("parsed %d jobs, want the two entries that carry links", len(jobs))
	}
	if jobs[0].Package != "First" || jobs[0].Dir != "/mnt/one" {
		t.Errorf("first job = %+v, want its own package and folder", jobs[0])
	}
	if jobs[1].Package != "Second" || jobs[1].Dir != "" {
		t.Errorf("second job = %+v, want its own package and no folder from the first", jobs[1])
	}
	if jobs[1].Priority == nil || *jobs[1].Priority != -3 {
		t.Errorf("second job priority = %v, want LOWEST", jobs[1].Priority)
	}
	if jobs[0].Priority != nil {
		t.Errorf("first job priority = %v, want the second entry's value not to leak back", jobs[0].Priority)
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

	job := only(t, mustParse(t, "Season 1.txt", body))
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
	// A .txt carries no package name, so the file name has to supply one. A blank
	// line in it is spacing and not an entry boundary.
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
		{"crawljob whose every entry is linkless", "drop.crawljob", "packageName=One\n\npackageName=Two\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := Parse(tt.file, strings.NewReader(tt.body)); err == nil {
				t.Fatal("Parse succeeded, want an error")
			}
		})
	}
}

// If this fails, an archive password with a comma in it is cut in half.
func TestParseKeepsABarePasswordWhole(t *testing.T) {
	job := only(t, mustParse(t, "drop.crawljob", "text=https://example.com/a\nextractPasswords=one,two\n"))
	if job.Password != "one,two" {
		t.Fatalf("Password = %q, want the bare value kept whole", job.Password)
	}
}

func mustParse(t *testing.T, name, body string) []Job {
	t.Helper()
	jobs, err := Parse(name, strings.NewReader(body))
	if err != nil {
		t.Fatalf("parse %s: %v", name, err)
	}
	return jobs
}

// If this fails, a file still being copied onto the share is consumed
// half-written and the links that had not landed yet are lost for good.
func TestPollWaitsUntilFileStopsGrowing(t *testing.T) {
	p, dir, rec := newPolled(t, false)
	path := filepath.Join(dir, "links.txt")
	write(t, path, "https://example.com/one\n")

	p.poll() // first sighting, nothing is known about it yet
	if n := rec.count(); n != 0 {
		t.Fatalf("consumed %d jobs on first sight, want 0", n)
	}

	appendTo(t, path, "https://example.com/two\n")
	p.poll() // grew since the last poll: still being written
	if n := rec.count(); n != 0 {
		t.Fatalf("consumed %d jobs while the file was still growing, want 0", n)
	}

	p.poll() // unchanged since the last poll: settled
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
	p, dir, rec := newPolled(t, false)
	path := filepath.Join(dir, "links.txt")
	write(t, path, "https://example.com/one\n")

	p.poll()
	p.poll()
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
	p.poll()
	p.poll()
	if n := rec.count(); n != 1 {
		t.Fatalf("consumed %d jobs after retiring the file, want 1", n)
	}
}

// If this fails, Delete: true still leaves .done files behind and the drop
// folder fills up on a box that asked for it not to.
func TestDeleteRemovesConsumedFile(t *testing.T) {
	p, dir, rec := newPolled(t, true)
	path := filepath.Join(dir, "drop.crawljob")
	write(t, path, "text=https://example.com/a\npackageName=Gone\n")

	p.poll()
	p.poll()
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

// If this fails, one file holding two jobs stages one of them and retires the
// file with the other still in it.
func TestOneFileWithTwoEntriesHandsOverBoth(t *testing.T) {
	p, dir, rec := newPolled(t, false)
	write(t, filepath.Join(dir, "drop.crawljob"),
		"text=https://example.com/one\npackageName=First\n\ntext=https://example.com/two\npackageName=Second\n")

	p.poll()
	p.poll()
	jobs := rec.all()
	if len(jobs) != 2 {
		t.Fatalf("handed over %d jobs, want both entries", len(jobs))
	}
	if jobs[0].Package != "First" || jobs[1].Package != "Second" {
		t.Fatalf("packages = %q, %q, want them in file order", jobs[0].Package, jobs[1].Package)
	}
}

// If this fails, one bad file in the drop folder either stops later files from
// being picked up or gets renamed away where the user cannot find it.
func TestUnparseableFileIsLeftAloneAndPollingContinues(t *testing.T) {
	p, dir, rec := newPolled(t, false)
	bad := filepath.Join(dir, "garbage.txt")
	write(t, bad, "this file contains no links whatsoever\n")

	p.poll()
	p.poll()
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
	p.poll()
	p.poll()
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
	if _, err := New(Options{Folders: []Folder{{Dir: " "}}, OnJob: func(Job) {}}); err == nil {
		t.Fatal("New with nothing but a blank folder succeeded, want an error")
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

// TestUnwritableFolderIsRefusedAtStartup is the failure this cost a live test
// to find: the share belonged to another user, so consuming a file — which
// means renaming it — could never succeed. The watcher started, logged that it
// was watching, and then ignored everything dropped into it forever.
func TestUnwritableFolderIsRefusedAtStartup(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root can write anywhere, so the permission cannot be simulated")
	}
	dir := t.TempDir()
	locked := filepath.Join(dir, "locked")
	if err := os.Mkdir(locked, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(locked, 0o700) })

	if _, err := New(Options{Dir: locked, OnJob: func(Job) {}}); err == nil {
		t.Skip("this filesystem does not enforce directory write permission")
	}
}

// If this fails, one unusable folder in the configured set turns the whole
// intake off instead of only itself.
func TestOneUnusableFolderDoesNotStopTheOthers(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root can write anywhere, so the permission cannot be simulated")
	}
	good := t.TempDir()
	locked := filepath.Join(t.TempDir(), "locked")
	if err := os.Mkdir(locked, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(locked, 0o700) })
	if err := writable(locked); err == nil {
		t.Skip("this filesystem does not enforce directory write permission")
	}

	rec := &sink{}
	w, err := New(Options{
		Folders:  []Folder{{Dir: locked}, {Dir: good}},
		Interval: time.Hour,
		OnJob:    rec.add,
	})
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	defer w.Close()
	if dirs := w.Dirs(); len(dirs) != 1 {
		t.Fatalf("Dirs = %v, want only the usable folder", dirs)
	}
}

// TestRefusedFileIsReportedOnce keeps a file that cannot be retired from being
// re-read on every single poll while still leaving it in place for the user.
func TestRefusedFileIsReportedOnce(t *testing.T) {
	dir := t.TempDir()
	var jobs int
	w, err := New(Options{Dir: dir, Interval: 10 * time.Millisecond, OnJob: func(Job) { jobs++ }})
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()

	// A file that parses to nothing is the portable stand-in for "cannot be
	// consumed": it must be left alone rather than retired or retried forever.
	bad := filepath.Join(dir, "junk.txt")
	if err := os.WriteFile(bad, []byte("no links in here at all\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	w.Start()
	time.Sleep(120 * time.Millisecond)

	if jobs != 0 {
		t.Errorf("OnJob fired %d times for a file with no links", jobs)
	}
	if _, err := os.Stat(bad); err != nil {
		t.Errorf("the unusable file was removed instead of being left for the user: %v", err)
	}
	if _, err := os.Stat(bad + ".done"); err == nil {
		t.Error("the unusable file was retired as if it had been consumed")
	}
}

// If this fails, only one of the configured drop folders is actually watched,
// which is the whole of what a second folder was added for.
func TestEveryConfiguredFolderIsWatched(t *testing.T) {
	one, two := t.TempDir(), t.TempDir()
	rec := &sink{}
	w, err := New(Options{Folders: []Folder{{Dir: one}, {Dir: two}}, Interval: time.Hour, OnJob: rec.add})
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	defer w.Close()

	write(t, filepath.Join(one, "a.txt"), "https://example.com/a\n")
	write(t, filepath.Join(two, "b.txt"), "https://example.com/b\n")
	pollAll(w)
	pollAll(w)

	jobs := rec.all()
	if len(jobs) != 2 {
		t.Fatalf("consumed %d jobs, want one from each folder", len(jobs))
	}
	seen := map[string]bool{}
	for _, j := range jobs {
		seen[j.URLs[0]] = true
	}
	if !seen["https://example.com/a"] || !seen["https://example.com/b"] {
		t.Fatalf("consumed %v, want the file from both folders", jobs)
	}
}

// TestSameDirectorySpelledTwiceIsWatchedOnce is the trap: two configured rows
// that resolve to one directory would each poll it, and a file dropped into it
// would be parsed by both before either had renamed it away - the same links
// staged twice, from one file, with nothing in the folder left to explain it.
func TestSameDirectorySpelledTwiceIsWatchedOnce(t *testing.T) {
	dir := t.TempDir()
	spellings := []Folder{
		{Dir: dir},
		{Dir: dir + string(filepath.Separator)},
		{Dir: filepath.Join(dir, "somewhere", "..")},
	}
	rec := &sink{}
	w, err := New(Options{Folders: spellings, Interval: time.Hour, OnJob: rec.add})
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	defer w.Close()

	if dirs := w.Dirs(); len(dirs) != 1 {
		t.Fatalf("Dirs = %v, want the three spellings collapsed into one", dirs)
	}
	write(t, filepath.Join(dir, "links.txt"), "https://example.com/one\n")
	pollAll(w)
	pollAll(w)
	if n := rec.count(); n != 1 {
		t.Fatalf("consumed %d jobs from one dropped file, want 1", n)
	}
}

// If this fails, a symlinked share and its target are two folders as far as the
// watcher is concerned, and a container handed the same directory under two
// names consumes every dropped file twice.
func TestASymlinkAndItsTargetAreOneFolder(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "real")
	if err := os.Mkdir(target, 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "link")
	if err := os.Symlink(target, link); err != nil {
		t.Skip("this machine does not let the test create a symlink:", err)
	}

	rec := &sink{}
	w, err := New(Options{Folders: []Folder{{Dir: target}, {Dir: link}}, Interval: time.Hour, OnJob: rec.add})
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	defer w.Close()
	if dirs := w.Dirs(); len(dirs) != 1 {
		t.Fatalf("Dirs = %v, want the link and its target to be one folder", dirs)
	}
}

// If this fails, saving an unrelated setting throws away what every folder knew
// about the files being copied into it, and a settings page held open re-probes
// every share behind it.
func TestAnUnchangedFolderKeepsItsPoller(t *testing.T) {
	one, two := t.TempDir(), t.TempDir()
	w, err := New(Options{Folders: []Folder{{Dir: one}}, Interval: time.Hour, OnJob: func(Job) {}})
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	defer w.Close()

	w.mu.Lock()
	first := w.live[Folder{Dir: one}]
	w.mu.Unlock()
	if first == nil {
		t.Fatal("the configured folder has no poller")
	}

	if errs := w.Apply([]Folder{{Dir: one}, {Dir: two}}); len(errs) != 0 {
		t.Fatalf("apply: %v", errs)
	}
	w.mu.Lock()
	again := w.live[Folder{Dir: one}]
	w.mu.Unlock()
	if again != first {
		t.Fatal("the unchanged folder was rebuilt, want its poller kept")
	}
	if dirs := w.Dirs(); len(dirs) != 2 {
		t.Fatalf("Dirs = %v, want both folders", dirs)
	}

	// Changing how a folder retires its files is a different folder, and that one
	// does have to be rebuilt: the rule is baked into the poller.
	if errs := w.Apply([]Folder{{Dir: one, Delete: true}}); len(errs) != 0 {
		t.Fatalf("apply: %v", errs)
	}
	w.mu.Lock()
	rebuilt := w.live[Folder{Dir: one, Delete: true}]
	stale := w.live[Folder{Dir: one}]
	w.mu.Unlock()
	if rebuilt == nil || rebuilt == first {
		t.Fatal("the folder was not rebuilt after its retirement rule changed")
	}
	if stale != nil {
		t.Fatal("the old poller is still in the live set")
	}
	if !rebuilt.del {
		t.Fatal("the rebuilt poller did not take the new retirement rule")
	}
}

// If this fails, a folder removed and added back leaves its old poller running.
// Nothing on screen says so: the folder is watched, files are picked up, and the
// only trace is a goroutine per settings save and a file consumed twice by the
// two pollers now racing for it.
func TestFolderAddedAndRemovedLeavesNoGoroutineBehind(t *testing.T) {
	one, two := t.TempDir(), t.TempDir()
	rec := &sink{}
	w, err := New(Options{Folders: []Folder{{Dir: one}}, Interval: time.Millisecond, OnJob: rec.add})
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	w.Start()

	base := runtime.NumGoroutine()
	for i := 0; i < 5; i++ {
		if errs := w.Apply([]Folder{{Dir: one}, {Dir: two}}); len(errs) != 0 {
			t.Fatalf("apply: %v", errs)
		}
		if errs := w.Apply([]Folder{{Dir: one}}); len(errs) != 0 {
			t.Fatalf("apply: %v", errs)
		}
	}
	// One folder is live, so the count is back where it started: every poller the
	// five rounds started has been joined by the Apply that dropped it.
	if !settles(func() bool { return runtime.NumGoroutine() <= base }) {
		t.Fatalf("goroutines = %d after five add/remove rounds, want no more than %d", runtime.NumGoroutine(), base)
	}

	// And the folder that came back is really being polled again.
	if errs := w.Apply([]Folder{{Dir: one}, {Dir: two}}); len(errs) != 0 {
		t.Fatalf("apply: %v", errs)
	}
	write(t, filepath.Join(two, "back.txt"), "https://example.com/back\n")
	if !settles(func() bool { return rec.count() == 1 }) {
		t.Fatalf("consumed %d jobs from the re-added folder, want exactly 1", rec.count())
	}

	if err := w.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	if !settles(func() bool { return runtime.NumGoroutine() < base }) {
		t.Fatalf("goroutines = %d after Close, want fewer than the %d with a folder live", runtime.NumGoroutine(), base)
	}
}

// If this fails, a closed watcher quietly takes a new folder list and polls none
// of it, which reads from the outside exactly like a watch folder that has
// stopped working for no reason.
func TestApplyAfterCloseIsRefused(t *testing.T) {
	dir := t.TempDir()
	w, err := New(Options{Dir: dir, Interval: time.Hour, OnJob: func(Job) {}})
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	if errs := w.Apply([]Folder{{Dir: dir}}); len(errs) == 0 {
		t.Fatal("Apply on a closed watcher succeeded, want it refused")
	}
	if dirs := w.Dirs(); len(dirs) != 0 {
		t.Fatalf("Dirs = %v, want nothing watched after Close", dirs)
	}
}

// settles waits for a condition that is true only after something else has
// finished: a goroutine returning after its done channel closed, or a poll
// picking a file up. A fixed sleep would either be flaky on a loaded CI box or
// slow on every run.
func settles(ok func() bool) bool {
	deadline := time.Now().Add(5 * time.Second)
	for {
		if ok() {
			return true
		}
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(2 * time.Millisecond)
	}
}
