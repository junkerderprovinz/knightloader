package app

// Free space on the destination volume is checked before bytes move, not only
// after a write fails with core.ReasonDiskFull.
//
// There are two marks because the two actions cost different amounts. Below
// settings.DiskLowSpace nothing new starts. Below settings.DiskCriticalSpace
// running transfers are stopped and requeued, which throws away the progress
// of a transfer that cannot resume.
//
// Every check fails open: when internal/diskspace cannot tell, the guard has
// no opinion. Blocking on ignorance would stop a healthy machine with no
// visible reason.

import (
	"log"
	"sort"
	"sync"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/diskspace"
	"github.com/junkerderprovinz/knightloader/internal/settings"
)

// freeSpace is a variable so tests can simulate a full disk. Only tests write
// it, before the App under test runs.
var freeSpace = diskspace.Free

// diskCheckInterval is how often the watcher checks the volumes running
// transfers write into. Much longer and a fast line could fill the last
// gigabyte between ticks; much shorter gains nothing.
const diskCheckInterval = 15 * time.Second

// diskState is one App's disk-guard bookkeeping, kept in a package-level map
// keyed by *App.
type diskState struct {
	startOnce sync.Once

	mu sync.Mutex
	// critical holds the folders currently under the pause mark, so the log
	// line is written on the crossing rather than on every pass. Stopped tasks
	// are not tracked here: they sit in the wait queue, and the dispatcher's
	// own check keeps them there.
	critical map[string]bool
}

var (
	diskMu  sync.Mutex
	diskReg = map[*App]*diskState{}
)

// diskStateFor returns this App's disk bookkeeping, building it on first use.
func (a *App) diskStateFor() *diskState {
	diskMu.Lock()
	defer diskMu.Unlock()
	st, ok := diskReg[a]
	if !ok {
		st = &diskState{critical: map[string]bool{}}
		diskReg[a] = st
	}
	return st
}

// ensureDiskWatcher starts the watch loop once per App. It is called from
// dispatchLocked, like ensureStallWatcher, and is cheap after the first call.
func (a *App) ensureDiskWatcher() {
	st := a.diskStateFor()
	st.startOnce.Do(func() { a.spawn(a.diskWatchLoop) })
}

// diskWatchLoop runs until a.ctx is done.
func (a *App) diskWatchLoop() {
	tick := time.NewTicker(diskCheckInterval)
	defer tick.Stop()
	for {
		select {
		case <-a.ctx.Done():
			return
		case <-tick.C:
			a.diskPass()
		}
	}
}

// diskPass stops transfers writing into a volume that is under the pause mark.
// It is a watcher rather than part of dispatchLocked because a running download
// produces no event that reaches the dispatcher until it ends.
func (a *App) diskPass() {
	cfg := a.Settings.Get()
	if cfg.DiskCriticalSpace <= 0 {
		return
	}
	// Grouped by folder so downloads sharing one cost a single stat.
	byDir := map[string][]string{}
	a.mu.Lock()
	for id := range a.active {
		t := a.tasks[id]
		if t == nil {
			continue
		}
		dir := a.dirFor(t)
		byDir[dir] = append(byDir[dir], id)
	}
	a.mu.Unlock()

	st := a.diskStateFor()
	var stop []string
	for dir, ids := range byDir {
		free, ok := freeSpace(dir)
		if !ok {
			continue
		}
		if free >= uint64(cfg.DiskCriticalSpace) {
			st.clearCritical(dir)
			continue
		}
		if st.markCritical(dir) {
			log.Printf("only %d bytes left on %s; stopping %d transfer(s) writing into it", free, dir, len(ids))
		}
		stop = append(stop, ids...)
	}
	// Sorted so repeated passes act in the same order and the logs compare.
	sort.Strings(stop)
	for _, id := range stop {
		// StopBack rather than Pause: the task returns to the wait queue and
		// starts again by itself once space is freed.
		a.StopBack(id)
	}
}

// markCritical records that dir is under the pause mark and reports whether
// this is the crossing rather than a repeat.
func (st *diskState) markCritical(dir string) bool {
	st.mu.Lock()
	defer st.mu.Unlock()
	if st.critical[dir] {
		return false
	}
	st.critical[dir] = true
	return true
}

// clearCritical removes the mark from a recovered volume, so the next crossing
// is logged again.
func (st *diskState) clearCritical(dir string) {
	st.mu.Lock()
	defer st.mu.Unlock()
	delete(st.critical, dir)
}

// spaceCheck is one dispatch pass's view of the destination volumes. It is
// rebuilt every pass because free space changes, and it caches per folder
// because many tasks share one. The stat runs under a.mu, like the collision
// check's os.Stat, so an unresponsive network mount can block the dispatcher.
type spaceCheck struct {
	cfg settings.Settings
	// free is what each folder's volume reported; known says whether it
	// reported at all, which a zero byte count cannot express.
	free  map[string]uint64
	known map[string]bool
	// promised is what this pass has already committed to each folder. The
	// engine pre-allocates the full file, so transfers from earlier passes are
	// already out of the reported number.
	promised map[string]int64
}

// newSpaceCheck builds the per-pass view.
func newSpaceCheck(cfg settings.Settings) *spaceCheck {
	return &spaceCheck{
		cfg:      cfg,
		free:     map[string]uint64{},
		known:    map[string]bool{},
		promised: map[string]int64{},
	}
}

// off reports that neither the reserve nor the floor is configured.
func (s *spaceCheck) off() bool {
	return s.cfg.DiskReserve <= 0 && s.cfg.DiskLowSpace <= 0
}

// freeAt is the volume's answer for dir, read at most once per pass.
func (s *spaceCheck) freeAt(dir string) (uint64, bool) {
	if known, seen := s.known[dir]; seen {
		return s.free[dir], known
	}
	got, ok := freeSpace(dir)
	s.free[dir] = got
	s.known[dir] = ok
	return got, ok
}

// admit returns why t may not start yet, or core.WaitingNone. A task without a
// known size skips the per-file arithmetic but still respects the floor; the
// backend reports a real size soon after starting, and the pause mark catches
// it if it does not fit.
func (s *spaceCheck) admit(dir string, t *core.Task) core.Waiting {
	if s.off() {
		return core.WaitingNone
	}
	need := remainingBytes(t)
	// With only a reserve configured and no size to measure, no stat could
	// change the answer.
	if s.cfg.DiskLowSpace <= 0 && need <= 0 {
		return core.WaitingNone
	}
	free, ok := s.freeAt(dir)
	if !ok {
		return core.WaitingNone
	}
	// Saturate at zero; free is unsigned.
	if p := uint64(s.promised[dir]); p >= free {
		free = 0
	} else {
		free -= p
	}
	if s.cfg.DiskLowSpace > 0 && free < uint64(s.cfg.DiskLowSpace) {
		return core.WaitingDisk
	}
	if need <= 0 {
		return core.WaitingNone
	}
	if s.cfg.DiskReserve <= 0 {
		return core.WaitingNone
	}
	if free < uint64(need)+uint64(s.cfg.DiskReserve) {
		return core.WaitingDisk
	}
	return core.WaitingNone
}

// commit books a task the pass just started against its folder, so the next
// task in the same pass sees what is left.
func (s *spaceCheck) commit(dir string, t *core.Task) {
	if s.off() {
		return
	}
	if need := remainingBytes(t); need > 0 {
		s.promised[dir] += need
	}
}

// remainingBytes is what t still has to fetch, or 0 when its size is unknown.
// Loaded bytes already occupy the volume, and the clamp covers a backend that
// reports more than the announced size.
func remainingBytes(t *core.Task) int64 {
	if t == nil || t.Size <= 0 {
		return 0
	}
	if t.Loaded >= t.Size {
		return 0
	}
	return t.Size - t.Loaded
}
