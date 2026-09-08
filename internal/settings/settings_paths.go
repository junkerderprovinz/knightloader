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
	// Everything after the root is a placeholder, so the root is what is left -
	// but ONLY when there was a root. "<jd:packagename>/unpacked" has no fixed
	// part at all and has to stay relative.
	//
	// Returning the bare separator for that case made the answer depend on the
	// platform, which is the worst kind of wrong here: filepath.IsAbs("/") is
	// true on Linux and filepath.IsAbs(`\`) is false on Windows, so the same
	// template was accepted by the container and refused by the desktop build.
	// Found by a settings test that was green on Windows and red in CI.
	if strings.HasPrefix(normalised, sep) {
		return sep
	}
	return ""
}

// FixedPrefix is fixedPrefix for callers outside this package: the deepest part
// of a configured folder that is a real path rather than a placeholder.
//
// Exported rather than copied, because there are already two of these - this
// one and internal/api's splitTemplate, whose own comment carries the warning
// about keeping the twins in step - and a third copy is a third thing to get
// wrong in the same way. Anything that MEASURES a configured folder needs it:
// stat-ing "/downloads/<jd:date>" asks about a directory that never exists, and
// the answer comes from whatever the walk up lands on, which on a fresh install
// is the volume root reported with total confidence as the download disk.
func FixedPrefix(dir string) string { return fixedPrefix(dir) }

// Validate reports why a directory cannot be used, so the API can refuse a bad
// path instead of silently downloading somewhere else.
//
// what names the field being checked, in the words the person typing into it
// sees ("the download folder", "the working folder"). It is a parameter and not
// a constant because five different fields are checked by this one function -
// the download folder, the working folder, a category's folder, a batch's
// folder and a single task's folder - and until this parameter existed every
// one of them reported "the download folder must be an absolute path". So the
// field that failed was the one field the message did not name, and the
// working folder, added later, made that visible: somebody typing a relative
// path into it was told to go and fix a download folder that was fine.
//
// Passing it in rather than hardcoding a label per call site is what makes the
// fix stick: a sixth caller cannot compile without answering the question.

// WriteProbeName is the throwaway file this package drops into a folder to
// find out whether it can be written to, and removes again immediately.
//
// EXPORTED SO THAT THE SECOND PLACE THAT NEEDS ONE REUSES THIS NAME RATHER
// THAN INVENTING A SECOND. internal/app's self-test asks the same question
// about the download and working folders (app_selftest.go), and a probe file
// under a different name would be a second thing every scanner in the tree has
// to be taught to ignore - internal/watch's poller skips dotfiles, which is
// precisely what makes THIS name safe from the folder watcher, and a new one
// would inherit that only by accident.
//
// The leading dot is therefore load-bearing rather than cosmetic. So is the
// fact that both callers remove the file straight away: it exists for one
// syscall's worth of time, and a probe left behind in somebody's download
// folder is litter this app has no business creating.
const WriteProbeName = ".knightloader-write-test"

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
