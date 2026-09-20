package settings

// Folders and the secrets that travel with them: where downloads land, which
// folder is watched for dropped jobs, and the archive passwords tried on them.

import (
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
	// Everything after the root is a placeholder, so the root is what is left,
	// and only when there was one. "<jd:packagename>/unpacked" has no fixed
	// part and stays relative.
	//
	// Returning the bare separator for that case makes the answer depend on the
	// platform: filepath.IsAbs("/") is true on Linux and filepath.IsAbs(`\`) is
	// false on Windows, so the same template is accepted by the container and
	// refused by the desktop build.
	if strings.HasPrefix(normalised, sep) {
		return sep
	}
	return ""
}

// FixedPrefix is fixedPrefix for callers outside this package: the deepest part
// of a configured folder that is a real path rather than a placeholder.
//
// Anything that measures a configured folder needs it. Stat-ing
// "/downloads/<jd:date>" asks about a directory that never exists, and the
// answer comes from whatever the walk up lands on, which on a fresh install is
// the volume root reported as the download disk. Exported rather than copied,
// since internal/api's splitTemplate is already a second one of these.
func FixedPrefix(dir string) string { return fixedPrefix(dir) }

// WriteProbeName is the throwaway file this package drops into a folder to find
// out whether it can be written to, and removes again immediately.
//
// Exported so that the second place needing one reuses the name.
// internal/app's self-test asks the same question about the download and
// working folders (app_selftest.go), and a probe under a different name would
// be a second thing every scanner in the tree has to be taught to ignore.
// internal/watch's poller skips dotfiles, which is what makes this name safe
// from the folder watcher, so the leading dot is load-bearing. Both callers
// remove the file straight away.
const WriteProbeName = ".knightloader-write-test"

// Validate reports why a directory cannot be used, so the API can refuse a bad
// path instead of downloading somewhere else.
//
// what names the field being checked in the words the person typing into it
// sees ("the download folder", "the working folder"). It is a parameter because
// five fields go through this one function: the download folder, the working
// folder, a category's folder, a batch's folder and a single task's folder.
// With a fixed label all five reported "the download folder must be an absolute
// path", so the field that failed was the one the message did not name. Passing
// it in means a sixth caller cannot compile without answering the question.
func Validate(what, dir string) error {
	if dir == "" {
		return nil // the built-in default is always usable
	}
	if !filepath.IsAbs(dir) {
		return fmt.Errorf("%s must be an absolute path", what)
	}
	// A folder may be a template like /downloads/<jd:date>/<jd:packagename>.
	// Only the part before the first placeholder is a real path: creating the
	// rest would put folders literally named "<jd:date>" on disk, and checking
	// it would test a path that never exists at download time.
	dir = fixedPrefix(dir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("cannot create %s: %w", dir, err)
	}
	probe := filepath.Join(dir, WriteProbeName)
	if err := os.WriteFile(probe, []byte("ok"), 0o644); err != nil {
		return fmt.Errorf("cannot write to %s: %w", dir, err)
	}
	return os.Remove(probe)
}
