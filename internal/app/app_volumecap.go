package app

// The volume cap: how much has finished downloading this period, and what
// happens once that reaches the allowance. It follows the disk guard's plan,
// but an allowance is a contract nothing on this box can see, so the figure is
// added up from what this instance has fetched.
//
// The counter reads the download history and nothing else (announced sizes,
// one row per task, see internal/store/volume.go). Emptying or trimming the
// history therefore empties the counter; a private total beside the table
// would be a number nobody could check. On instances upgraded through
// migrations 8 and 9, one day holds the whole back catalogue, and nothing here
// can tell those rows from real ones.
//
// Enforcement lives in two reads elsewhere, not in a flag:
//
//	pause     a gate in dispatchLocked that sets core.WaitingVolume on the queue
//	throttle  one fold into applyBudget's read of the limit in force
//
// a.halted is recomputed by every schedule pass and a.limitInForce already has
// two writers, so a flag in either would be undone at the next save or window
// boundary.
//
// The state is package-level and keyed by *App, like the disk guard's. Names
// say "volume cap" because "volume" already means a mounted disk in this
// package and "budget" is the speed limit's share-out.

import (
	"log"
	"sync"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/settings"
	"github.com/junkerderprovinz/knightloader/internal/store"
)

// volumeCapInterval is how often the counter is added up again from the
// history. A finish is counted at once (volumeCapRecordLocked); the tick
// corrects for what the settle path cannot see, such as a trimmed history or a
// period boundary passing while idle.
const volumeCapInterval = 30 * time.Second

// VolumeUsage is the counter as the route, the websocket push and the status
// bar chip read it.
type VolumeUsage struct {
	// Used is the announced size of everything finished since PeriodStart. It
	// moves when a download finishes, not while one runs.
	Used int64 `json:"used"`
	// Cap is the allowance in bytes; 0 means none, and Reached is then false.
	Cap int64 `json:"cap"`
	// Action and Throttle are echoed so the chip can say why the queue waits
	// without a second request.
	Action   string `json:"action"`
	Throttle int64  `json:"throttle"`
	// PeriodStart and PeriodEnd are the half-open window [start, end) Used was
	// summed over, in the server's zone, where the cap is enforced.
	PeriodStart time.Time `json:"periodStart"`
	PeriodEnd   time.Time `json:"periodEnd"`
	Reached     bool      `json:"reached"`
}

// same reports whether two readings say the same thing, so broadcasts happen on
// a change. Times are compared with Equal, since == also compares the
// monotonic reading and location.
func (u VolumeUsage) same(o VolumeUsage) bool {
	return u.Used == o.Used && u.Cap == o.Cap && u.Action == o.Action &&
		u.Throttle == o.Throttle && u.Reached == o.Reached &&
		u.PeriodStart.Equal(o.PeriodStart) && u.PeriodEnd.Equal(o.PeriodEnd)
}

// capDaysInMonth is the length of a month, leap years included: day 0 of the
// next month normalises to the last day of this one.
func capDaysInMonth(year int, month time.Month, loc *time.Location) int {
	return time.Date(year, month+1, 0, 0, 0, 0, 0, loc).Day()
}

// capMonthDay is local midnight on day of the given month, clamped to the
// month's length. Without the clamp, 31 February normalises to 3 March.
func capMonthDay(year int, month time.Month, day int, loc *time.Location) time.Time {
	if n := capDaysInMonth(year, month, loc); day > n {
		day = n
	}
	return time.Date(year, month, day, 0, 0, 0, 0, loc)
}

// capPeriodStart is the most recent reset boundary at or before now: local
// midnight in the server's zone, since the server holds the queue back.
func capPeriodStart(now time.Time, day int) time.Time {
	day = capResetDay(day)
	loc := now.Location()
	start := capMonthDay(now.Year(), now.Month(), day, loc)
	if now.Before(start) {
		// This month's boundary is still ahead, so the running period opened
		// last month. AddDate rolls the year over in December.
		prev := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, loc).AddDate(0, -1, 0)
		start = capMonthDay(prev.Year(), prev.Month(), day, loc)
	}
	return start
}

// capPeriodEnd is the next boundary after the period that began at start.
// Working from start keeps a clamped period on the requested day: a cap reset
// on the 31st runs 28 February to 31 March. A finish exactly on the boundary
// belongs to the new period.
func capPeriodEnd(start time.Time, day int) time.Time {
	day = capResetDay(day)
	loc := start.Location()
	next := time.Date(start.Year(), start.Month(), 1, 0, 0, 0, 0, loc).AddDate(0, 1, 0)
	return capMonthDay(next.Year(), next.Month(), day, loc)
}

// capResetDay applies the settings store's clamp again, since this file also
// sees settings that were never saved through the store.
func capResetDay(day int) int {
	if day < 1 {
		return settings.DefaultVolumeCapResetDay
	}
	if day > 31 {
		return 31
	}
	return day
}

// volumeCapFinish is one download the settle path has counted but no query has
// seen yet. See volumeCapState.pending.
type volumeCapFinish struct {
	bytes int64
	at    time.Time
}

// volumeCapState is one App's volume-cap bookkeeping.
type volumeCapState struct {
	startOnce sync.Once

	mu sync.Mutex
	// usage is the last answer, and queried says whether there is one. Until
	// then readers treat it as no opinion rather than zero, and fail open like
	// the disk guard.
	usage   VolumeUsage
	queried bool
	// pending is what has settled since the last query started, by task id.
	// A finish is counted under a.mu, the dispatcher hands out the freed slot in
	// the same critical section, and the history row is written afterwards; this
	// keeps the gate right for that slot. Keying by id counts a re-reported
	// finish once, as the history does.
	pending map[string]volumeCapFinish
}

var (
	volumeCapMu  sync.Mutex
	volumeCapReg = map[*App]*volumeCapState{}
)

// volumeCapStateFor returns this App's volume-cap bookkeeping, building it on
// first use.
func (a *App) volumeCapStateFor() *volumeCapState {
	volumeCapMu.Lock()
	defer volumeCapMu.Unlock()
	st, ok := volumeCapReg[a]
	if !ok {
		st = &volumeCapState{pending: map[string]volumeCapFinish{}}
		volumeCapReg[a] = st
	}
	return st
}

// ensureVolumeCapWatcher starts the watch loop once per App. It is called from
// dispatchLocked, like ensureDiskWatcher.
func (a *App) ensureVolumeCapWatcher() {
	st := a.volumeCapStateFor()
	st.startOnce.Do(func() { a.spawn(a.volumeCapWatchLoop) })
}

// volumeCapWatchLoop runs until a.ctx is done, so Close waits for it. It passes
// once before the first tick, since until then the gate has no opinion and an
// instance booting over its allowance would keep downloading.
func (a *App) volumeCapWatchLoop() {
	a.volumeCapPass()
	tick := time.NewTicker(volumeCapInterval)
	defer tick.Stop()
	for {
		select {
		case <-a.ctx.Done():
			return
		case <-tick.C:
			a.volumeCapPass()
		}
	}
}

// volumeCapPass adds the period up again from the history and publishes the
// answer if it changed.
//
// It never runs under a.mu: a query on a slow volume would block every task.
// The dispatcher reads the cached number instead.
func (a *App) volumeCapPass() VolumeUsage {
	cfg := a.Settings.Get()
	now := time.Now()
	start := capPeriodStart(now, cfg.VolumeCapResetDay)
	end := capPeriodEnd(start, cfg.VolumeCapResetDay)

	// Taken before the query; it decides which pending finishes the query has
	// already counted (see pruneLocked).
	queryAt := time.Now()
	// The whole period rather than up to now, so a row stamped slightly ahead by
	// a since-corrected clock still counts, as it does on the chart.
	sum, _, _, err := a.Store.VolumeSince(start, end)

	st := a.volumeCapStateFor()
	st.mu.Lock()
	defer st.mu.Unlock()
	if err != nil {
		// Keep the previous answer: falling to zero on a failed query would
		// release a queue that is over its allowance.
		log.Printf("could not add up the download volume for this period: %v", err)
		return st.usage
	}
	st.pruneLocked(queryAt, start)
	used := sum + st.pendingLocked()
	next := VolumeUsage{
		Used:        used,
		Cap:         cfg.VolumeCap,
		Action:      cfg.VolumeCapAction,
		Throttle:    cfg.VolumeCapThrottle,
		PeriodStart: start,
		PeriodEnd:   end,
		Reached:     cfg.VolumeCap > 0 && used >= cfg.VolumeCap,
	}
	crossed := next.Reached && (!st.queried || !st.usage.Reached)
	changed := !st.queried || !next.same(st.usage)
	st.usage, st.queried = next, true
	if crossed {
		// Logged on the crossing, not every tick.
		log.Printf("the volume cap is reached: %d of %d bytes have finished since %s (action: %s)",
			used, cfg.VolumeCap, start.Format(time.RFC3339), cfg.VolumeCapAction)
	}
	if changed {
		// Broadcast never waits on a client, so holding st.mu is safe.
		a.Hub.Broadcast("volume", next)
	}
	return next
}

// pruneLocked drops the pending finishes the query has already counted, and
// anything from a period that has ended.
//
// A finish booked before the query started has its row written before the
// query's snapshot in every case except a query starting between the booking
// under a.mu and the row write. That undercounts one file until the next tick,
// which beats double counting every finish for a grace period.
//
// Caller holds st.mu.
func (st *volumeCapState) pruneLocked(queryAt, start time.Time) {
	for id, f := range st.pending {
		if f.at.Before(queryAt) || f.at.Before(start) {
			delete(st.pending, id)
		}
	}
}

// pendingLocked is what the settle path has counted since the last query.
// Caller holds st.mu.
func (st *volumeCapState) pendingLocked() int64 {
	var n int64
	for _, f := range st.pending {
		n += f.bytes
	}
	return n
}

// volumeCapRecordLocked counts a download that has just finished, so the next
// dispatch decision already knows about it. It uses the announced size, the
// figure the history row will carry.
//
// Caller holds a.mu. It takes st.mu inside it, and nothing takes the two in
// the other order.
func (a *App) volumeCapRecordLocked(t *core.Task) {
	if t == nil || t.Size <= 0 {
		return
	}
	st := a.volumeCapStateFor()
	st.mu.Lock()
	defer st.mu.Unlock()
	if !st.queried {
		// The first pass will read this finish from the history; booking it
		// here too would count it twice.
		return
	}
	if _, seen := st.pending[t.ID]; seen {
		return
	}
	st.pending[t.ID] = volumeCapFinish{bytes: t.Size, at: time.Now()}
	st.usage.Used += t.Size
	st.usage.Reached = st.usage.Cap > 0 && st.usage.Used >= st.usage.Cap
}

// volumeCapHolds reports whether the queue may start nothing more this period.
//
// The cap and action are read live from the settings and only Used from the
// cache, so raising the cap takes effect on the next dispatch pass. With no
// opinion yet it does not hold.
//
// Caller holds a.mu. It reads a cached number and never the store.
func (a *App) volumeCapHolds() bool {
	cfg := a.Settings.Get()
	if cfg.VolumeCap <= 0 || cfg.VolumeCapAction != settings.VolumeCapPause {
		return false
	}
	used, ok := a.volumeCapUsed()
	return ok && used >= cfg.VolumeCap
}

// volumeCapLimit folds the cap's throttle into the speed limit where it is
// read, rather than writing a.limitInForce, whose existing writers would undo
// it at the next window boundary or quiet press. Zero means no limit, so the
// result is the smaller non-zero of the two.
func (a *App) volumeCapLimit(limit int64) int64 {
	cfg := a.Settings.Get()
	if cfg.VolumeCap <= 0 || cfg.VolumeCapAction != settings.VolumeCapThrottle || cfg.VolumeCapThrottle <= 0 {
		return limit
	}
	used, ok := a.volumeCapUsed()
	if !ok || used < cfg.VolumeCap {
		return limit
	}
	if limit <= 0 || cfg.VolumeCapThrottle < limit {
		return cfg.VolumeCapThrottle
	}
	return limit
}

// volumeCapUsed is the cached figure, and false when no pass has produced one yet.
func (a *App) volumeCapUsed() (int64, bool) {
	st := a.volumeCapStateFor()
	st.mu.Lock()
	defer st.mu.Unlock()
	return st.usage.Used, st.queried
}

// VolumeUsage is the counter for a client, freshly added up so a page shows
// the current figure. It runs on an HTTP goroutine, off the dispatcher.
func (a *App) VolumeUsage() VolumeUsage {
	return a.volumeCapPass()
}

// VolumeBuckets is the curve, straight from the store. The window and shape
// are decided in internal/api/routes_stats.go.
func (a *App) VolumeBuckets(from time.Time, unit string) ([]store.VolumeBucket, error) {
	return a.Store.VolumeBuckets(from, unit)
}

// VolumeHistoryEdge is where the record itself runs out.
type VolumeHistoryEdge struct {
	// Oldest is the earliest finish still on file; Known is false for an empty
	// history.
	Oldest time.Time
	Known  bool
	// Trimmed says the history is at settings.HistoryMax, so months before
	// Oldest are missing rather than empty and must not be drawn as zero.
	Trimmed bool
}

// VolumeHistoryEdge reports how far back the record goes and whether it has
// been cut. Reading the settings here keeps the store free of them.
func (a *App) VolumeHistoryEdge() (VolumeHistoryEdge, error) {
	oldest, known, err := a.Store.OldestFinished()
	if err != nil {
		return VolumeHistoryEdge{}, err
	}
	trimmed, err := a.Store.HistoryFull(a.Settings.Get().HistoryMax)
	if err != nil {
		return VolumeHistoryEdge{}, err
	}
	return VolumeHistoryEdge{Oldest: oldest, Known: known, Trimmed: trimmed}, nil
}
