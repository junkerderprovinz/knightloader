package app

import (
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
// second. A backend that just started has no measured speed yet, and a purely
// proportional split would starve it.
const budgetFloor = 16 * 1024

// budget holds the last shares, so the yt-dlp backend can read its own.
type budget struct {
	mu     sync.RWMutex
	engine int64
	jd     int64
	ytdlp  int64
}

func (b *budget) ytdlpLimit() int64 {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.ytdlp
}

func (b *budget) set(engine, jd, ytdlp int64) {
	b.mu.Lock()
	b.engine, b.jd, b.ytdlp = engine, jd, ytdlp
	b.mu.Unlock()
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
//  2. A meter with nothing running gets 0 until the next tick.
//  3. A meter using less than an equal share keeps what it uses plus headroom,
//     and the rest goes to the saturated meters.
//  4. Every working meter gets at least budgetFloor.
//
// The sum exceeds limit only when the floor forces it, below 48 KiB/s.
func shareOut(limit int64, speed [familyCount]int64, working [familyCount]bool) [familyCount]int64 {
	var out [familyCount]int64
	if limit <= 0 {
		return out
	}

	n := int64(0)
	for _, w := range working {
		if w {
			n++
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

	equal := limit / n
	spare := int64(0)
	greedy := int64(0)
	for i := range out {
		if !working[i] {
			continue
		}
		if speed[i] < equal {
			// A share pinned to the last measurement would prevent the growth
			// it is measuring, hence the headroom.
			take := speed[i] + speed[i]/4
			if take < budgetFloor {
				take = budgetFloor
			}
			if take > equal {
				take = equal
			}
			out[i] = take
			spare += equal - take
			continue
		}
		out[i] = equal
		greedy++
	}
	if greedy > 0 && spare > 0 {
		per := spare / greedy
		for i := range out {
			if working[i] && speed[i] >= equal {
				out[i] += per
			}
		}
	}
	for i := range out {
		if working[i] && out[i] < budgetFloor {
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

	share := shareOut(limit, speed, working)
	a.budget.set(share[familyEngine], share[familyJD], share[familyYtdlp])

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
