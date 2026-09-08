package app

// The speed record: what this instance's own aggregate download speed has been
// doing since the process started, kept in memory so that a browser opening the
// Overview page draws a curve that already has a shape instead of a flat line
// that spends the next minute filling up.
//
// WHY HERE AND NOT IN THE BROWSER. The curve was a browser-side rolling buffer
// and nothing else (web/src/components/SpeedGraph.tsx's useSpeedSamples, seeded
// with Array(points).fill(0)). Every reload, every navigation away from the
// Overview page and back, and every phone that suspends a background tab threw
// the whole window away and started again at zero - while the one participant
// that had been watching the entire time was this process. A ring on this side
// is the only place a window can outlive a client that comes and goes, and the
// shell's meter and the page's hero can then be seeded from the same numbers
// instead of each keeping a private buffer that disagrees with the other.
//
// WHY MEMORY ONLY, AND WHY THAT IS NOT AN OMISSION. Nothing here reaches the
// store and a restart empties it. This is a live reading, not a record anybody
// audits afterwards: persisting it would mean a write a second, for ever, for a
// figure nobody is looking at while the browser is closed, on boxes whose store
// is routinely an SD card or a USB stick. The whole ring is 480 int64 - about
// 4 KB - and it costs one goroutine that wakes once a second.
//
// The price of that choice is a curve that IS flat again after a restart, which
// looks exactly like a broken sampler to whoever meets it first. That is why
// the same sentence is said in three places rather than left to be discovered:
// the route's own summary in the self-describing index (routes_speedhistory.go),
// the diagnostics row that counts the samples ("0 of 120" is a dead sampler,
// "120 of 120 since 02:14" is a quiet box), and the info bubble beside the graph.
//
// WHY TWO RESOLUTIONS. One second for two minutes is what the live curve is
// drawn at, so seeding it needs exactly that. "What has the last hour looked
// like" at one sample a second would be 3600 numbers to draw a 600 pixel wide
// picture with, so the hour is kept at ten seconds - and each ten second slot
// holds the MEAN of its ten readings rather than whichever instant the tenth
// tick happened to land on. Sampling one instant per bucket turns an hour of
// bursty traffic into noise: a transfer that alternates 40 MB/s and 0 draws as
// either a solid 40 or a solid 0 depending on nothing but phase.
//
// WHERE THE STATE LIVES. Package level, keyed by the owning *App, for exactly
// the reason app_activity.go:19-26 sets out and captchaState, hosterAuth and
// accountHealthState already follow: app.go's struct is not this wave's file to
// grow, and a keyed registry gives the same per-instance guarantee without
// touching it. Unlike those, this one is REMOVED again when the sampler exits
// (which Close waits for), because a few kilobytes per App is enough that a
// long test binary building hundreds of them would notice, where a pair of
// counter maps is not.

import (
	"log"
	"sync"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/core"
)

const (
	// The fine ring: one reading a second for two minutes. Two minutes rather
	// than the sixty seconds the graph draws, so that the seed still covers the
	// window after a client has been away long enough for its own buffer to be
	// worthless but not long enough to want the hour view.
	speedFineStep  = time.Second
	speedFineSlots = 120
	// The coarse ring: one bucket per ten seconds for an hour.
	speedCoarseStep  = 10 * time.Second
	speedCoarseSlots = 360
	// How many fine readings make one coarse bucket. Derived rather than
	// written a second time, so the two step constants cannot drift apart.
	speedCoarseEvery = int(speedCoarseStep / speedFineStep)
	// How many steps may go missing before the gap is treated as a real gap
	// rather than as the ordinary lateness of a ticker on a busy machine. Three
	// is well past anything scheduling jitter produces and well short of
	// anything a person would call an outage.
	speedGapSteps = 3
)

// SpeedHistory is what GET /api/stats/speed answers: what this instance has
// been recording about its own aggregate download speed since the process
// started.
//
// Two resolutions and not one, because the two are different questions. Recent
// is what the live curve is drawn at and seeds it exactly; Hour is the shape of
// the last hour, which at one sample a second would be 3600 numbers nobody
// draws. Both are oldest first and both are ALWAYS present, empty rather than
// null, so a client can take .length without checking first.
//
// Neither is padded to its capacity. A ring holding six samples reports six,
// and the client draws a six second window and says so on its abscissa; padding
// with zeros would say "this instance was idle for the fifty-nine minutes
// before it booted", which is the same lie fillCurve's `oldest` field exists to
// prevent over in routes_stats.go.
//
// It lives in this package rather than in internal/api because App.SpeedHistory
// returns it, and a method on App cannot return a type from the package that
// imports it. The api side adds nothing to it: the route is one writeJSON.
type SpeedHistory struct {
	Recent     []int64 `json:"recent"`     // bytes/s, one per second
	RecentStep int     `json:"recentStep"` // seconds, 1
	RecentCap  int     `json:"recentCap"`  // 120
	Hour       []int64 `json:"hour"`       // bytes/s, mean over each step
	HourStep   int     `json:"hourStep"`   // seconds, 10
	HourCap    int     `json:"hourCap"`    // 360
	// RecordingSince is when the oldest sample still held anywhere was taken,
	// absent when nothing has been recorded yet. It is what tells "this
	// instance was quiet" apart from "this instance had not started yet", and
	// it is the OLDER of the two rings' own edges: the hour ring reaches
	// further back than the fine one for all but the first ten seconds of a
	// process's life, and the fine one is the only one with anything in it
	// during those ten seconds.
	RecordingSince *time.Time `json:"recordingSince,omitempty"`
	SampledAt      time.Time  `json:"sampledAt"`
}

// speedRing is a fixed-capacity ring of readings, oldest overwritten first.
//
// Deliberately NOT the shape internal/logring uses. That one appends to a slice
// and re-slices the front off, and its own doc comment (logring.go:74-77) has to
// explain why it blanks the dropped entries first: the strings would otherwise
// stay reachable through the backing array. Nothing of the sort applies to a
// fixed array of int64 written in place - there is no reference to strand and no
// allocation per sample, which is the point of choosing this shape for something
// that writes once a second for the life of the process.
type speedRing struct {
	mu     sync.Mutex
	slots  []int64
	next   int // where the NEXT sample goes
	filled int // how many of the slots have ever been written, capped at len
	// first is when the oldest sample still held was taken. Derived on every
	// push from the newest stamp rather than remembered from the first push and
	// shifted along: shifting accumulates the ticker's lateness for as long as
	// the process runs, so an instance up for three weeks would report a
	// recordingSince minutes away from the truth.
	first time.Time
	step  time.Duration
}

func newSpeedRing(slots int, step time.Duration) *speedRing {
	return &speedRing{slots: make([]int64, slots), step: step}
}

// push records one reading, taken at `at`.
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
// oldest entry.
//
// make(..., 0, n) and not `var out []int64`: a ring that has never been written
// would otherwise hand back a nil slice, which marshals as `null`, and the
// client's `.length` throws on exactly the first load after a restart - which is
// the moment this whole feature exists for. The zero time means "nothing here".
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

// speedHistoryState is one App's pair of rings plus the sampler's own
// bookkeeping.
type speedHistoryState struct {
	fine   *speedRing
	coarse *speedRing

	// bucket and last are touched by sampleSpeedLoop's goroutine and by
	// nothing else, so they carry no lock of their own - the rings do their own
	// guarding, and those are the only fields a route ever reads. Putting a
	// mutex here as well would suggest a second writer exists.
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

// speedHistoryFor returns this App's rings, building them on first use - the
// same lazy-registry shape activityStateFor (app_activity.go) uses, for the
// identical reason.
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

// dropSpeedHistory forgets this App's rings. Called when the sampler exits,
// which Close waits for. A route handler that is mid-snapshot keeps working: it
// is holding the *speedHistoryState itself, and removing the map entry does not
// touch what it points at.
func (a *App) dropSpeedHistory() {
	speedHistoryMu.Lock()
	defer speedHistoryMu.Unlock()
	delete(speedHistoryReg, a)
}

// SpeedNow is the aggregate download speed right now: the sum of Speed over the
// tasks that are actually RUNNING, and nothing else.
//
// IT IS DELIBERATELY NOT Counters().Speed (app_queue.go:1373-1377), and the
// difference is not a detail. Counters sums every non-terminal task, running or
// not, and drops disabled links entirely - both of which are right for what
// Counters is for (an ETA over what is still owed). They are wrong here for one
// reason: the browser draws the LIVE half of this same curve by summing
// `status === 'running'` and nothing else (web/src/pages/Dashboard.tsx:46 and
// web/src/components/QuickSettings.tsx:217). If the seeded half were measured
// by a different rule, there would be a visible step at the join between the
// history and the live tail - a step that changed size depending on how many
// links happen to be disabled, and that nobody looking at it could explain.
// Two halves of one curve have to be one measurement.
//
// It walks a.tasks rather than a.active for the same kind of reason. Iterating
// a.active would be cheaper (it is only the dispatched ones), but a.active is
// documented as "dispatched and not yet terminal/paused" (app.go:361), which is
// not provably the same set as "Status == StatusRunning" - it is a dispatch
// ledger, not a status index. Cheapness is not worth a curve that is quietly
// the wrong number; speedhistory_test.go cross-checks the two instead.
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

// sampleSpeedLoop writes one reading a second for the life of the process.
//
// Spawned from New through a.spawn, so a.wg.Done is the wrapper's job and not
// this function's, and it selects on a.ctx.Done() the way budgetLoop
// (app_budget.go:243-255) does - which is the whole of its shutdown contract.
func (a *App) sampleSpeedLoop() {
	st := a.speedHistoryFor()
	// Dropped when this returns rather than left to accumulate: see the file
	// header on why this one entry is removed where app_activity.go's is not.
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

// sample records one reading taken at `now`, filling in whatever the ticker
// missed first.
//
// A method on the state rather than on *App, and split out from the loop, for
// two reasons. It takes nothing from the App but the figure it is handed, and
// the tests can then drive a state of their OWN with fabricated times instead
// of sleeping through real ones - which matters more than convenience here,
// because a test that drove a live App's state would be a second writer of
// st.bucket and st.last racing the sampler goroutine that owns them.
//
// It is called with exactly what the loop calls it with (SpeedNow's own figure
// and the tick's own instant), so nothing a test builds through it is a state
// the running program cannot reach.
func (st *speedHistoryState) sample(v int64, now time.Time) {
	st.fillSpeedGap(now)
	st.recordFine(v, now)
	st.last = now
}

// fillSpeedGap writes zeros for the steps the ticker did not fire for.
//
// This is the part that matters on a machine that runs for years rather than
// for an afternoon. A 1 s ticker does not fire while a laptop is suspended, a
// container is stopped or a hypervisor has the guest paused; it fires once on
// wake and carries on as if nothing happened. Without this, the sample from
// before the suspend and the sample after it sit ADJACENT in the ring, four
// hours apart, and the hour view draws one continuous line straight across a
// gap that swallowed the entire window. The same code covers an operator
// moving the clock forward.
//
// The fill is capped at each ring's own capacity, because writing more zeros
// than the ring holds only costs time - everything before the last `cap` of
// them has already been pushed out. It is logged once per gap, through the
// standard logger, which puts the line in internal/logring and therefore in the
// diagnostics bundle for free.
//
// A clock moved BACKWARDS produces a negative elapsed, no fill, and one sample
// stamped earlier than its predecessor. That is left alone on purpose: the
// alternative is guessing which of the two clocks was right, and one crooked
// stamp on recordingSince is a far smaller lie than a fabricated hour of
// history.
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
	// The half-built coarse bucket goes with it. Averaging three readings from
	// before a four hour suspend with seven from after it would produce one
	// ten-second bucket describing neither.
	st.bucket = st.bucket[:0]

	coarse := min(int(elapsed/speedCoarseStep), speedCoarseSlots)
	for i := coarse; i >= 1; i-- {
		st.coarse.push(0, now.Add(-time.Duration(i)*speedCoarseStep))
	}

	log.Printf("speed history: filled %d missed samples as idle after a %ds gap "+
		"(the process was suspended, or the clock moved)", fine, int(elapsed.Seconds()))
}

// recordFine puts one reading into the fine ring and, every tenth one, closes a
// coarse bucket with the MEAN of the ten. See the file header for why the mean
// and not the tick's own value.
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

// SpeedHistory is the snapshot GET /api/stats/speed serves, and the three
// counts the diagnostics bundle reads out of it.
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
	// The older of the two edges, ignoring whichever ring is still empty - see
	// RecordingSince's own comment on the struct.
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
