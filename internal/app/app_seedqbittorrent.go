package app

// A torrent a debrid service fetched, handed to an external qBittorrent to
// seed rather than to the built-in client, so cross-seeding tools that watch
// qBittorrent find it. KnightLoader checks the files as it does before seeding
// them itself, and qBittorrent is told to skip its own check.

import (
	"context"
	"log"
	"path/filepath"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/engine"
	"github.com/junkerderprovinz/knightloader/internal/pathvars"
	"github.com/junkerderprovinz/knightloader/internal/qbittorrent"
	"github.com/junkerderprovinz/knightloader/internal/resolver/debrid"
	"github.com/junkerderprovinz/knightloader/internal/resolver/torrent"
	"github.com/junkerderprovinz/knightloader/internal/settings"
)

// qbitTimeout bounds one hand-over: a login and an add.
const qbitTimeout = 2 * time.Minute

// handover is a finished torrent as seedInQBittorrent needs it. file is its
// one file, or its folder when folder says it has several.
type handover struct {
	link   string
	file   string
	folder bool
	files  []core.TorrentFile
}

// seedInQBittorrent checks the torrent's files and adds it to qBittorrent,
// and lets the service's copy go once qBittorrent has it. A torrent that
// fails either step is not seeded anywhere, and its row says why.
func (a *App) seedInQBittorrent(id string, tb *debrid.TorrentBackend, h handover, q settings.QBittorrent) {
	ctx, cancel := context.WithTimeout(a.ctx, qbitTimeout)
	defer cancel()
	note := "Seeding in qBittorrent"
	if err := a.handToQBittorrent(ctx, h, q); err != nil {
		log.Printf("task %s is not seeded in qBittorrent: %v", id, err)
		note = "Not seeded: " + qbittorrent.Reason(err)
	} else {
		tb.Release(id)
	}
	a.mu.Lock()
	t := a.tasks[id]
	if t == nil {
		a.mu.Unlock()
		return
	}
	t.Note = note
	c := a.copyLocked(t)
	a.mu.Unlock()
	a.publish(&c)
}

func (a *App) handToQBittorrent(ctx context.Context, h handover, q settings.QBittorrent) error {
	dir := filepath.Dir(h.file)
	// A magnet link carries no pieces, so its files can only be held to the
	// sizes the task knows.
	if torrent.IsMagnet(h.link) {
		if err := checkSeedFiles(&core.Task{File: h.file, TorrentFiles: h.files}); err != nil {
			return err
		}
	} else if err := engine.CheckPieces(dir, h.link); err != nil {
		return err
	}
	save, err := qbittorrent.SavePath(dir, pathvars.Root(a.defaultDir()), q.DownloadsPath)
	if err != nil {
		return err
	}
	add := qbittorrent.Torrent{SavePath: save, Category: q.Category, Folder: h.folder}
	if torrent.IsMagnet(h.link) {
		add.Magnet = h.link
	} else if add.File, err = torrent.DecodeBytes(h.link); err != nil {
		return err
	}
	c, err := qbittorrent.New(q.URL, q.Username, q.Password)
	if err != nil {
		return err
	}
	if err := c.Login(ctx); err != nil {
		return err
	}
	return c.Add(ctx, add)
}
