package engine

import (
	"fmt"
	"path"
	"time"

	"github.com/GopeedLab/gopeed/pkg/base"
	"github.com/GopeedLab/gopeed/pkg/download"
	gbt "github.com/GopeedLab/gopeed/pkg/protocol/bt"
	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/resolver/torrent"
)

// defaultMetadataTimeout is how long a magnet may wait for the swarm to send
// its metadata before the task fails. Resolve takes no context, so this bounds
// the wait rather than cancelling the resolve.
const defaultMetadataTimeout = 2 * time.Minute

// torrentStatsInterval is how often a torrent task's swarm numbers are read.
// Gopeed sends no events after done, while seeding goes on for hours, so
// peers, ratio and the end of seeding have to be polled.
const torrentStatsInterval = 3 * time.Second

// DownloadTorrent starts a magnet link or an uploaded .torrent. It feeds the
// same downloader as Download, whose HTTP-shaped arguments mean nothing to
// a swarm; gopeed's default fetch managers already include the bt fetcher.
//
// sel names the files to fetch by index in the resolved file list; nil
// fetches all of them.
func (e *Engine) DownloadTorrent(taskID, uri, dir string, sel []int) {
	e.Start(Job{TaskID: taskID, URL: uri, Dir: dir, TorrentSelect: sel})
}

// startTorrent is Start's torrent branch: resolve (for a magnet, wait on the
// swarm), check where every file would land, then create the task. Start has
// already called wg.Add for it.
func (e *Engine) startTorrent(j Job) {
	go func() {
		defer e.wg.Done()
		rr, err := e.resolveTorrent(j)
		if err != nil {
			e.emit(j.TaskID, core.Update{Status: core.StatusError, Err: err.Error()})
			return
		}
		// A magnet's file list comes from a stranger over the network and is
		// seen here for the first time; an uploaded .torrent was already
		// checked by the resolver. This runs before Create starts writing.
		if err := torrent.Contained(j.writeDir(), landingPaths(rr.Res)); err != nil {
			e.emit(j.TaskID, core.Update{Status: core.StatusError, Err: err.Error()})
			return
		}
		name, size := torrentMeta(rr.Res, j.TorrentSelect)
		e.emit(j.TaskID, core.Update{Status: core.StatusRunning, Name: name, Size: size})
		gid, err := e.d.Create(rr.ID)
		if err != nil {
			e.emit(j.TaskID, core.Update{Status: core.StatusError, Err: err.Error()})
			return
		}
		e.mu.Lock()
		e.toKL[gid] = j.TaskID
		e.toGopeed[j.TaskID] = gid
		e.torrents[j.TaskID] = true
		e.mu.Unlock()
		e.startTorrentPoll()
	}()
}

// resolveTorrent waits on a resolve that cannot be cancelled. The resolve
// runs on its own goroutine and this one gives up on the deadline or on
// Close. Downloader.Close releases the abandoned goroutine, and its channel
// is buffered so it never blocks on a result nobody reads.
func (e *Engine) resolveTorrent(j Job) (*download.ResolveResult, error) {
	req := &base.Request{URL: j.URL, Proxy: requestProxy(j.Route)}
	if len(j.Trackers) > 0 {
		// The bt fetcher type-asserts its own extra type; an http one would
		// convert into an empty value.
		req.Extra = &gbt.ReqExtra{Trackers: j.Trackers}
	}
	opts := &base.Options{Path: j.writeDir(), SelectFiles: j.TorrentSelect}

	type answer struct {
		rr  *download.ResolveResult
		err error
	}
	ch := make(chan answer, 1)
	go func() {
		// The torrent stack panics on input it considers impossible
		// (anacrolix/torrent asserts a non-zero info hash), and a panic here
		// would take the process down. The resolver rejects the known cases
		// (torrent.checkMagnet); this turns any other into one failed
		// download. Gopeed's bt fetcher recovers around Stats the same way.
		defer func() {
			if r := recover(); r != nil {
				ch <- answer{nil, fmt.Errorf("the torrent library refused this link: %v", r)}
			}
		}()
		rr, err := e.d.Resolve(req, opts)
		ch <- answer{rr, err}
	}()

	e.mu.Lock()
	timeout := e.metadataTimeout
	e.mu.Unlock()
	if timeout <= 0 {
		timeout = defaultMetadataTimeout
	}
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case a := <-ch:
		if a.err != nil {
			return nil, a.err
		}
		if a.rr == nil || a.rr.Res == nil {
			return nil, fmt.Errorf("the torrent resolved to nothing")
		}
		return a.rr, nil
	case <-timer.C:
		return nil, fmt.Errorf("no peer sent this torrent's file list within %s", timeout)
	case <-e.done:
		return nil, fmt.Errorf("shutting down")
	}
}

// landingPaths is where each file of a resolved torrent would be written,
// relative to the download folder, assembled the way the library does it: a
// resource with a Name is a folder containing every file, one without is a
// single file.
func landingPaths(res *base.Resource) []string {
	if res == nil {
		return nil
	}
	out := make([]string, 0, len(res.Files))
	for _, f := range res.Files {
		if f == nil {
			continue
		}
		out = append(out, path.Join(res.Name, f.Path, f.Name))
	}
	if len(out) == 0 && res.Name != "" {
		out = append(out, res.Name)
	}
	return out
}

// torrentMeta is the name and size to show for a resolved torrent. The size
// is summed over the selection because a bt resolve reports the whole torrent
// and gopeed only narrows it at Create, after this update has gone out.
func torrentMeta(res *base.Resource, sel []int) (name string, size int64) {
	if res == nil {
		return "", 0
	}
	name = res.Name
	if name == "" && len(res.Files) > 0 {
		name = res.Files[0].Name
	}
	if len(sel) == 0 {
		return name, res.Size
	}
	for _, i := range sel {
		if i >= 0 && i < len(res.Files) && res.Files[i] != nil {
			size += res.Files[i].Size
		}
	}
	return name, size
}

// readTorrentStats reads a live task's swarm numbers: for a bt task,
// Downloader.Stats returns a *bt.Stats.
//
// Seeding is derived from Uploading and a done status together. Gopeed sets
// Task.Uploading at creation for every torrent, so on its own it is true from
// the first second of the download.
func (e *Engine) readTorrentStats(gid string) (core.TorrentStats, *download.Task, bool) {
	t := e.d.GetTask(gid)
	if t == nil {
		return core.TorrentStats{}, nil, false
	}
	sr, err := e.d.Stats(gid)
	if err != nil {
		return core.TorrentStats{}, t, false
	}
	s, ok := sr.(*gbt.Stats)
	if !ok || s == nil {
		return core.TorrentStats{}, t, false
	}
	return core.TorrentStats{
		Peers:    s.TotalPeers,
		Seeds:    s.ConnectedSeeders,
		Ratio:    s.SeedRatio,
		Uploaded: s.SeedBytes,
		Seeding:  t.Uploading && t.Status == base.DownloadStatusDone,
	}, t, true
}

// startTorrentPoll starts the stats loop with the first torrent and keeps it
// until Close, so an install without torrents never runs it.
func (e *Engine) startTorrentPoll() {
	e.pollOnce.Do(func() {
		e.wg.Add(1)
		go func() {
			defer e.wg.Done()
			e.pollTorrents()
		}()
	})
}

func (e *Engine) pollTorrents() {
	tick := time.NewTicker(torrentStatsInterval)
	defer tick.Stop()
	for {
		select {
		case <-e.done:
			return
		case <-tick.C:
			for taskID, gid := range e.torrentPairs() {
				e.pollOne(taskID, gid)
			}
		}
	}
}

// pollOne reports one torrent and decides whether to keep watching it.
//
// The update carries no status. The app treats a done update as an event
// (rename, checksum sweep, re-dispatch), and a seeding torrent would fire all
// of that every poll. Loaded is sent because gopeed stops progress events at
// done. Speed is not sent for a done torrent: gopeed never zeroes
// Progress.Speed, so it would repeat the last download sample forever.
func (e *Engine) pollOne(taskID, gid string) {
	s, t, ok := e.readTorrentStats(gid)
	if !ok {
		if t == nil {
			// The library no longer has the task.
			e.forgetTorrent(taskID)
		}
		return
	}
	u := core.Update{Torrent: &s}
	if t.Progress != nil {
		u.Loaded = t.Progress.Downloaded
		if t.Status != base.DownloadStatusDone {
			u.Speed = t.Progress.Speed
		}
	}
	e.emit(taskID, u)
	if t.Status == base.DownloadStatusDone && !t.Uploading {
		// Seeding reached its target and the fetcher closed. Stop polling,
		// since Stats on a task without a fetcher makes the library restore
		// one.
		e.forgetTorrent(taskID)
	}
}

func (e *Engine) torrentPairs() map[string]string {
	e.mu.Lock()
	defer e.mu.Unlock()
	out := make(map[string]string, len(e.torrents))
	for taskID := range e.torrents {
		if gid := e.toGopeed[taskID]; gid != "" {
			out[taskID] = gid
		}
	}
	return out
}

func (e *Engine) isTorrent(taskID string) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.torrents[taskID]
}

func (e *Engine) forgetTorrent(taskID string) {
	e.mu.Lock()
	delete(e.torrents, taskID)
	e.mu.Unlock()
}
