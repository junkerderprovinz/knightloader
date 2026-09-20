package app

// The speed record: this instance's aggregate download speed since the process
// started, kept in memory so the Overview page draws a curve that already has a
// shape. A browser-side buffer starts from zero on every reload or suspended
// tab; the process is the one observer that was watching all along, and the
// shell's meter and the page's graph can seed from the same numbers.
//
// It is memory only. Persisting a reading a second, on stores that are often an
// SD card, is not worth it for a live figure. After a restart the curve is flat
// again, which the route summary, the diagnostics row and the info bubble all
// say.
//
// The fine ring holds one reading a second for the live curve. The hour is kept
// at ten-second buckets, each the mean of its readings, since sampling one
// instant per bucket turns bursty traffic into noise.
//
// The state is package-level and keyed by *App, like activityState. Unlike
// that one, it is removed when the sampler exits, since a few kilobytes per App
// adds up in a test binary that builds hundreds.

import (
	"log"
	"sync"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/core"
)

const (
	// The fine ring: one reading a second for two minutes, longer than the
	// graph's sixty seconds so the seed still covers a client that was away a
	// while.
	speedFineStep  = time.Second
	speedFineSlots = 120
	// The coarse ring: one bucket per ten seconds for an hour.
	speedCoarseStep  = 10 * time.Second
	speedCoarseSlots = 360
	// speedCoarseEvery is how many fine readings make one coarse bucket.
	speedCoarseEvery = int(speedCoarseStep / speedFineStep)
	// speedGapSteps is how many steps may go missing before it counts as a gap
	// rather than scheduling jitter.
	speedGapSteps = 3
)

// SpeedHistory is what GET /api/stats/speed answers.
//
// Recent seeds the live curve exactly; Hour is the shape of the last hour. Both
// are oldest first and always present, empty rather than null. Neither is
// padded to capacity, since zeros would claim the instance was idle before it
// booted.
//
// It lives here rather than in internal/api because App.SpeedHistory returns it.
type SpeedHistory struct {
	Recent     []int64 `json:"recent"`     // bytes/s, one per second
	RecentStep int     `json:"recentStep"` // seconds, 1
	RecentCap  int     `json:"recentCap"`  // 120
	Hour       []int64 `json:"hour"`       // bytes/s, mean over each step
	HourStep   int     `json:"hourStep"`   // seconds, 10
	HourCap    int     `json:"hourCap"`    // 360
	// RecordingSince is when the oldest sample held in either ring was taken,
	// absent when nothing has been recorded. It tells a quiet instance from one
	// that had not started yet.
	RecordingSince *time.Time `json:"recordingSince,omitempty"`
	SampledAt      time.Time  `json:"sampledAt"`
}

// speedRing is a fixed-capacity ring of readings, oldest overwritten first. A
// fixed array written in place allocates nothing per sample.
type speedRing struct {
	mu     sync.Mutex
	slots  []int64
	next   int // where the next sample goes
	filled int // how many of the slots have ever been written, capped at len
	// first is when the oldest sample still held was taken. It is derived from
	// the newest stamp on every push, since shifting it along would accumulate
	// the ticker's lateness over weeks of uptime.
	first time.Time
	step  time.Duration
}

func newSpeedRing(slots int, step time.Duration) *speedRing {
	return &speedRing{slots: make([]int64, slots), step: step}
}

// push records one reading, taken at at.
func (r *speedRing) push(v int64, at time.Time) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.slots[r.next] = v
	r.next = (r.next + 1) % len(r.slots)
	if r.filled < len(r.slots) {
		r.filled++
	}
	r.first = at.Add(-time.Duration(r.filled-1) * r.step)
}

// snapshot copies out what the ring holds, oldest first, with the stamp of the
// oldest entry; the zero time means the ring is empty. The slice is never nil,
// so it encodes as [] on the first load after a restart.
func (r *speedRing) snapshot() ([]int64, time.Time) {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]int64, 0, r.filled)
	start := (r.next - r.filled + len(r.slots)) % len(r.slots)
	for i := 0; i < r.filled; i++ {
		out = append(out, r.slots[(start+i)%len(r.slots)])
	}
	return out, r.first
}

// speedHistoryState is one App's pair of rings plus the sampler's bookkeeping.
type speedHistoryState struct {
	fine   *speedRing
	coarse *speedRing

	// bucket and last belong to the sampler goroutine alone; routes only read
	// the rings, which lock themselves.
	bucket []int64   // the fine readings since the last coarse bucket closed
	last   time.Time // wall clock of the last fine write, for the gap check
}

func newSpeedHistoryState() *speedHistoryState {
	return &speedHistoryState{
		fine:   newSpeedRing(speedFineSlots, speedFineStep),
		coarse: newSpeedRing(speedCoarseSlots, speedCoarseStep),
		bucket: make([]int64, 0, speedCoarseEvery),
	}
}

var (
	speedHistoryMu  sync.Mutex
	speedHistoryReg = map[*App]*speedHistoryState{}
)

// speedHistoryFor returns this App's rings, building them on first use.
func (a *App) speedHistoryFor() *speedHistoryState {
	speedHistoryMu.Lock()
	defer speedHistoryMu.Unlock()
	st, ok := speedHistoryReg[a]
	if !ok {
		st = newSpeedHistoryState()
		speedHistoryReg[a] = st
	}
	return st
}

// dropSpeedHistory forgets this App's rings when the sampler exits. A handler
// mid-snapshot still holds the state itself and keeps working.
func (a *App) dropSpeedHistory() {
	speedHistoryMu.Lock()
	defer speedHistoryMu.Unlock()
	delete(speedHistoryReg, a)
}

// SpeedNow is the aggregate download speed right now: the sum of Speed over
// running tasks only.
//
// It is not Counters().Speed, which sums every non-terminal task for an ETA.
// The browser draws the live half of the curve by summing running tasks, and
// the seeded half has to be the same measurement or the join shows a step. It
// walks a.tasks rather than a.active, which is a dispatch ledger and not a
// status index; speedhistory_test.go cross-checks the two.
func (a *App) SpeedNow() int64 {
	a.mu.Lock()
	defer a.mu.Unlock()
	var total int64
	for _, t := range a.tasks {
		if t.Status == core.StatusRunning {
			total += t.Speed
		}
	}
	return total
}

// sampleSpeedLoop writes one reading a second for the life of the process. It
// runs under a.spawn and stops with a.ctx.
func (a *App) sampleSpeedLoop() {
	st := a.speedHistoryFor()
	defer a.dropSpeedHistory()

	st.last = time.Now()
	tick := time.NewTicker(speedFineStep)
	defer tick.Stop()
	for {
		select {
		case <-a.ctx.Done():
			return
		case now := <-tick.C:
			st.sample(a.SpeedNow(), now)
		}
	}
}

// sample records one reading taken at now, filling in whatever the ticker
// missed first. It is a method on the state so tests can drive their own state
// with fabricated times instead of racing the live sampler.
func (st *speedHistoryState) sample(v int64, now time.Time) {
	st.fillSpeedGap(now)
	st.recordFine(v, now)
	st.last = now
}

// fillSpeedGap writes zeros for the steps the ticker did not fire for.
//
// A ticker does not fire while a laptop sleeps or a container is paused, so
// without this the samples before and after sit next to each other and the
// hour view draws a line across the gap. The same covers a clock moved forward.
// The fill is capped at each ring's capacity and logged once per gap.
//
// A clock moved backwards yields no fill and one out-of-order stamp, which is
// preferable to guessing which clock was right.
func (st *speedHistoryState) fillSpeedGap(now time.Time) {
	if st.last.IsZero() {
		return
	}
	elapsed := now.Sub(st.last)
	missed := int(elapsed/speedFineStep) - 1
	if missed < speedGapSteps {
		return
	}

	fine := min(missed, speedFineSlots)
	for i := fine; i >= 1; i-- {
		st.fine.push(0, now.Add(-time.Duration(i)*speedFineStep))
	}
	// A half-built bucket spanning the gap would describe neither side.
	st.bucket = st.bucket[:0]

	coarse := min(int(elapsed/speedCoarseStep), speedCoarseSlots)
	for i := coarse; i >= 1; i-- {
		st.coarse.push(0, now.Add(-time.Duration(i)*speedCoarseStep))
	}

	log.Printf("speed history: filled %d missed samples as idle after a %ds gap "+
		"(the process was suspended, or the clock moved)", fine, int(elapsed.Seconds()))
}

// recordFine puts one reading into the fine ring and, every tenth one, closes a
// coarse bucket with the mean of the ten.
func (st *speedHistoryState) recordFine(v int64, at time.Time) {
	st.fine.push(v, at)
	st.bucket = append(st.bucket, v)
	if len(st.bucket) < speedCoarseEvery {
		return
	}
	var sum int64
	for _, s := range st.bucket {
		sum += s
	}
	st.coarse.push(sum/int64(len(st.bucket)), at)
	st.bucket = st.bucket[:0]
}

// SpeedHistory is the snapshot GET /api/stats/speed serves, and what the
// diagnostics bundle counts.
func (a *App) SpeedHistory() SpeedHistory {
	st := a.speedHistoryFor()
	recent, recentFrom := st.fine.snapshot()
	hour, hourFrom := st.coarse.snapshot()

	out := SpeedHistory{
		Recent:     recent,
		RecentStep: int(speedFineStep / time.Second),
		RecentCap:  speedFineSlots,
		Hour:       hour,
		HourStep:   int(speedCoarseStep / time.Second),
		HourCap:    speedCoarseSlots,
		SampledAt:  time.Now().UTC(),
	}
	// The older of the two edges, ignoring an empty ring.
	since := time.Time{}
	for _, t := range []time.Time{recentFrom, hourFrom} {
		if t.IsZero() {
			continue
		}
		if since.IsZero() || t.Before(since) {
			since = t
		}
	}
	if !since.IsZero() {
		utc := since.UTC()
		out.RecordingSince = &utc
	}
	return out
}
