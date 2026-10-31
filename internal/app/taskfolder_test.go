package app

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/settings"
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
