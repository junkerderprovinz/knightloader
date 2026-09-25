package app

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/settings"
)

// newFilesTestApp returns an App with a real, writable download folder, since
// SafeTaskFile stats and resolves real paths.
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
	// The temp dir itself may sit behind a symlink (macOS /tmp).
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

// A download that had to be saved beside a file of its name is served from
// where it was saved, still under the task's name.
func TestSafeTaskFileServesTheFileTheDownloadWrote(t *testing.T) {
	a, base := newFilesTestApp(t)
	writeTestFile(t, base, "movie.mkv", []byte("somebody else's"))
	writeTestFile(t, base, "movie (1).mkv", []byte("the download"))
	task := putTask(t, a, core.Task{
		URL: "https://host.example/movie.mkv", Name: "movie.mkv", Status: core.StatusDone,
		File: filepath.Join(base, "movie (1).mkv"),
	})

	got, err := a.SafeTaskFile(task.ID)
	if err != nil {
		t.Fatalf("SafeTaskFile: %v", err)
	}
	wantReal, err := filepath.EvalSymlinks(filepath.Join(base, "movie (1).mkv"))
	if err != nil {
		t.Fatal(err)
	}
	if got.Path != wantReal || got.Name != "movie.mkv" {
		t.Errorf("served %q as %q, want %q as movie.mkv", got.Path, got.Name, wantReal)
	}
}

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

// TestSafeTaskFileRefusesADirOutsideTheDownloadRoot: t.Dir is client-supplied,
// and joining a single-segment name onto it is always inside it, so only the
// root check stops a Dir pointed at the app's own settings.json.
func TestSafeTaskFileRefusesADirOutsideTheDownloadRoot(t *testing.T) {
	a, _ := newFilesTestApp(t)
	elsewhere := t.TempDir()
	writeTestFile(t, elsewhere, "settings.json", []byte(`{"reconnect":{"password":"not for a browser"}}`))
	task := putTask(t, a, core.Task{
		URL: "https://host.example/settings.json", Name: "settings.json", Status: core.StatusDone, Dir: elsewhere,
	})

	if _, err := a.SafeTaskFile(task.ID); !errors.Is(err, ErrTaskFileEscape) {
		t.Errorf("err = %v, want ErrTaskFileEscape because a Dir outside the download root must never be served", err)
	}
}

// TestSafeTaskFileRespectsKLBrowseRoots: a folder the chooser could offer under
// KL_BROWSE_ROOTS is served even outside the download tree.
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
		t.Fatalf("SafeTaskFile: %v, want success since KL_BROWSE_ROOTS allows this folder", err)
	}
	wantReal, err := filepath.EvalSymlinks(filepath.Join(elsewhere, "movie.mkv"))
	if err != nil {
		t.Fatal(err)
	}
	if got.Path != wantReal {
		t.Errorf("Path = %q, want %q", got.Path, wantReal)
	}
}

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
		t.Errorf("err = %v, want ErrTaskFileEscape since elsewhere is not under KL_BROWSE_ROOTS", err)
	}
}

// TestSafeTaskFileSizeIsWhatIsOnDiskRightNow: Content-Length must be the bytes
// on disk, or a client waits for bytes that never come.
func TestSafeTaskFileSizeIsWhatIsOnDiskRightNow(t *testing.T) {
	a, base := newFilesTestApp(t)
	writeTestFile(t, base, "movie.mkv", []byte("only nine"))
	task := putTask(t, a, core.Task{
		URL: "https://host.example/movie.mkv", Name: "movie.mkv",
		Status: core.StatusRunning, Size: 9_000_000_000,
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

// TestSafeTaskFileNotYetResolvedIsNoBytesNotAnEscape covers both ways filename()
// returns nothing: no name at all, and a name that is still the URL.
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

func TestSafeTaskFileNothingWrittenYet(t *testing.T) {
	a, _ := newFilesTestApp(t)
	task := putTask(t, a, core.Task{URL: "https://host.example/x.bin", Name: "movie.mkv", Status: core.StatusQueued})
	if _, err := a.SafeTaskFile(task.ID); !errors.Is(err, ErrTaskFileNoBytes) {
		t.Errorf("err = %v, want ErrTaskFileNoBytes", err)
	}
}

func TestSafeTaskFileNotLocalRefusesAJDTask(t *testing.T) {
	a, base := newFilesTestApp(t)
	// A file at the joined path must not change the answer for a JD task.
	writeTestFile(t, base, "movie.mkv", []byte("x"))
	task := putTask(t, a, core.Task{
		URL: "https://host.example/movie.mkv", Name: "movie.mkv", Status: core.StatusDone, Resolver: "jd",
	})
	if _, err := a.SafeTaskFile(task.ID); !errors.Is(err, ErrTaskFileNotLocal) {
		t.Errorf("err = %v, want ErrTaskFileNotLocal", err)
	}
}

func TestSafeTaskFileNameWithSeparatorIsRefused(t *testing.T) {
	a, base := newFilesTestApp(t)
	// The file a naive join would serve.
	writeTestFile(t, filepath.Dir(base), "passwd", []byte("root:x:0:0"))
	task := putTask(t, a, core.Task{
		URL: "https://host.example/x", Name: "../passwd", Status: core.StatusDone,
	})
	if _, err := a.SafeTaskFile(task.ID); !errors.Is(err, ErrTaskFileEscape) {
		t.Errorf("err = %v, want ErrTaskFileEscape", err)
	}
}

func TestSafeTaskFileSymlinkEscapeIsRefused(t *testing.T) {
	a, base := newFilesTestApp(t)
	outside := t.TempDir()
	writeTestFile(t, outside, "secret.bin", []byte("not for this task"))
	if err := os.Symlink(filepath.Join(outside, "secret.bin"), filepath.Join(base, "movie.mkv")); err != nil {
		// Windows needs a privilege to create symlinks.
		t.Skipf("symlinks are not available here: %v", err)
	}
	task := putTask(t, a, core.Task{URL: "https://host.example/movie.mkv", Name: "movie.mkv", Status: core.StatusDone})

	if _, err := a.SafeTaskFile(task.ID); !errors.Is(err, ErrTaskFileEscape) {
		t.Errorf("err = %v, want ErrTaskFileEscape", err)
	}
}
