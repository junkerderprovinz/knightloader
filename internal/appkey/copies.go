package appkey

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"
)

// CopiesPrefix starts the name of the folder KeepCopies makes.
const CopiesPrefix = "before-app-key-"

// KeepCopies copies those of names that exist in dataDir into a new folder
// beside them and returns the folder, or "" when none of them exists. It runs
// before the first start with a key rewrites the files sealed, so a sealing
// that goes wrong can be undone by hand. The copies are as readable as the
// files were, which is why the log line that names the folder asks for it to
// be deleted once the instance runs.
func KeepCopies(dataDir string, names []string, now time.Time) (string, error) {
	dir := filepath.Join(dataDir, CopiesPrefix+now.Format("20060102-150405"))
	made := false
	for _, name := range names {
		b, err := os.ReadFile(filepath.Join(dataDir, name))
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return "", fmt.Errorf("keeping a copy of %s: %w", name, err)
		}
		if !made {
			if err := os.MkdirAll(dir, 0o700); err != nil {
				return "", fmt.Errorf("keeping a copy of %s: %w", name, err)
			}
			made = true
		}
		if err := os.WriteFile(filepath.Join(dir, name), b, 0o600); err != nil {
			return "", fmt.Errorf("keeping a copy of %s: %w", name, err)
		}
	}
	if !made {
		return "", nil
	}
	return dir, nil
}
