package engine

// Torrents run in the torrent client (see internal/torrents). The engine
// decides where each one lands and which of its files it fetches before the
// client writes a byte, and reads every torrent's numbers in one call every
// few seconds, since the client tells nothing on its own.

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"log"
	"path"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/resolver/torrent"
	"github.com/junkerderprovinz/knightloader/internal/torrents"
)

// defaultMetadataTimeout is how long a magnet may wait for the swarm to send
// its metadata before the task fails.
const defaultMetadataTimeout = 2 * time.Minute

var (
	errNothingSelected = errors.New("no file of the torrent is selected")
	errNoClient        = errors.New("no torrent client is set up")
	errShuttingDown    = errors.New("shutting down")
)

// torrentStatsInterval is how often every torrent's numbers are read.
const torrentStatsInterval = 3 * time.Second

// filesPoll is how often a torrent just handed over is asked for its file
// list until it has one.
const filesPoll = 250 * time.Millisecond

// clientWait bounds one call to the torrent client outside a start, which
// waits for as long as the start does.
const clientWait = 30 * time.Second

// torrentRun is one torrent the engine handed to the client, for one task.
// Its fields are guarded by Engine.mu.
type torrentRun struct {
	hash string
	job  Job
	c    content
	sel  []int
	root string
	// from is what earlier runs of a seed run's torrent uploaded, and base
	// the client's numbers when this run took the torrent over, which the
	// client goes on counting from.
	from, base core.TorrentStats
	paused     bool
	// done is the download reported done, or for a seed run its files found
	// complete.
	done bool
	// closed is the torrent taken out of the client at a seeding target.
	closed  bool
	streams int
	// starts is where each file begins in the torrent's bytes, read when
	// the first reader opens one.
	starts []int64
}

// totals is a reading of the client's as the task counts it.
func (r *torrentRun) totals(x torrents.Reading) core.TorrentStats {
	return core.TorrentStats{
		Peers:       x.Peers,
		Seeds:       x.Seeds,
		Ratio:       r.from.Ratio + x.Ratio - r.base.Ratio,
		Uploaded:    r.from.Uploaded + x.Uploaded - r.base.Uploaded,
		SeedSeconds: r.from.SeedSeconds + x.SeedSeconds - r.base.SeedSeconds,
	}
}

// UseTorrents sets the client every torrent runs in and hands it the settings
// of the last SetTorrentConfig.
func (e *Engine) UseTorrents(c torrents.Client) {
	e.mu.Lock()
	e.bt = c
	e.mu.Unlock()
	e.pushPrefs()
}

// Torrents is the client every torrent runs in.
func (e *Engine) Torrents() torrents.Client { return e.client() }

func (e *Engine) client() torrents.Client {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.bt
}

// DownloadTorrent starts a magnet link or an uploaded .torrent. sel names the
// files to fetch by index in the torrent's file list; nil fetches all of them.
func (e *Engine) DownloadTorrent(taskID, uri, dir string, sel []int) {
	e.Start(Job{TaskID: taskID, URL: uri, Dir: dir, TorrentSelect: sel})
}

// startTorrent is Start's torrent branch: settle where the torrent lands,
// hand it to the client stopped, wait for its file list, check where every
// file would land, pick the files and start it. Start has already called
// wg.Add for it.
func (e *Engine) startTorrent(j Job, s *start) {
	go func() {
		defer e.wg.Done()
		defer e.startEnded(j.TaskID, s)
		e.bringUp(j, s)
	}()
}

func (e *Engine) bringUp(j Job, s *start) {
	// Compiled before the swarm is asked, so a broken rule costs no metadata
	// fetch.
	pick, err := j.FileRules.Compile()
	if err != nil {
		e.failStart(j, err)
		return
	}
	md, err := (torrent.Resolver{}).Describe(j.URL)
	if err != nil {
		e.failStart(j, err)
		return
	}
	hash := torrents.HashOf(md.InfoHash)
	magnet := torrent.IsMagnet(j.URL)
	sel := j.TorrentSelect
	if sel == nil && !magnet {
		sel = core.SelectedTorrentIndices(pick.Pick(md.Files))
	}
	bt := e.client()
	handed := false
	fail := func(err error) {
		if handed {
			e.letGo(bt, hash)
		}
		e.unplace(j.TaskID)
		if e.proceed(s, j) {
			e.failStart(j, err)
		}
	}
	switch {
	case sel != nil && len(sel) == 0:
		fail(errNothingSelected)
		return
	case bt == nil:
		fail(errNoClient)
		return
	}
	dir, err := e.placeTorrent(j, j.writeDir())
	if err != nil {
		fail(err)
		return
	}
	complete := false
	if j.Seed && !magnet {
		// The client takes these files as they are, so they are checked here.
		if err := seedFiles(dir, contentOfFiles(md.Name, md.Files), sel); err != nil {
			fail(err)
			return
		}
		complete = true
		if j.Verify || j.Recheck {
			err := CheckPieces(s.ctx, dir, j.URL)
			_, damaged := errors.AsType[*notInPlace](err)
			switch {
			case damaged && j.Recheck:
				complete = false
			case err != nil:
				fail(err)
				return
			}
		}
	}
	base, err := e.handOver(s.ctx, bt, j, hash, dir, complete)
	if err != nil {
		fail(e.startErr(s, err))
		return
	}
	handed = true
	files, err := e.awaitFiles(s, bt, hash)
	if err != nil {
		fail(err)
		return
	}
	c := contentOf(files)
	if sel == nil && magnet {
		sel = autoSelect(pick, c)
	}
	// Somebody changed the files while the swarm was being asked.
	if p, ok := e.takePendingPick(j.TaskID); ok {
		sel = p
	}
	// A magnet's file list comes from a stranger over the network and is
	// seen here for the first time; an uploaded .torrent was already checked
	// by the resolver. The client has written nothing yet.
	if err := checkLanding(dir, c); err != nil {
		fail(err)
		return
	}
	if j.Seed && magnet {
		if err := seedFiles(dir, c, sel); err != nil {
			fail(err)
			return
		}
	}
	root, err := e.settleTorrent(j.TaskID, c)
	if err != nil {
		fail(err)
		return
	}
	// A seed run reports nothing of the start but the file list, which a
	// torrent finished before the list was kept has not got.
	u := core.Update{TorrentFiles: pickedFiles(c, sel)}
	if !j.Seed {
		u.Status, u.File = core.StatusRunning, root
		u.Name, u.Size = c.shown(sel)
		if magnet {
			u.MagnetFiles = c.paths()
		}
	}
	e.emit(j.TaskID, u)
	if !e.proceed(s, j) {
		e.letGo(bt, hash)
		e.unplace(j.TaskID)
		return
	}
	run := &torrentRun{hash: hash, job: j, c: c, sel: slices.Clone(sel), root: root, base: base}
	if j.Seed {
		run.from = j.SeedFrom
	}
	e.pickMu.Lock()
	err = e.launch(bt, run)
	e.pickMu.Unlock()
	if err != nil {
		e.letGo(bt, hash)
		e.unplace(j.TaskID)
		e.failStart(j, err)
		return
	}
	e.startTorrentPoll()
	e.kickPoll()
}

// startErr is err, or the start giving up when its context ended.
func (e *Engine) startErr(s *start, err error) error {
	select {
	case <-e.done:
		return errShuttingDown
	default:
	}
	if s.ctx.Err() != nil {
		return context.Canceled
	}
	return err
}

// launch picks a run's files, starts it and records it. A change of the files
// that came while the torrent was being handed over is applied too. Caller
// holds pickMu.
func (e *Engine) launch(bt torrents.Client, run *torrentRun) error {
	ctx, cancel := context.WithTimeout(e.ctx, clientWait)
	defer cancel()
	if err := bt.Pick(ctx, run.hash, picked(run.sel, len(run.c.files))); err != nil {
		return err
	}
	if err := bt.Start(ctx, run.hash); err != nil {
		return err
	}
	taskID := run.job.TaskID
	e.mu.Lock()
	e.runs[taskID] = run
	e.mu.Unlock()
	if p, ok := e.takePendingPick(taskID); ok {
		if err := e.repick(run, p); err != nil {
			log.Printf("task %s: the files chosen while it started were not applied: %v", taskID, err)
		}
	}
	return nil
}

// handOver gives the client a torrent, or takes over the one it already has
// in dir, which it kept from before a restart. A seed run counts on from the
// client's numbers as they stand then, which base returns. A torrent the
// client has elsewhere is taken out and added again in dir.
func (e *Engine) handOver(ctx context.Context, bt torrents.Client, j Job, hash, dir string, complete bool) (base core.TorrentStats, err error) {
	readings, err := bt.Readings(ctx)
	if err != nil {
		return base, err
	}
	if r, ok := readings[hash]; ok {
		if samePath(r.Dir, dir) {
			if j.Seed {
				base = core.TorrentStats{Ratio: r.Ratio, Uploaded: r.Uploaded, SeedSeconds: r.SeedSeconds}
			}
			return base, nil
		}
		if err := bt.Delete(ctx, hash, false); err != nil {
			return base, err
		}
	}
	add := torrents.Add{Hash: hash, Dir: dir, Trackers: j.Trackers, Complete: complete}
	if torrent.IsMagnet(j.URL) {
		add.Magnet = j.URL
	} else if add.File, err = torrent.DecodeBytes(j.URL); err != nil {
		return base, err
	}
	return base, bt.Add(ctx, add)
}

// samePath reports whether the client's folder a and the engine's b are one,
// compared as the system's disks compare names.
func samePath(a, b string) bool {
	a, b = filepath.Clean(filepath.FromSlash(a)), filepath.Clean(b)
	if runtime.GOOS == "windows" || runtime.GOOS == "darwin" {
		return strings.EqualFold(a, b)
	}
	return a == b
}

// letGo takes a torrent the engine handed over out of the client again,
// leaving its files.
func (e *Engine) letGo(bt torrents.Client, hash string) {
	ctx, cancel := context.WithTimeout(context.Background(), clientWait)
	defer cancel()
	if err := bt.Delete(ctx, hash, false); err != nil && !errors.Is(err, torrents.ErrUnknown) {
		log.Printf("torrent %s could not be taken out of the torrent client: %v", hash, err)
	}
}

// awaitFiles waits for the client to have a torrent's file list, which for a
// magnet comes from the swarm. No peer can answer while the interface the
// client is tied to is down, or while torrents are held, so a magnet waits
// however long that lasts and then gets its whole timeout again.
func (e *Engine) awaitFiles(s *start, bt torrents.Client, hash string) ([]torrents.File, error) {
	e.mu.Lock()
	timeout, iface, held := e.metadataTimeout, e.prefs.Interface, e.held
	e.mu.Unlock()
	if timeout <= 0 {
		timeout = defaultMetadataTimeout
	}
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	tick := time.NewTicker(filesPoll)
	defer tick.Stop()
	for {
		// qBittorrent takes an add in the background, and has no file list,
		// nor the torrent at all, for a moment after it.
		files, err := bt.Files(s.ctx, hash)
		if err != nil && !errors.Is(err, torrents.ErrUnknown) {
			return nil, e.startErr(s, err)
		}
		if len(files) > 0 {
			return files, nil
		}
		select {
		case <-tick.C:
		case <-timer.C:
			if !InterfaceUp(iface) || held != nil && held() {
				timer.Reset(timeout)
				continue
			}
			return nil, fmt.Errorf("no peer sent this torrent's file list within %s", timeout)
		case <-s.ctx.Done():
			return nil, e.startErr(s, s.ctx.Err())
		}
	}
}

// content is a torrent's files as the client lands them: name is the folder
// a torrent of several files lands in, empty for a torrent of one file, and
// each file's path is inside that folder, as a .torrent's own file tree shows
// it.
type content struct {
	name  string
	files []core.TorrentFile
}

// contentOf reads the client's file list, whose paths start with the
// torrent's folder when it has one.
func contentOf(files []torrents.File) content {
	var c content
	if len(files) > 0 {
		if top, _, ok := strings.Cut(files[0].Path, "/"); ok && allUnder(files, top) {
			c.name = top
		}
	}
	c.files = make([]core.TorrentFile, len(files))
	for i, f := range files {
		p := f.Path
		if c.name != "" {
			p = strings.TrimPrefix(p, c.name+"/")
		}
		c.files[i] = core.TorrentFile{Path: p, Size: f.Size}
	}
	return c
}

func allUnder(files []torrents.File, top string) bool {
	for _, f := range files {
		if !strings.HasPrefix(f.Path, top+"/") {
			return false
		}
	}
	return true
}

// contentOfFiles is the content of a .torrent read by the resolver, whose
// single file carries the torrent's name as its path.
func contentOfFiles(name string, files []core.TorrentFile) content {
	c := content{files: files}
	if len(files) != 1 || files[0].Path != name {
		c.name = name
	}
	return c
}

// landing is where each file would be written, relative to the folder the
// torrent lands in.
func (c content) landing() []string {
	out := make([]string, len(c.files))
	for i, f := range c.files {
		out[i] = path.Join(c.name, f.Path)
	}
	return out
}

// paths is every file by its path inside the torrent.
func (c content) paths() []string {
	out := make([]string, len(c.files))
	for i, f := range c.files {
		out[i] = f.Path
	}
	return out
}

// shown is the name and size to show for a torrent, the size summed over the
// selection.
func (c content) shown(sel []int) (name string, size int64) {
	name = c.name
	if name == "" && len(c.files) > 0 {
		name = path.Base(c.files[0].Path)
	}
	for i, f := range c.files {
		if sel == nil || slices.Contains(sel, i) {
			size += f.Size
		}
	}
	return name, size
}

// checkLanding refuses a torrent that would write outside dir, or outside its
// own folder there. A file that climbs out of that folder lands beside the
// torrent, where removing it with its files would delete whatever else has
// that name.
func checkLanding(dir string, c content) error {
	if err := torrent.Contained(dir, c.landing()); err != nil || c.name == "" {
		return err
	}
	return torrent.Contained(filepath.Join(dir, filepath.FromSlash(c.name)), c.paths())
}

// autoSelect is the selection pick makes from a torrent's files, by index, or
// nil when it keeps every file.
func autoSelect(pick torrent.Picker, c content) []int {
	return core.SelectedTorrentIndices(pick.Pick(slices.Clone(c.files)))
}

// picked is sel as one entry for each of count files.
func picked(sel []int, count int) []bool {
	out := make([]bool, count)
	for i := range out {
		out[i] = sel == nil || slices.Contains(sel, i)
	}
	return out
}

// startTorrentPoll starts the stats loop with the first torrent and keeps it
// until Close, so an install without torrents never runs it.
func (e *Engine) startTorrentPoll() {
	e.pollOnce.Do(func() {
		e.mu.Lock()
		if e.closed {
			e.mu.Unlock()
			return
		}
		e.wg.Add(1)
		e.mu.Unlock()
		go func() {
			defer e.wg.Done()
			e.pollTorrents()
		}()
	})
}

// kickPoll has the stats loop read the torrents now, rather than at its next
// tick.
func (e *Engine) kickPoll() {
	select {
	case e.kick <- struct{}{}:
	default:
	}
}

func (e *Engine) pollTorrents() {
	tick := time.NewTicker(torrentStatsInterval)
	defer tick.Stop()
	for {
		select {
		case <-e.done:
			return
		case <-tick.C:
		case <-e.kick:
		}
		e.readTorrents()
	}
}

// readTorrents reads every torrent in one call and reports each run.
func (e *Engine) readTorrents() {
	bt := e.client()
	if bt == nil {
		return
	}
	ctx, cancel := context.WithTimeout(e.ctx, clientWait)
	readings, err := bt.Readings(ctx)
	cancel()
	if err != nil {
		if e.ctx.Err() == nil {
			log.Printf("the torrent client could not be read: %v", err)
		}
		return
	}
	e.mu.Lock()
	ids := make([]string, 0, len(e.runs))
	for id := range e.runs {
		ids = append(ids, id)
	}
	watch := e.watch
	e.mu.Unlock()
	if watch != nil {
		watch(readings)
	}
	for _, id := range ids {
		e.report(bt, id, readings)
	}
}

// report turns one torrent's reading into the task's update.
//
// A done torrent's update carries no status. The app treats a done update as
// an event (rename, checksum sweep, re-dispatch), and a seeding torrent would
// fire all of that every poll. Neither does it carry a speed, which is the
// download's.
func (e *Engine) report(bt torrents.Client, taskID string, readings map[string]torrents.Reading) {
	e.mu.Lock()
	run := e.runs[taskID]
	if run == nil || run.closed {
		e.mu.Unlock()
		return
	}
	x, held := readings[run.hash]
	switch {
	case !held, x.State == torrents.Failed:
		delete(e.runs, taskID)
		e.mu.Unlock()
		why := cmp.Or(x.Err, "the torrent client no longer has this torrent")
		if held {
			e.letGo(bt, run.hash)
		}
		e.endRun(run, errors.New(why))
		return
	}
	st := run.totals(x)
	var u core.Update
	switch {
	case run.job.Seed && !run.done && !x.State.Complete():
		// Still checking the files it was started on.
		e.mu.Unlock()
		return
	case run.job.Seed:
		run.done = true
	case !run.done && x.State.Complete():
		run.done = true
		u.Status, u.File = core.StatusDone, run.root
	case !run.done:
		e.mu.Unlock()
		if !run.paused {
			e.emit(taskID, core.Update{Status: core.StatusRunning, Loaded: x.Done, Speed: x.Speed, File: run.root, Torrent: &st})
		}
		return
	}
	st.Seeding = x.State == torrents.Seeding
	reached := run.done && e.targets.reached(run.job.SeedMark.Since(st))
	if reached {
		run.closed = true
		st.Seeding, st.AtTarget = false, true
	}
	e.mu.Unlock()
	if u.Status == core.StatusDone {
		// Before done goes out, since done is what moves the files on.
		e.settleModes(taskID)
	}
	if reached {
		e.letGo(bt, run.hash)
	}
	u.Loaded, u.Torrent = x.Done, &st
	if run.job.Seed {
		u.Loaded = 0
	}
	e.emit(taskID, u)
}

// endRun reports a torrent that could not start to seed, or that the client
// gave up on or no longer has: a download as failed, a finished one as seeding
// no more, with the totals it had.
func (e *Engine) endRun(run *torrentRun, err error) {
	taskID := run.job.TaskID
	if !run.done && !run.job.Seed {
		e.emit(taskID, core.Update{Status: core.StatusError, Err: err.Error(), File: run.root})
		return
	}
	log.Printf("task %s could not go on seeding: %v", taskID, err)
	st := &core.TorrentStats{Uploaded: run.from.Uploaded, Ratio: run.from.Ratio, SeedSeconds: run.from.SeedSeconds}
	if nip, ok := errors.AsType[*notInPlace](err); ok {
		st.NotSeeded = nip.why
	}
	e.emit(taskID, core.Update{Torrent: st})
}

// RunningTorrents is the hash of every torrent the engine keeps running in
// the client, so one the client starts again after a restart of its own can
// be told from one nobody wants running.
func (e *Engine) RunningTorrents() []string {
	e.mu.Lock()
	defer e.mu.Unlock()
	var out []string
	for _, run := range e.runs {
		if !run.paused && !run.closed {
			out = append(out, run.hash)
		}
	}
	slices.Sort(out)
	return out
}

// runOf is the run of a task's torrent, or nil.
func (e *Engine) runOf(taskID string) *torrentRun {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.runs[taskID]
}

// pauseTorrent stops a task's torrent in the client.
func (e *Engine) pauseTorrent(run *torrentRun) {
	bt := e.client()
	if bt == nil {
		return
	}
	ctx, cancel := context.WithTimeout(e.ctx, clientWait)
	defer cancel()
	if err := bt.Stop(ctx, run.hash); err != nil {
		log.Printf("task %s could not be stopped in the torrent client: %v", run.job.TaskID, err)
		return
	}
	e.mu.Lock()
	run.paused = true
	done := run.done
	e.mu.Unlock()
	if !done {
		e.emit(run.job.TaskID, core.Update{Status: core.StatusPaused, File: run.root})
	}
}

// resumeTorrent starts a task's torrent in the client again.
func (e *Engine) resumeTorrent(run *torrentRun) {
	bt := e.client()
	if bt == nil {
		return
	}
	ctx, cancel := context.WithTimeout(e.ctx, clientWait)
	defer cancel()
	if err := bt.Start(ctx, run.hash); err != nil {
		e.emit(run.job.TaskID, core.Update{Status: core.StatusError, Err: err.Error(), File: run.root})
		return
	}
	e.mu.Lock()
	run.paused = false
	done := run.done
	e.mu.Unlock()
	if !done {
		e.emit(run.job.TaskID, core.Update{Status: core.StatusRunning, File: run.root})
	}
	e.kickPoll()
}

// removeTorrent takes a task's torrent out of the client.
func (e *Engine) removeTorrent(run *torrentRun) {
	if run.closed {
		return
	}
	if bt := e.client(); bt != nil {
		e.letGo(bt, run.hash)
	}
}
