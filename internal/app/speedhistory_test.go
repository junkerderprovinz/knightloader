package app

// The speed record (app_speedhistory.go): the measurement it takes, the ring it
// keeps it in, and what happens to that ring when the machine it is running on
// stops existing for four hours.
//
// Everything that drives the sampler builds a speedHistoryState of its own
// rather than reaching for a live App's. Two reasons, and the second is the
// hard one: fabricated instants make a four hour suspend a test that runs in
// microseconds instead of one that cannot be written at all, and a test writing
// st.bucket/st.last on a running App's state would be a second writer racing
// the sampler goroutine that owns those two fields - a data race the -race
// build would rightly fail on. newSpeedHistoryState is exactly what New hands
// the sampler, and sample() is exactly what the tick calls, so nothing below is
// a state the running program cannot reach.

import (
	"bytes"
	"encoding/json"
	"testing"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/core"
)

// speedTestBase is a fixed instant, so that a failure prints a stamp somebody
// can reason about instead of whatever time the suite happened to run at.
var speedTestBase = time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)

// TestSpeedNowIsTheSameMeasurementTheGraphDraws is the reason SpeedNow exists
// beside Counters instead of calling it.
//
// The browser sums Speed over `status === 'running'` and nothing else
// (web/src/pages/Dashboard.tsx:46, web/src/components/QuickSettings.tsx:217).
// Counters (app_queue.go) sums every non-terminal task and DROPS disabled ones,
// which is right for an ETA over what is still owed and wrong for a curve whose
// live half is drawn by the browser's rule. Measured by Counters, the seeded
// history and the live tail would meet at a visible step whose size depended on
// how many links somebody happened to have switched off - a step nobody looking
// at the graph could explain.
func TestSpeedNowIsTheSameMeasurementTheGraphDraws(t *testing.T) {
	a, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { a.Close() })

	// A running link the user has switched off is the case that separates the
	// two rules. Disabling does not stop a transfer that is already going -
	// Counters' own doc comment says the switch is about what is still owed -
	// so the bytes are genuinely arriving and the graph genuinely draws them.
	a.mu.Lock()
	a.tasks["on"] = &core.Task{ID: "on", Status: core.StatusRunning, Enabled: true, Speed: 100}
	a.tasks["off"] = &core.Task{ID: "off", Status: core.StatusRunning, Enabled: false, Speed: 50}
	a.tasks["waiting"] = &core.Task{ID: "waiting", Status: core.StatusQueued, Enabled: true}
	a.tasks["finished"] = &core.Task{ID: "finished", Status: core.StatusDone, Enabled: true}
	a.mu.Unlock()

	if got := a.SpeedNow(); got != 150 {
		t.Errorf("SpeedNow = %d, want 150 (100 + the 50 of a running link that is switched off, "+
			"which is what Dashboard.tsx:46 adds up)", got)
	}
	if got := a.Counters().Speed; got != 100 {
		t.Fatalf("Counters().Speed = %d, want 100 - this test's premise is that the two rules differ "+
			"on a disabled running link, and they no longer do", got)
	}
}

// TestSpeedNowAgreesWithTheDispatchLedgerOnAnOrdinaryTask is the cross-check
// app_speedhistory.go's own comment promises. SpeedNow walks a.tasks rather
// than a.active, because a.active is "dispatched and not yet terminal/paused"
// (app.go:361) - a dispatch ledger, not a status index, and not provably the
// same set. On the ordinary dispatched task the two agree, and this is what
// would notice the day they stop.
func TestSpeedNowAgreesWithTheDispatchLedgerOnAnOrdinaryTask(t *testing.T) {
	a, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { a.Close() })

	a.mu.Lock()
	a.tasks["one"] = &core.Task{ID: "one", Status: core.StatusRunning, Enabled: true, Speed: 700}
	a.tasks["two"] = &core.Task{ID: "two", Status: core.StatusRunning, Enabled: true, Speed: 300}
	a.active["one"] = true
	a.active["two"] = true
	viaLedger, _ := a.measureLocked()
	a.mu.Unlock()

	var ledger int64
	for _, v := range viaLedger {
		ledger += v
	}
	if got := a.SpeedNow(); got != ledger {
		t.Errorf("SpeedNow = %d but the budget meter's own sum over a.active = %d; "+
			"the two describe the same dispatched transfers and must not disagree", got, ledger)
	}
}

// TestACoarseBucketIsTheMeanOfItsTenFineSamples pins the rule that makes the
// hour view worth drawing. A ten second bucket that took whichever instant the
// tenth tick landed on turns an hour of bursty traffic into noise: a transfer
// alternating 40 MB/s and nothing draws as a solid 40 or a solid 0 depending on
// phase alone.
func TestACoarseBucketIsTheMeanOfItsTenFineSamples(t *testing.T) {
	st := newSpeedHistoryState()

	// Nine idle seconds and one busy one. The mean is 100, the last reading is
	// 1000, and the first reading is 0 - so this fails loudly whichever of the
	// three a broken implementation happens to keep.
	readings := []int64{0, 0, 0, 0, 0, 0, 0, 0, 0, 1000}
	for i, v := range readings {
		st.sample(v, speedTestBase.Add(time.Duration(i+1)*time.Second))
	}

	hour, _ := st.coarse.snapshot()
	if len(hour) != 1 {
		t.Fatalf("ten one second readings closed %d ten second buckets, want exactly 1", len(hour))
	}
	if hour[0] != 100 {
		t.Errorf("the bucket is %d; want 100, the mean of %v (1000 would be the tenth tick's own "+
			"value, 0 the first)", hour[0], readings)
	}
	// And the fine ring still holds all ten, unaveraged - the two resolutions
	// are two answers, not one answer computed twice.
	if recent, _ := st.fine.snapshot(); len(recent) != 10 || recent[9] != 1000 {
		t.Errorf("the fine ring holds %v, want all ten readings with 1000 last", recent)
	}
}

// TestTheFineRingIsBoundedByConstruction is the promise that lets the route do
// without a ?limit (routes_stats.go:14-18 states the same rule for the volume
// curves). The ring is exactly as long as it says it is, on an instance that
// booted a second ago and on one that has been running for a year.
func TestTheFineRingIsBoundedByConstruction(t *testing.T) {
	st := newSpeedHistoryState()

	const extra = 10
	total := speedFineSlots + extra
	for i := 1; i <= total; i++ {
		st.sample(int64(i), speedTestBase.Add(time.Duration(i)*time.Second))
	}

	recent, since := st.fine.snapshot()
	if len(recent) != speedFineSlots {
		t.Fatalf("%d readings into a %d slot ring left %d", total, speedFineSlots, len(recent))
	}
	// Oldest first, with the first `extra` readings dropped off the front -
	// which is what makes an abscissa drawable straight from the order.
	if recent[0] != extra+1 {
		t.Errorf("the oldest kept reading is %d, want %d (the first %d fell off the front)",
			recent[0], extra+1, extra)
	}
	if recent[len(recent)-1] != int64(total) {
		t.Errorf("the newest kept reading is %d, want %d", recent[len(recent)-1], total)
	}
	for i := 1; i < len(recent); i++ {
		if recent[i] != recent[i-1]+1 {
			t.Fatalf("the ring is not in order at %d: %v", i, recent[i-3:i+1])
		}
	}
	// And the stamp follows the front of the ring rather than the boot: an
	// instance up for a year must not report that it has been recording since
	// last spring, because everything before the last two minutes is gone.
	want := speedTestBase.Add(time.Duration(extra+1) * time.Second)
	if !since.Equal(want) {
		t.Errorf("the oldest kept reading is stamped %v, want %v", since, want)
	}
}

// TestOrdinaryTickerLatenessIsNotTreatedAsAGap is the other half of the gap
// check. A busy machine delivers a 1 s tick late all the time, and a sampler
// that wrote an idle reading every time it did would draw a curve full of
// notches that describe the scheduler rather than the traffic.
func TestOrdinaryTickerLatenessIsNotTreatedAsAGap(t *testing.T) {
	st := newSpeedHistoryState()
	st.sample(1000, speedTestBase.Add(time.Second))
	// Three steps between the two readings: late, and still within tolerance.
	st.sample(2000, speedTestBase.Add(4*time.Second))

	recent, _ := st.fine.snapshot()
	if len(recent) != 2 {
		t.Fatalf("a three second stretch produced %d readings (%v), want the 2 that were actually "+
			"taken - ordinary lateness is not an outage", len(recent), recent)
	}
}

// TestASuspendedProcessGetsZerosAndNotAStraightLine is the case that matters on
// a machine that runs for years rather than for an afternoon.
//
// A 1 s ticker does not fire while a laptop is asleep, a container is stopped or
// a hypervisor has the guest paused. It fires once on wake and carries on as if
// nothing happened, so without the fill the reading from before the suspend and
// the reading after it sit ADJACENT in the ring - and the hour view draws one
// continuous line straight across a gap that swallowed the entire window.
func TestASuspendedProcessGetsZerosAndNotAStraightLine(t *testing.T) {
	st := newSpeedHistoryState()
	st.sample(5_000_000, speedTestBase.Add(time.Second))

	// Four hours later the lid opens and the ticker fires once.
	st.sample(6_000_000, speedTestBase.Add(4*time.Hour))

	recent, _ := st.fine.snapshot()
	if len(recent) != speedFineSlots {
		t.Fatalf("the fine ring holds %d readings after a four hour gap, want the full %d - "+
			"anything less means the missing steps were not written at all", len(recent), speedFineSlots)
	}
	if last := recent[len(recent)-1]; last != 6_000_000 {
		t.Errorf("the newest reading is %d, want the 6000000 taken on wake", last)
	}
	for i := 0; i < len(recent)-1; i++ {
		if recent[i] != 0 {
			t.Fatalf("reading %d of the fine ring is %d; everything before the wake is time this "+
				"process did not exist for and must read as idle", i, recent[i])
		}
	}

	// The hour ring is longer than the gap is deep, so all of it is idle - and
	// crucially the 5 MB/s from before the suspend is nowhere in it.
	hour, _ := st.coarse.snapshot()
	if len(hour) != speedCoarseSlots {
		t.Fatalf("the hour ring holds %d buckets after a four hour gap, want the full %d",
			len(hour), speedCoarseSlots)
	}
	for i, v := range hour {
		if v != 0 {
			t.Fatalf("bucket %d of the hour ring is %d, want an hour of idle", i, v)
		}
	}
}

// TestAGapDoesNotAverageAcrossItself is the half-built bucket. Three readings
// taken before a four hour suspend and seven taken after it would otherwise be
// averaged into one ten second bucket describing neither.
func TestAGapDoesNotAverageAcrossItself(t *testing.T) {
	st := newSpeedHistoryState()
	for i := 1; i <= 3; i++ {
		st.sample(9_000_000, speedTestBase.Add(time.Duration(i)*time.Second))
	}
	woke := speedTestBase.Add(4 * time.Hour)
	for i := 0; i < 10; i++ {
		st.sample(1_000, woke.Add(time.Duration(i)*time.Second))
	}

	hour, _ := st.coarse.snapshot()
	if len(hour) == 0 {
		t.Fatal("no hour buckets at all after ten readings past the wake")
	}
	if last := hour[len(hour)-1]; last != 1_000 {
		t.Errorf("the first bucket after the wake is %d, want 1000 - the three 9 MB/s readings from "+
			"before the suspend were folded into it", last)
	}
}

// TestAnEmptyRecordCrossesTheWireAsArraysAndNotNull is trap 9, and it is the
// one that bites at the worst possible moment: a ring that has never been
// written answers "recent": null if it is built with `var out []int64`, and the
// client's `.length` throws on exactly the first load after a restart - which
// is the moment this whole feature exists for.
func TestAnEmptyRecordCrossesTheWireAsArraysAndNotNull(t *testing.T) {
	st := newSpeedHistoryState()
	recent, since := st.fine.snapshot()
	if recent == nil {
		t.Fatal("an empty fine ring snapshots as a nil slice, which marshals as null")
	}
	if len(recent) != 0 {
		t.Fatalf("an empty fine ring snapshots as %v", recent)
	}
	if !since.IsZero() {
		t.Errorf("an empty fine ring reports a start of %v; nothing has been recorded", since)
	}

	hour, _ := st.coarse.snapshot()
	out, err := json.Marshal(SpeedHistory{Recent: recent, Hour: hour})
	if err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{`"recent":null`, `"hour":null`} {
		if bytes.Contains(out, []byte(bad)) {
			t.Errorf("an empty record marshals as %s", out)
		}
	}
	if bytes.Contains(out, []byte(`"recordingSince"`)) {
		t.Errorf("an empty record carries a recordingSince: %s", out)
	}
}

// TestSpeedHistoryDeclaresItsOwnBounds pins the document App.SpeedHistory
// assembles, on a real App, with whatever its sampler has or has not managed to
// record by the time the test runs. Everything asserted here is true at every
// instant of that App's life, which is the point: a client reads the caps and
// the steps out of the answer rather than carrying a second copy of them.
func TestSpeedHistoryDeclaresItsOwnBounds(t *testing.T) {
	a, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { a.Close() })

	got := a.SpeedHistory()
	if got.Recent == nil || got.Hour == nil {
		t.Fatal("SpeedHistory hands back a nil slice, which marshals as null")
	}
	if got.RecentStep != 1 || got.RecentCap != speedFineSlots {
		t.Errorf("the fine record describes itself as %d s x %d, want 1 s x %d",
			got.RecentStep, got.RecentCap, speedFineSlots)
	}
	if got.HourStep != 10 || got.HourCap != speedCoarseSlots {
		t.Errorf("the hour record describes itself as %d s x %d, want 10 s x %d",
			got.HourStep, got.HourCap, speedCoarseSlots)
	}
	if len(got.Recent) > got.RecentCap || len(got.Hour) > got.HourCap {
		t.Errorf("%d of %d recent and %d of %d hourly: an answer longer than the cap it declares",
			len(got.Recent), got.RecentCap, len(got.Hour), got.HourCap)
	}
	// Present exactly when there is something to be recording since. This is
	// what tells "this instance was quiet" apart from "this instance had not
	// started yet", and a stamp on an empty record would collapse the two.
	recorded := len(got.Recent) > 0 || len(got.Hour) > 0
	if recorded != (got.RecordingSince != nil) {
		t.Errorf("%d recent samples with recordingSince = %v", len(got.Recent), got.RecordingSince)
	}
	if got.SampledAt.IsZero() {
		t.Error("no sampledAt, so nothing can say how old the answer is")
	}
}
