package app

// Torrents a debrid service fetches. The service takes the torrent onto its
// own servers, and debrid.TorrentBackend then hands the files to the engine one
// at a time, each under an engine id of its own, while the task stays one row.
// The engine reports every file to App.engineUpdate, which passes those reports
// to the backend fetching the file instead of the list.

import (
	"context"
	"errors"
	"log"
	"maps"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"sync"

	"github.com/junkerderprovinz/knightloader/internal/collide"
	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/resolver/debrid"
	"github.com/junkerderprovinz/knightloader/internal/resolver/torrent"
	"github.com/junkerderprovinz/knightloader/internal/settings"
)

// torrentParts is every torrent file the engine is fetching for a debrid
// service, by engine id. Its zero value is ready to use.
type torrentParts struct {
	partsMu sync.Mutex
	waits   map[string]*partWait
	// current is each task's file in flight, by engine id.
	current map[string]string
}

// partWait is one FetchPart waiting for the engine.
type partWait struct {
	taskID   string
	progress func(loaded, speed int64, file string)
	// done takes the one terminal report; it is buffered, so the engine never
	// waits on a FetchPart that has already given up.
	done chan partResult
}

type partResult struct {
	file string
	err  error
}

func (p *torrentParts) watch(taskID, id string, progress func(loaded, speed int64, file string)) *partWait {
	w := &partWait{taskID: taskID, progress: progress, done: make(chan partResult, 1)}
	p.partsMu.Lock()
	defer p.partsMu.Unlock()
	if p.waits == nil {
		p.waits, p.current = map[string]*partWait{}, map[string]string{}
	}
	p.waits[id] = w
	p.current[taskID] = id
	return w
}

// forget drops w, unless a newer FetchPart of the same file has taken its
// place.
func (p *torrentParts) forget(id string, w *partWait) {
	p.partsMu.Lock()
	defer p.partsMu.Unlock()
	if p.waits[id] != w {
		return
	}
	delete(p.waits, id)
	if p.current[w.taskID] == id {
		delete(p.current, w.taskID)
	}
}

// deliver hands one engine report to the FetchPart waiting for it, and reports
// whether there was one.
func (p *torrentParts) deliver(id string, u core.Update) bool {
	p.partsMu.Lock()
	w := p.waits[id]
	p.partsMu.Unlock()
	if w == nil {
		return false
	}
	switch u.Status {
	case core.StatusDone:
		w.finish(partResult{file: u.File})
	case core.StatusError:
		w.finish(partResult{err: errors.New(u.Err)})
	case core.StatusRunning:
		w.progress(u.Loaded, u.Speed, u.File)
	}
	return true
}

func (w *partWait) finish(r partResult) {
	select {
	case w.done <- r:
	default:
	}
}

// engineIDFor is the id the engine fetches a task under: the file in flight
// for a torrent a debrid service fetched, the task's own id for everything
// else.
func (p *torrentParts) engineIDFor(taskID string) string {
	p.partsMu.Lock()
	defer p.partsMu.Unlock()
	if id := p.current[taskID]; id != "" {
		return id
	}
	return taskID
}

// engineUpdate is where the engine reports. A file of a torrent a debrid
// service fetched goes to the backend fetching it; everything else is a task.
func (a *App) engineUpdate(id string, u core.Update) {
	if a.torrentFiles.deliver(id, u) {
		return
	}
	a.onUpdate(id, u)
}

// FetchPart downloads one file of a torrent into the task's folder, with the
// task's collision policy, and waits for it. Under "skip" a file already there
// counts as fetched, which is the policy's promise for a single download.
func (h engineHandoff) FetchPart(ctx context.Context, p debrid.Part) (string, error) {
	h.a.mu.Lock()
	t := h.a.tasks[p.TaskID]
	if t == nil {
		h.a.mu.Unlock()
		return "", errors.New("the download was removed")
	}
	job := h.a.engineJobLocked(t, h.a.Settings.Get(), p.URL, nil, p.Conns)
	h.a.mu.Unlock()
	// The path comes from whoever made the torrent.
	if err := torrent.Contained(job.Dir, []string{p.Path}); err != nil {
		return "", err
	}
	job.TaskID = p.ID
	job.Dir = filepath.Join(job.Dir, filepath.FromSlash(path.Dir(p.Path)))
	job.WorkDir = ""
	job.Name = path.Base(p.Path)
	job.Relink = p.Relink
	// The library preallocates a file whole, so one still the size of this
	// file is the attempt a restart cut short, not a finished copy.
	leftover{path: p.Leftover, size: p.Size}.drop(p.TaskID)
	if job.Collision == collide.Skip {
		target := filepath.Join(job.Dir, collide.SafeName(job.Name))
		if _, err := os.Lstat(target); err == nil {
			return target, nil
		}
	}
	w := h.a.torrentFiles.watch(p.TaskID, p.ID, p.Progress)
	defer h.a.torrentFiles.forget(p.ID, w)
	h.Engine.Start(job)
	select {
	case r := <-w.done:
		return r.file, r.err
	case <-ctx.Done():
		return "", ctx.Err()
	}
}

// serviceRuns is the runs of every account that takes torrents, by slot. Its
// zero value is ready to use.
type serviceRuns struct {
	mu     sync.Mutex
	bySlot map[string]*debrid.Runs
}

// of returns the runs of one account, made on first use.
func (s *serviceRuns) of(slot string) *debrid.Runs {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.bySlot == nil {
		s.bySlot = map[string]*debrid.Runs{}
	}
	rs := s.bySlot[slot]
	if rs == nil {
		rs = debrid.NewRuns(slot)
		s.bySlot[slot] = rs
	}
	return rs
}

// stop stops the runs of every account (see debrid.Runs.Stop).
func (s *serviceRuns) stop() {
	s.mu.Lock()
	all := slices.Collect(maps.Values(s.bySlot))
	s.mu.Unlock()
	for _, rs := range all {
		rs.Stop()
	}
}

// restoreServiceJob hands the job a task held before the restart back to its
// account's runs, so the task carries on with it, and a removal still deletes
// it there.
func (a *App) restoreServiceJob(t *core.Task) {
	if j := t.ServiceJob; j != nil && t.Status != core.StatusDone {
		a.serviceRuns.of(j.Slot).Restore(t.ID, t.URL, *j)
	}
}

// applyServiceJobLocked records the job a debrid service holds for t, as the
// backend reported it. Caller holds a.mu.
func applyServiceJobLocked(t *core.Task, j *core.ServiceJob) {
	if j == nil {
		return
	}
	if j.ID == "" {
		t.ServiceJob = nil
		return
	}
	c := *j
	t.ServiceJob = &c
}

// carriesOnLocked reports whether a failed task goes back to the torrent its
// debrid service still holds, keeping the job there and the files already
// here: its backend holds one, and the route leads there again. Caller holds
// a.mu.
func (a *App) carriesOnLocked(t *core.Task) bool {
	h, ok := a.backendFor(t.Resolver).(interface{ Holds(string) bool })
	if !ok || !h.Holds(t.ID) {
		return false
	}
	res := a.resolverForTaskLocked(t)
	return res != nil && res.Info().ID == t.Resolver
}

// torrentsVia lets a debrid slot take torrents through svc as well, with links
// still doing everything else.
func (a *App) torrentsVia(slot string, svc debrid.TorrentService, links backend, eng engineHandoff) backend {
	tb := debrid.NewTorrentBackend(svc, links, eng, a.serviceRuns.of(slot), a.onUpdate)
	tb.Files = a.torrentSelection
	tb.Rules = a.torrentRules
	tb.Keep = func() bool { return a.Settings.Get().Torrent.KeepOnService }
	tb.CachedOnly = a.cachedOnly
	tb.Added = func(job string) { a.claimImported(slot, job) }
	return tb
}

// cachedOnly reports whether a task's torrent may go to a debrid service only
// when the service has it cached. A task pinned to a service goes there
// whatever it has, since somebody chose that service for it.
func (a *App) cachedOnly(taskID string) bool {
	if !a.Settings.Get().Torrent.DebridCachedOnly {
		return false
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	t := a.tasks[taskID]
	return t != nil && t.ResolverPin == ""
}

// torrentRules is the file rules of a torrent's category, which choose its
// files on a debrid service as they do in the built-in client (see
// torrentJobLocked). A download imported from an account is not a torrent
// this instance was handed, and fetches what the account holds.
func (a *App) torrentRules(taskID string) (func(string, int64) bool, error) {
	a.mu.Lock()
	t := a.tasks[taskID]
	if t == nil || !torrent.IsURI(t.URL) {
		a.mu.Unlock()
		return nil, nil
	}
	category := t.Category
	a.mu.Unlock()
	rules := fileRules(a.Settings.Get().TorrentFileRulesFor(category))
	if rules.Empty() {
		return nil, nil
	}
	p, err := rules.Compile()
	if err != nil {
		return nil, err
	}
	return p.Wants, nil
}

// torrentSelection is the files the user picked from a task's torrent.
func (a *App) torrentSelection(taskID string) []core.TorrentFile {
	a.mu.Lock()
	defer a.mu.Unlock()
	if t := a.tasks[taskID]; t != nil {
		return slices.Clone(t.TorrentFiles)
	}
	return nil
}

// seedFromService hands a torrent a debrid service fetched to a client to seed
// once its files are here, when the Torrents page asks for that. The built-in
// client takes the files up where they are and seeds them under the seeding
// targets like a torrent of its own; qBittorrent is handed them as they are
// (see seedInQBittorrent). Neither fetches anything: a torrent whose files are
// not all there, or do not match, is not seeded (see engine/seed.go).
func (a *App) seedFromService(id string) {
	tc := a.Settings.Get().Torrent
	qbit := tc.SeedIn == settings.SeedInQBittorrent
	builtIn := (torrent.Resolver{}).Info().ID
	if !tc.SeedAfterDebrid || !qbit && a.resolverOff(builtIn) {
		return
	}
	a.mu.Lock()
	t := a.tasks[id]
	if t == nil || t.Status != core.StatusDone || !torrent.IsURI(t.URL) || a.relocating[id] {
		a.mu.Unlock()
		return
	}
	tb, ok := a.backendFor(t.Resolver).(*debrid.TorrentBackend)
	if !ok {
		a.mu.Unlock()
		return
	}
	if slices.ContainsFunc(t.TorrentFiles, func(f core.TorrentFile) bool { return !f.Selected }) {
		t.Note = "Not seeded: only some of its files were fetched, and seeding it would fetch the rest"
		c := a.copyLocked(t)
		a.mu.Unlock()
		a.publish(&c)
		return
	}
	from, file := t.Resolver, t.File
	// A torrent of several files is in a folder of its name, as the service
	// fetched it (see debrid.localPath).
	folder := t.File == ""
	if folder {
		file = filepath.Join(a.dirFor(t), strings.TrimSpace(t.Name))
	}
	if qbit {
		h := handover{link: t.URL, file: file, folder: folder, files: slices.Clone(t.TorrentFiles)}
		a.mu.Unlock()
		a.seedInQBittorrent(id, tb, h, tc.QBittorrent)
		return
	}
	t.Resolver, t.File = builtIn, file
	job, err := a.seedJobLocked(t)
	if err != nil {
		t.Resolver = from
		if folder {
			t.File = ""
		}
		a.mu.Unlock()
		log.Printf("task %s is not seeded: %v", id, err)
		return
	}
	// Everything the torrent has, and nothing a file rule would leave out:
	// the service fetched it whole, and a file that is not here ends the
	// seeding rather than being fetched. The files came from the service, not
	// the swarm, so their pieces are checked too.
	job.TorrentSelect, job.FileRules, job.Verify = nil, torrent.FileRules{}, true
	a.started[id] = true
	c := a.copyLocked(t)
	a.mu.Unlock()
	a.publish(&c)
	tb.Release(id)
	a.Engine.Start(job)
}
