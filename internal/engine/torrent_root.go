package engine

// Where a torrent lands. The library writes a torrent into <folder>/<its
// name>, fixes that folder while it resolves the link, and would delete all of
// <folder>/<its name> with the task's files. So the folder is chosen here,
// before the resolve and from the name the link carries, as one no other
// download writes into, and a deletion takes only the torrent's own files.

import (
	"errors"
	"fmt"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"

	"github.com/GopeedLab/gopeed/pkg/base"
	"github.com/GopeedLab/gopeed/pkg/download"
	"github.com/junkerderprovinz/knightloader/internal/resolver/torrent"
)

// torrentRoot is where one torrent lands: dir is the folder it was resolved
// in, path its own folder or, for a single file, that file, and files every
// file it has, relative to dir with "/". nest is the folder made for it when
// its own name was taken, and empty otherwise.
type torrentRoot struct {
	dir, path, nest string
	files           []string
}

// placeTorrent sets the folder a torrent is resolved in and notes where it is
// expected to land. The same task starting again in the same folder takes up
// the place its earlier start had, so its files are found again.
func (e *Engine) placeTorrent(j Job, opts *base.Options) error {
	e.rootMu.Lock()
	defer e.rootMu.Unlock()
	dir := opts.Path
	if prev := j.TorrentRoot; prev != "" {
		if parent := filepath.Dir(prev); parent == dir || filepath.Dir(parent) == dir {
			r := torrentRoot{dir: parent, path: prev}
			if parent != dir {
				r.nest = parent
			}
			opts.Path = parent
			e.roots[j.TaskID] = r
			return nil
		}
	}
	name := expectedName(j)
	if name == "" {
		// Checked once the resolve has named it (see settleTorrent).
		e.roots[j.TaskID] = torrentRoot{dir: dir}
		return nil
	}
	target := filepath.Join(dir, name)
	if !e.takenLocked(j.TaskID, target) {
		e.roots[j.TaskID] = torrentRoot{dir: dir, path: target}
		return nil
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	for n := 1; ; n++ {
		nest := filepath.Join(dir, name+"."+strconv.Itoa(n))
		if e.takenLocked(j.TaskID, nest) {
			continue
		}
		switch err := os.Mkdir(nest, 0o755); {
		case err == nil:
			log.Printf("%s is taken, so the torrent goes into %s", target, nest)
			opts.Path = nest
			e.roots[j.TaskID] = torrentRoot{dir: nest, path: filepath.Join(nest, name), nest: nest}
			return nil
		case !errors.Is(err, fs.ErrExist):
			return err
		}
	}
}

// expectedName is the name a torrent's folder or file will have, as far as
// is known before the resolve: the task's own from an earlier start, else the
// one the link carries.
func expectedName(j Job) string {
	if n := j.TorrentName; n != "" && n == filepath.Base(n) && n != "." && n != ".." {
		return n
	}
	md, err := (torrent.Resolver{}).Describe(j.URL)
	if err != nil {
		return ""
	}
	return md.Name
}

// takenLocked reports whether p is on disk, as a finished file or one still
// being written, or where another torrent of this engine lands. Caller holds
// rootMu.
func (e *Engine) takenLocked(taskID, p string) bool {
	for _, q := range []string{p, p + ".part"} {
		if _, err := os.Lstat(q); err == nil {
			return true
		}
	}
	for id, r := range e.roots {
		if id != taskID && (r.path == p || r.nest == p) {
			return true
		}
	}
	return false
}

// settleTorrent records where a resolved torrent lands and which files are
// its own, and returns its folder or file. A torrent that turned out to have
// another name than its link said is refused when that name is taken, since
// the library has already fixed the folder and would write into another
// download's files.
func (e *Engine) settleTorrent(taskID string, res *base.Resource) (string, error) {
	files := landingPaths(res)
	if len(files) == 0 {
		return "", errors.New("the torrent names no files")
	}
	rel := files[0]
	if res.Name != "" {
		rel = res.Name
	}
	e.rootMu.Lock()
	defer e.rootMu.Unlock()
	r, ok := e.roots[taskID]
	if !ok {
		return "", errors.New("the torrent was removed while it was being resolved")
	}
	root := filepath.Join(r.dir, filepath.FromSlash(rel))
	if r.path != root && r.nest == "" && e.takenLocked(taskID, root) && !writesEmptyFiles(res) {
		delete(e.roots, taskID)
		return "", fmt.Errorf("not downloaded: %s already exists", root)
	}
	r.path, r.files = root, files
	e.roots[taskID] = r
	if res.Name != "" {
		// On disk at once, so a torrent of the same name started later finds
		// it taken even before the first piece arrives.
		if err := os.MkdirAll(root, 0o755); err != nil {
			return "", err
		}
	}
	return root, nil
}

// writesEmptyFiles reports whether the library creates some of the torrent's
// files while it resolves, which would make its folder look taken.
func writesEmptyFiles(res *base.Resource) bool {
	return slices.ContainsFunc(res.Files, func(f *base.FileInfo) bool { return f != nil && f.Size == 0 })
}

// unplace forgets where a torrent that did not start would have landed, and
// removes the folders made for it while they are empty.
func (e *Engine) unplace(taskID string) {
	r, ok := e.takeRoot(taskID)
	if !ok {
		return
	}
	for _, d := range []string{r.path, r.nest} {
		if fi, err := os.Lstat(d); err == nil && fi.IsDir() {
			_ = os.Remove(d)
		}
	}
}

// takeRoot removes a torrent from the ones this engine knows and returns
// where it landed.
func (e *Engine) takeRoot(taskID string) (torrentRoot, bool) {
	e.rootMu.Lock()
	defer e.rootMu.Unlock()
	r, ok := e.roots[taskID]
	delete(e.roots, taskID)
	return r, ok
}

// dropTorrent takes a torrent out of the library and leaves its files alone.
//
// gopeed v1.9.3 closes a torrent's fetcher itself once seeding reaches its
// target, and sets the torrent client every fetcher shares to nil when none is
// left; Delete closes the fetcher again, and that second Close dereferences the
// nil client. The library has already dropped the task from its lists by then,
// so a nil dereference from that call is the torrent being gone already, and
// any other panic is passed on.
func (e *Engine) dropTorrent(gid string) {
	defer func() {
		if p := recover(); p != nil {
			if re, ok := p.(runtime.Error); !ok || !strings.Contains(re.Error(), "nil pointer") {
				panic(p)
			}
		}
	}()
	_ = e.d.Delete(&download.TaskFilter{IDs: []string{gid}}, false)
}

// remove deletes the torrent's own files, finished or still being written,
// and then the folders they leave empty, its own and the one made for it.
// Anything else that ended up in them stays.
func (r torrentRoot) remove() {
	folders := map[string]bool{}
	for _, rel := range r.files {
		p := filepath.Join(r.dir, filepath.FromSlash(rel))
		for _, f := range []string{p, p + ".part"} {
			if err := os.Remove(f); err != nil && !errors.Is(err, fs.ErrNotExist) {
				log.Printf("could not delete %s: %v", f, err)
			}
		}
		for d := filepath.Dir(p); len(d) > len(r.dir); d = filepath.Dir(d) {
			folders[d] = true
		}
	}
	deepest := make([]string, 0, len(folders))
	for d := range folders {
		deepest = append(deepest, d)
	}
	slices.SortFunc(deepest, func(a, b string) int { return len(b) - len(a) })
	for _, d := range deepest {
		_ = os.Remove(d)
	}
	if r.nest != "" {
		_ = os.Remove(r.nest)
	}
}
