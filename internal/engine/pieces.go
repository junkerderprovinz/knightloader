package engine

// Which bytes of an HTTP transfer are here while it runs.
//
// The library saves a running task's connections whenever bytes have arrived,
// a few times a second: the range each was given and how much of it has come.
// What no connection still owes of its range is written, and a connection
// receiving writes at the end of what it has. Nothing but that save says where
// the ranges are, and the save belongs to the library's own goroutine, so the
// layout is copied as it passes and only for a task somebody asked about.

import (
	"encoding/json"
	"slices"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/core"
)

// layoutWatch is how long after the last question a task's layout is still
// copied from every save.
const layoutWatch = 30 * time.Second

// connReceiving is the library's state number for a connection whose request
// was answered and whose bytes are coming.
const connReceiving = 2

type watchedLayout struct {
	asked time.Time
	raw   []byte
}

// watch returns the layout the library last saved for task gid, nil until its
// next save, and keeps it coming for layoutWatch.
func (s *layoutStore) watch(gid string) []byte {
	now := time.Now()
	s.mu.Lock()
	defer s.mu.Unlock()
	for k, w := range s.watched {
		if now.Sub(w.asked) > layoutWatch {
			delete(s.watched, k)
		}
	}
	w := s.watched[gid]
	if w == nil {
		w = &watchedLayout{}
		s.watched[gid] = w
	}
	w.asked = now
	return w.raw
}

// Pieces is which bytes of HTTP task taskID are written and where its
// connections are writing. It reports false for a torrent, for a transfer of
// unknown size and for one the engine is not running, and for a moment after
// the first question about a task, until the library has saved it again.
func (e *Engine) Pieces(taskID string) (core.PieceMap, bool) {
	e.mu.Lock()
	gid, torrent := e.toGopeed[taskID], e.torrents[taskID]
	// A mend fetches its gaps one after the other, from the first.
	if m := e.mends[taskID]; m != nil {
		pm := byteMap(m.gaps, m.size)
		if m.cancel != nil && len(m.gaps) > 0 {
			pm.Heads = []int64{m.gaps[0].from}
		}
		e.mu.Unlock()
		return pm, true
	}
	e.mu.Unlock()
	if gid == "" || torrent {
		return core.PieceMap{}, false
	}
	t := e.d.GetTask(gid)
	if t == nil || t.Protocol != "http" || t.Meta == nil || t.Meta.Res == nil || t.Meta.Res.Size <= 0 {
		return core.PieceMap{}, false
	}
	raw := e.layouts.watch(gid)
	if raw == nil {
		return core.PieceMap{}, false
	}
	return layoutPieces(raw, t.Meta.Res.Size, t.Meta.Res.Range)
}

// layoutPieces reads a saved layout of a file of size bytes as a piece map.
func layoutPieces(raw []byte, size int64, ranged bool) (core.PieceMap, bool) {
	var l savedLayout
	if json.Unmarshal(raw, &l) != nil || len(l.Connections) == 0 {
		return core.PieceMap{}, false
	}
	pm := byteMap(l.gaps(size, ranged), size)
	for _, c := range l.Connections {
		if c.State != connReceiving {
			continue
		}
		at := c.Downloaded
		if ranged {
			if c.Chunk == nil || c.Chunk.Begin+c.Chunk.Downloaded > c.Chunk.End {
				continue
			}
			at = c.Chunk.Begin + c.Chunk.Downloaded
		}
		if at < size {
			pm.Heads = append(pm.Heads, at)
		}
	}
	slices.Sort(pm.Heads)
	return pm, true
}

// byteMap is a file of size bytes with everything written but gaps.
func byteMap(gaps []span, size int64) core.PieceMap {
	pm := core.PieceMap{Unit: core.UnitBytes, Total: size}
	at := int64(0)
	for _, g := range gaps {
		if g.from > at {
			pm.Runs = append(pm.Runs, core.PieceRun{State: core.PieceDone, Len: g.from - at})
		}
		pm.Runs = append(pm.Runs, core.PieceRun{State: core.PieceMissing, Len: g.len()})
		at = g.to + 1
	}
	if at < size {
		pm.Runs = append(pm.Runs, core.PieceRun{State: core.PieceDone, Len: size - at})
	}
	return pm
}
