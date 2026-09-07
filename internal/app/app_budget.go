package app

import (
	"sync"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/core"
)

// One speed limit, shared out, instead of three copies of the same number.
//
// The limit used to be handed WHOLE to each of the three things that can move
// bytes: the engine's own throttle, the JDownloader sidecar, and yt-dlp. Each of
// them then honoured it faithfully on its own, so somebody who set 10 MB/s and
// happened to have all three working got 30. A limit that does not limit is
// worse than none: it is the one setting a nightly window exists to enforce, and
// it quietly did not.
//
// What this does instead: measure what each of the three is actually pulling,
// split the configured budget between the ones that are working, and re-adjust.
// Nothing here throttles anything itself; it only decides the three numbers and
// hands them to the three throttles that already existed.
//
// Deliberately NOT a fourth throttle in the middle. The engine meters through
// its own loopback proxy, JD meters in its own process, and yt-dlp is told per
// spawn on the command line. None of those bytes passes through this app, so a
// central limiter would have nothing to limit; the only lever is the number each
// of them is given.

// budgetInterval is how often the split is recomputed.
//
// Three seconds, not the upkeep minute: a backend that finishes its last
// transfer should hand its share back while somebody is still looking at the
// screen, and a share handed back a minute late is a minute of the limit being
// wrong in the other direction. Not shorter either - JD is told over the network
// and yt-dlp only reads its limit when it next spawns, so a faster tick would
// mostly be traffic.
const budgetInterval = 3 * time.Second

// budgetFloor is the smallest share a working backend is given, in bytes per
// second.
//
// A backend that has just started has measured no speed yet, so a purely
// proportional split would give it nothing and it would never get going - the
// classic way a fair-share scheme starves exactly the transfer it is about to
// need to measure. 16 KiB/s is small enough not to matter against any real
// limit and large enough that a transfer can begin and be measured.
const budgetFloor = 16 * 1024

// budget holds the three shares last decided, so the yt-dlp backend can read
// its own without going through the engine's throttle (which is a different
// number now).
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

// budgetFamily names which of the three meters a task's bytes go through. It is
// not the resolver id: several resolvers share one meter, and the meter is what
// the limit is set on.
type budgetFamily int

const (
	familyEngine budgetFamily = iota
	familyJD
	familyYtdlp
	familyCount
)

// meterFor says which meter a task's bytes pass through.
//
// Everything that is not JD or yt-dlp ends at the engine, including the debrid
// services and TorBox: those resolve a link to a direct URL and then hand it to
// the engine, so their bytes go through the loopback proxy like any other direct
// download. That is why this is a function of the RESOLVER and not of the
// service the account belongs to.
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

// shareOut splits `limit` between the three meters according to what each is
// currently pulling.
//
// The rule, in order:
//
//  1. A limit of 0 is "unlimited" and stays unlimited everywhere. Splitting
//     infinity three ways is not a thing, and turning "off" into three finite
//     numbers would be a limit nobody asked for.
//  2. A meter with nothing running gets 0. It is not that it may not download;
//     it is that it will get a share on the next tick, three seconds after it
//     starts, and until then the floor below keeps it moving.
//  3. Everything else is split by demand: a meter using less than its equal
//     share keeps what it uses, and what it leaves over is handed to the ones
//     that are saturated. Two passes are enough for three meters and the result
//     never exceeds the limit, which is the property that matters.
//  4. Every working meter gets at least budgetFloor, so nothing is starved to a
//     standstill by a measurement it has not had a chance to produce yet.
//
// Returns the three shares in family order. The sum is at most `limit` except
// where the floor forces otherwise, which can only happen with a limit smaller
// than three floors, i.e. under 48 KiB/s - a setting at which honouring the
// floor is more useful than honouring the arithmetic.
func shareOut(limit int64, speed [familyCount]int64, working [familyCount]bool) [familyCount]int64 {
	var out [familyCount]int64
	if limit <= 0 {
		return out // unlimited stays unlimited on every meter
	}

	n := int64(0)
	for _, w := range working {
		if w {
			n++
		}
	}
	if n == 0 {
		// Nothing is downloading. Hand each meter the whole limit rather than
		// zero: the next transfer to start must not be pinned at the floor for
		// three seconds waiting to be measured, and while nothing is running
		// there is nothing to overshoot with.
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
			// Using less than its share: give it what it uses plus a little
			// headroom, and put the rest in the pot. The headroom matters -
			// a share pinned exactly to the last measurement is a ceiling that
			// prevents the very growth it is measuring.
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

// measureLocked reports what each meter is pulling right now and whether it has
// anything running at all. Caller holds a.mu.
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

// applyBudget measures, splits and pushes the three numbers.
func (a *App) applyBudget() {
	a.mu.Lock()
	// The limit IN FORCE, which is not always the one in settings: a schedule
	// window can carry its own, and a nightly 2 MB/s window that this read
	// straight from settings would be shared out at the daytime figure. Zero
	// means no window has spoken since boot, so settings is the answer.
	limit := a.limitInForce
	if limit < 0 {
		limit = a.Settings.Get().SpeedLimit
	}
	speed, working := a.measureLocked()
	a.mu.Unlock()

	share := shareOut(limit, speed, working)
	a.budget.set(share[familyEngine], share[familyJD], share[familyYtdlp])

	// The engine's own throttle is set here and nowhere else now. Anything that
	// used to call Throttle.Set with the raw configured limit would undo this on
	// its next pass, which is why applySchedule hands its limit to the settings
	// store and lets the next tick do the sharing.
	a.Throttle.Set(share[familyEngine])
	a.pushJDSpeedLimit(share[familyJD])
	// yt-dlp is not pushed: it reads budget.ytdlpLimit when it next spawns, so
	// a running transfer keeps the limit it was started with. That is a real
	// limitation and it is yt-dlp's, not ours - --limit-rate is a launch
	// argument, and there is no way to retune a process that is already running.
}

// budgetLoop keeps the split current. Mirrors upkeep's shape: ctx-aware, no work
// until the first tick.
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
