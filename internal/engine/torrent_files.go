package engine

// The files of a running torrent: how far each of them has got, and which of
// them it fetches, changed while it runs.

import (
	"context"
	"errors"
	"slices"

	"github.com/junkerderprovinz/knightloader/internal/core"
)

// ErrTorrentFinished is a selection change refused because the torrent has
// finished. A finished torrent fetches nothing more and only seeds.
var ErrTorrentFinished = errors.New("the torrent has finished, so its files cannot be changed")

// pickedFiles is a torrent's files with sel marked, nil marking all.
func pickedFiles(c content, sel []int) []core.TorrentFile {
	files := slices.Clone(c.files)
	for i := range files {
		files[i].Selected = sel == nil || slices.Contains(sel, i)
	}
	return files
}

// TorrentFileProgress is how many bytes of each file of a torrent task are
// here, by the file's index in the torrent, with -1 for a file the torrent
// does not fetch. It reports false when the engine is not running the task or
// the client cannot be read.
func (e *Engine) TorrentFileProgress(taskID string) ([]int64, bool) {
	run, bt := e.runOf(taskID), e.client()
	if run == nil || bt == nil {
		return nil, false
	}
	ctx, cancel := context.WithTimeout(e.ctx, clientWait)
	defer cancel()
	files, err := bt.Files(ctx, run.hash)
	if err != nil {
		return nil, false
	}
	out := make([]int64, len(files))
	for i, f := range files {
		out[i] = -1
		if f.Picked {
			out[i] = f.Done
		}
	}
	return out, true
}

// SelectTorrentFiles changes which files of a torrent task are fetched, by
// index in the torrent's file list, nil for all of them. The client stops
// asking for the pieces only files left out need, starts on the files added
// and keeps every piece it already has, so nothing is fetched twice. A torrent
// still being handed over takes the selection as it starts. A task the engine
// is not running is left alone; its next start brings the selection with it.
func (e *Engine) SelectTorrentFiles(taskID string, sel []int) error {
	e.pickMu.Lock()
	defer e.pickMu.Unlock()
	run := e.runOf(taskID)
	if run == nil {
		if e.resolving(taskID) {
			e.mu.Lock()
			e.pendingPicks[taskID] = slices.Clone(sel)
			e.mu.Unlock()
		}
		return nil
	}
	return e.repick(run, sel)
}

// repick hands the client a new selection for a run. Caller holds pickMu.
func (e *Engine) repick(run *torrentRun, sel []int) error {
	e.mu.Lock()
	done, count := run.done, len(run.c.files)
	e.mu.Unlock()
	if done {
		return ErrTorrentFinished
	}
	bt := e.client()
	if bt == nil {
		return errNoClient
	}
	ctx, cancel := context.WithTimeout(e.ctx, clientWait)
	defer cancel()
	if err := bt.Pick(ctx, run.hash, picked(sel, count)); err != nil {
		return err
	}
	e.mu.Lock()
	run.sel = slices.Clone(sel)
	e.mu.Unlock()
	return nil
}

// resolving reports whether a torrent of taskID is between its start and its
// hand-over to the client.
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
