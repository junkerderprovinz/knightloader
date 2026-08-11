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

// defaultMetadataTimeout is how long a magnet may spend asking the swarm for
// its own metadata before the task is failed with a sentence rather than left
// spinning.
//
// There is no shorter honest answer available: Downloader.Resolve takes no
// context and blocks inside the torrent client until GotInfo fires, so this is
// a deadline on WAITING for it, not a cancellation of it. A magnet with live
// peers answered in two seconds on the live spike; two minutes is the point
// past which nobody is coming.
const defaultMetadataTimeout = 2 * time.Minute

// torrentStatsInterval is how often a torrent task's swarm numbers are read.
//
// A poll and not an event, because gopeed has no event for any of this. Its
// progress events stop the moment a download is done (Downloader.watch returns
// after emitting done) while seeding carries on for hours afterwards - verified
// live: SeedTime kept climbing three, six, nine seconds past the done event
// with no further event of any kind. Peers, ratio and the end of seeding are
// therefore only visible to something that asks.
const torrentStatsInterval = 3 * time.Second

// DownloadTorrent starts a magnet link or an uploaded .torrent, and is the
// torrent-shaped sibling of DownloadTo.
//
// It is a NEW CALL SHAPE INTO THE SAME DOWNLOADER, not a second pipeline.
// DownloadTo is HTTP-shaped - a URL, a header map, a connection count - and
// none of those three mean anything to a swarm, while the two things that do
// (which files, which extra trackers) have nowhere to go in it. The downloader
// underneath is the identical one: gopeed's default FetchManagers already
// include its bt fetcher, so there is nothing to register and nothing to
// configure, only a request built the shape the bt fetcher reads.
//
// sel names the files to fetch by their index in the resolved file list; nil
// fetches all of them. See core.SelectedTorrentIndices for where that list
// comes from.
func (e *Engine) DownloadTorrent(taskID, uri, dir string, sel []int) {
	e.Start(Job{TaskID: taskID, URL: uri, Dir: dir, TorrentSelect: sel})
}

// startTorrent is Start's torrent branch. It resolves the link (which for a
// magnet means waiting on the swarm), checks where every file in it would
// actually land, and only then creates the task.
//
// It does not call e.wg.Add itself - Start already did, under e.mu, before
// branching here, and a second Add here would double-count this one job
// against a single eventual Done. See Start's own comment for why that Add
// has to happen before the branch, not inside either side of it.
func (e *Engine) startTorrent(j Job) {
	go func() {
		defer e.wg.Done()
		rr, err := e.resolveTorrent(j)
		if err != nil {
			e.emit(j.TaskID, core.Update{Status: core.StatusError, Err: err.Error()})
			return
		}
		// THE CHECK THE MAGNET PATH EXISTS FOR. An uploaded .torrent had every
		// path in it examined by the resolver before it ever became a task; a
		// magnet's file list was invented by a stranger and arrived from the
		// network thirty seconds ago, and this is the first moment anything in
		// this app has seen it. Run before Create, because Create is what starts
		// writing.
		if err := torrent.Contained(j.Dir, landingPaths(rr.Res)); err != nil {
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

// resolveTorrent waits on a resolve that cannot be cancelled.
//
// Downloader.Resolve has no context and blocks inside the torrent client, so
// the wait is put on this side of it: the call runs on its own goroutine, and
// this one gives up on the deadline or on the engine closing. The abandoned
// goroutine is not leaked past shutdown - Downloader.Close tears down the
// torrent client, which releases the GotInfo wait it is parked on - and its
// channel is buffered so it can never block trying to hand back a result
// nobody is waiting for any more.
func (e *Engine) resolveTorrent(j Job) (*download.ResolveResult, error) {
	req := &base.Request{URL: j.URL, Proxy: requestProxy(j.Route)}
	if len(j.Trackers) > 0 {
		// The bt fetcher reads its own extra type here and casts to it directly
		// (base.ParseReqExtra, then a type assertion in addTorrent). An
		// http-shaped Extra, which is what every other job in this engine
		// carries, would be converted field-by-name into an empty one - so
		// torrent requests state theirs or state nothing.
		req.Extra = &gbt.ReqExtra{Trackers: j.Trackers}
	}
	opts := &base.Options{Path: j.Dir, SelectFiles: j.TorrentSelect}

	type answer struct {
		rr  *download.ResolveResult
		err error
	}
	ch := make(chan answer, 1)
	go func() {
		// A RECOVER, ON A LIBRARY BOUNDARY, AND IT IS NOT DEFENSIVE PADDING.
		// The torrent stack underneath asserts rather than returns on input it
		// considers impossible - anacrolix/torrent's Client.AddTorrentOpt opens
		// with panicif.Zero on the info hash - and this goroutine is where that
		// assertion fires. An unrecovered panic on a goroutine takes the process
		// with it, so without this a pasted line of text is a way to kill the
		// app. The resolver refuses the two links known to reach it
		// (see torrent.checkMagnet); this is what happens the day there is a
		// third, and it fails one download instead of the whole instance.
		//
		// gopeed's own bt fetcher already does the same thing around
		// torrent.Stats for the same reason, so this is the house style of the
		// library being wrapped and not an opinion imposed on it.
		defer func() {
			if r := recover(); r != nil {
				ch <- answer{nil, fmt.Errorf("the torrent library refused this link: %v", r)}
			}
		}()
		rr, err := e.d.Resolve(req, opts)
		ch <- answer{rr, err}
	}()

	// Through the same lock the setter writes it under. Every other field this
	// engine shares between goroutines already goes through e.mu and this one is
	// no different for being read once.
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

// landingPaths is where each file in a resolved torrent would be written,
// relative to the download folder.
//
// It mirrors the library's own assembly rather than approximating it: a
// resource with a Name is a folder torrent and every file sits under it, a
// resource without one is a single file whose name is the whole path. Getting
// this wrong in the safe direction would check paths nothing writes to; getting
// it wrong in the other would check nothing at all.
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

// torrentMeta is the name and size to show for a resolved torrent.
//
// The size is recomputed over the selection rather than taken from the
// resource, because at this point in the sequence nothing else has: a bt
// resolve calls base.Resource.CalcSize with nil, so the resource that comes
// back states the WHOLE torrent however few files were asked for - verified
// live, a resolve limited to one 1.5 KB subtitle still reported 129 MB. The
// library does narrow it, but not until Create; this update goes out before
// that, and a row that reads 129 GB for a minute and then 4 MB is a row nobody
// believes the second time.
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

// readTorrentStats is the verified path from a live task to swarm numbers, and
// it is the whole answer to the one question docs/torrent-support.md left open.
//
// Confirmed against gopeed v1.9.3 with a real magnet link (cmd/spike-torrent),
// not read off a type name: Downloader.Stats(taskID) returns an `any`, and for
// a bt task the concrete value behind it is a *pkg/protocol/bt.Stats. Nothing
// hangs off Task.Meta and no protocol-specific cast of the task itself is
// needed - the cast is on the stats value.
//
// SEEDING IS DERIVED HERE AND NOWHERE ELSE, because the obvious field is a
// trap. download.Task.Uploading is assigned at create time from a type
// assertion on the fetcher ("_, task.Uploading = f.(fetcher.Uploader)"), so it
// is true for every torrent task from its first second - the live spike showed
// Uploading true on a task two seconds into a 129 MB download. It only becomes
// the thing its name suggests once the download is done, which is why it is
// read together with the status and never alone.
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

// startTorrentPoll brings the stats loop up on the first torrent this engine
// ever starts and leaves it up until Close. Started lazily so an install that
// never touches a torrent never runs it at all.
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
// The update it sends CARRIES NO STATUS, deliberately. A finished torrent seeds
// for hours, and every one of those polls would otherwise be another
// StatusDone: the app treats a done update as an event, not as a restatement -
// it renames the finished file, re-runs the checksum sweep and re-dispatches
// the queue - so a status here would fire all of that every three seconds for
// as long as the torrent seeds. Loaded is sent because it is read as a value
// and is the live one, and because gopeed stops sending progress events the
// moment a torrent is done.
//
// Speed is NOT read from t.Progress.Speed for a done torrent, unlike Loaded -
// that field is a trap the same shape as Uploading (readTorrentStats' own
// comment): gopeed does not zero it once the download stops, so it reads as
// whatever the last download-phase sample happened to be, forever. Reproduced
// live: a completed, seeding torrent kept reporting its final download speed
// unchanged for 56+ seconds of polling. Speed here means download throughput,
// and a done torrent has none, seeding or not - a future upload-speed reading
// would be a different, explicitly-named field, not this one wearing two
// meanings.
func (e *Engine) pollOne(taskID, gid string) {
	s, t, ok := e.readTorrentStats(gid)
	if !ok {
		if t == nil {
			// The library has no such task any more - deleted, or never restored.
			// Nothing further will ever be known about it.
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
		// Seeding is over: the ratio or the time target was reached and the
		// fetcher closed itself. The reading just sent already says Seeding
		// false, so this is only the point at which there stops being anything
		// worth asking about - and it matters that it stops, because Stats on a
		// task with no fetcher left makes the library restore one.
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
