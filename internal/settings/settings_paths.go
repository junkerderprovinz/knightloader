package settings

// Folders and the secrets that travel with them: where downloads land, which
// folder is watched for dropped jobs, and the archive passwords tried on them.

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func sanitizePaths(n Settings) Settings {
	n.DownloadDir = strings.TrimSpace(n.DownloadDir)
	n.WatchDir = strings.TrimSpace(n.WatchDir)
	// A relative watch folder has the same problem as a relative download
	// folder: nobody can say where it actually is.
	if n.WatchDir != "" && !filepath.IsAbs(n.WatchDir) {
		n.WatchDir = ""
	}
	// A relative path would be resolved against whatever the process's working
	// directory happens to be, which is not something a user can reason about.
	if n.DownloadDir != "" && !filepath.IsAbs(n.DownloadDir) {
		n.DownloadDir = ""
	}
	var pw []string
	for _, p := range n.ArchivePasswords {
		if p = strings.TrimSpace(p); p != "" {
			pw = append(pw, p)
		}
	}
	n.ArchivePasswords = pw
	return n
}

// fixedPrefix returns the leading path segments of a folder template that hold
// no placeholder, which is the deepest directory that is the same for every
// task.
func fixedPrefix(dir string) string {
	if !strings.Contains(dir, "<") {
		return dir
	}
	sep := string(filepath.Separator)
	normalised := strings.ReplaceAll(dir, "/", sep)
	parts := strings.Split(normalised, sep)
	keep := parts[:0:0]
	for _, p := range parts {
		if strings.Contains(p, "<") {
			break
		}
		keep = append(keep, p)
	}
	if out := strings.Join(keep, sep); out != "" {
		return out
	}
	// Everything after the root is a placeholder; the root is what is left.
	return sep
}

// Validate reports why a download directory cannot be used, so the API can
// refuse a bad path instead of silently downloading somewhere else.
func Validate(dir string) error {
	if dir == "" {
		return nil // the built-in default is always usable
	}
	if !filepath.IsAbs(dir) {
		return errors.New("the download folder must be an absolute path")
	}
	// A folder may be a template like /downloads/<jd:date>/<jd:packagename>.
	// Only the part before the first placeholder is a real path: creating the
	// rest would put folders literally named "<jd:date>" on disk, and checking
	// it would test a path that never exists at download time.
	dir = fixedPrefix(dir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("cannot create %s: %w", dir, err)
	}
	probe := filepath.Join(dir, ".knightloader-write-test")
	if err := os.WriteFile(probe, []byte("ok"), 0o644); err != nil {
		return fmt.Errorf("cannot write to %s: %w", dir, err)
	}
	return os.Remove(probe)
}
