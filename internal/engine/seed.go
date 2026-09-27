package engine

// A finished torrent taken up again only to seed, after its files moved or
// after a restart (see Job.Seed). The library opens a torrent's files in the
// folder it was added in and keeps nothing across a restart, so such a torrent
// has to be added again; the library then checks the files it finds, calls the
// torrent done and seeds. That start and that finish belong to a download the
// app already has as done, so they go unreported, and the swarm readings carry
// on from where the earlier run left them.
//
// The library measures its seeding targets from the new start, so it would
// seed such a torrent to the full targets once more. The engine stops it
// itself when the totals reach them.

import (
	"log"

	"github.com/GopeedLab/gopeed/pkg/download"
	"github.com/junkerderprovinz/knightloader/internal/core"
)

// seedRun is one torrent started only to seed: from is what it had uploaded
// before, and seeding is whether the library has found its files complete.
type seedRun struct {
	from    core.TorrentStats
	seeding bool
}

// seedTargets are the seeding targets as the settings last set them, zero
// meaning no target.
type seedTargets struct {
	ratio   float64
	seconds int64
}

// reached reports whether a torrent's totals meet one of the targets.
func (t seedTargets) reached(s core.TorrentStats) bool {
	return (t.ratio > 0 && s.Ratio >= t.ratio) || (t.seconds > 0 && s.SeedSeconds >= t.seconds)
}

// seedOf is a copy of a task's seed run, and false for any other task.
func (e *Engine) seedOf(taskID string) (seedRun, bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if sd := e.seeds[taskID]; sd != nil {
		return *sd, true
	}
	return seedRun{}, false
}

// seedEvent is onEvent for a torrent started only to seed. Done is the files
// found complete, from when on the torrent seeds; an error ends the seeding.
// The rest is the download's and goes unreported.
func (e *Engine) seedEvent(taskID string, ev *download.Event) {
	switch ev.Key {
	case download.EventKeyDone:
		e.mu.Lock()
		if sd := e.seeds[taskID]; sd != nil {
			sd.seeding = true
		}
		e.mu.Unlock()
		if s, _, ok := e.readTorrentStats(ev.Task.ID); ok {
			e.emit(taskID, core.Update{Torrent: e.seedStats(taskID, s)})
		}
	case download.EventKeyError:
		e.endSeed(taskID, ev.Err)
	}
}

// seedStats adds what a seed run had uploaded before to a reading of the
// library's, which counts from the new start.
func (e *Engine) seedStats(taskID string, s core.TorrentStats) *core.TorrentStats {
	if sd, ok := e.seedOf(taskID); ok {
		s.Uploaded += sd.from.Uploaded
		s.Ratio += sd.from.Ratio
		s.SeedSeconds += sd.from.SeedSeconds
	}
	return &s
}

// seedDone reports whether a seed run's totals have reached a target, and if
// so takes the torrent out of the library, which closes its upload, and out
// of the poll. The files stay, and so does the rest of what the engine knows
// of the task, for a later removal with its files.
func (e *Engine) seedDone(taskID, gid string, s core.TorrentStats) bool {
	e.mu.Lock()
	reached := e.targets.reached(s)
	e.mu.Unlock()
	if !reached {
		return false
	}
	e.forgetTorrent(taskID)
	e.dropTorrent(gid)
	e.dropUnpicked(taskID, true)
	return true
}

// endSeed reports a seed run that could not start or broke off as seeding no
// more, with the totals it had, and leaves the task done.
func (e *Engine) endSeed(taskID string, err error) {
	e.mu.Lock()
	sd := e.seeds[taskID]
	delete(e.seeds, taskID)
	e.mu.Unlock()
	if sd == nil {
		return
	}
	log.Printf("task %s could not go on seeding: %v", taskID, err)
	e.emit(taskID, core.Update{Torrent: &core.TorrentStats{Uploaded: sd.from.Uploaded, Ratio: sd.from.Ratio, SeedSeconds: sd.from.SeedSeconds}})
}

// failStart reports a torrent start that failed: as an error on the task, or
// for a start only to seed, as the end of its seeding.
func (e *Engine) failStart(j Job, err error) {
	if j.Seed {
		e.endSeed(j.TaskID, err)
		return
	}
	e.emit(j.TaskID, core.Update{Status: core.StatusError, Err: err.Error()})
}
