package app

// Seeding across a restart. The download library keeps its transfers in
// memory only, so a finished torrent stops seeding with the process. Each
// start takes every torrent that still owes seeding up again where its files
// are, and it seeds on from the figures saved with the task, so its targets
// count the whole of its seeding, or all of it since somebody started it by
// hand.

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/engine"
	"github.com/junkerderprovinz/knightloader/internal/resolver/torrent"
	"github.com/junkerderprovinz/knightloader/internal/settings"
)

// seedSaveEvery is how often a seeding torrent's figures are written, and so
// how much of them a crash can lose.
const seedSaveEvery = time.Minute

// SeedPending reports whether a finished torrent of the built-in client is not
// seeding but owes more: it stopped at a shutdown or a crash, not at its
// targets. A disabled one, and one held back by the torrent module being off,
// owe it as well and seed once they may.
func SeedPending(t *core.Task) bool {
	return seedsHere(t) && !t.Seeding && !t.SeedingOver
}

// resumeSeeding takes up every torrent that owes seeding and may seed: it is
// enabled, the torrent module is on, and no transfer has it yet. It runs at
// start, and again when a link is enabled or the settings are saved, which is
// where the module comes back on.
//
// A torrent that has met the targets as they are now, or whose files are not
// all there, is done with seeding instead. A start only to seed would fetch
// whatever is missing again.
func (a *App) resumeSeeding() {
	if a.resolverOff((torrent.Resolver{}).Info().ID) {
		return
	}
	a.mu.Lock()
	var owed []core.Task
	for _, t := range a.tasks {
		if SeedPending(t) && t.Enabled && !a.started[t.ID] {
			owed = append(owed, *t)
		}
	}
	a.mu.Unlock()
	if len(owed) == 0 {
		return
	}
	// The disk is asked outside the lock; a torrent can have thousands of
	// files.
	tc := a.Settings.Get().Torrent
	ended := map[string]string{}
	for i := range owed {
		if why := seedingEnd(&owed[i], tc); why != "" {
			ended[owed[i].ID] = why
		}
	}

	now := time.UnixMilli(time.Now().UnixMilli())
	var jobs []engine.Job
	var copies []taskCopy
	a.mu.Lock()
	for _, o := range owed {
		t := a.tasks[o.ID]
		if t == nil || !SeedPending(t) || !t.Enabled || a.started[t.ID] || a.relocating[t.ID] {
			continue
		}
		if why, ok := ended[t.ID]; ok {
			log.Printf("task %s seeds no more: %s", t.ID, why)
			t.SeedingOver = true
			if t.SeedingEnded.IsZero() {
				t.SeedingEnded = now
			}
			copies = append(copies, a.copyLocked(t))
			continue
		}
		job, err := a.seedJobLocked(t)
		if err != nil {
			log.Printf("task %s could not go on seeding: %v", t.ID, err)
			continue
		}
		a.started[t.ID] = true
		jobs = append(jobs, job)
	}
	a.mu.Unlock()
	a.publishTasks(copies)
	for _, j := range jobs {
		a.Engine.Start(j)
	}
}

// seedingEnd says why a torrent that owes seeding is done with it, or "" when
// it can seed on.
func seedingEnd(t *core.Task, tc settings.Torrent) string {
	since := t.SeedMark.Since(core.TorrentStats{Ratio: t.Ratio, SeedSeconds: t.SeedSeconds})
	switch {
	case tc.SeedRatioTarget > 0 && since.Ratio >= tc.SeedRatioTarget,
		tc.SeedDurationSeconds > 0 && since.SeedSeconds >= int64(tc.SeedDurationSeconds):
		return "it has reached its seeding target"
	case t.File == "":
		return "nothing records where its files are"
	}
	if err := checkSeedFiles(t); err != nil {
		return err.Error()
	}
	return ""
}

// checkSeedFiles reports the first selected file of a finished torrent that is
// not on disk at its full size, where t.File says the torrent landed.
func checkSeedFiles(t *core.Task) error {
	fi, err := os.Stat(t.File)
	if err != nil {
		return err
	}
	if !fi.IsDir() {
		if len(t.TorrentFiles) == 1 && fi.Size() != t.TorrentFiles[0].Size {
			return fmt.Errorf("%s is not whole", t.File)
		}
		return nil
	}
	var paths []string
	for _, f := range t.TorrentFiles {
		if f.Selected {
			paths = append(paths, f.Path)
		}
	}
	if err := torrent.Contained(t.File, paths); err != nil {
		return err
	}
	for _, f := range t.TorrentFiles {
		if !f.Selected {
			continue
		}
		p := filepath.Join(t.File, filepath.FromSlash(f.Path))
		fi, err := os.Stat(p)
		if err != nil {
			return err
		}
		if fi.Size() != f.Size {
			return fmt.Errorf("%s is not whole", p)
		}
	}
	return nil
}

// seedFiguresDueLocked reports whether a seeding torrent's figures are due to
// be written, and counts them as written. Caller holds a.mu.
func (a *App) seedFiguresDueLocked(id string) bool {
	now := time.Now()
	if now.Sub(a.seedSaved[id]) < seedSaveEvery {
		return false
	}
	if a.seedSaved == nil {
		a.seedSaved = map[string]time.Time{}
	}
	a.seedSaved[id] = now
	return true
}

// seedsHere reports whether a task is a finished torrent of the built-in
// client, the only kind that seeds.
func seedsHere(t *core.Task) bool {
	return t.Resolver == (torrent.Resolver{}).Info().ID && t.Status == core.StatusDone
}

// StopSeeding ends the seeding of the torrents among ids that seed or owe it,
// and marks it over, so a restart does not take them up again. It returns the
// ones it stopped.
func (a *App) StopSeeding(ids []string) []string {
	now := time.UnixMilli(time.Now().UnixMilli())
	var touched, drop []string
	var copies []taskCopy
	a.mu.Lock()
	for _, id := range ids {
		t := a.tasks[id]
		if t == nil || !seedsHere(t) || t.SeedingOver {
			continue
		}
		// No longer started, so onUpdate drops a reading the poll took before
		// the engine let go.
		if a.started[id] {
			drop = append(drop, id)
			delete(a.started, id)
		}
		if t.Seeding {
			t.SeedingEnded = now
		}
		t.Seeding, t.SeedingOver, t.Peers, t.Seeds = false, true, 0, 0
		copies = append(copies, a.copyLocked(t))
		touched = append(touched, id)
	}
	a.mu.Unlock()
	for _, id := range drop {
		a.Engine.Remove(id, false)
	}
	a.publishTasks(copies)
	return touched
}

// StartSeeding takes up the torrents among ids whose seeding is over and
// returns them. Each seeds to the targets once more, counted from its figures
// now, as the download library counts them on a new start. One that may not
// seed yet, disabled or with the torrent module off, seeds once it may.
func (a *App) StartSeeding(ids []string) []string {
	a.mu.Lock()
	var over, held []string
	for _, id := range ids {
		t := a.tasks[id]
		if t == nil || !seedsHere(t) || !t.SeedingOver {
			continue
		}
		over = append(over, id)
		if a.started[id] {
			held = append(held, id)
		}
	}
	a.mu.Unlock()
	// The engine still knows a torrent that stopped at its targets, and the new
	// start must not find that one in its way.
	for _, id := range held {
		a.Engine.Remove(id, false)
	}
	var touched []string
	var copies []taskCopy
	a.mu.Lock()
	for _, id := range over {
		t := a.tasks[id]
		if t == nil || !seedsHere(t) || !t.SeedingOver {
			continue
		}
		delete(a.started, id)
		t.SeedingOver = false
		t.SeedMark = core.SeedMark{Ratio: t.Ratio, SeedSeconds: t.SeedSeconds}
		copies = append(copies, a.copyLocked(t))
		touched = append(touched, id)
	}
	a.mu.Unlock()
	a.publishTasks(copies)
	a.resumeSeeding()
	return touched
}
