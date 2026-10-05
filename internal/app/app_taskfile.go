package app

// A task's own file. The download library renames around a file that already
// has the resolved name and forgets its own tasks on a restart, so a name
// proves nothing: two tasks can resolve to one name, and the half-written file
// of an earlier attempt looks exactly like somebody else's download. What a
// task owns is the path it recorded (core.Task.File), as long as no other task
// recorded the same one and the file is still the size this task was writing.

import (
	"errors"
	"fmt"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/junkerderprovinz/knightloader/internal/collide"
	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/engine"
	"github.com/junkerderprovinz/knightloader/internal/extract"
	"github.com/junkerderprovinz/knightloader/internal/resolver/remotefs"
	"github.com/junkerderprovinz/knightloader/internal/resolver/torrent"
	"github.com/junkerderprovinz/knightloader/internal/resolver/ytdlp"
	"github.com/junkerderprovinz/knightloader/internal/usenet/local"
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
	// empty is a download that finished with nothing in it, the one case
	// where an empty file is the task's own rather than one never written.
	empty bool
	// sidecars describe the file and go with it (see ytdlp.Sidecars).
	sidecars []string
}

// ownFileLocked returns t's recorded file as a leftover it may delete, or the
// zero value when another task recorded or writes the same path. A sidecar
// another task's file has as well, such as the .nfo the video and the audio
// row of one link share, stays for that task. Caller holds a.mu.
func (a *App) ownFileLocked(t *core.Task) leftover {
	if t.File == "" {
		return leftover{}
	}
	shared := map[string]bool{}
	for id, other := range a.tasks {
		if id == t.ID {
			continue
		}
		if samePath(other.File, t.File) || slices.ContainsFunc(other.WorkFiles, func(p string) bool { return samePath(p, t.File) }) {
			return leftover{}
		}
		for _, s := range sidecarsOf(other) {
			shared[filepath.Clean(s)] = true
		}
	}
	size := t.Size
	// A server that sent no length leaves Size unknown, and a finished
	// download's byte count is then the length of its file.
	if size == 0 && t.Status == core.StatusDone {
		size = t.Loaded
	}
	l := leftover{path: t.File, size: size, empty: size == 0 && t.Status == core.StatusDone}
	for _, s := range sidecarsOf(t) {
		if !shared[filepath.Clean(s)] {
			l.sidecars = append(l.sidecars, s)
		}
	}
	return l
}

// sidecarsOf lists the files that describe t's recorded file (see
// ytdlp.Sidecars).
func sidecarsOf(t *core.Task) []string {
	if t.File == "" || t.Resolver != (ytdlp.Resolver{}).Info().ID {
		return nil
	}
	kind, _ := variantDecode(t.Variant)
	if kind == "" {
		kind = ytdlp.VariantVideo
	}
	return ytdlp.Sidecars(kind, t.File)
}

// usedByOther reports whether a task other than id has path as its file or
// among what yt-dlp wrote for it. Two downloads of one title write the same
// names, and the video and the audio row of a link the same info file.
func (a *App) usedByOther(id, path string) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	same := func(p string) bool { return samePath(p, path) }
	for other, t := range a.tasks {
		if other != id && (same(t.File) || slices.ContainsFunc(t.WorkFiles, same)) {
			return true
		}
	}
	return false
}

// intact reports whether the file at the leftover's path is still the one its
// task was writing.
func (l leftover) intact() bool {
	fi, err := os.Lstat(l.path)
	return err == nil && fi.Mode().IsRegular() && (l.size > 0 || l.empty) && fi.Size() == l.size
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
	// A multi-file torrent records its folder, whose own files the engine
	// deletes.
	if fi, err := os.Lstat(l.path); err != nil || fi.IsDir() {
		return
	}
	if !l.intact() {
		log.Printf("left %s where it is: nothing shows it is still the file this download was writing%s", l.path, taskTag(taskID))
		return
	}
	if err := os.Remove(l.path); err != nil {
		log.Printf("could not delete %s: %v%s", l.path, err, taskTag(taskID))
		return
	}
	for _, s := range l.sidecars {
		if err := os.Remove(s); err != nil && !errors.Is(err, fs.ErrNotExist) {
			log.Printf("could not delete %s: %v%s", s, err, taskTag(taskID))
		}
	}
}

// torrentLeftover is where a torrent of the built-in client landed, for
// deleting its files when the engine does not know it, as after a restart.
// paths is a magnet's file list, which the task keeps once the swarm has sent
// it.
type torrentLeftover struct {
	dir, root, uri string
	paths          []string
}

// torrentLeftoverLocked is t's torrent as it lies on disk, or the zero value
// for anything else. Caller holds a.mu.
func (a *App) torrentLeftoverLocked(t *core.Task) torrentLeftover {
	if t.Resolver != (torrent.Resolver{}).Info().ID || t.File == "" {
		return torrentLeftover{}
	}
	return torrentLeftover{dir: a.dirFor(t), root: t.File, uri: t.URL, paths: t.MagnetFiles}
}

// drop deletes the files the torrent names: a magnet's by the list the task
// kept, a .torrent's by the list in its link. A magnet without a list names
// none, so of it only a single file goes, never a folder that could hold
// anything else.
func (l torrentLeftover) drop() {
	if l.root == "" {
		return
	}
	paths := l.paths
	if paths == nil {
		if md, err := (torrent.Resolver{}).Describe(l.uri); err == nil {
			for _, f := range md.Files {
				paths = append(paths, f.Path)
			}
		}
	}
	engine.DeleteTorrentFiles(l.dir, l.root, paths)
}

// usenetPartLocked is the part file of a task from the own Usenet servers, for
// deleting it with the task when their backend does not know the task, as
// after a restart. Caller holds a.mu.
func (a *App) usenetPartLocked(t *core.Task) string {
	if !strings.HasPrefix(t.URL, local.ResolverID+"://") {
		return ""
	}
	return local.PartFile(a.dirFor(t), t.URL, t.ID)
}

// partFileLocked is the part file t's FTP or SFTP download writes until it
// finishes, for deleting it when the backend does not know the task, as after
// a restart. The task's id in its name makes it t's alone, and the link names
// the rest, since a paused row can be renamed. Caller holds a.mu.
func (a *App) partFileLocked(t *core.Task) string {
	if t.Resolver != remotefs.ResolverID {
		return ""
	}
	return remotefs.PartFile(a.dirFor(t), t.URL, t.ID)
}

// recordFileLocked notes where a backend is writing t's bytes, and says so in
// the task's log when that is not under the task's own name: the library
// steps around a file that is already there and rewrites characters a file
// name cannot hold. A yt-dlp row shows its extension apart from its name (see
// core.Task.Ext), and a file under the name with that extension is under the
// task's own name. Caller holds a.mu.
func (a *App) recordFileLocked(t *core.Task, file string) {
	if file == "" || file == t.File {
		return
	}
	t.File = file
	base := filepath.Base(file)
	if t.Name != "" && t.Name != t.URL && base != t.Name && (t.Ext == "" || base != t.Name+"."+t.Ext) {
		log.Printf("this download is saved as %s rather than %s%s", file, t.Name, taskTag(t.ID))
	}
}

// noteMovedLocked records that the app moved the finished file of task id off
// the path its backend wrote it to (see App.movedFiles). Caller holds a.mu.
func (a *App) noteMovedLocked(id string) {
	if a.movedFiles == nil {
		a.movedFiles = map[string]bool{}
	}
	a.movedFiles[id] = true
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
// archive. The error is a missing part and names both files. A part whose
// recorded file is gone was moved by hand, most likely to where the error said,
// and no longer stands in the way. Caller holds a.mu.
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
		missing := &extract.PartError{Part: part.Name, Problem: extract.ErrPartMissing}
		if filepath.Base(part.File) == part.Name {
			missing.Err = fmt.Errorf("%s was downloaded to %s, and the archive looks for it beside its first part in %s. "+
				"Move it there and start the extraction again", part.Name, filepath.Dir(part.File), dir)
			return missing
		}
		missing.Err = fmt.Errorf("%s was saved as %s, and the archive reads each part under its own name. "+
			"Move anything already at %s out of the way, move the download there, and start the extraction again",
			part.Name, part.File, byName)
		return missing
	}
	return nil
}

// samePath reports whether two paths name the same file once cleaned. An empty
// path names nothing.
func samePath(x, y string) bool {
	return x != "" && y != "" && filepath.Clean(x) == filepath.Clean(y)
}
