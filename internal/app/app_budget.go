package app

import (
	"math"
	"sync"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/core"
)

// The speed limit is shared out between the three things that move bytes: the
// engine's throttle, the JDownloader sidecar and yt-dlp. Handing each the whole
// limit would let all three together exceed it. None of their bytes pass
// through this app, so the only lever is the number each one is given.

// budgetInterval is how often the split is recomputed: fast enough that an
// idle backend's share returns while someone is looking, slow enough not to
// flood JD with updates.
const budgetInterval = 3 * time.Second

// budgetFloor is the smallest share a working backend gets, in bytes per
// second, so a meter that has been quiet for a while can still show it wants
// more.
const budgetFloor = 16 * 1024

// budgetTick is one look at the meters and the shares it handed out. raised
// marks a share larger than the one before it, which the meter's averaged
// speed has not caught up with yet.
type budgetTick struct {
	speed   [familyCount]int64
	working [familyCount]bool
	share   [familyCount]int64
	raised  [familyCount]bool
}

// budget keeps the last tick. A reading means little on its own: a meter
// pulling 2 MB/s may be held there by its share or have nothing more to pull,
// and only the share it was given tells the two apart. The yt-dlp backend also
// reads its own share from here.
type budget struct {
	mu   sync.RWMutex
	last budgetTick
}

func (b *budget) ytdlpLimit() int64 {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.last.share[familyYtdlp]
}

// next splits limit against the last tick and keeps the result for the one
// after.
func (b *budget) next(limit int64, speed [familyCount]int64, working [familyCount]bool) [familyCount]int64 {
	b.mu.Lock()
	defer b.mu.Unlock()
	now := budgetTick{speed: speed, working: working}
	now.share = shareOut(limit, now, b.last)
	for i := range now.raised {
		now.raised[i] = now.share[i] > b.last.share[i]
	}
	b.last = now
	return now.share
}

// budgetFamily names the meter a task's bytes go through. Several resolvers
// share one meter.
type budgetFamily int

const (
	familyEngine budgetFamily = iota
	familyJD
	familyYtdlp
	familyCount
)

// meterFor returns the meter for a resolver. Debrid services and TorBox hand a
// direct URL to the engine, so everything but JD and yt-dlp is metered there.
func meterFor(resolverID string) budgetFamily {
	switch resolverID {
	case "jd":
		return familyJD
	case "ytdlp":
		return familyYtdlp
	default:
		return familyEngine
	}
}

// shareOut splits limit between the three meters by what each is pulling:
//
//  1. A limit of 0 means unlimited and stays unlimited on every meter.
//  2. A meter with nothing running gets 0. With nothing running anywhere,
//     every meter gets the whole limit.
//  3. A working meter pulling under nine tenths of its last share has no use
//     for more and is offered a quarter more than its pull. Every other
//     working meter is offered all it can get: its share is what holds it
//     back; or it reads 0 for the first time, which is a meter unlocking,
//     connecting or between two files rather than one with nothing to pull; or
//     its share was raised on the last tick, and its speed, averaged over
//     seconds, still trails the raise.
//  4. Offers are granted smallest first, and the meters left over split what
//     remains evenly. What nobody was offered goes to the working meters, so
//     a lone meter always gets the whole limit.
//  5. Every working meter gets at least budgetFloor.
//
// A quarter more and nine tenths are far enough apart that a meter held back
// by its server reads four fifths of its share on the next tick and stays
// where it is, and close enough that beside one other meter at most a tenth of
// the limit goes unused. A meter that fills its share reads nearly all of it
// once a raise has passed through its average, and a quarter more than nine
// tenths of a share is more than the share, so a raise is never taken back.
//
// The sum exceeds limit only when the floor forces it, below 48 KiB/s.
func shareOut(limit int64, now, last budgetTick) [familyCount]int64 {
	var out [familyCount]int64
	if limit <= 0 {
		return out
	}

	var offer [familyCount]int64
	n := 0
	for i := range offer {
		if !now.working[i] {
			continue
		}
		n++
		offer[i] = math.MaxInt64
		speed, given := now.speed[i], last.share[i]
		firstZero := speed == 0 && (!last.working[i] || last.speed[i] > 0)
		catchingUp := speed > 0 && last.raised[i]
		if !firstZero && !catchingUp && given > 0 && speed*10 < given*9 {
			offer[i] = max(budgetFloor, speed+speed/4)
		}
	}
	if n == 0 {
		// With nothing running there is nothing to overshoot, and the next
		// transfer should not wait at the floor to be measured.
		for i := range out {
			out[i] = limit
		}
		return out
	}

	// An offer below an even split of what is left is granted as it stands,
	// which only raises the split for the rest.
	left, open := limit, n
	for open > 0 {
		even := left / int64(open)
		granted := false
		for i := range out {
			if now.working[i] && out[i] == 0 && offer[i] < even {
				out[i] = offer[i]
				left -= offer[i]
				open--
				granted = true
			}
		}
		if !granted {
			for i := range out {
				if now.working[i] && out[i] == 0 {
					out[i] = even
				}
			}
			left, open = 0, 0
		}
	}
	if left > 0 {
		// What nobody was offered goes to the meters moving bytes, since a
		// quiet one has no use for it, or to every working meter when none is.
		takers, k := now.working, n
		var moving [familyCount]bool
		m := 0
		for i := range moving {
			moving[i] = now.working[i] && now.speed[i] > 0
			if moving[i] {
				m++
			}
		}
		if m > 0 {
			takers, k = moving, m
		}
		for i := range out {
			if takers[i] {
				out[i] += left / int64(k)
			}
		}
	}
	for i := range out {
		if now.working[i] && out[i] < budgetFloor {
			out[i] = budgetFloor
		}
	}
	return out
}

// measureLocked reports what each meter is pulling and whether it has anything
// running. Caller holds a.mu.
func (a *App) measureLocked() (speed [familyCount]int64, working [familyCount]bool) {
	for id := range a.active {
		t := a.tasks[id]
		if t == nil || t.Status != core.StatusRunning {
			continue
		}
		f := meterFor(t.Resolver)
		speed[f] += t.Speed
		working[f] = true
	}
	return speed, working
}

// applyBudget measures, splits and pushes the three shares.
func (a *App) applyBudget() {
	a.mu.Lock()
	// The limit in force, which a schedule window may override. Negative means
	// no window has set one since boot.
	limit := a.limitInForce
	if limit < 0 {
		limit = a.Settings.Get().SpeedLimit
	}
	speed, working := a.measureLocked()
	a.mu.Unlock()

	// The volume cap is folded in here because a.limitInForce already has two
	// writers and a third would be undone at the next window boundary.
	limit = a.volumeCapLimit(limit)

	share := a.budget.next(limit, speed, working)

	// This is the only place the engine throttle is set; anything else setting
	// the raw limit would be undone on the next tick.
	a.Throttle.Set(share[familyEngine])
	a.pushJDSpeedLimit(share[familyJD])
	// yt-dlp reads budget.ytdlpLimit when it spawns, since --limit-rate cannot
	// be changed on a running process.
}

// budgetLoop keeps the split current until the app shuts down.
func (a *App) budgetLoop() {
	defer a.wg.Done()
	tick := time.NewTicker(budgetInterval)
	defer tick.Stop()
	for {
		select {
		case <-a.ctx.Done():
			return
		case <-tick.C:
			a.applyBudget()
		}
	}
}
