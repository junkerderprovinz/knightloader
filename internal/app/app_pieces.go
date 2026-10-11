package app

import (
	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/usenet/local"
)

// Pieces is which parts of the file of task id are here, in the units its
// transfer counts in: the bytes of an HTTP download, the articles of a file
// from the own Usenet servers, or the blocks of a file a par2 check found
// damaged, until the repair is over. It reports false where nothing can say,
// as for a torrent, whose pieces the client keeps to itself, and for a task
// that is not being fetched.
func (a *App) Pieces(id string) (core.PieceMap, bool) {
	a.mu.Lock()
	t := a.tasks[id]
	if t == nil {
		a.mu.Unlock()
		return core.PieceMap{}, false
	}
	resolver := t.Resolver
	if t.Repair != nil {
		if c := a.usenetStateFor().checks[local.LinkJob(t.URL)]; c != nil {
			if pm, ok := c.blocks[id]; ok {
				a.mu.Unlock()
				return pm, true
			}
		}
	}
	a.mu.Unlock()

	if b, ok := a.backendFor(resolver).(*local.Backend); ok {
		return b.Pieces(id)
	}
	if a.Engine == nil {
		return core.PieceMap{}, false
	}
	return a.Engine.Pieces(id)
}
