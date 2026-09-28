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
// itself when the totals reach them. A torrent started to seed by hand counts
// them from its mark instead, which is where the library counts from as well.
//
// A seed run never fetches. The library would fetch whatever it finds missing
// or damaged, so a file that is not there at its full size ends the run before
// the library is handed the torrent, and a torrent whose files are all chosen
// ends the moment the library writes into one of them while it checks them.

import (
	"errors"
	"fmt"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"slices"
	"time"

	"github.com/GopeedLab/gopeed/pkg/base"
	"github.com/GopeedLab/gopeed/pkg/download"
	"github.com/junkerderprovinz/knightloader/internal/core"
)

// seedRun is one torrent started only to seed: from is what it had uploaded
// before, mark where its targets count from, and seeding is whether the
// library has found its files complete. stamps is when each file was last
// written as the run found it, for a torrent whose files are all chosen, and
// stopped is set once the run has ended because the library began to fetch.
type seedRun struct {
	from    core.TorrentStats
	mark    core.SeedMark
	seeding bool
	stamps  map[string]time.Time
	stopped bool
}

// notInPlace is a seed run whose files are not as the torrent has them.
type notInPlace struct{ why string }

func (e *notInPlace) Error() string { return e.why }

// seedFiles checks that every chosen file of a resolved torrent is in dir at
// its full size, and returns when each was last written. sel is the chosen
// files by index, nil for all of them. A file of no bytes is left out: the
// library creates it as it resolves.
func seedFiles(dir string, res *base.Resource, sel []int) (map[string]time.Time, error) {
	stamps := map[string]time.Time{}
	for i, f := range res.Files {
		if f == nil || f.Size == 0 || sel != nil && !slices.Contains(sel, i) {
			continue
		}
		p := filepath.Join(dir, filepath.FromSlash(res.Name), filepath.FromSlash(f.Path), f.Name)
		fi, err := os.Stat(p)
		switch {
		case errors.Is(err, fs.ErrNotExist):
			return nil, &notInPlace{why: p + " is missing"}
		case err != nil:
			return nil, &notInPlace{why: err.Error()}
		case fi.Size() != f.Size:
			return nil, &notInPlace{why: fmt.Sprintf("%s has %d bytes, the torrent says %d", p, fi.Size(), f.Size)}
		}
		stamps[p] = fi.ModTime()
	}
	return stamps, nil
}

// written reports a file of the run that changed since the run found it,
// which only the library fetching into it does while it checks the files.
func (sd seedRun) written() error {
	for p, at := range sd.stamps {
		fi, err := os.Stat(p)
		if err != nil {
			return &notInPlace{why: err.Error()}
		}
		if !fi.ModTime().Equal(at) {
			return &notInPlace{why: p + " does not match the torrent"}
		}
	}
	return nil
}

// watchSeed notes the files of a seed run whose files are all chosen, so the
// run ends if the library writes into one of them.
func (e *Engine) watchSeed(taskID string, stamps map[string]time.Time) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if sd := e.seeds[taskID]; sd != nil {
		sd.stamps = stamps
	}
}

// stopSeed ends a seed run whose files the library found wanting, before it
// fetches more than it has, and leaves the files as they are.
func (e *Engine) stopSeed(taskID, gid string, err error) {
	e.mu.Lock()
	sd := e.seeds[taskID]
	if sd == nil || sd.stopped {
		e.mu.Unlock()
		return
	}
	sd.stopped = true
	from := sd.from
	e.mu.Unlock()
	e.forgetTorrent(taskID)
	e.dropTorrent(gid)
	log.Printf("task %s is not seeded: %v", taskID, err)
	e.emit(taskID, core.Update{Torrent: &core.TorrentStats{
		Uploaded: from.Uploaded, Ratio: from.Ratio, SeedSeconds: from.SeedSeconds, NotSeeded: err.Error(),
	}})
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
	sd, ok := e.seedOf(taskID)
	if !ok || sd.stopped {
		return
	}
	switch ev.Key {
	case download.EventKeyDone:
		if err := sd.written(); err != nil {
			// The listener must not wait on the library it listens to.
			e.wg.Add(1)
			go func() {
				defer e.wg.Done()
				e.stopSeed(taskID, ev.Task.ID, err)
			}()
			return
		}
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

// seedDone reports whether a seed run's totals have reached a target since its
// mark, and if so takes the torrent out of the library, which closes its
// upload, and out of the poll. The files stay, and so does the rest of what
// the engine knows of the task, for a later removal with its files.
func (e *Engine) seedDone(taskID, gid string, s core.TorrentStats) bool {
	e.mu.Lock()
	var mark core.SeedMark
	if sd := e.seeds[taskID]; sd != nil {
		mark = sd.mark
	}
	reached := e.targets.reached(mark.Since(s))
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
	st := &core.TorrentStats{Uploaded: sd.from.Uploaded, Ratio: sd.from.Ratio, SeedSeconds: sd.from.SeedSeconds}
	var nip *notInPlace
	if errors.As(err, &nip) {
		st.NotSeeded = nip.why
	}
	e.emit(taskID, core.Update{Torrent: st})
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
