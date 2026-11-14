package app

// The torrents of the built-in client KnightLoader had before it ran
// qBittorrent. Nothing about such a task changes but where its torrent runs,
// so each one is handed to qBittorrent the way it would be after a restart:
// a download carries on from the bytes it has, and a finished torrent seeds on.
// Two things differ, once. The built-in client kept a file it had not finished
// under the name <file>.part, which qBittorrent would not find, so those take
// their real names. And a finished torrent is checked here piece by piece
// before qBittorrent takes it as complete; one that does not pass is checked
// by qBittorrent instead.

import (
	"errors"
	"io/fs"
	"log"
	"os"
	"path/filepath"

	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/filemode"
	"github.com/junkerderprovinz/knightloader/internal/resolver/torrent"
)

// handedOverFile marks a data folder whose torrents have been handed over.
const handedOverFile = "torrents-handed-to-qbittorrent"

// handOverTorrents gets the torrents of tasks ready for qBittorrent, once per
// data folder, and returns the finished ones, whose first seed run checks
// their pieces.
func handOverTorrents(dataDir string, tasks []*core.Task) map[string]bool {
	mark := filepath.Join(dataDir, handedOverFile)
	if _, err := os.Stat(mark); err == nil {
		return nil
	}
	check := map[string]bool{}
	for _, t := range tasks {
		if t.Resolver != (torrent.Resolver{}).Info().ID || t.File == "" {
			continue
		}
		if t.Status == core.StatusDone {
			check[t.ID] = true
		}
		for _, p := range torrentFilePaths(t) {
			namePart(p)
		}
	}
	if err := os.WriteFile(mark, nil, filemode.File); err != nil {
		log.Printf("could not note that the torrents went to qBittorrent: %v", err)
	}
	return check
}

// torrentFilePaths is where each file of a task's torrent lands. t.File is
// the torrent's folder, or its one file.
func torrentFilePaths(t *core.Task) []string {
	files := t.TorrentFiles
	if len(files) == 0 {
		md, err := (torrent.Resolver{}).Describe(t.URL)
		if err != nil {
			return nil
		}
		files = md.Files
	}
	if fi, err := os.Stat(t.File); err != nil || !fi.IsDir() {
		return []string{t.File}
	}
	out := make([]string, 0, len(files))
	for _, f := range files {
		out = append(out, filepath.Join(t.File, filepath.FromSlash(f.Path)))
	}
	return out
}

// namePart gives the unfinished <p>.part its real name p, unless p is there
// already.
func namePart(p string) {
	if _, err := os.Lstat(p); !errors.Is(err, fs.ErrNotExist) {
		return
	}
	if err := os.Rename(p+".part", p); err != nil && !errors.Is(err, fs.ErrNotExist) {
		log.Printf("could not give %s.part its own name for qBittorrent: %v", p, err)
	}
}
