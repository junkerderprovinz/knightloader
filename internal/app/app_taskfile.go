package app

// A task's own file. The download library renames around a file that already
// has the resolved name and forgets its own tasks on a restart, so a name
// proves nothing: two tasks can resolve to one name, and the half-written file
// of an earlier attempt looks exactly like somebody else's download. What a
// task owns is the path it recorded (core.Task.File), as long as no other task
// recorded the same one and the file is still the size this task was writing.

import (
	"fmt"
	"log"
	"os"
	"path/filepath"

	"github.com/junkerderprovinz/knightloader/internal/collide"
	"github.com/junkerderprovinz/knightloader/internal/core"
)

// fileOfLocked is where t's file is: the path it recorded, or where its name
// puts it in the folder its bytes are written to.
//
// A recorded file that is gone, with a file under the task's own name beside
// it, was renamed back by hand, "name (1).rar" to "name.rar", and the file
// under the name is then the task's, unless another task recorded it. The
// look at the disk is made only for a recorded name that differs from the
// task's, which few tasks have. Caller holds a.mu.
func (a *App) fileOfLocked(t *core.Task) string {
	if t.File == "" {
		return filepath.Join(a.workDirFor(t), t.Name)
	}
	named, ok := namedBeside(t)
	if !ok || samePath(named, t.File) {
		return t.File
	}
	if _, err := os.Lstat(t.File); err == nil {
		return t.File
	}
	if fi, err := os.Lstat(named); err != nil || !fi.Mode().IsRegular() {
		return t.File
	}
	for id, other := range a.tasks {
		if id != t.ID && samePath(other.File, named) {
			return t.File
		}
	}
	return named
}

// namedBeside is where t's own name puts its file in the folder of the file it
// recorded, and false when the task has no name of its own yet.
func namedBeside(t *core.Task) (string, bool) {
	if t.File == "" || t.Name == "" || t.Name == t.URL {
		return "", false
	}
	return filepath.Join(filepath.Dir(t.File), collide.SafeName(t.Name)), true
}

// leftover is a file an earlier attempt of a task wrote, with the size that
// attempt was writing. The library preallocates the whole file before the
// first byte arrives, so a half-written file of this task's already has
// exactly that size, and a file of any other size at the path is not it.
type leftover struct {
	path string
	size int64
}

// ownFileLocked returns t's recorded file as a leftover it may delete, or the
// zero value when another task recorded the same path. Caller holds a.mu.
func (a *App) ownFileLocked(t *core.Task) leftover {
	if t.File == "" {
		return leftover{}
	}
	for id, other := range a.tasks {
		if id != t.ID && samePath(other.File, t.File) {
			return leftover{}
		}
	}
	return leftover{path: t.File, size: t.Size}
}

// intact reports whether the file at the leftover's path is still the one its
// task was writing.
func (l leftover) intact() bool {
	fi, err := os.Lstat(l.path)
	return err == nil && fi.Mode().IsRegular() && l.size > 0 && fi.Size() == l.size
}

// at reports whether path is this leftover, still intact.
func (l leftover) at(path string) bool {
	return samePath(l.path, path) && l.intact()
}

// drop deletes the leftover if it is still the file the task was writing. It
// runs off a.mu, since a stat on a network share can take its time.
func (l leftover) drop(taskID string) {
	if l.path == "" {
		return
	}
	if _, err := os.Lstat(l.path); err != nil {
		return
	}
	if !l.intact() {
		log.Printf("left %s where it is: nothing shows it is still the file this download was writing%s", l.path, taskTag(taskID))
		return
	}
	if err := os.Remove(l.path); err != nil {
		log.Printf("could not delete %s: %v%s", l.path, err, taskTag(taskID))
	}
}

// recordFileLocked notes where a backend is writing t's bytes, and says so in
// the task's log when that is not under the task's own name: the library
// steps around a file that is already there and rewrites characters a file
// name cannot hold. Caller holds a.mu.
func (a *App) recordFileLocked(t *core.Task, file string) {
	if file == "" || file == t.File {
		return
	}
	t.File = file
	if t.Name != "" && t.Name != t.URL && filepath.Base(file) != t.Name {
		log.Printf("this download is saved as %s rather than %s%s", file, t.Name, taskTag(t.ID))
	}
}

// setPathLocked is where a set of parts is opened from: its first part's name,
// in the folder that part was written to, since every later part is looked
// for by name beside it. A set of one is opened from its file, whatever that
// is called. Caller holds a.mu.
func (a *App) setPathLocked(first *core.Task, parts int) string {
	if parts < 2 {
		return a.fileOfLocked(first)
	}
	return filepath.Join(filepath.Dir(a.fileOfLocked(first)), first.Name)
}

// volumeMismatchLocked checks every part of first's set against the file its
// download recorded. The readers find each part by name beside the first one,
// so a part that had to be written under another name or into another folder
// would be read from the wrong file, and that failure reads as a damaged
// archive. The error names both files. A part whose recorded file is gone was
// moved by hand, most likely to where the error said, and no longer stands in
// the way. Caller holds a.mu.
func (a *App) volumeMismatchLocked(first *core.Task) error {
	set := a.volumeSetLocked(first)
	if len(set) < 2 {
		return nil
	}
	dir := filepath.Dir(a.setPathLocked(first, len(set)))
	for _, part := range set {
		byName := filepath.Join(dir, part.Name)
		if part.File == "" || samePath(part.File, byName) {
			continue
		}
		if _, err := os.Lstat(part.File); err != nil {
			continue
		}
		if filepath.Base(part.File) == part.Name {
			return fmt.Errorf("%s was downloaded to %s, and the archive looks for it beside its first part in %s. "+
				"Move it there and start the extraction again", part.Name, filepath.Dir(part.File), dir)
		}
		return fmt.Errorf("%s was saved as %s, and the archive reads each part under its own name. "+
			"Move anything already at %s out of the way, move the download there, and start the extraction again",
			part.Name, part.File, byName)
	}
	return nil
}

// samePath reports whether two paths name the same file once cleaned. An empty
// path names nothing.
func samePath(x, y string) bool {
	return x != "" && y != "" && filepath.Clean(x) == filepath.Clean(y)
}
