package app

// Room on the destination volume, asked BEFORE the bytes move.
//
// Until this file existed, the only thing in the whole tree that knew about
// disk space was core.ReasonDiskFull, and that is a label put on a write that
// has ALREADY failed. By then the transfer has spent the line time, left a
// part file on the volume that had no room for it, and - because nothing else
// knows either - every other download aimed at the same volume is still
// running and still writing. The one failure the user could actually have
// prevented is the one the app finds out about last.
//
// TWO MARKS, BECAUSE THE TWO ACTS ARE DIFFERENT SIZES. Below
// settings.DiskLowSpace nothing NEW starts, which costs a queued download a
// while longer in the queue. Below settings.DiskCriticalSpace what is already
// running is stopped and put back in that queue, which for a transfer that
// cannot resume throws away everything it had fetched. Those do not belong
// behind one number, and a single threshold would have to be set for whichever
// of the two consequences you feared more.
//
// AND IT FAILS OPEN, EVERYWHERE. internal/diskspace answers "I do not know" on
// any platform it has no call for, and every check here treats that as no
// opinion rather than as zero bytes. A guard that blocks when it is ignorant
// stops a healthy machine, which is a worse and much more confusing failure
// than the full disk it was built for - and the person it happens to has no
// way at all to work out why nothing starts.

import (
	"log"
	"sort"
	"sync"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/diskspace"
	"github.com/junkerderprovinz/knightloader/internal/settings"
)

// freeSpace is diskspace.Free behind a package variable so a test can drive a
// full disk without owning one.
//
// The same reasoning App.Probe carries on the struct: a reading nothing can
// replace is a reading no test can control, and the alternative here would be
// a test that only says anything on a machine that happens to be nearly full.
// It is written by tests only, before the App under test is running.
var freeSpace = diskspace.Free

// diskCheckInterval is how often the watcher looks at the volumes the running
// transfers are writing into.
//
// Fifteen seconds is a compromise between the two things that go wrong at the
// ends. Much longer and a fast line fills the last gigabyte between two ticks,
// which is exactly the case the watcher exists for. Much shorter and a stat of
// each distinct destination folder becomes a poll for no gain, since the mark
// it is looking for is minutes of downloading away at any sane line speed.
const diskCheckInterval = 15 * time.Second

// diskState is one App's disk-guard bookkeeping.
//
// It lives at package level keyed by the owning *App rather than as a field on
// App, the same trade app_stallwatch.go, app_captcha.go and app_accounts.go
// already document: app.go's struct is not this file's to grow.
type diskState struct {
	startOnce sync.Once

	mu sync.Mutex
	// critical is the destination folders currently under the pause mark, and
	// it exists ONLY so the log line is written when a volume crosses that mark
	// rather than once every fifteen seconds for as long as it stays there.
	//
	// Deliberately not a record of "what the guard stopped". Nothing needs one:
	// a stopped task goes back into the wait queue and the dispatcher's own
	// check keeps it there while the volume is low, so the queue itself is the
	// state and there is no second copy of it to fall out of step - the same
	// argument StopBack makes for the hard stop.
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

// ensureDiskWatcher starts the watch loop exactly once per App. Called from
// dispatchLocked for the reason ensureStallWatcher is: this package's files own
// no start-up hook, and dispatchLocked is the closest thing to "runs once at
// start-up and on nearly everything after". Idempotent and cheap after the
// first call.
func (a *App) ensureDiskWatcher() {
	st := a.diskStateFor()
	st.startOnce.Do(func() { a.spawn(a.diskWatchLoop) })
}

// diskWatchLoop runs until a.ctx is done, which is what makes Close wait for it
// - see a.spawn's own contract.
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

// diskPass is one look at the volumes the running transfers are filling, and
// it stops the ones writing into a volume that has gone under the pause mark.
//
// A WATCHER AND NOT A BRANCH IN dispatchLocked, because the case this is for
// happens while nothing else does. dispatchLocked runs on nearly every event in
// the app, but a download that is simply running produces no event that reaches
// it - onUpdate only dispatches on a terminal status - so a queue of four
// transfers filling the last of a disk over twenty minutes would reach the
// dispatcher only once they had already failed.
func (a *App) diskPass() {
	cfg := a.Settings.Get()
	if cfg.DiskCriticalSpace <= 0 {
		return // the blanket pause is off, which is the default
	}
	// Grouped by destination folder rather than checked per task: several
	// downloads normally share one, and this is the difference between one
	// stat per pass and one per transfer.
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
			// No answer is no opinion. See this file's own header.
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
	// Sorted only so two identical passes act in the same order; map iteration
	// would make the log of one incident unreadable against the log of the next.
	sort.Strings(stop)
	for _, id := range stop {
		// StopBack and not Pause: the transfer stops and the task goes BACK into
		// the wait queue. The dispatcher's own floor then keeps it there while
		// the volume is low, and lets it go again on its own once somebody frees
		// space - with no flag to clear and nobody to remember to clear it. A
		// Pause would take it out of the queue instead and leave a queue that
		// silently never restarts, which is the exact defect StopBack was
		// written for.
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

// clearCritical takes the mark off a volume that has recovered, so the next
// crossing is logged again. A volume that fills, is emptied and fills once more
// is two incidents and deserves two lines.
func (st *diskState) clearCritical(dir string) {
	st.mu.Lock()
	defer st.mu.Unlock()
	delete(st.critical, dir)
}

// spaceCheck is ONE dispatch pass's view of the destination volumes.
//
// It is built per pass and thrown away, and both halves of that are
// deliberate. Per pass, because free space genuinely changes while transfers
// run and a reading cached across passes would hand out slots against a disk
// that emptied ten minutes ago. Thrown away, because a queue of two hundred
// tasks normally shares one or two destination folders, so the map turns what
// would be two hundred syscalls per pass into two.
//
// The stat happens under a.mu, like the os.Stat collide.Check already does a
// few lines further down the same loop. It is a local syscall on the order of
// microseconds; the honest caveat is that a destination on an unresponsive
// network mount can block it, and then it blocks the app - which is exactly
// what the collision check on that same mount would already do today.
type spaceCheck struct {
	cfg settings.Settings
	// free is what each folder's volume reported, and known says whether it
	// reported at all. Two maps rather than one of a pair, because the zero
	// value of the second is the answer that matters and it must not be
	// mistakable for a zero byte count.
	free  map[string]uint64
	known map[string]bool
	// promised is what THIS pass has already committed to each folder: the
	// remaining bytes of every task it has just started.
	//
	// It exists because the reading is taken once and four downloads started
	// against it would each be told, truthfully and uselessly, that the whole
	// twenty gigabytes were free. A transfer already running in an EARLIER pass
	// needs no such entry: this build's engine creates the destination file at
	// its full final length before the first byte arrives (see
	// settings.ReclaimTrust's own comment, which had to work around the same
	// fact), so its room is already out of the number the volume reports. The
	// case that leaves is a delegated backend that does not pre-allocate, and
	// that one is what the blanket floor is the backstop for.
	promised map[string]int64
}

// newSpaceCheck builds the per-pass view. off reports that nothing here has
// anything to say, so the caller can skip the whole thing on the overwhelming
// majority of installs.
func newSpaceCheck(cfg settings.Settings) *spaceCheck {
	return &spaceCheck{
		cfg:      cfg,
		free:     map[string]uint64{},
		known:    map[string]bool{},
		promised: map[string]int64{},
	}
}

// off reports that neither the reserve nor the floor is configured, so no
// syscall is worth making.
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

// admit answers why this task may not start yet, or core.WaitingNone.
//
// THE UNKNOWN SIZE IS THE INTERESTING CASE, and it is most of them: a task that
// was never checked, or whose host would not say, carries Size == 0. Blocking
// it would be wrong, because the overwhelming likelihood is that it fits and
// the app would be refusing downloads on a healthy machine over a number it
// simply does not have. Ignoring it would be wrong too, because a file of
// unknown length is exactly as capable of filling a disk as a known one.
//
// So it is neither: a task with no size is exempt from the per-file arithmetic
// and STILL subject to the floor. On a comfortable volume it starts, as it
// always did. On one already under the mark it waits, like everything else.
// And the exemption is temporary by construction - the backend reports a real
// size within seconds of starting (core.Update.Size), after which it is an
// ordinary sized task, and the pause mark is what catches it if the size turns
// out to be the one that will not fit.
func (s *spaceCheck) admit(dir string, t *core.Task) core.Waiting {
	if s.off() {
		return core.WaitingNone
	}
	need := remainingBytes(t)
	// The default install's own case, answered before any syscall is made: the
	// reserve alone is configured and this task has no size for it to be
	// measured against, so there is nothing the volume could say that would
	// change the answer. Without this line a queue of two hundred unchecked
	// links costs a stat per destination folder on every dispatch pass to learn
	// nothing.
	if s.cfg.DiskLowSpace <= 0 && need <= 0 {
		return core.WaitingNone
	}
	free, ok := s.freeAt(dir)
	if !ok {
		return core.WaitingNone
	}
	// What this pass has already handed out comes off first, and it saturates
	// at zero rather than wrapping: free is unsigned, and a pass that has
	// promised more than the volume holds is a pass with nothing left to give,
	// not one with eighteen exabytes.
	if p := uint64(s.promised[dir]); p >= free {
		free = 0
	} else {
		free -= p
	}
	if s.cfg.DiskLowSpace > 0 && free < uint64(s.cfg.DiskLowSpace) {
		return core.WaitingDisk
	}
	if need <= 0 {
		return core.WaitingNone // no size to measure - see this function's own comment
	}
	if s.cfg.DiskReserve <= 0 {
		// The floor above was the whole check. Without a reserve there is no
		// per-file arithmetic to do, and comparing against a bare `free` would
		// be a guard that lets a download fill the volume to the last byte.
		return core.WaitingNone
	}
	if free < uint64(need)+uint64(s.cfg.DiskReserve) {
		return core.WaitingDisk
	}
	return core.WaitingNone
}

// commit books a task the pass has just started against its destination, so
// the next task in the same pass is measured against what is left rather than
// against what was there before either of them started.
func (s *spaceCheck) commit(dir string, t *core.Task) {
	if s.off() {
		return
	}
	if need := remainingBytes(t); need > 0 {
		s.promised[dir] += need
	}
}

// remainingBytes is what this task still has to fetch, or 0 for one whose
// length nobody knows.
//
// Loaded is subtracted because a resumed transfer's existing bytes are already
// occupying the volume the check just measured: counting the full size again
// would refuse a download that is one per cent from finishing on the grounds
// that it does not fit from scratch. The clamp at zero covers a backend that
// has reported more loaded than announced, which happens with a size that was
// a guess.
func remainingBytes(t *core.Task) int64 {
	if t == nil || t.Size <= 0 {
		return 0
	}
	if t.Loaded >= t.Size {
		return 0
	}
	return t.Size - t.Loaded
}
