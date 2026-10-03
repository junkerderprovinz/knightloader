package app

// SafeTaskFile is the one place a task's file on disk is located, for both the
// streaming route and the desktop reveal/open bindings, so the security check
// exists only once.

import (
	"context"
	"errors"
	"io"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"

	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/realpath"
	"github.com/junkerderprovinz/knightloader/internal/resolver/torrent"
	"github.com/junkerderprovinz/knightloader/internal/settings"
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
	// ErrTaskFileNoSuchFile is a file index the task's torrent does not have
	// or does not fetch.
	ErrTaskFileNoSuchFile = errors.New("the torrent has no such file, or it is not being downloaded")
	// ErrTaskFileNoMedia is a torrent of several files with no audio or video
	// among the ones it fetches, so there is nothing for Play to open.
	ErrTaskFileNoMedia = errors.New("this torrent fetches no audio or video file")
	// ErrTaskFileIncomplete is a download that stopped before it finished,
	// after it had fetched something. Only a running one can be read before
	// it is complete.
	ErrTaskFileIncomplete = errors.New("this download stopped before it finished; start it again to play the file while it downloads")
	// ErrTaskFileMending is a running download whose file is not all there
	// and which the engine does not stream, mostly because the ranges that
	// never arrived are being fetched again.
	ErrTaskFileMending = errors.New("part of this download is being fetched again, so it plays once that part is back")
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
//  3. dirFor(t), with every link resolved, must be inside fileServeRoots.
//     t.Dir is a client-supplied override with no validation of its own, so
//     without this a crafted Dir could serve settings.json or the database.
//  4. The joined file, with every link resolved, must still be inside that
//     folder, which catches a planted symlink.
func (a *App) SafeTaskFile(id string) (TaskFile, error) {
	return a.SafeTaskFileAt(id, -1)
}

// SafeTaskFileAt is SafeTaskFile for one file of a torrent, by its index in
// the torrent, under the task's folder. A negative index is the task's own
// file.
func (a *App) SafeTaskFileAt(id string, index int) (TaskFile, error) {
	snap, err := a.taskSnapshot(id)
	if err != nil {
		return TaskFile{}, err
	}
	at, err := a.fileTarget(&snap, index)
	if err != nil {
		return TaskFile{}, err
	}
	realFull, err := realpath.Resolve(at.path)
	if err != nil {
		return TaskFile{}, ErrTaskFileNoBytes
	}
	boundary := at.realDir
	if at.inside != "" {
		if boundary, err = realpath.Resolve(at.inside); err != nil {
			return TaskFile{}, ErrTaskFileNoBytes
		}
		if !withinDir(at.realDir, boundary) {
			return TaskFile{}, ErrTaskFileEscape
		}
	}
	if !withinDir(boundary, realFull) {
		return TaskFile{}, ErrTaskFileEscape
	}
	fi, err := os.Stat(realFull)
	if err != nil || fi.IsDir() {
		return TaskFile{}, ErrTaskFileNoBytes
	}
	return TaskFile{Name: at.name, Path: realFull, Size: fi.Size()}, nil
}

func (a *App) taskSnapshot(id string) (core.Task, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	t := a.tasks[id]
	if t == nil {
		return core.Task{}, ErrTaskFileNotFound
	}
	snap := *t
	snap.TorrentFiles = slices.Clone(t.TorrentFiles)
	return snap, nil
}

// fileTarget is where a file of a task should be: its name, its path as
// joined and not yet resolved, and the task's folder with every link
// resolved. inside is the torrent's own folder for a file of a torrent of
// several, which the file must not leave either.
type fileTarget struct {
	name, path, realDir, inside string
}

// fileTarget runs steps 1 to 3 of SafeTaskFile, which need nothing on disk
// but the task's folder.
func (a *App) fileTarget(snap *core.Task, index int) (fileTarget, error) {
	if !filesAreLocal(snap) {
		return fileTarget{}, ErrTaskFileNotLocal
	}
	name := filename(snap)
	if name == "" {
		return fileTarget{}, ErrTaskFileNoBytes
	}
	if !usableFilename(name) {
		return fileTarget{}, ErrTaskFileEscape
	}

	dir := a.dirFor(snap)
	full := filepath.Join(dir, name)
	// The library saves a download beside a file that already has its name,
	// and what sits under the task's name is then somebody else's file. A
	// torrent whose name is taken goes one level deeper, into a folder made
	// for it there.
	if snap.File != "" && sameDir(filepath.Dir(snap.File), dir) {
		full = filepath.Join(dir, filepath.Base(snap.File))
	} else if snap.File != "" && torrent.IsURI(snap.URL) && sameDir(filepath.Dir(filepath.Dir(snap.File)), dir) {
		full = snap.File
	}
	var inside string
	if index >= 0 {
		if index >= len(snap.TorrentFiles) || !snap.TorrentFiles[index].Selected {
			return fileTarget{}, ErrTaskFileNoSuchFile
		}
		// A torrent of one file is that file, and the paths of a larger one
		// are inside its own folder.
		rel := snap.TorrentFiles[index].Path
		name = path.Base(rel)
		if len(snap.TorrentFiles) > 1 {
			inside = full
			full = filepath.Join(full, filepath.FromSlash(rel))
		}
	}

	realDir, err := realpath.Resolve(dir)
	if err != nil {
		return fileTarget{}, ErrTaskFileNoBytes
	}
	roots, err := a.fileServeRoots(realDir)
	if err != nil {
		return fileTarget{}, ErrTaskFileEscape
	}
	for _, root := range roots {
		if withinDir(root, realDir) {
			return fileTarget{name: name, path: full, realDir: realDir, inside: inside}, nil
		}
	}
	return fileTarget{}, ErrTaskFileEscape
}

// StreamFile is a file the file route sends. ReadContext gives up when ctx
// ends before the bytes are there, which only a download that is still
// running can keep it waiting for.
type StreamFile interface {
	io.ReadSeekCloser
	ReadContext(ctx context.Context, p []byte) (int, error)
}

// OpenedFile is a task's file opened for the file route.
type OpenedFile struct {
	Name string
	File StreamFile
	// Index is the file of the torrent this is, or -1 for the task's own file.
	Index int
	// Live is set when the bytes come from a download that is still running.
	Live bool
}

// OpenTaskFile opens a file of a task for the file route. index picks a file
// of a torrent; a negative one picks the task's own file or, in a torrent of
// several files, the one Play opens (see core.PlayFile). While the engine is
// still downloading the file its bytes come from the engine, which fetches
// the part being read before the rest (see engine.Stream). Otherwise they
// come from disk, through the checks of SafeTaskFile.
func (a *App) OpenTaskFile(id string, index int) (OpenedFile, error) {
	snap, err := a.taskSnapshot(id)
	if err != nil {
		return OpenedFile{}, err
	}
	isTorrent := torrent.IsURI(snap.URL)
	if index < 0 && isTorrent && len(snap.TorrentFiles) == 1 {
		index = 0
	} else if index < 0 && isTorrent && len(snap.TorrentFiles) > 1 {
		if index = core.PlayFile(snap.TorrentFiles); index < 0 {
			return OpenedFile{}, ErrTaskFileNoMedia
		}
	}
	// A torrent's file is known by its index, and before the swarm has sent
	// the list there is none.
	if snap.Status == core.StatusRunning && (index >= 0 || !isTorrent) {
		if e := a.runningOn(snap.Resolver); e != nil {
			at, err := a.fileTarget(&snap, index)
			if err != nil {
				return OpenedFile{}, err
			}
			if r, err := e.Stream(id, max(index, 0)); err == nil {
				return OpenedFile{Name: at.name, File: r, Index: index, Live: true}, nil
			}
			// The engine gives an HTTP download's file its full size as it
			// starts, so the ranges a mend is still fetching read as zeros.
			if !isTorrent && snap.Size > 0 && snap.Loaded < snap.Size {
				return OpenedFile{}, ErrTaskFileMending
			}
		}
	}
	stopped := snap.Status != core.StatusRunning && snap.Status != core.StatusDone && snap.Status != core.StatusExtracting
	incomplete := ErrTaskFileIncomplete
	if snap.Loaded == 0 {
		incomplete = ErrTaskFileNoBytes
	}
	// The same full size makes a stopped one read as zeros wherever its bytes
	// have not arrived.
	if stopped && !isTorrent && snap.Size > 0 && snap.Loaded < snap.Size {
		return OpenedFile{}, incomplete
	}
	tf, err := a.SafeTaskFileAt(id, index)
	// A torrent keeps a file under another name until it is complete.
	if stopped && isTorrent && errors.Is(err, ErrTaskFileNoBytes) {
		return OpenedFile{}, incomplete
	}
	if err != nil {
		return OpenedFile{}, err
	}
	// Opened by the path SafeTaskFileAt resolved and confirmed, and never
	// joined again here.
	f, err := openShared(tf.Path)
	if err != nil {
		return OpenedFile{}, ErrTaskFileNoBytes
	}
	return OpenedFile{Name: tf.Name, File: diskFile{f}, Index: index}, nil
}

// diskFile is a file on disk, whose reads have nothing to wait for.
type diskFile struct{ *os.File }

func (f diskFile) ReadContext(_ context.Context, p []byte) (int, error) { return f.Read(p) }

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
		root := settings.FixedPrefix(a.defaultDir())
		real, err := realpath.Resolve(root)
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
		if real, err := realpath.Resolve(part); err == nil {
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
