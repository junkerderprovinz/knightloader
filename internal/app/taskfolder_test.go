package app

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/settings"
	"github.com/junkerderprovinz/knightloader/internal/workdir"
)

func TestTheFolderOpensWhereTheDownloadGoesOrTheNearestParentThatIsThere(t *testing.T) {
	a, _ := newRuleApp(t, func(*settings.Settings, string) {})
	root := t.TempDir()
	pkg := filepath.Join(root, "Films", "Hellraiser")
	a.mu.Lock()
	a.tasks["t1"] = &core.Task{ID: "t1", URL: "https://host.example/a.mkv", Dir: pkg, Status: core.StatusQueued}
	a.mu.Unlock()

	if got, err := a.NearestTaskFolder("t1"); err != nil || got != root {
		t.Errorf("before anything is written: %q, %v; want %q", got, err, root)
	}
	if err := os.MkdirAll(pkg, 0o755); err != nil {
		t.Fatal(err)
	}
	if got, err := a.NearestTaskFolder("t1"); err != nil || got != pkg {
		t.Errorf("once the folder is there: %q, %v; want %q", got, err, pkg)
	}
	if _, err := a.NearestTaskFolder("gone"); !errors.Is(err, ErrTaskFileNotFound) {
		t.Errorf("an unknown task: %v, want ErrTaskFileNotFound", err)
	}
}

// With a working folder, a finished file is found in its package folder
// under the destination, and one still arriving points at where it will land
// rather than at the working folder, without that folder being made.
func TestAPackageFolderIsWhereTheFileLandsNotTheWorkingFolder(t *testing.T) {
	a, base := newFilesTestApp(t)
	work := t.TempDir()
	cfg := a.Settings.Get()
	cfg.WorkDir, cfg.SubfolderByPackage = work, true
	if _, err := a.ApplySettings(cfg); err != nil {
		t.Fatal(err)
	}

	done := filepath.Join(base, "My Package")
	writeTestFile(t, done, "Some File.mkv", []byte("x"))
	finished := putTask(t, a, core.Task{URL: "https://host.example/a", Name: "Some File.mkv", Package: "My Package", Status: core.StatusDone})
	f, err := a.SafeTaskFile(finished.ID)
	if err != nil {
		t.Fatalf("a delivered file: %v", err)
	}
	if !sameFile(t, f.Path, filepath.Join(done, "Some File.mkv")) {
		t.Errorf("a delivered file is at %q, want the one in %q", f.Path, done)
	}

	arriving := putTask(t, a, core.Task{URL: "https://host.example/b", Name: "Next One.mkv", Package: "Other Package", Status: core.StatusRunning})
	dest := filepath.Join(base, "Other Package")
	writeTestFile(t, workdir.For(work, dest), "Next One.mkv", []byte("x"))
	if _, err := a.SafeTaskFile(arriving.ID); !errors.Is(err, ErrTaskFileNoBytes) {
		t.Errorf("a file still in the working folder: %v, want ErrTaskFileNoBytes", err)
	}
	if got, err := a.NearestTaskFolder(arriving.ID); err != nil || got != base {
		t.Errorf("a file still in the working folder opens %q, %v; want %q", got, err, base)
	}
	if _, err := os.Stat(dest); !os.IsNotExist(err) {
		t.Errorf("the package folder was made before anything landed in it: %v", err)
	}
}

// sameFile compares by identity, since a resolved path may be spelt another
// way, as a mapped drive resolves to its share.
func sameFile(t *testing.T, a, b string) bool {
	t.Helper()
	fa, err := os.Stat(a)
	if err != nil {
		t.Fatal(err)
	}
	fb, err := os.Stat(b)
	if err != nil {
		t.Fatal(err)
	}
	return os.SameFile(fa, fb)
}
