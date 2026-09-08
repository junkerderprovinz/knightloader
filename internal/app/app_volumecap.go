package app

// The volume allowance: how much has finished downloading this period, and what
// happens once that is as much as somebody said they could afford.
//
// It is the disk guard's opposite number and is built to the same plan on
// purpose, down to the package-level state and the ticker. The difference is
// what the two are counting. Free space is a fact about this machine that a
// syscall answers; an allowance is a number in a contract that nothing on this
// box can see, so the figure has to be added up out of what this instance has
// already fetched.
//
// WHAT THE COUNTER IS A READING OF, because everything below depends on it: the
// download history, and nothing else. internal/store/volume.go says what that
// makes it - announced sizes, one row per task, no upload - and there are two
// consequences worth having in front of you here:
//
//   - EMPTYING THE HISTORY EMPTIES THE COUNTER. The button is on the settings
//     page, the trim runs on the upkeep tick, and either of them can take rows
//     out from under a period that is still running. That is deliberate rather
//     than repaired: a second private total kept beside the table would be a
//     number nobody could check against anything, and it would survive a restore
//     from backup as the one figure in the app that disagrees with the record.
//     The interface says so in as many words (volume.meterHint).
//   - THE UPGRADE SPIKE IS REAL. Migration 8 stamped every already-finished
//     download with the moment of the upgrade and migration 9 carried all of
//     them into the history, so on an instance that came through that version
//     one calendar day holds the entire back catalogue. A period containing that
//     day can be spent before its first download. Nothing here can tell those
//     rows apart from honest ones, so nothing here pretends to.
//
// THE ENFORCEMENT IS TWO LINES SOMEWHERE ELSE, and that is the whole design:
//
//	pause     a gate in dispatchLocked that sets core.WaitingVolume on the queue
//	throttle  one fold into applyBudget's read of the limit in force
//
// Neither of them is a flag. A flag would have to be cleared by whoever fixed
// the thing that set it, and the two obvious places to keep one are already
// spoken for: a.halted is recomputed from a.manualHalt by every schedule pass,
// so a cap that wrote it would be released by the next settings save with
// nothing on screen to say why, and a.limitInForce already has two writers
// (applySchedule and SetQuiet) so a third would lose at the next window
// boundary. Both act instead by being READ at the one place that decides,
// which is what core.Waiting's own doc comment argues for at length.
//
// The state lives at package level keyed by the owning *App rather than as a
// field on App, the same trade app_diskguard.go, app_stallwatch.go and
// app_captcha.go already document: app.go's struct is not this file's to grow.
//
// EVERYTHING HERE IS NAMED "volume cap" AND NOT "volume", ON PURPOSE. This
// package already uses "volume" for a mounted disk (app_diskguard.go, and
// VolumeReport next door), and "budget" is taken by the speed limit's share-out
// (app_budget.go's own `budget` type). A bare volumeSomething in package app is
// therefore a word with two meanings, and the reader who guesses wrong guesses
// wrong about a guard.

import (
	"log"
	"sync"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/settings"
	"github.com/junkerderprovinz/knightloader/internal/store"
)

// volumeCapInterval is how often the counter is added up again from the
// history.
//
// Half a minute, and it is not what makes the figure move: a download that
// finishes is counted the moment it settles (volumeCapRecordLocked), because a
// gate that learned about it a tick later would already have handed the slot to
// the next task. What the tick is for is everything the settle path cannot see -
// a history somebody emptied, the trim taking the oldest rows out, a re-download
// that moved its bytes to today instead of adding them, and the period boundary
// passing while nothing at all is happening. Those are all corrections, none of
// them is urgent, and each tick is one indexed SUM.
const volumeCapInterval = 30 * time.Second

// VolumeUsage is the counter as everything outside this file reads it: the
// route, the websocket push, and the status bar chip that draws it.
type VolumeUsage struct {
	// Used is the announced size of everything that has finished since
	// PeriodStart. It moves when a download FINISHES and never while one is
	// running, so it steps rather than creeps.
	Used int64 `json:"used"`
	// Cap is the allowance in bytes, and 0 means there is none. Reached is then
	// always false, and nothing is ever held back.
	Cap int64 `json:"cap"`
	// Action and Throttle are echoed back rather than left for the caller to
	// look up in the settings, so a chip that has to say WHY the queue is
	// waiting does not need a second request to find out.
	Action   string `json:"action"`
	Throttle int64  `json:"throttle"`
	// PeriodStart and PeriodEnd are the half-open window Used was summed over,
	// [start, end). Both are instants in the server's own zone, which is the
	// zone the cap is enforced in.
	PeriodStart time.Time `json:"periodStart"`
	PeriodEnd   time.Time `json:"periodEnd"`
	Reached     bool      `json:"reached"`
}

// same reports whether two readings say the same thing, so a broadcast happens
// on a CHANGE rather than once per tick.
//
// Field by field rather than ==, because two time.Time values that describe the
// same instant are not necessarily equal by == (a monotonic reading and a
// location pointer both ride along), and a comparison that is wrong in that
// direction would push a message every thirty seconds for ever.
func (u VolumeUsage) same(o VolumeUsage) bool {
	return u.Used == o.Used && u.Cap == o.Cap && u.Action == o.Action &&
		u.Throttle == o.Throttle && u.Reached == o.Reached &&
		u.PeriodStart.Equal(o.PeriodStart) && u.PeriodEnd.Equal(o.PeriodEnd)
}

// capDaysInMonth is how long a given month actually is, leap years included.
//
// Day 0 of the NEXT month, which is the one place in this file that leans on
// Go's date normalisation on purpose rather than in spite of it: time.Date
// normalises out of range values, so "the zeroth of March" is the last day of
// February and it is right in a leap year without anybody writing down which
// years those are.
func capDaysInMonth(year int, month time.Month, loc *time.Location) int {
	return time.Date(year, month+1, 0, 0, 0, 0, 0, loc).Day()
}

// capMonthDay is local midnight on `day` of the given month, with the day clamped
// to the length of that month first.
//
// THE CLAMP IS THE WHOLE FUNCTION. time.Date(2026, February, 31, ...) is not an
// error and it is not the 28th: it is 3 March, silently normalised, and a reset
// day of 31 would therefore start February's period in March and hand somebody a
// period that is not a month and overlaps the next one. Every date this file
// builds goes through here.
func capMonthDay(year int, month time.Month, day int, loc *time.Location) time.Time {
	if n := capDaysInMonth(year, month, loc); day > n {
		day = n
	}
	return time.Date(year, month, day, 0, 0, 0, 0, loc)
}

// capPeriodStart is the most recent reset boundary at or before now.
//
// Local midnight, in the server's own zone, because the day the counter starts
// over on is a calendar day and the server is what holds the queue back. A
// browser in another zone is shown this instant and never asked to work one out.
func capPeriodStart(now time.Time, day int) time.Time {
	day = capResetDay(day)
	loc := now.Location()
	start := capMonthDay(now.Year(), now.Month(), day, loc)
	if now.Before(start) {
		// This month's boundary has not happened yet, so the period running is
		// the one that opened last month. AddDate on the first of the month
		// rather than month-1 arithmetic by hand, so the year rolls over on its
		// own in December.
		prev := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, loc).AddDate(0, -1, 0)
		start = capMonthDay(prev.Year(), prev.Month(), day, loc)
	}
	return start
}

// capPeriodEnd is the next boundary after the period that began at start, which is
// where the counter goes back to zero.
//
// Worked out from the period's own start rather than from `now`, so a period
// that was clamped keeps the day it was asked for: a cap set to the 31st runs
// 28 February to 31 March, not 28 February to 28 March. The window is half-open,
// so a download that finishes exactly on the boundary belongs to the new period
// and to that one only.
func capPeriodEnd(start time.Time, day int) time.Time {
	day = capResetDay(day)
	loc := start.Location()
	next := time.Date(start.Year(), start.Month(), 1, 0, 0, 0, 0, loc).AddDate(0, 1, 0)
	return capMonthDay(next.Year(), next.Month(), day, loc)
}

// capResetDay is the same clamp the settings store applies, applied again
// here.
//
// Twice on purpose. The store sanitises what is SAVED, and this file is also
// handed settings that were never saved: a test's literal, a document written by
// hand, an install that upgraded into the key and has a zero in it. A guard
// applied once is a guard missing everywhere else it is needed.
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
	// usage is the last answer worked out, and queried says whether one has been
	// worked out at all. Before the first pass every reader treats this as NO
	// OPINION rather than as a zero: a guard that holds the queue back because it
	// has not looked yet would stop a healthy instance for the same reason the
	// disk guard refuses to read an unanswerable stat as a full disk.
	usage   VolumeUsage
	queried bool
	// pending is what has settled since the last query STARTED, keyed by task
	// id, and it is what makes the gate right in the pass that matters.
	//
	// The order of events on a finish is: this file counts it under a.mu, the
	// dispatcher hands out the freed slot in that same critical section, and the
	// store writes the history row afterwards, once the lock is released. A
	// counter that waited for the next query would therefore let the batch the
	// finish paid for start anyway, which is the one moment a cap most needs to
	// be right.
	//
	// Keyed by task id rather than summed into one number so that the same
	// download counted twice - a backend re-reporting a terminal status, a task
	// fetched again inside one window - lands once, which is also what the
	// history's one-row-per-task rule will say when the next query runs.
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

// ensureVolumeCapWatcher starts the watch loop exactly once per App, from
// dispatchLocked and for the reasons ensureDiskWatcher is: this package's files
// own no start-up hook, and dispatchLocked is the closest thing to "runs once at
// start-up and on nearly everything after". Idempotent and cheap after the first
// call.
func (a *App) ensureVolumeCapWatcher() {
	st := a.volumeCapStateFor()
	st.startOnce.Do(func() { a.spawn(a.volumeCapWatchLoop) })
}

// volumeCapWatchLoop runs until a.ctx is done, which is what makes Close wait for
// it - see a.spawn's own contract.
//
// One pass before the first tick, unlike the disk guard, because this watcher's
// answer is CACHED and read by a gate: until a pass has run there is no opinion
// at all, and half a minute of "no opinion" on an instance that boots with its
// allowance already spent is half a minute of downloading somebody did not
// authorise.
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

// volumeCapPass adds the period up again from the history and publishes the answer
// if it changed.
//
// NOT UNDER a.mu, ever, and this is the rule the whole file is arranged around.
// It is a GROUP-free SUM over an indexed range, which is fast, and "fast" is
// exactly the argument that was made for the local stat in the disk guard - a
// database on a slow volume would block every task in the app for as long as it
// took. The dispatcher reads the cached number instead, and the query happens
// out here on the watcher's own goroutine.
func (a *App) volumeCapPass() VolumeUsage {
	cfg := a.Settings.Get()
	now := time.Now()
	start := capPeriodStart(now, cfg.VolumeCapResetDay)
	end := capPeriodEnd(start, cfg.VolumeCapResetDay)

	// Taken before the query rather than after, because it is what decides which
	// settled downloads the query has already counted. See pruneLocked.
	queryAt := time.Now()
	// The window is the period and not "up to now": a row stamped a little in
	// the future by a clock that has since been corrected still belongs to the
	// period it claims, and dropping it would make the counter disagree with the
	// chart drawn from the same table.
	sum, _, _, err := a.Store.VolumeSince(start, end)

	st := a.volumeCapStateFor()
	st.mu.Lock()
	defer st.mu.Unlock()
	if err != nil {
		// The previous answer is kept rather than replaced with a zero. A store
		// that cannot be read says nothing about how much has been downloaded,
		// and a counter that fell to zero on a failed query would un-pause a
		// queue that is over its allowance. With no previous answer this is
		// still "no opinion", which is the fail-open state everything reads it as.
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
		// On the crossing and not once per tick, the same rule the disk guard's
		// critical mark exists for: an allowance that stays spent for three weeks
		// is one incident and deserves one line.
		log.Printf("the volume cap is reached: %d of %d bytes have finished since %s (action: %s)",
			used, cfg.VolumeCap, start.Format(time.RFC3339), cfg.VolumeCapAction)
	}
	if changed {
		// So the counter in the status bar is a step function rather than a
		// poll. Broadcast never waits for a client write, which is what makes it
		// safe to call with this lock held.
		a.Hub.Broadcast("volume", next)
	}
	return next
}

// pruneLocked drops the settled downloads a query has certainly already counted,
// and anything left over from a period that has ended.
//
// "Certainly" is doing real work in that sentence. An entry recorded before the
// query started has had its history row written before the query's own snapshot
// in every case but one: the entry is booked under a.mu and the row is written
// once that lock is released, so a query that starts inside that gap can miss a
// download this then forgets. The cost is one file's worth of undercount until
// the next tick, at most half a minute later, and the alternative - keeping
// entries for a grace period - double counts every finish on screen for as long
// as the grace lasts, which is the same error made visible.
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

// volumeCapRecordLocked counts a download that has just finished, so the very
// next thing dispatchLocked does is measured against a counter that knows about
// it.
//
// The announced size, which is the same number the history row is about to
// carry, so the fast path and the correction that replaces it are counting the
// same thing. A download with no size adds nothing here and nothing there.
//
// Caller holds a.mu. It takes st.mu inside it, and nothing in this file ever
// takes those two in the other order.
func (a *App) volumeCapRecordLocked(t *core.Task) {
	if t == nil || t.Size <= 0 {
		return
	}
	st := a.volumeCapStateFor()
	st.mu.Lock()
	defer st.mu.Unlock()
	if !st.queried {
		// No pass has run, so there is nothing to be a fast path ON: the first
		// pass will read this download out of the history like every other one,
		// and booking it here as well would count it twice.
		return
	}
	if _, seen := st.pending[t.ID]; seen {
		return
	}
	st.pending[t.ID] = volumeCapFinish{bytes: t.Size, at: time.Now()}
	st.usage.Used += t.Size
	st.usage.Reached = st.usage.Cap > 0 && st.usage.Used >= st.usage.Cap
}

// volumeCapHolds reports whether the queue may not start anything else this
// period.
//
// The CAP AND THE ACTION are read live from the settings and only the USED
// figure comes from the cache, which is what makes raising the cap or switching
// the action take effect on the very next dispatch pass instead of at the next
// tick. Somebody who has just typed a bigger number is watching the screen.
//
// No opinion until a pass has run, and no opinion is not a hold: a guard that
// stopped the queue while it was ignorant would be indistinguishable from the
// app being broken, which is the disk guard's fail-open argument word for word.
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

// volumeCapLimit folds the cap's own ceiling into the speed limit in force.
//
// It is a fold at the point the limit is READ and not a fourth writer of
// a.limitInForce, because that field already has two (applySchedule and
// SetQuiet) and a third would be undone at the next window boundary or the next
// press of the turtle - a throttle that sometimes works, which is worse than one
// that never does.
//
// Zero on either side is "no limit here", so the other one wins outright. That
// is why this is a smaller-of and not a sum or an override: a nightly window or
// quiet mode asking for less than the capped speed is still the answer, and the
// cap asking for less than the window is too.
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

// VolumeUsage is the counter for a client, freshly added up.
//
// It runs a pass rather than handing back the cache, so the number a page draws
// on load is this instant's and not up to half a minute old, and so there is
// exactly one piece of code that decides what the counter says. This is an HTTP
// goroutine and not the dispatcher, so the query is in the right place.
func (a *App) VolumeUsage() VolumeUsage {
	return a.volumeCapPass()
}

// VolumeBuckets is the curve, straight from the store. The window and the shape
// of the answer are the caller's decision - see internal/api/routes_stats.go,
// which is the one place that says how far back a chart looks.
func (a *App) VolumeBuckets(from time.Time, unit string) ([]store.VolumeBucket, error) {
	return a.Store.VolumeBuckets(from, unit)
}

// VolumeHistoryEdge is where the record itself runs out.
type VolumeHistoryEdge struct {
	// Oldest is the earliest finish still on file, and Known is false for a
	// history with nothing in it at all.
	Oldest time.Time
	Known  bool
	// Trimmed says the history is sitting at settings.HistoryMax, so the months
	// before Oldest are missing rather than empty. A curve that drew them as
	// zeroes would be claiming this instance downloaded nothing in a month it
	// may well have been busy in.
	Trimmed bool
}

// VolumeHistoryEdge reports how far back the record goes and whether it has been
// cut. The settings read happens here rather than in the route, so the store
// keeps having no opinion about settings.
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
