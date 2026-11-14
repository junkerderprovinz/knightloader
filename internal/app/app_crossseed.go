package app

// Cross-seeding: a finished torrent is looked for on the other trackers the
// user has an account on, and a twin with the same files is put into the
// qBittorrent this app runs to seed beside it. The twins stay in qBittorrent
// and never become tasks (see docs/decisions.md): they hold no byte the task
// did not fetch, KnightLoader's seeding targets do not stop them, and they
// leave qBittorrent with the task they are the twins of.

import (
	"context"
	"errors"
	"fmt"
	"log"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/crossseed"
	"github.com/junkerderprovinz/knightloader/internal/resolver/torrent"
	"github.com/junkerderprovinz/knightloader/internal/settings"
	"github.com/junkerderprovinz/knightloader/internal/torrents"
)

const (
	crossSeedLogFile   = "cross-seed.jsonl"
	crossSeedAskedFile = "cross-seed-asked.json"
	// CrossSeedTag marks the twins in qBittorrent, besides torrents.Tag.
	CrossSeedTag = "cross-seed"
)

// crossSeedState is embedded on App so its fields stay in this file.
type crossSeedState struct {
	xsMu sync.Mutex
	// xsSites is one client for each Gazelle site, by name, so every call to
	// a site, from the search and from a test of a key, counts against the
	// one limit the site sets.
	xsSites  map[string]*crossseed.Gazelle
	xsConf   crossseed.Config
	xsWas    settings.CrossSeed
	xsAsked  *crossseed.History
	xsLog    *crossseed.Log
	xsRunner *crossseed.Runner
}

// startCrossSeed opens the answers and the log and starts the search, once
// the task list is loaded.
func (a *App) startCrossSeed(c settings.CrossSeed) {
	a.xsSites = map[string]*crossseed.Gazelle{}
	for _, site := range []crossseed.Site{crossseed.RED, crossseed.OPS} {
		a.xsSites[site.Name] = crossseed.NewGazelle(site, "")
	}
	a.xsAsked = crossseed.OpenHistory(filepath.Join(a.DataDir, crossSeedAskedFile))
	a.xsLog = crossseed.OpenLog(filepath.Join(a.DataDir, crossSeedLogFile))
	a.xsRunner = crossseed.Start(crossseed.Options{
		Config:  a.crossSeedConfig,
		History: a.xsAsked,
		Log:     a.xsLog,
		Inject:  a.injectCrossSeed,
	})
	a.applyCrossSeed(c, true)
}

func (a *App) stopCrossSeed() {
	if a.xsRunner != nil {
		a.xsRunner.Close()
	}
}

func (a *App) crossSeedConfig() crossseed.Config {
	a.xsMu.Lock()
	defer a.xsMu.Unlock()
	return a.xsConf
}

// gazelleSite is the client for the site named name.
func (a *App) gazelleSite(name string) *crossseed.Gazelle {
	a.xsMu.Lock()
	defer a.xsMu.Unlock()
	return a.xsSites[name]
}

func accountOn(c settings.CrossSeed, site string) settings.GazelleSite {
	if site == crossseed.RED.Name {
		return c.RED
	}
	return c.OPS
}

// applyCrossSeed takes the saved settings up. What a tracker failed with is
// asked again once its settings change, and the finished torrents are looked
// over at boot, when cross-seeding comes on and when a tracker changes.
func (a *App) applyCrossSeed(c settings.CrossSeed, boot bool) {
	if a.xsRunner == nil {
		return
	}
	var conf crossseed.Config
	a.xsMu.Lock()
	was := a.xsWas
	a.xsWas = c
	if c.Enabled {
		if c.Daemon.URL != "" {
			conf.Daemon = crossseed.NewDaemon(c.Daemon.URL, c.Daemon.APIKey)
		}
		for _, name := range []string{crossseed.RED.Name, crossseed.OPS.Name} {
			if acc := accountOn(c, name); acc.Ready() {
				conf.Gazelle = append(conf.Gazelle, crossseed.Account{Client: a.xsSites[name].WithKey(acc.APIKey), Announce: acc.Announce})
			}
		}
		for _, x := range c.Indexers {
			conf.Torznab = append(conf.Torznab, crossseed.NewTorznab(x.Name, x.URL, x.APIKey))
		}
	}
	a.xsConf = conf
	a.xsMu.Unlock()

	if was.RED != c.RED {
		a.xsAsked.ForgetErrors(crossseed.RED.Name)
	}
	if was.OPS != c.OPS {
		a.xsAsked.ForgetErrors(crossseed.OPS.Name)
	}
	for _, x := range c.Indexers {
		if !slices.Contains(was.Indexers, x) {
			a.xsAsked.ForgetErrors(crossseed.IndexerKey(x.Name))
		}
	}
	if was.Daemon != c.Daemon {
		a.xsAsked.ForgetErrors(crossseed.DaemonKey)
	}
	changed := was.RED != c.RED || was.OPS != c.OPS || !slices.Equal(was.Indexers, c.Indexers) || was.Daemon != c.Daemon
	if c.Enabled && (boot || !was.Enabled || changed) {
		a.offerFinishedTorrents()
	}
}

// offerFinishedTorrents offers every finished torrent of the list.
func (a *App) offerFinishedTorrents() {
	a.mu.Lock()
	var done []core.Task
	for _, t := range a.tasks {
		if seedsHere(t) {
			done = append(done, *t)
		}
	}
	a.mu.Unlock()
	var srcs []crossseed.Source
	for i := range done {
		if src, ok := crossSeedSource(&done[i]); ok {
			srcs = append(srcs, src)
		}
	}
	a.xsRunner.Offer(srcs...)
}

// offerCrossSeed offers a torrent that has just finished.
func (a *App) offerCrossSeed(t *core.Task) {
	if a.xsRunner == nil || !a.Settings.Get().Torrent.CrossSeed.Enabled {
		return
	}
	if src, ok := crossSeedSource(t); ok {
		a.xsRunner.Offer(src)
	}
}

// crossSeedSource is a finished torrent as the search takes it. A torrent of
// which some files were left out has no twin to seed, and neither has one
// whose files are not known.
func crossSeedSource(t *core.Task) (crossseed.Source, bool) {
	if !seedsHere(t) || t.File == "" || t.InfoHash == "" {
		return crossseed.Source{}, false
	}
	for _, f := range t.TorrentFiles {
		if !f.Selected {
			return crossseed.Source{}, false
		}
	}
	src := crossseed.Source{Hash: strings.ToLower(t.InfoHash), Name: filepath.Base(t.File), Dir: filepath.Dir(t.File)}
	if !torrent.IsMagnet(t.URL) {
		b, err := torrent.DecodeBytes(t.URL)
		if err != nil {
			return crossseed.Source{}, false
		}
		src.Torrent = b
		if name, _, err := crossseed.FilesOf(b); err == nil {
			src.Name = name
		}
		return src, true
	}
	if len(t.TorrentFiles) == 0 {
		return crossseed.Source{}, false
	}
	for _, f := range t.TorrentFiles {
		p := f.Path
		if len(t.TorrentFiles) > 1 || p != src.Name {
			p = src.Name + "/" + p
		}
		src.Files = append(src.Files, crossseed.FileEntry{Path: p, Size: f.Size})
	}
	return src, true
}

// injectCrossSeed makes the twin's files reachable the way the settings say
// and hands it to qBittorrent to seed them unchecked.
func (a *App) injectCrossSeed(ctx context.Context, src crossseed.Source, f crossseed.Found) error {
	c := a.Settings.Get().Torrent.CrossSeed
	dir := src.Dir
	if mode := crossseed.LinkMode(c.LinkMode); mode != "" && mode != crossseed.LinkNone {
		dir = filepath.Join(c.LinkDir, folderName(f.Tracker))
		if err := crossseed.Link(mode, src.Dir, dir, f.Files); err != nil {
			return err
		}
	}
	if err := crossseed.CheckFiles(dir, f.Files); err != nil {
		return err
	}
	bt := a.Engine.Torrents()
	readings, err := bt.Readings(ctx)
	if errors.Is(err, torrents.ErrOff) {
		return crossseed.ErrNotNow
	}
	if err != nil {
		return err
	}
	if _, ok := readings[f.Hash]; ok {
		return errors.New("qBittorrent has this torrent already")
	}
	add := torrents.Add{Hash: f.Hash, File: f.Torrent, Dir: dir, Complete: true, Tags: []string{CrossSeedTag}, Category: c.Category}
	if err := bt.Add(ctx, add); err != nil {
		return err
	}
	return bt.Start(ctx, f.Hash)
}

// folderName is a tracker's name as a folder of the link folder.
func folderName(tracker string) string {
	name := strings.Map(func(r rune) rune {
		if strings.ContainsRune(`/\:*?"<>|`, r) || r < ' ' {
			return '_'
		}
		return r
	}, strings.TrimSpace(tracker))
	if name == "" || name == "." || name == ".." {
		return "tracker"
	}
	return name
}

// dropCrossSeeds takes the twins of a removed task out of qBittorrent. A
// twin seeding from linked files takes them along when the task's files go;
// one seeding from the task's own files leaves them to the removal.
func (a *App) dropCrossSeeds(t *core.Task, files bool) {
	if a.xsAsked == nil || t.InfoHash == "" {
		return
	}
	twins := a.xsAsked.Injected(t.InfoHash)
	if len(twins) == 0 {
		return
	}
	own := filepath.Dir(t.File)
	a.spawn(func() {
		ctx, cancel := context.WithTimeout(a.ctx, time.Minute)
		defer cancel()
		bt := a.Engine.Torrents()
		readings, err := bt.Readings(ctx)
		if err != nil {
			log.Printf("the cross-seeds of %s could not be taken out of qBittorrent: %v", t.Name, err)
			return
		}
		for _, h := range twins {
			r, ok := readings[h]
			if !ok {
				continue
			}
			linked := files && t.File != "" && !mayBeOneFile(filepath.Clean(filepath.FromSlash(r.Dir)), own)
			if err := bt.Delete(ctx, h, linked); err != nil && !errors.Is(err, torrents.ErrUnknown) {
				log.Printf("the cross-seed %s of %s could not be taken out of qBittorrent: %v", h, t.Name, err)
			}
		}
	})
}

// CrossSeedLog is the newest n lines of the cross-seed log, newest first.
func (a *App) CrossSeedLog(n int) ([]crossseed.Entry, error) {
	if a.xsLog == nil {
		return nil, nil
	}
	return a.xsLog.Last(n)
}

// TestCrossSeed asks one part of c with a single call: "red" or "ops" whose
// key it is, "indexer" the Torznab feed at index what it can do, and
// "daemon" whether the daemon answers. The stored secrets are put back as a
// save would, so the test covers what a save would write. It returns what
// answered, such as the account's name.
func (a *App) TestCrossSeed(ctx context.Context, c settings.CrossSeed, what string, index int) (string, error) {
	c = c.WithSecretsFrom(a.Settings.Get().Torrent.CrossSeed)
	switch what {
	case "red", "ops":
		site := strings.ToUpper(what)
		acc := accountOn(c, site)
		if acc.APIKey == "" {
			return "", fmt.Errorf("there is no %s API key to test", site)
		}
		return a.gazelleSite(site).WithKey(acc.APIKey).Index(ctx)
	case "indexer":
		if index < 0 || index >= len(c.Indexers) {
			return "", errors.New("there is no such indexer")
		}
		x := c.Indexers[index]
		return "", crossseed.NewTorznab(x.Name, x.URL, x.APIKey).Caps(ctx)
	case "daemon":
		return "", crossseed.NewDaemon(c.Daemon.URL, c.Daemon.APIKey).Ping(ctx)
	}
	return "", fmt.Errorf("%q is not a part of cross-seeding to test", what)
}
