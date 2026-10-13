package app

// The speed record in app_speedhistory.go: the measurement it takes, the ring
// it keeps it in, and what that ring does when the machine stops existing for
// four hours.
//
// The tests drive a speedHistoryState of their own rather than a live App's.
// Fabricated instants turn a four hour suspend into a test that runs in
// microseconds, and writing st.bucket and st.last on a running App would race
// the sampler goroutine that owns them. newSpeedHistoryState is what New hands
// the sampler and sample is what the tick calls, so nothing below is a state
// the running program cannot reach.

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

// Why SpeedNow exists beside Counters. The browser sums Speed over
// `status === 'running'` (Dashboard.tsx, QuickSettings.tsx), while Counters sums
// every non-terminal task and drops disabled ones, which suits an ETA over what
// is still owed. Measured by Counters, the seeded history and the live tail
// would meet at a step whose size depends on how many links are switched off.
func TestSpeedNowIsTheSameMeasurementTheGraphDraws(t *testing.T) {
	a, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { a.Close() })

	// A running link the user has switched off separates the two rules.
	// Disabling does not stop a transfer that is already going, so the bytes
	// arrive and the graph draws them.
	a.mu.Lock()
	a.tasks["on"] = &core.Task{ID: "on", Status: core.StatusRunning, Enabled: true, Speed: 100}
	a.tasks["off"] = &core.Task{ID: "off", Status: core.StatusRunning, Enabled: false, Speed: 50}
	a.tasks["waiting"] = &core.Task{ID: "waiting", Status: core.StatusQueued, Enabled: true}
	a.tasks["finished"] = &core.Task{ID: "finished", Status: core.StatusDone, Enabled: true}
	a.mu.Unlock()

	if got := a.SpeedNow(); got != 150 {
		t.Errorf("SpeedNow = %d, want 150 (100 plus the 50 of a running link that is switched off, "+
			"which is what the dashboard adds up)", got)
	}
	if got := a.Counters().Speed; got != 100 {
		t.Fatalf("Counters().Speed = %d, want 100; this test rests on the two rules differing "+
			"on a disabled running link", got)
	}
}

// SpeedNow walks a.tasks rather than a.active, which is a dispatch ledger
// ("dispatched and not yet terminal or paused") and not a status index. On an
// ordinary dispatched task the two agree, and this notices the day they stop.
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
		t.Errorf("SpeedNow = %d but the budget meter's sum over a.active = %d; "+
			"the two describe the same dispatched transfers", got, ledger)
	}
}

// A ten second bucket that kept whichever instant the tenth tick landed on
// would turn bursty traffic into noise: a transfer alternating 40 MB/s and
// nothing draws as a solid 40 or a solid 0 depending on phase.
func TestACoarseBucketIsTheMeanOfItsTenFineSamples(t *testing.T) {
	st := newSpeedHistoryState()

	// Nine idle seconds and one busy one: the mean is 100, the last reading is
	// 1000 and the first is 0, so each of the three answers is distinguishable.
	readings := []int64{0, 0, 0, 0, 0, 0, 0, 0, 0, 1000}
	for i, v := range readings {
		st.sample(v, 0, speedTestBase.Add(time.Duration(i+1)*time.Second))
	}

	hour, _, _ := st.coarse.snapshot()
	if len(hour) != 1 {
		t.Fatalf("ten one second readings closed %d ten second buckets, want exactly 1", len(hour))
	}
	if hour[0] != 100 {
		t.Errorf("the bucket is %d; want 100, the mean of %v (1000 would be the tenth tick's own "+
			"value, 0 the first)", hour[0], readings)
	}
	// The fine ring still holds all ten unaveraged: the two resolutions are two
	// answers, not one answer computed twice.
	if recent, _, _ := st.fine.snapshot(); len(recent) != 10 || recent[9] != 1000 {
		t.Errorf("the fine ring holds %v, want all ten readings with 1000 last", recent)
	}
}

// The ring is as long as it says it is, on an instance that booted a second ago
// and on one that has run for a year, which is what lets the route do without a
// limit parameter.
func TestTheFineRingIsBoundedByConstruction(t *testing.T) {
	st := newSpeedHistoryState()

	const extra = 10
	total := speedFineSlots + extra
	for i := 1; i <= total; i++ {
		st.sample(int64(i), 0, speedTestBase.Add(time.Duration(i)*time.Second))
	}

	recent, _, since := st.fine.snapshot()
	if len(recent) != speedFineSlots {
		t.Fatalf("%d readings into a %d slot ring left %d", total, speedFineSlots, len(recent))
	}
	// Oldest first, with the first `extra` readings dropped off the front, so
	// the abscissa is drawable straight from the order.
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
	// The stamp follows the front of the ring rather than the boot, or an
	// instance up for a year would claim to have been recording all of it.
	want := speedTestBase.Add(time.Duration(extra+1) * time.Second)
	if !since.Equal(want) {
		t.Errorf("the oldest kept reading is stamped %v, want %v", since, want)
	}
}

// A busy machine delivers a one second tick late all the time, and a sampler
// that wrote an idle reading for each would draw notches describing the
// scheduler rather than the traffic.
func TestOrdinaryTickerLatenessIsNotTreatedAsAGap(t *testing.T) {
	st := newSpeedHistoryState()
	st.sample(1000, 0, speedTestBase.Add(time.Second))
	// Three steps between the two readings: late, and still within tolerance.
	st.sample(2000, 0, speedTestBase.Add(4*time.Second))

	recent, _, _ := st.fine.snapshot()
	if len(recent) != 2 {
		t.Fatalf("a three second stretch produced %d readings (%v), want the 2 that were taken; "+
			"ordinary lateness is not an outage", len(recent), recent)
	}
}

// A one second ticker does not fire while a laptop is asleep, a container is
// stopped or a hypervisor has the guest paused. It fires once on wake and
// carries on, so without the fill the readings from either side of the suspend
// sit next to each other and the hour view draws a line straight across it.
func TestASuspendedProcessGetsZerosAndNotAStraightLine(t *testing.T) {
	st := newSpeedHistoryState()
	st.sample(5_000_000, 0, speedTestBase.Add(time.Second))

	// Four hours later the lid opens and the ticker fires once.
	st.sample(6_000_000, 0, speedTestBase.Add(4*time.Hour))

	recent, _, _ := st.fine.snapshot()
	if len(recent) != speedFineSlots {
		t.Fatalf("the fine ring holds %d readings after a four hour gap, want the full %d; "+
			"anything less means the missing steps were not written", len(recent), speedFineSlots)
	}
	if last := recent[len(recent)-1]; last != 6_000_000 {
		t.Errorf("the newest reading is %d, want the 6000000 taken on wake", last)
	}
	for i := 0; i < len(recent)-1; i++ {
		if recent[i] != 0 {
			t.Fatalf("reading %d of the fine ring is %d; everything before the wake is time this "+
				"process did not exist for and reads as idle", i, recent[i])
		}
	}

	// The hour ring is longer than the gap is deep, so all of it is idle and the
	// 5 MB/s from before the suspend is nowhere in it.
	hour, _, _ := st.coarse.snapshot()
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

// The half-built bucket: three readings taken before a four hour suspend and
// seven after it would otherwise average into one bucket describing neither.
func TestAGapDoesNotAverageAcrossItself(t *testing.T) {
	st := newSpeedHistoryState()
	for i := 1; i <= 3; i++ {
		st.sample(9_000_000, 0, speedTestBase.Add(time.Duration(i)*time.Second))
	}
	woke := speedTestBase.Add(4 * time.Hour)
	for i := 0; i < 10; i++ {
		st.sample(1_000, 0, woke.Add(time.Duration(i)*time.Second))
	}

	hour, _, _ := st.coarse.snapshot()
	if len(hour) == 0 {
		t.Fatal("no hour buckets at all after ten readings past the wake")
	}
	if last := hour[len(hour)-1]; last != 1_000 {
		t.Errorf("the first bucket after the wake is %d, want 1000; the three 9 MB/s readings from "+
			"before the suspend were folded into it", last)
	}
}

// A ring that has never been written answers "recent": null if it is built with
// `var out []int64`, and the client's `.length` then throws on the first load
// after a restart.
func TestAnEmptyRecordCrossesTheWireAsArraysAndNotNull(t *testing.T) {
	st := newSpeedHistoryState()
	recent, _, since := st.fine.snapshot()
	if recent == nil {
		t.Fatal("an empty fine ring snapshots as a nil slice, which marshals as null")
	}
	if len(recent) != 0 {
		t.Fatalf("an empty fine ring snapshots as %v", recent)
	}
	if !since.IsZero() {
		t.Errorf("an empty fine ring reports a start of %v; nothing has been recorded", since)
	}

	hour, _, _ := st.coarse.snapshot()
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

// The document App.SpeedHistory assembles, on a real App and whatever its
// sampler has managed to record. Everything asserted holds at every instant of
// that App's life, so a client reads the caps and the steps out of the answer
// rather than carrying a second copy of them.
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
	// Present exactly when there is something to be recording since, which is
	// what tells a quiet instance apart from one that had not started yet.
	recorded := len(got.Recent) > 0 || len(got.Hour) > 0
	if recorded != (got.RecordingSince != nil) {
		t.Errorf("%d recent samples with recordingSince = %v", len(got.Recent), got.RecordingSince)
	}
	if got.SampledAt.IsZero() {
		t.Error("no sampledAt, so nothing can say how old the answer is")
	}
}

// A day bucket is five minutes of readings, and its mean is what the day curve
// draws against the planned limit.
func TestADayBucketIsTheMeanOfItsFiveMinutes(t *testing.T) {
	st := newSpeedHistoryState()

	// One busy minute in five: the mean is a fifth of the busy reading.
	for i := 1; i <= speedDayEvery; i++ {
		v := int64(0)
		if i > 240 {
			v = 5000
		}
		st.sample(v, 0, speedTestBase.Add(time.Duration(i)*time.Second))
	}

	day, _, closed := st.day.snapshot()
	if len(day) != 1 || day[0] != 1000 {
		t.Fatalf("five minutes of readings left the day ring at %v, want one bucket of 1000", day)
	}
	if want := speedTestBase.Add(speedDayStep); !closed.Equal(want) {
		t.Errorf("the bucket is stamped %v, want %v, the moment it closed", closed, want)
	}
}

// The day ring places its buckets by their number, so the readings a suspend
// swallowed have to be counted in or everything after it moves on the clock.
func TestASuspendKeepsTheDayBucketsOnTheClock(t *testing.T) {
	st := newSpeedHistoryState()
	for i := 1; i <= 120; i++ {
		st.sample(3000, 0, speedTestBase.Add(time.Duration(i)*time.Second))
	}
	// Two minutes in, the machine sleeps for twenty and then carries on.
	woke := speedTestBase.Add(22 * time.Minute)
	for i := 0; i < 180; i++ {
		st.sample(600, 0, woke.Add(time.Duration(i)*time.Second))
	}

	day, _, closed := st.day.snapshot()
	// 120 readings of 3000 and 180 idle ones, three idle buckets, and none of
	// the readings after the wake yet: the fifth bucket is still open.
	want := []int64{1200, 0, 0, 0}
	if len(day) != len(want) {
		t.Fatalf("the day ring holds %v, want %v", day, want)
	}
	for i := range want {
		if day[i] != want[i] {
			t.Fatalf("the day ring holds %v, want %v", day, want)
		}
	}
	if first := speedTestBase.Add(speedDayStep); !closed.Equal(first) {
		t.Errorf("the oldest bucket is stamped %v, want %v", closed, first)
	}
}

// A day is 288 buckets however long the instance has run.
func TestTheDayRingHoldsOneDay(t *testing.T) {
	st := newSpeedHistoryState()
	st.sample(1, 0, speedTestBase.Add(time.Second))
	st.sample(1, 0, speedTestBase.Add(30*time.Hour))

	day, _, _ := st.day.snapshot()
	if len(day) != speedDaySlots {
		t.Fatalf("thirty hours left %d buckets in the day ring, want %d", len(day), speedDaySlots)
	}
}

// SpeedDay says when its oldest bucket opened, which is what puts the curve on
// the clock, and says nothing while there is no bucket.
func TestSpeedDayDatesItsOldestBucket(t *testing.T) {
	a, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { a.Close() })

	got := a.SpeedDay()
	if got.Samples == nil || len(got.Samples) != 0 || got.From != nil {
		t.Fatalf("a fresh instance answers %+v, want no samples as an empty list and no start", got)
	}
	if got.Step != 300 || got.Cap != speedDaySlots {
		t.Errorf("the day record describes itself as %d s x %d, want 300 s x %d", got.Step, got.Cap, speedDaySlots)
	}

	closed := time.Now().Truncate(time.Second)
	a.speedHistoryFor().day.push(speedReading{down: 4096}, closed)
	got = a.SpeedDay()
	if len(got.Samples) != 1 || got.Samples[0] != 4096 {
		t.Fatalf("samples = %v, want the one bucket", got.Samples)
	}
	if got.From == nil || !got.From.Equal(closed.Add(-speedDayStep)) {
		t.Errorf("from = %v, want %v, five minutes before the bucket closed", got.From, closed.Add(-speedDayStep))
	}
}

// The upload rides in the same slots as the download, so every resolution
// averages it the same way and the two lines stay on one clock.
func TestUploadIsAveragedAlongsideTheDownload(t *testing.T) {
	st := newSpeedHistoryState()
	for i := 1; i <= speedDayEvery; i++ {
		up := int64(0)
		if i%10 == 0 {
			up = 3000
		}
		st.sample(5000, up, speedTestBase.Add(time.Duration(i)*time.Second))
	}

	recent, recentUp, _ := st.fine.snapshot()
	if len(recentUp) != len(recent) || recentUp[len(recentUp)-1] != 3000 {
		t.Errorf("the fine ring holds %d download and %v upload readings, want as many of each with 3000 last",
			len(recent), recentUp)
	}
	hour, hourUp, _ := st.coarse.snapshot()
	if len(hourUp) != len(hour) || hourUp[0] != 300 || hour[0] != 5000 {
		t.Errorf("the first hour bucket is %d down and %v up, want 5000 and a mean of 300", hour[0], hourUp)
	}
	day, dayUp, _ := st.day.snapshot()
	if len(day) != 1 || len(dayUp) != 1 || dayUp[0] != 300 {
		t.Errorf("the day ring holds %v down and %v up, want one bucket with an upload mean of 300", day, dayUp)
	}
}

// A suspend is idle in both directions; the upload from before it must not
// stand across the gap.
func TestASuspendFillsTheUploadWithZerosToo(t *testing.T) {
	st := newSpeedHistoryState()
	st.sample(0, 7000, speedTestBase.Add(time.Second))
	st.sample(0, 8000, speedTestBase.Add(time.Hour))

	_, up, _ := st.fine.snapshot()
	for i, v := range up[:len(up)-1] {
		if v != 0 {
			t.Fatalf("upload reading %d is %d after an hour's gap, want zero", i, v)
		}
	}
	if up[len(up)-1] != 8000 {
		t.Errorf("the newest upload reading is %d, want the 8000 taken on wake", up[len(up)-1])
	}
}

// A torrent sends only while it downloads or seeds. The reading on one that
// stopped is history, and adding it would draw a line that never falls.
func TestUploadNowCountsOnlyTorrentsThatDownloadOrSeed(t *testing.T) {
	a, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { a.Close() })

	a.mu.Lock()
	a.tasks["leech"] = &core.Task{ID: "leech", Status: core.StatusRunning, UploadSpeed: 100}
	a.tasks["seed"] = &core.Task{ID: "seed", Status: core.StatusDone, Seeding: true, UploadSpeed: 40}
	a.tasks["stopped"] = &core.Task{ID: "stopped", Status: core.StatusDone, UploadSpeed: 900}
	a.tasks["paused"] = &core.Task{ID: "paused", Status: core.StatusPaused, UploadSpeed: 800}
	now := time.Now()
	for id, t := range a.tasks {
		a.tally.readUpload(id, 0, now.Add(-time.Second))
		a.tally.readUpload(id, t.UploadSpeed, now)
	}
	a.mu.Unlock()

	if got := a.UploadNow(); got != 140 {
		t.Errorf("UploadNow = %d, want 140, the torrent downloading and the one seeding", got)
	}
}

// The task carries the rate the tally reads from two upload totals, which is
// what the browser sums for the live end of the line.
func TestATorrentUpdatePutsItsUploadRateOnTheTask(t *testing.T) {
	a, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { a.Close() })
	task := &core.Task{ID: "t1", Resolver: "torrent", Status: core.StatusDone, Seeding: true, Uploaded: 1000}
	a.mu.Lock()
	a.tasks[task.ID] = task
	a.mu.Unlock()

	a.onUpdate(task.ID, core.Update{Status: core.StatusDone, Torrent: &core.TorrentStats{Uploaded: 1000, Seeding: true}})
	time.Sleep(20 * time.Millisecond)
	a.onUpdate(task.ID, core.Update{Status: core.StatusDone, Torrent: &core.TorrentStats{Uploaded: 9000, Seeding: true}})

	a.mu.Lock()
	got := task.UploadSpeed
	a.mu.Unlock()
	if got <= 0 {
		t.Fatalf("UploadSpeed = %d after 8000 bytes went out, want a rate above zero", got)
	}
	if now := a.UploadNow(); now != got {
		t.Errorf("UploadNow = %d, want the task's %d", now, got)
	}
}
