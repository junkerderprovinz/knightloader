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
	// A relative path would be resolved against whatever the process's working
	// directory happens to be, which is not something a user can reason about.
	// A save refuses one (CheckFolders), so this only cleans a hand-edited
	// settings file.
	if relative(n.WatchDir, false) {
		n.WatchDir = ""
	}
	if relative(n.DownloadDir, false) {
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

// relative reports whether dir is set but not an absolute path. A template
// only needs its fixed prefix to be absolute.
func relative(dir string, template bool) bool {
	if dir == "" {
		return false
	}
	if template {
		dir = fixedPrefix(dir)
	}
	return !filepath.IsAbs(dir)
}

// PathProblem is why a folder cannot be used. Code is what the interface
// translates: "notAbsolute", "cannotCreate" or "cannotWrite".
type PathProblem struct {
	// Field is the folder's key in the settings document, set by CheckFolders
	// and empty for a folder that is not a top-level setting.
	Field string
	What  string
	Code  string
	Dir   string
	Err   error
}

func (p *PathProblem) Error() string {
	switch p.Code {
	case "cannotCreate":
		return fmt.Sprintf("cannot create %s: %v", p.Dir, p.Err)
	case "cannotWrite":
		return fmt.Sprintf("cannot write to %s: %v", p.Dir, p.Err)
	}
	return p.What + " must be an absolute path"
}

func (p *PathProblem) Unwrap() error { return p.Err }

// folderFields are the top-level folders a save can name, with the words a
// refusal uses for each. The download and working folders are probed at once,
// since every download writes there. The others only have to be absolute, the
// rule sanitize holds them to. All of them are created on first use.
var folderFields = []struct {
	key, what       string
	get             func(Settings) string
	probe, template bool
}{
	{"downloadDir", "the download folder", func(s Settings) string { return s.DownloadDir }, true, false},
	{"workDir", "the working folder", func(s Settings) string { return s.WorkDir }, true, false},
	{"watchDir", "the watch folder", func(s Settings) string { return s.WatchDir }, false, false},
	{"extractTo", "the extraction folder", func(s Settings) string { return s.ExtractTo }, false, true},
	{"extractMoveTo", "the folder unpacked files move to", func(s Settings) string { return s.ExtractMoveTo }, false, true},
}

// CheckFolders returns a *PathProblem for the first top-level folder in s that
// a save cannot keep, so the save is refused rather than sanitize clearing
// what was typed. named limits the check to the fields a patch sends; nil
// checks every one.
func CheckFolders(s Settings, named func(key string) bool) error {
	for _, f := range folderFields {
		if named != nil && !named(f.key) {
			continue
		}
		dir := strings.TrimSpace(f.get(s))
		var err error
		if f.probe {
			err = Validate(f.what, dir)
		} else if relative(dir, f.template) {
			err = &PathProblem{What: f.what, Code: "notAbsolute", Dir: dir}
		}
		if err != nil {
			if p := (*PathProblem)(nil); errors.As(err, &p) {
				p.Field = f.key
			}
			return err
		}
	}
	return nil
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
// out whether it can be written to, and removes again immediately. With a
// suffix it also names the throwaway folder that asks whether a missing folder
// could be created.
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
// It creates nothing. The settings page saves while a path is still being
// typed, so a folder made here would leave "D:\Down" behind on the way to
// "D:\Downloads". A folder that is not there yet passes when the nearest folder
// above it that does exist would let this process create it; the first download
// into it, or the folder chooser's New folder, makes it for real.
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
		return &PathProblem{What: what, Code: "notAbsolute", Dir: dir}
	}
	// A folder may be a template like /downloads/<jd:date>/<jd:packagename>.
	// Only the part before the first placeholder is a real path: checking the
	// rest would test a path that never exists at download time.
	dir = fixedPrefix(dir)
	if fi, err := os.Stat(dir); err == nil {
		if !fi.IsDir() {
			return &PathProblem{What: what, Code: "cannotCreate", Dir: dir, Err: errNotAFolder}
		}
		probe := filepath.Join(dir, WriteProbeName)
		if err := os.WriteFile(probe, []byte("ok"), 0o644); err != nil {
			return &PathProblem{What: what, Code: "cannotWrite", Dir: dir, Err: err}
		}
		return os.Remove(probe)
	}
	if err := canCreateBelow(nearestExisting(dir)); err != nil {
		return &PathProblem{What: what, Code: "cannotCreate", Dir: dir, Err: err}
	}
	return nil
}

var errNotAFolder = errors.New("a file is in the way")

// nearestExisting walks up from dir to the first path that exists, or returns
// the volume root when nothing on the way does.
func nearestExisting(dir string) string {
	for {
		if _, err := os.Stat(dir); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return dir
		}
		dir = parent
	}
}

// canCreateBelow finds out whether a folder can be made inside parent by making
// one and removing it again. A file probe would give the wrong answer on a
// Windows drive root, which lets users create folders but not files.
func canCreateBelow(parent string) error {
	fi, err := os.Stat(parent)
	if err != nil {
		return err
	}
	if !fi.IsDir() {
		return errNotAFolder
	}
	probe, err := os.MkdirTemp(parent, WriteProbeName+"-")
	if err != nil {
		return err
	}
	return os.Remove(probe)
}
