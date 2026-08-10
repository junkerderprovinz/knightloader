package app

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/settings"
)

// newFilesTestApp is an App with a real, writable download folder - SafeTaskFile
// touches the real filesystem (Stat, EvalSymlinks), so a fixture that only sets
// fields on a struct is not enough to exercise it.
func newFilesTestApp(t *testing.T) (*App, string) {
	t.Helper()
	a, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { a.Close() })
	base := t.TempDir()
	if _, err := a.ApplySettings(settings.Settings{MaxConcurrent: 2, MaxPerHost: 1, DownloadDir: base}); err != nil {
		t.Fatal(err)
	}
	return a, base
}

func writeTestFile(t *testing.T, dir, name string, data []byte) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestSafeTaskFileHappyPath(t *testing.T) {
	a, base := newFilesTestApp(t)
	writeTestFile(t, base, "movie.mkv", []byte("hello world"))
	task := putTask(t, a, core.Task{URL: "https://host.example/movie.mkv", Name: "movie.mkv", Status: core.StatusDone})

	got, err := a.SafeTaskFile(task.ID)
	if err != nil {
		t.Fatalf("SafeTaskFile: %v", err)
	}
	// Resolved on both sides before comparing: t.TempDir() itself resolves
	// through a symlink on some platforms (macOS's /tmp is the textbook
	// case), and SafeTaskFile is documented to hand back the resolved path.
	wantReal, err := filepath.EvalSymlinks(filepath.Join(base, "movie.mkv"))
	if err != nil {
		t.Fatal(err)
	}
	if got.Path != wantReal {
		t.Errorf("Path = %q, want %q", got.Path, wantReal)
	}
	if got.Name != "movie.mkv" {
		t.Errorf("Name = %q, want movie.mkv", got.Name)
	}
	if got.Size != int64(len("hello world")) {
		t.Errorf("Size = %d, want %d", got.Size, len("hello world"))
	}
}

// TestSafeTaskFilePerTaskDirWorksWithinTheDownloadRoot proves the legitimate
// half of the per-task Dir override still works: a task redirected to a
// subfolder of the configured download tree (the ordinary case - a
// collector "target folder" field pointed at a subdirectory) is served.
func TestSafeTaskFilePerTaskDirWorksWithinTheDownloadRoot(t *testing.T) {
	a, base := newFilesTestApp(t)
	sub := filepath.Join(base, "Movies", "2026")
	writeTestFile(t, sub, "movie.mkv", []byte("x"))
	task := putTask(t, a, core.Task{
		URL: "https://host.example/movie.mkv", Name: "movie.mkv", Status: core.StatusDone, Dir: sub,
	})

	got, err := a.SafeTaskFile(task.ID)
	if err != nil {
		t.Fatalf("SafeTaskFile: %v", err)
	}
	wantReal, err := filepath.EvalSymlinks(filepath.Join(sub, "movie.mkv"))
	if err != nil {
		t.Fatal(err)
	}
	if got.Path != wantReal {
		t.Errorf("Path = %q, want %q", got.Path, wantReal)
	}
}

// TestSafeTaskFileRefusesADirOutsideTheDownloadRoot is the fix for a real,
// live vulnerability: t.Dir is a client-supplied field with no validation
// of its own (SetTaskOptions/AddLinks only trim it), and the join-then-
// resolve check alone can never catch a Dir set outside the app's own
// download tree, because join(dir, singleSegmentName) is inside dir by
// construction - it was proving "X is inside X-ish", not "X is somewhere
// this app is allowed to read from". Before this test's fix landed, this
// exact case served the file: a task's Dir set to an arbitrary directory
// handed back whatever file matched its stored name, this app's own
// settings.json or database included if a name happened to match.
func TestSafeTaskFileRefusesADirOutsideTheDownloadRoot(t *testing.T) {
	a, _ := newFilesTestApp(t)
	elsewhere := t.TempDir()
	writeTestFile(t, elsewhere, "settings.json", []byte(`{"reconnect":{"password":"not for a browser"}}`))
	task := putTask(t, a, core.Task{
		URL: "https://host.example/settings.json", Name: "settings.json", Status: core.StatusDone, Dir: elsewhere,
	})

	if _, err := a.SafeTaskFile(task.ID); !errors.Is(err, ErrTaskFileEscape) {
		t.Errorf("err = %v, want ErrTaskFileEscape - a Dir outside the download root must never be served", err)
	}
}

// TestSafeTaskFileRespectsKLBrowseRoots proves an operator's explicit
// KL_BROWSE_ROOTS narrowing is honoured exactly the way the folder chooser
// (internal/api/routes_folders.go) already honours it: a Dir the operator
// deliberately allowed (even outside the configured download tree) is
// still served, matching the folder picker's own promise that anything it
// could offer, this route can later read.
func TestSafeTaskFileRespectsKLBrowseRoots(t *testing.T) {
	a, _ := newFilesTestApp(t)
	elsewhere := t.TempDir()
	writeTestFile(t, elsewhere, "movie.mkv", []byte("x"))
	t.Setenv("KL_BROWSE_ROOTS", elsewhere)
	task := putTask(t, a, core.Task{
		URL: "https://host.example/movie.mkv", Name: "movie.mkv", Status: core.StatusDone, Dir: elsewhere,
	})

	got, err := a.SafeTaskFile(task.ID)
	if err != nil {
		t.Fatalf("SafeTaskFile: %v, want success - KL_BROWSE_ROOTS explicitly allows this folder", err)
	}
	wantReal, err := filepath.EvalSymlinks(filepath.Join(elsewhere, "movie.mkv"))
	if err != nil {
		t.Fatal(err)
	}
	if got.Path != wantReal {
		t.Errorf("Path = %q, want %q", got.Path, wantReal)
	}
}

// TestSafeTaskFileKLBrowseRootsStillRefusesOutsideIt proves the narrowing
// really is a boundary and not merely a hint: a Dir outside even an
// explicitly configured KL_BROWSE_ROOTS is refused, not silently widened.
func TestSafeTaskFileKLBrowseRootsStillRefusesOutsideIt(t *testing.T) {
	a, _ := newFilesTestApp(t)
	allowed := t.TempDir()
	elsewhere := t.TempDir()
	writeTestFile(t, elsewhere, "movie.mkv", []byte("x"))
	t.Setenv("KL_BROWSE_ROOTS", allowed)
	task := putTask(t, a, core.Task{
		URL: "https://host.example/movie.mkv", Name: "movie.mkv", Status: core.StatusDone, Dir: elsewhere,
	})

	if _, err := a.SafeTaskFile(task.ID); !errors.Is(err, ErrTaskFileEscape) {
		t.Errorf("err = %v, want ErrTaskFileEscape - elsewhere is not under the configured KL_BROWSE_ROOTS", err)
	}
}

// TestSafeTaskFileSizeIsWhatIsOnDiskRightNow is the other half of the security
// check: a running task's Content-Length has to be the bytes really there, not
// the task's own expected total, or a client hangs waiting for bytes that are
// never coming.
func TestSafeTaskFileSizeIsWhatIsOnDiskRightNow(t *testing.T) {
	a, base := newFilesTestApp(t)
	writeTestFile(t, base, "movie.mkv", []byte("only nine"))
	task := putTask(t, a, core.Task{
		URL: "https://host.example/movie.mkv", Name: "movie.mkv",
		Status: core.StatusRunning, Size: 9_000_000_000, // what the task believes the finished size will be
	})

	got, err := a.SafeTaskFile(task.ID)
	if err != nil {
		t.Fatalf("SafeTaskFile: %v", err)
	}
	if want := int64(len("only nine")); got.Size != want {
		t.Errorf("Size = %d, want the %d bytes actually on disk, not the task's expected total", got.Size, want)
	}
}

func TestSafeTaskFileUnknownTask(t *testing.T) {
	a, _ := newFilesTestApp(t)
	if _, err := a.SafeTaskFile("does-not-exist"); !errors.Is(err, ErrTaskFileNotFound) {
		t.Errorf("err = %v, want ErrTaskFileNotFound", err)
	}
}

// TestSafeTaskFileNotYetResolvedIsNoBytesNotAnEscape checks the two branches
// filename() can take to the same empty answer: a Name that was never set, and
// one that still equals the URL because nothing has resolved it yet. Neither is
// a security refusal - a link sitting in the collector is not a break-in
// attempt, and the distinction is what a caller uses to say "not yet" instead
// of "refused".
func TestSafeTaskFileNotYetResolvedIsNoBytesNotAnEscape(t *testing.T) {
	a, _ := newFilesTestApp(t)
	cases := map[string]core.Task{
		"empty name":         {URL: "https://host.example/x.bin", Status: core.StatusCollected},
		"name still the url": {URL: "https://host.example/x.bin", Name: "https://host.example/x.bin", Status: core.StatusCollected},
	}
	for name, tmpl := range cases {
		t.Run(name, func(t *testing.T) {
			task := putTask(t, a, tmpl)
			if _, err := a.SafeTaskFile(task.ID); !errors.Is(err, ErrTaskFileNoBytes) {
				t.Errorf("err = %v, want ErrTaskFileNoBytes", err)
			}
		})
	}
}

// TestSafeTaskFileNothingWrittenYet is a name that did resolve (the collector's
// own probe can do that before a download ever starts) but nothing under it
// exists on disk yet.
func TestSafeTaskFileNothingWrittenYet(t *testing.T) {
	a, _ := newFilesTestApp(t)
	task := putTask(t, a, core.Task{URL: "https://host.example/x.bin", Name: "movie.mkv", Status: core.StatusQueued})
	if _, err := a.SafeTaskFile(task.ID); !errors.Is(err, ErrTaskFileNoBytes) {
		t.Errorf("err = %v, want ErrTaskFileNoBytes", err)
	}
}

func TestSafeTaskFileNotLocalRefusesAJDTask(t *testing.T) {
	a, base := newFilesTestApp(t)
	// Written to disk on purpose: even a file that happens to exist at the
	// join must still be refused, because a "jd" task's bytes are not
	// this app's to vouch for regardless of what sits at the path.
	writeTestFile(t, base, "movie.mkv", []byte("x"))
	task := putTask(t, a, core.Task{
		URL: "https://host.example/movie.mkv", Name: "movie.mkv", Status: core.StatusDone, Resolver: "jd",
	})
	if _, err := a.SafeTaskFile(task.ID); !errors.Is(err, ErrTaskFileNotLocal) {
		t.Errorf("err = %v, want ErrTaskFileNotLocal", err)
	}
}

// TestSafeTaskFileNameWithSeparatorIsRefused is the lexical half of the escape
// check: a resolved name that is not one path segment must not reach
// filepath.Join at all, the same rule SetTaskOptions already enforces on a
// rename.
func TestSafeTaskFileNameWithSeparatorIsRefused(t *testing.T) {
	a, base := newFilesTestApp(t)
	// The file a naive join would have served, so a bug here would not just
	// error out, it would successfully hand back somebody else's bytes.
	writeTestFile(t, filepath.Dir(base), "passwd", []byte("root:x:0:0"))
	task := putTask(t, a, core.Task{
		URL: "https://host.example/x", Name: "../passwd", Status: core.StatusDone,
	})
	if _, err := a.SafeTaskFile(task.ID); !errors.Is(err, ErrTaskFileEscape) {
		t.Errorf("err = %v, want ErrTaskFileEscape", err)
	}
}

// TestSafeTaskFileSymlinkEscapeIsRefused is why the check resolves symlinks
// instead of comparing prefixes: a link planted inside the task's own folder
// passes any check that stops at the folder's name.
func TestSafeTaskFileSymlinkEscapeIsRefused(t *testing.T) {
	a, base := newFilesTestApp(t)
	outside := t.TempDir()
	writeTestFile(t, outside, "secret.bin", []byte("not for this task"))
	if err := os.Symlink(filepath.Join(outside, "secret.bin"), filepath.Join(base, "movie.mkv")); err != nil {
		// Windows needs a privilege for this; the rule is the same either way
		// and the platform that ships is the one that can make the link.
		t.Skipf("symlinks are not available here: %v", err)
	}
	task := putTask(t, a, core.Task{URL: "https://host.example/movie.mkv", Name: "movie.mkv", Status: core.StatusDone})

	if _, err := a.SafeTaskFile(task.ID); !errors.Is(err, ErrTaskFileEscape) {
		t.Errorf("err = %v, want ErrTaskFileEscape", err)
	}
}
