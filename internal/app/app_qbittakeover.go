package app

import (
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"

	"github.com/junkerderprovinz/knightloader/internal/fsretry"
	"github.com/junkerderprovinz/knightloader/internal/qbittorrent"
	"github.com/junkerderprovinz/knightloader/internal/resolver/torrent"
	"github.com/junkerderprovinz/knightloader/internal/torrents"
)

// leftTorrentsFile keeps the twins of tasks removed while qBittorrent could
// not be reached, for its next start to take out.
const leftTorrentsFile = "torrents-left.json"

// leftTorrent is a torrent to take out of qBittorrent at its next start. Its
// files go along when Files is set, unless they are those in Keep, the
// removed task's own folder, which the removal saw to.
type leftTorrent struct {
	Files bool   `json:"files,omitempty"`
	Keep  string `json:"keep,omitempty"`
}

// leftStore is leftTorrentsFile.
type leftStore struct {
	mu   sync.Mutex
	path string
}

func (s *leftStore) load() (map[string]leftTorrent, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.loadLocked()
}

func (s *leftStore) loadLocked() (map[string]leftTorrent, error) {
	out := map[string]leftTorrent{}
	b, err := os.ReadFile(s.path)
	if errors.Is(err, fs.ErrNotExist) {
		return out, nil
	}
	if err != nil {
		return out, err
	}
	return out, json.Unmarshal(b, &out)
}

func (s *leftStore) add(more map[string]leftTorrent) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	m, err := s.loadLocked()
	if err != nil {
		log.Printf("%s could not be read and starts anew: %v", s.path, err)
	}
	for h, l := range more {
		m[h] = l
	}
	b, err := json.Marshal(m)
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return fsretry.Rename(tmp, s.path)
}

func (s *leftStore) clear() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := os.Remove(s.path); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return nil
}

// takeOverTorrents runs with every start of qBittorrent, before anything else
// reaches it. qBittorrent starts the torrents it kept on its own, so each of
// KnightLoader's stops, and only those the engine runs and the twins of tasks
// on the list start again; the others start once their task does. A torrent
// whose task left the list while qBittorrent could not hear of it is taken
// out, with its files only where the removal said so and they are not the
// task's own.
func (a *App) takeOverTorrents(ctx context.Context, c *qbittorrent.Client) error {
	list, err := c.Torrents(ctx, qbittorrent.Filter{Tag: torrents.Tag})
	if err != nil {
		return err
	}
	tasks, twins := a.torrentOwners()
	running := a.runningTorrents()
	left, err := a.leftTorrents.load()
	if err != nil {
		log.Printf("%s could not be read, so a torrent left in qBittorrent goes without its files: %v", leftTorrentsFile, err)
	}
	var stop, start []string
	for _, t := range list {
		h := strings.ToLower(t.Hash)
		twin := slices.Contains(strings.Split(strings.ReplaceAll(t.Tags, " ", ""), ","), CrossSeedTag)
		switch {
		case slices.Contains(running, h), twin && twins[h]:
			start = append(start, h)
		case !twin && tasks[h]:
			stop = append(stop, h)
		default:
			l := left[h]
			files := l.Files && !mayBeOneFile(filepath.Clean(filepath.FromSlash(t.SavePath)), l.Keep)
			if err := c.Delete(ctx, files, h); err != nil {
				return err
			}
			log.Printf("qBittorrent held %s, whose task is gone, and lets it go (its files too: %v)", t.Name, files)
		}
	}
	if len(stop) > 0 {
		if err := c.Stop(ctx, stop...); err != nil {
			return err
		}
	}
	if len(start) > 0 {
		if err := c.Start(ctx, start...); err != nil {
			return err
		}
	}
	return a.leftTorrents.clear()
}

// torrentOwners are the hashes of the torrents of the tasks on the list, as
// qBittorrent names them, and those of their twins.
func (a *App) torrentOwners() (tasks, twins map[string]bool) {
	a.mu.Lock()
	var hashes []string
	for _, t := range a.tasks {
		h := t.InfoHash
		if h == "" && isBuiltInTorrent(t) {
			if md, err := (torrent.Resolver{}).Describe(t.URL); err == nil {
				h = md.InfoHash
			}
		}
		if h != "" {
			hashes = append(hashes, torrents.HashOf(h))
		}
	}
	a.mu.Unlock()
	tasks, twins = map[string]bool{}, map[string]bool{}
	for _, h := range hashes {
		tasks[h] = true
		if a.xsAsked == nil {
			continue
		}
		for _, twin := range a.xsAsked.Injected(h) {
			twins[strings.ToLower(twin)] = true
		}
	}
	return tasks, twins
}
