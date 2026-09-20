package app

// SafeTaskFile is the one place a task's file on disk is located, for both the
// streaming route and the desktop reveal/open bindings, so the security check
// exists only once.

import (
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/junkerderprovinz/knightloader/internal/core"
)

// envBrowseRoots is the variable internal/api/routes_folders.go reads too, so
// narrowing the folder chooser narrows file access the same way.
const envBrowseRoots = "KL_BROWSE_ROOTS"

// Errors SafeTaskFile returns. The HTTP route and the desktop bindings report
// them differently, so callers branch with errors.Is.
var (
	ErrTaskFileNotFound = errors.New("no such task")
	// ErrTaskFileNotLocal is a task whose file lives in another process's
	// filesystem, which is the JD backend (see filesAreLocal).
	ErrTaskFileNotLocal = errors.New("this task's file was not downloaded by this app, so it cannot be reached from here")
	// ErrTaskFileNoBytes is a task with nothing on disk yet. It is kept apart
	// from ErrTaskFileEscape because a link that has not started is not an
	// attack.
	ErrTaskFileNoBytes = errors.New("nothing has been downloaded yet")
	// ErrTaskFileEscape means the task's folder or file resolves outside where
	// it is allowed to be.
	ErrTaskFileEscape = errors.New("refused: this task's file does not resolve inside its own download folder")
)

// TaskFile is a task's file as it currently is on disk. Path has been checked
// after symlink resolution, so callers open exactly that path.
type TaskFile struct {
	// Name is the task's file name, for a Content-Disposition header. It may
	// differ from the last segment of Path.
	Name string
	Path string
	// Size is a snapshot from the same Stat; a running download still grows.
	Size int64
}

// SafeTaskFile locates a task's file the way dirFor does and refuses when the
// result does not check out:
//
//  1. The task's files must be local (see filesAreLocal).
//  2. The stored name must pass usableFilename, the same rule a rename uses.
//  3. dirFor(t), after filepath.EvalSymlinks, must be inside fileServeRoots.
//     t.Dir is a client-supplied override with no validation of its own, so
//     without this a crafted Dir could serve settings.json or the database.
//  4. The joined file, after filepath.EvalSymlinks, must still be inside that
//     folder, which catches a planted symlink.
func (a *App) SafeTaskFile(id string) (TaskFile, error) {
	a.mu.Lock()
	t := a.tasks[id]
	var snap core.Task
	if t != nil {
		snap = *t
	}
	a.mu.Unlock()
	if t == nil {
		return TaskFile{}, ErrTaskFileNotFound
	}
	if !filesAreLocal(&snap) {
		return TaskFile{}, ErrTaskFileNotLocal
	}
	name := filename(&snap)
	if name == "" {
		return TaskFile{}, ErrTaskFileNoBytes
	}
	if !usableFilename(name) {
		return TaskFile{}, ErrTaskFileEscape
	}

	dir := a.dirFor(&snap)
	full := filepath.Join(dir, name)

	realDir, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return TaskFile{}, ErrTaskFileNoBytes
	}
	realFull, err := filepath.EvalSymlinks(full)
	if err != nil {
		return TaskFile{}, ErrTaskFileNoBytes
	}
	roots, err := a.fileServeRoots(realDir)
	if err != nil {
		return TaskFile{}, ErrTaskFileEscape
	}
	inRoots := false
	for _, root := range roots {
		if withinDir(root, realDir) {
			inRoots = true
			break
		}
	}
	if !inRoots || !withinDir(realDir, realFull) {
		return TaskFile{}, ErrTaskFileEscape
	}

	fi, err := os.Stat(realFull)
	if err != nil || fi.IsDir() {
		return TaskFile{}, ErrTaskFileNoBytes
	}
	return TaskFile{Name: name, Path: realFull, Size: fi.Size()}, nil
}

// withinDir reports whether the resolved path p is dir or below it. It uses
// filepath.Rel because a Windows path comparison must ignore case. internal/api
// has the same helper but cannot be imported from here.
func withinDir(dir, p string) bool {
	rel, err := filepath.Rel(dir, p)
	if err != nil {
		return false
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)))
}

// fileServeRoots returns the folders a task's Dir must resolve inside. With
// KL_BROWSE_ROOTS set they match the folder chooser's roots. Unset, the
// boundary is the download tree rather than the whole filesystem the chooser
// allows: in a container the settings and database share a volume with the
// downloads, and this route serves bytes, not folder names.
func (a *App) fileServeRoots(p string) ([]string, error) {
	set := strings.TrimSpace(os.Getenv(envBrowseRoots))
	if set == "" {
		root := fixedPathPrefix(a.defaultDir())
		real, err := filepath.EvalSymlinks(root)
		if err != nil {
			// Refuse rather than fall back to something wider while setup is
			// incomplete.
			return nil, err
		}
		return []string{real}, nil
	}
	var out []string
	for _, part := range filepath.SplitList(set) {
		part = strings.TrimSpace(part)
		if part == "" || !filepath.IsAbs(part) {
			continue
		}
		if real, err := filepath.EvalSymlinks(part); err == nil {
			out = append(out, filepath.Clean(real))
			continue
		}
		out = append(out, filepath.Clean(part))
	}
	if len(out) == 0 {
		return nil, errors.New(envBrowseRoots + " is set but names no absolute folder, so nothing may be reached")
	}
	return out, nil
}

// fixedPathPrefix returns the leading segments of a folder template that hold
// no <jd:...> placeholder. It mirrors internal/settings' unexported
// fixedPrefix.
func fixedPathPrefix(dir string) string {
	if !strings.Contains(dir, "<") {
		return dir
	}
	sep := string(filepath.Separator)
	parts := strings.Split(strings.ReplaceAll(dir, "/", sep), sep)
	var keep []string
	for _, p := range parts {
		if strings.Contains(p, "<") {
			break
		}
		keep = append(keep, p)
	}
	if out := strings.Join(keep, sep); out != "" {
		return out
	}
	return sep
}
