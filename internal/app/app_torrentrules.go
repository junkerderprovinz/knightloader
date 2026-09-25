package app

// What the Torrents settings page asks of a torrent besides seeding: which of
// its files are fetched when nobody chose, which extra trackers it announces
// to, and which trackers keep it out of the list altogether.
//
// The file rules and the tracker policy are plain functions in
// internal/resolver/torrent, so any other code that hands a torrent on can
// apply the same ones; this file only reads the settings into them.

import (
	"errors"
	"fmt"
	"log"
	"path/filepath"
	"slices"
	"sync"

	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/engine"
	"github.com/junkerderprovinz/knightloader/internal/httpx"
	"github.com/junkerderprovinz/knightloader/internal/resolver/torrent"
	"github.com/junkerderprovinz/knightloader/internal/rules"
	"github.com/junkerderprovinz/knightloader/internal/settings"
	"github.com/junkerderprovinz/knightloader/internal/trackerlist"
)

// trackerListState holds the public tracker list. It is embedded in App so its
// fields stay in this file, and built on first use.
type trackerListState struct {
	trackerListOnce sync.Once
	trackerList     *trackerlist.List
}

func (a *App) publicTrackers() *trackerlist.List {
	a.trackerListOnce.Do(func() {
		a.trackerList = trackerlist.New(filepath.Join(a.DataDir, "trackerlist.json"), httpx.New(httpx.Options{}))
	})
	return a.trackerList
}

// fileRules is a file selection from the settings as the torrent package takes
// it.
func fileRules(r settings.TorrentFileRules) torrent.FileRules {
	return torrent.FileRules{MinSize: r.MinFileSize, Include: r.IncludeFiles, Exclude: r.ExcludeFiles}
}

// PickTorrentFiles chooses from files by the Torrents page's file rules, for
// the review of a .torrent and the size it shows until it starts, both before
// any category is known.
func (a *App) PickTorrentFiles(files []core.TorrentFile) ([]core.TorrentFile, error) {
	return fileRules(a.Settings.Get().Torrent.TorrentFileRules).Pick(files)
}

// torrentJobLocked adds the Torrents page to an engine job that starts a
// torrent: the file rules of the task's category when nobody chose the files,
// and the extra trackers. A task with a file list had its files ticked by
// hand, and the engine must not choose over that. Any other job is left alone.
// Caller holds a.mu.
func (a *App) torrentJobLocked(job *engine.Job, t *core.Task, cfg settings.Settings) {
	if !torrent.IsURI(job.URL) {
		return
	}
	if len(t.TorrentFiles) == 0 {
		job.FileRules = fileRules(cfg.TorrentFileRulesFor(t.Category))
	}
	tc := cfg.Torrent
	extra := slices.Clone(tc.ExtraTrackers)
	if tc.TrackerListURL != "" {
		a.refreshTrackerList(tc.TrackerListURL)
		extra = append(extra, a.publicTrackers().Trackers(tc.TrackerListURL)...)
	}
	job.Trackers = torrent.ExtraTrackers(job.URL, extra, tc.BannedTrackers)
}

// refreshTrackerList fetches the public tracker list in the background when it
// is due. It runs at boot, on every settings save and whenever a torrent
// starts, so a list a day old is renewed by whichever of those comes next and
// an address typed into the page is tried at once.
func (a *App) refreshTrackerList(address string) {
	list := a.publicTrackers()
	if !list.Due(address) {
		return
	}
	a.spawn(func() {
		switch err := list.Refresh(a.ctx, address); {
		case errors.Is(err, trackerlist.ErrNotSaved):
			log.Printf("%v; a restart fetches it again", err)
		case err != nil:
			log.Printf("tracker list not fetched (%v); the last good one stays in use", err)
		}
		// An address saved while this one was being fetched found the list
		// busy and was not fetched. The page autosaves as it is typed, so
		// that is the whole address after a half-typed one.
		if now := a.Settings.Get().Torrent.TrackerListURL; now != address {
			a.refreshTrackerList(now)
		}
	})
}

// TrackerListStatus reports on the public tracker list the Torrents page
// names.
func (a *App) TrackerListStatus() trackerlist.Status {
	return a.publicTrackers().Status(a.Settings.Get().Torrent.TrackerListURL)
}

// trackerBan refuses a torrent that announces a banned tracker, in the shape
// the link filter refuses a link, so both are held back alike.
//
// Refused rather than stripped of the tracker: a banned tracker is usually one
// whose rules forbid this client or one known to log who asks, and removing
// the entry leaves the rest of the torrent announcing the same info hash. A
// private torrent without its tracker would find no peer at all.
//
// A torrent restored from the holding area is past the lines that caught the
// tracker it was held for, and only those, so a line added since still stops
// it.
func trackerBan(t *core.Task, cfg settings.Torrent) rules.Verdict {
	banned := cfg.BannedTrackers
	if filterWaived(t) {
		banned = slices.DeleteFunc(slices.Clone(banned), func(line string) bool {
			return slices.ContainsFunc(t.Trackers, func(tr string) bool {
				host, hit := torrent.Banned([]string{tr}, []string{line})
				return hit && t.SkipReason == bannedBecause(host)
			})
		})
	}
	if host, hit := torrent.Banned(t.Trackers, banned); hit {
		return rules.Verdict{Rejected: true, Reason: bannedBecause(host)}
	}
	return rules.Verdict{}
}

// bannedBecause is the reason a torrent announcing host is held back with.
func bannedBecause(host string) string {
	return fmt.Sprintf("announces %s, which is on the banned trackers list", host)
}
