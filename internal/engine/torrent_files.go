package engine

// The files of a torrent the built-in client runs: how far each of them has
// got, and which of them it fetches, changed while it runs.
//
// The library counts a torrent's bytes per file but hands out only the total.
// The per-file counts reach its task store with every save, twice a second
// while bytes arrive, so layoutStore keeps the last of them for each torrent.
// They run over the selected files in selection order, which is why the
// engine remembers the selection it gave the library.

import (
	"encoding/json"
	"errors"
	"io/fs"
	"log"
	"os"
	"path"
	"path/filepath"
	"slices"

	"github.com/GopeedLab/gopeed/pkg/base"
	"github.com/junkerderprovinz/knightloader/internal/core"
)

// ErrTorrentFinished is a selection change refused because the library has
// finished the torrent. A finished torrent fetches nothing more and
// only seeds.
var ErrTorrentFinished = errors.New("the torrent has finished, so its files cannot be changed")

// torrentPick is what the engine gave the library for one torrent: how many
// files the torrent has and which of them to fetch, nil for all.
type torrentPick struct {
	count int
	sel   []int
}

// resolvedFiles is every file of a resolved torrent by its path inside the
// torrent, none of them selected.
func resolvedFiles(res *base.Resource) []core.TorrentFile {
	files := make([]core.TorrentFile, len(res.Files))
	for i, f := range res.Files {
		if f != nil {
			files[i] = core.TorrentFile{Path: path.Join(f.Path, f.Name), Size: f.Size}
		}
	}
	return files
}

// pickedFiles is a resolved torrent's files with sel marked, nil marking all.
func pickedFiles(res *base.Resource, sel []int) []core.TorrentFile {
	files := resolvedFiles(res)
	for i := range files {
		files[i].Selected = sel == nil || slices.Contains(sel, i)
	}
	return files
}

// allOr is sel as the library takes it, which reads an empty selection as the
// whole torrent only at the start and not in a change.
func allOr(sel []int, count int) []int {
	if sel != nil {
		return slices.Clone(sel)
	}
	out := make([]int, count)
	for i := range out {
		out[i] = i
	}
	return out
}

// spreadProgress puts the library's per-file byte counts, which run over the
// selected files in selection order, on the torrent's own file indices. A file
// left out gets -1. It reports false for counts that do not fit the selection,
// as the library's first save after a change can.
func spreadProgress(done []int64, p torrentPick) ([]int64, bool) {
	sel := allOr(p.sel, p.count)
	if len(done) != len(sel) {
		return nil, false
	}
	out := make([]int64, p.count)
	for i := range out {
		out[i] = -1
	}
	for i, at := range sel {
		if at < 0 || at >= p.count {
			return nil, false
		}
		out[at] = done[i]
	}
	return out, true
}

// TorrentFileProgress is how many bytes of each file of a torrent task are
// here, by the file's index in the torrent, with -1 for a file the torrent
// does not fetch. It reports false when the engine is not running the task or
// has no reading of it yet.
func (e *Engine) TorrentFileProgress(taskID string) ([]int64, bool) {
	e.mu.Lock()
	gid := e.toGopeed[taskID]
	p, ok := e.picks[taskID]
	e.mu.Unlock()
	if gid == "" || !ok {
		return nil, false
	}
	done, ok := e.layouts.fileProgress(gid)
	if !ok {
		return nil, false
	}
	return spreadProgress(done, p)
}

// SelectTorrentFiles changes which files of a torrent task are fetched, by
// index in the torrent's file list, nil for all of them. The library changes
// the selection of a running or paused torrent in place: it stops asking for
// the pieces only files left out need, starts on the files added and keeps
// every piece it already has, so nothing is fetched twice. A torrent still
// being resolved takes the selection as it starts. A task the engine is not
// running is left alone; its next start brings the selection with it.
func (e *Engine) SelectTorrentFiles(taskID string, sel []int) error {
	e.pickMu.Lock()
	defer e.pickMu.Unlock()
	e.mu.Lock()
	gid := e.toGopeed[taskID]
	p, running := e.picks[taskID]
	e.mu.Unlock()
	if gid == "" || !running {
		if e.resolving(taskID) {
			e.mu.Lock()
			e.pendingPicks[taskID] = slices.Clone(sel)
			e.mu.Unlock()
		}
		return nil
	}
	return e.repick(taskID, gid, p.count, sel)
}

// repick hands the library a new selection for a torrent it holds. Caller
// holds pickMu.
func (e *Engine) repick(taskID, gid string, count int, sel []int) error {
	t := e.d.GetTask(gid)
	if t == nil {
		return nil
	}
	if t.Status == base.DownloadStatusDone {
		return ErrTorrentFinished
	}
	if err := e.d.Patch(gid, nil, &base.Options{SelectFiles: allOr(sel, count)}); err != nil {
		return err
	}
	// The library saves zeros for the new selection as it takes it, and counts
	// the real bytes on its next tick.
	e.layouts.forgetFileProgress(gid)
	e.mu.Lock()
	e.picks[taskID] = torrentPick{count: count, sel: slices.Clone(sel)}
	e.mu.Unlock()
	return nil
}

// dropUnpicked deletes what a finished torrent wrote of the files it no
// longer fetches. The library deletes those by their finished names when it
// finishes, but a file left out part way is still a .part file, which it
// misses, and which can be as large as the whole file. While the torrent
// seeds, Windows refuses to delete a file the library holds open, so this
// runs again once the library lets go (see forgetTorrent and Remove), and
// only that last try reports a failure.
func (e *Engine) dropUnpicked(taskID string, last bool) {
	e.mu.Lock()
	p, ok := e.picks[taskID]
	e.mu.Unlock()
	e.rootMu.Lock()
	r, placed := e.roots[taskID]
	e.rootMu.Unlock()
	if ok && placed {
		removeUnpicked(r, p, last)
	}
}

func removeUnpicked(r torrentRoot, p torrentPick, report bool) {
	if p.sel == nil || len(r.files) != p.count {
		return
	}
	for i, rel := range r.files {
		if slices.Contains(p.sel, i) {
			continue
		}
		part := filepath.Join(r.dir, filepath.FromSlash(rel)) + ".part"
		if err := os.Remove(part); err != nil && !errors.Is(err, fs.ErrNotExist) && report {
			log.Printf("could not delete %s: %v", part, err)
		}
	}
}

// resolving reports whether a torrent of taskID is between its start and its
// hand-over to the library.
func (e *Engine) resolving(taskID string) bool {
	e.rootMu.Lock()
	defer e.rootMu.Unlock()
	_, placed := e.roots[taskID]
	return placed
}

// takePendingPick returns the selection made while taskID was resolving.
func (e *Engine) takePendingPick(taskID string) ([]int, bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	sel, ok := e.pendingPicks[taskID]
	delete(e.pendingPicks, taskID)
	return sel, ok
}

// watchFiles starts keeping the per-file reading of torrent gid.
func (s *layoutStore) watchFiles(gid string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.files[gid]; !ok {
		s.files[gid] = nil
	}
}

// forgetFileProgress drops the reading of torrent gid until its next save.
func (s *layoutStore) forgetFileProgress(gid string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.files[gid]; ok {
		s.files[gid] = nil
	}
}

func (s *layoutStore) unwatchFiles(gid string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.files, gid)
}

// fileProgress is the last per-file reading of torrent gid, in the library's
// selection order.
func (s *layoutStore) fileProgress(gid string) ([]int64, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	done, ok := s.files[gid]
	return slices.Clone(done), ok && done != nil
}

// savedFileProgress reads the per-file byte counts out of what the library
// saves for a torrent. Its type is internal to the library; Progress is an
// exported field it saves as it is.
func savedFileProgress(v any) ([]int64, bool) {
	raw, err := json.Marshal(v)
	if err != nil {
		return nil, false
	}
	var saved struct {
		Progress []int64
	}
	if json.Unmarshal(raw, &saved) != nil || saved.Progress == nil {
		return nil, false
	}
	return saved.Progress, true
}
