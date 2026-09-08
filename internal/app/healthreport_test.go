package app

// The subsystem report. What is tested here is the part that has no route in
// front of it: which rows exist, how a row's state is decided, how nine rows
// become one summary, and that the expensive half is shared while the cheap
// half is not.

import (
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/settings"
)

// newHealthApp is an app on a throwaway directory with no sidecar in the
// environment.
//
// KL_JD is cleared deliberately rather than left alone. Two of the nine rows
// read it, and a developer machine with a real JD running would otherwise make
// this file pass or fail depending on whether that container happens to be up -
// which is the definition of a test that reports something other than the code.
func newHealthApp(t *testing.T) *App {
	t.Helper()
	t.Setenv("KL_JD", "")
	a, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { a.Close() })
	return a
}

// TestEveryPartIsReportedEveryTimeAndInOrder is the guard for the failure the
// fixed order exists to prevent: a row that only appears when it has something
// to say is a row nobody can find when they go looking for it, and a series
// that stops being written keeps its last value in a monitoring system for the
// whole staleness window after the fault clears.
func TestEveryPartIsReportedEveryTimeAndInOrder(t *testing.T) {
	a := newHealthApp(t)
	rep := a.HealthReport()

	if len(rep.Subsystems) != len(subsystemOrder) {
		t.Fatalf("got %d rows, want %d: %+v", len(rep.Subsystems), len(subsystemOrder), rep.Subsystems)
	}
	seen := map[string]bool{}
	for i, row := range rep.Subsystems {
		if row.ID != subsystemOrder[i] {
			t.Errorf("row %d is %q, want %q - the order is fixed so a reader finds the same row in the same place",
				i, row.ID, subsystemOrder[i])
		}
		if seen[row.ID] {
			t.Errorf("%q is reported twice", row.ID)
		}
		seen[row.ID] = true
		if row.State == "" {
			t.Errorf("%q has no state at all; every row answers one of the five", row.ID)
		}
	}

	// A fresh instance has no relay, no sidecar and no accounts, and none of
	// that is a fault. If this ever fails the instance is being reported as
	// impaired for being freshly installed.
	if rep.Status == StateFailed {
		t.Errorf("a fresh instance reports itself as failed: %+v", rep.Subsystems)
	}
	if rep.Volumes == nil {
		t.Error("volumes came back nil; it encodes as JSON null and whatever walks it throws")
	}
	if rep.Tasks.WaitingBy == nil || rep.Tasks.FailedBy == nil {
		t.Error("a breakdown map came back nil; both are promised as objects")
	}
	if rep.StartedAt.IsZero() || rep.UptimeSeconds < 0 {
		t.Errorf("startedAt %v / uptime %ds - the process start is a fact this always knows", rep.StartedAt, rep.UptimeSeconds)
	}
}

// TestUnusedAndUnknownNeverWorsenTheSummary is the rule that makes the summary
// worth reading at all. Without it a fresh install - no relay, no JD, no
// yt-dlp, a kernel that cannot measure a disk - reports itself as impaired
// forever, and a status light that is never green is a status light nobody
// looks at.
func TestUnusedAndUnknownNeverWorsenTheSummary(t *testing.T) {
	rows := func(states ...SubsystemState) []Subsystem {
		out := make([]Subsystem, 0, len(states))
		for i, s := range states {
			out = append(out, Subsystem{ID: subsystemOrder[i], State: s})
		}
		return out
	}
	cases := []struct {
		name string
		in   []Subsystem
		want SubsystemState
	}{
		{"nothing set up", rows(StateUnused, StateUnused, StateUnknown, StateUnused), StateOK},
		{"all working", rows(StateOK, StateOK, StateOK), StateOK},
		{"one fault beside the unused", rows(StateUnused, StateDegraded, StateUnknown), StateDegraded},
		{"failed beats degraded whatever the order", rows(StateDegraded, StateFailed, StateOK), StateFailed},
		{"failed first", rows(StateFailed, StateDegraded), StateFailed},
		{"no rows at all", nil, StateOK},
	}
	for _, c := range cases {
		if got := worstState(c.in); got != c.want {
			t.Errorf("%s: worstState = %q, want %q", c.name, got, c.want)
		}
	}
}

// TestTheDiskRowReadsTheThreeAnswersRatherThanTwo covers the one place in this
// file where a zero means nothing at all.
//
// A volume this platform cannot measure carries Free/Used/Total of 0 that mean
// NOTHING (VolumeReport.Known says so at length), and reading those as "no
// space left" would report a full disk that does not exist - on the exact
// platform where every disk guard in the app is already holding nothing back.
func TestTheDiskRowReadsTheThreeAnswersRatherThanTwo(t *testing.T) {
	const gib = 1 << 30
	cfg := settings.Settings{DiskLowSpace: 10 * gib, DiskCriticalSpace: 2 * gib}

	cases := []struct {
		name       string
		rep        DiskReport
		wantState  SubsystemState
		wantRemedy string
	}{
		{
			name: "nothing could be measured",
			rep: DiskReport{Volumes: []VolumeReport{
				{Dir: "/downloads", Measured: "/downloads", Exists: true, Known: false},
			}},
			wantState: StateUnknown,
		},
		{
			name: "an unmeasurable volume is not a full one",
			rep: DiskReport{Volumes: []VolumeReport{
				// Zeroes everywhere and Known false: the shape that would read
				// as "0 bytes free" to anything that skipped the flag.
				{Dir: "/downloads", Measured: "/downloads", Exists: true, Known: false},
				{Dir: "/work", Measured: "/work", Exists: true, Known: true, Free: 500 * gib, Total: 1000 * gib},
			}},
			wantState: StateOK,
		},
		{
			name: "under the start floor holds new downloads back",
			rep: DiskReport{Volumes: []VolumeReport{
				{Dir: "/downloads", Measured: "/downloads", Exists: true, Known: true, Free: 5 * gib, Total: 1000 * gib},
			}},
			wantState:  StateDegraded,
			wantRemedy: remedyDiskLow,
		},
		{
			name: "under the pause floor puts running downloads back",
			rep: DiskReport{Volumes: []VolumeReport{
				{Dir: "/downloads", Measured: "/downloads", Exists: true, Known: true, Free: 1 * gib, Total: 1000 * gib},
			}},
			wantState:  StateFailed,
			wantRemedy: remedyDiskLow,
		},
		{
			name: "a folder that is not there describes a different disk",
			rep: DiskReport{Volumes: []VolumeReport{
				{Dir: "/mnt/tv", Measured: "/", Exists: false, Known: true, Free: 500 * gib, Total: 1000 * gib},
			}},
			wantState:  StateDegraded,
			wantRemedy: remedyDiskElsewhere,
		},
		{
			name: "a real shortage outranks the substitution caveat",
			rep: DiskReport{Volumes: []VolumeReport{
				{Dir: "/mnt/tv", Measured: "/", Exists: false, Known: true, Free: 1 * gib, Total: 1000 * gib},
			}},
			wantState:  StateFailed,
			wantRemedy: remedyDiskLow,
		},
		{
			name: "plenty of room everywhere",
			rep: DiskReport{Volumes: []VolumeReport{
				{Dir: "/downloads", Measured: "/downloads", Exists: true, Known: true, Free: 900 * gib, Total: 1000 * gib},
			}},
			wantState: StateOK,
		},
	}
	for _, c := range cases {
		got := diskSubsystem(c.rep, cfg)
		if got.State != c.wantState {
			t.Errorf("%s: state = %q, want %q", c.name, got.State, c.wantState)
		}
		if got.Remedy != c.wantRemedy {
			t.Errorf("%s: remedy = %q, want %q", c.name, got.Remedy, c.wantRemedy)
		}
	}

	// The floors are off by default (0 is the off state everywhere on that
	// settings card), and an unset floor must never make a volume look short.
	off := diskSubsystem(DiskReport{Volumes: []VolumeReport{
		{Dir: "/downloads", Measured: "/downloads", Exists: true, Known: true, Free: 0, Total: 1000 * gib},
	}}, settings.Settings{})
	if off.State != StateOK {
		t.Errorf("with both floors off the row reads %q; 0 means the floor is not set, not that everything is below it", off.State)
	}
}

// TestTheTaskWalkCountsOneListOnce is the guard on the breakdown maps.
//
// Three things at once, because they are one walk and a bug in it shows up as
// any of the three: every row lands in exactly one status bucket, a reason
// nothing is waiting on is absent rather than zero, and an unclassified failure
// is filed under a word instead of under the empty string core.Reason really
// carries.
func TestTheTaskWalkCountsOneListOnce(t *testing.T) {
	a := newHealthApp(t)
	a.mu.Lock()
	a.tasks = map[string]*core.Task{
		"run":       {ID: "run", Status: core.StatusRunning, Enabled: true},
		"wait-disk": {ID: "wait-disk", Status: core.StatusQueued, Enabled: true, Waiting: core.WaitingDisk},
		"wait-slot": {ID: "wait-slot", Status: core.StatusQueued, Enabled: true, Waiting: core.WaitingSlot},
		"wait-slot2": {
			ID: "wait-slot2", Status: core.StatusQueued, Enabled: true, Waiting: core.WaitingSlot,
		},
		// Queued with nothing holding it back: it is simply next.
		"wait-none": {ID: "wait-none", Status: core.StatusQueued, Enabled: true},
		// Queued and switched off. It counts in BOTH waiting and disabled on
		// purpose - the buckets have to add up to the list somebody is looking
		// at, and a row does not stop being queued because its toggle is off.
		"off":        {ID: "off", Status: core.StatusQueued, Enabled: false, Waiting: core.WaitingDisabled},
		"paused":     {ID: "paused", Status: core.StatusPaused, Enabled: true},
		"extracting": {ID: "extracting", Status: core.StatusExtracting, Enabled: true},
		"collected":  {ID: "collected", Status: core.StatusCollected, Enabled: true},
		"done":       {ID: "done", Status: core.StatusDone, Enabled: true},
		"gone":       {ID: "gone", Status: core.StatusError, Enabled: true, Reason: core.ReasonGone},
		"mystery":    {ID: "mystery", Status: core.StatusError, Enabled: true},
	}
	a.mu.Unlock()

	c := a.healthTaskCounts()
	for _, want := range []struct {
		name string
		got  int
		n    int
	}{
		{"running", c.Running, 1},
		{"waiting", c.Waiting, 5},
		{"paused", c.Paused, 1},
		{"extracting", c.Extracting, 1},
		{"collected", c.Collected, 1},
		{"disabled", c.Disabled, 1},
		{"failed", c.Failed, 2},
	} {
		if want.got != want.n {
			t.Errorf("%s = %d, want %d", want.name, want.got, want.n)
		}
	}
	if c.WaitingBy["slot"] != 2 || c.WaitingBy["disk"] != 1 || c.WaitingBy["disabled"] != 1 {
		t.Errorf("waitingBy = %v, want slot 2, disk 1, disabled 1", c.WaitingBy)
	}
	if _, ok := c.WaitingBy[""]; ok {
		t.Errorf("waitingBy carries an empty label: %v - a task nothing is holding back must not invent a reason", c.WaitingBy)
	}
	if len(c.WaitingBy) != 3 {
		t.Errorf("waitingBy = %v, want exactly the three reasons that have something behind them", c.WaitingBy)
	}
	if c.FailedBy["gone"] != 1 || c.FailedBy["unknown"] != 1 {
		t.Errorf("failedBy = %v, want gone 1 and unknown 1", c.FailedBy)
	}
	if _, ok := c.FailedBy[""]; ok {
		t.Errorf("failedBy carries an empty label: %v - core.ReasonUnknown is the empty string and must be named on the way out", c.FailedBy)
	}
}

// TestTheProbeIsSharedWhileTheCountsAreNot is the shape the whole file is built
// around, and it is the difference between a status page and an outage.
//
// App.JDStatus pings and then asks for a version against a fifteen-second
// client, so a sidecar that has gone away costs up to thirty seconds - and that
// is exactly the state this feature reports on. A scrape every fifteen seconds
// against an uncached probe stacks a goroutine per scrape until the monitoring
// declares the app down for a reason that is the monitoring. So the probe is
// shared. The task counts are not, because they are a map walk.
func TestTheProbeIsSharedWhileTheCountsAreNot(t *testing.T) {
	a := newHealthApp(t)

	first := a.HealthReport()
	a.mu.Lock()
	a.tasks["fresh"] = &core.Task{ID: "fresh", Status: core.StatusRunning, Enabled: true}
	a.mu.Unlock()
	second := a.HealthReport()

	if !second.SampledAt.Equal(first.SampledAt) {
		t.Errorf("the probe was taken again %v after the first; it is shared for %v",
			second.SampledAt.Sub(first.SampledAt), sysHealthTTL)
	}
	if second.Tasks.Running != first.Tasks.Running+1 {
		t.Errorf("running went %d -> %d; the counts are walked on every call, only the probe is shared",
			first.Tasks.Running, second.Tasks.Running)
	}

	// And the shared half really does go stale on its own rather than being
	// pinned forever: aged past the TTL, the next call takes a new reading.
	//
	// Checked against the AGED MARKER and not as "the third reading is later
	// than the first", which is what this said until the clock got a vote.
	// Windows' monotonic clock moves in half-millisecond steps, and a second
	// probe with no sidecar to wait for and a disk report still inside its own
	// five-second cache finishes well inside one of those - so both readings
	// come back byte-identical and After() answers false about a probe that
	// really was retaken. A marker a minute in the past cannot tie.
	st := a.sysHealthStateFor()
	aged := time.Now().Add(-2 * sysHealthTTL)
	st.mu.Lock()
	st.at, st.probe.sampledAt = aged, aged
	st.mu.Unlock()
	third := a.HealthReport()
	if third.SampledAt.Equal(aged) {
		t.Errorf("the probe still reads %v after the TTL expired; nothing would ever be re-read", third.SampledAt)
	}
	if third.SampledAt.Before(first.SampledAt) {
		t.Errorf("the new reading (%v) is older than the one it replaced (%v)", third.SampledAt, first.SampledAt)
	}
}

// TestManyReadersAtOnceIsTheOrdinaryCase is written for the race detector
// rather than for its assertions.
//
// It is not a stress test looking for a rare interleaving: several readers at
// once IS the ordinary case here. Every open browser tab polls this every ten
// seconds and a collector scrapes it on its own clock, all while the dispatcher
// is adding and finishing downloads. Three things in this file are shared
// across those callers - the probe cache, the seen/since pair behind
// Subsystem.Since, and the task map itself - and the second one is the one a
// reviewer would most easily assume is per-call.
func TestManyReadersAtOnceIsTheOrdinaryCase(t *testing.T) {
	a := newHealthApp(t)

	// The writers run FOR AS LONG AS the readers do rather than for a fixed
	// number of turns, and that is the whole difference between this test and
	// one that reports nothing. A writer counting to sixty finishes in
	// microseconds while a reader's first pass is doing a database ping and a
	// disk walk, so a fixed count would be over before the first report is
	// assembled: every later reader would then find the states already
	// recorded, take no write path at all, and the detector would have nothing
	// to look at. Proved by removing the lock in stampSince and watching this
	// fail.
	done := make(chan struct{})
	var writers sync.WaitGroup
	writers.Add(2)
	// A report taken while the list is being changed is the normal one, not the
	// odd one.
	go func() {
		defer writers.Done()
		for i := 0; ; i++ {
			select {
			case <-done:
				return
			default:
			}
			a.mu.Lock()
			a.tasks["churn"] = &core.Task{ID: "churn", Status: core.StatusRunning, Enabled: true}
			delete(a.tasks, "churn")
			a.mu.Unlock()
		}
	}()
	// The master switch, flipped under them. The queue row is the only one that
	// can change state without a second machine, and a state CHANGE is what
	// makes a reader WRITE to the shared seen/since pair instead of only
	// reading it. Somebody pressing stop and start while three tabs are polling
	// is exactly this.
	go func() {
		defer writers.Done()
		for i := 0; ; i++ {
			select {
			case <-done:
				return
			default:
			}
			a.SetHalted(i%2 == 0)
			// The probe expired as well, which is what the cache falling due
			// under several readers at once looks like.
			st := a.sysHealthStateFor()
			st.mu.Lock()
			st.at = time.Time{}
			st.mu.Unlock()
		}
	}()

	var readers sync.WaitGroup
	for i := 0; i < 8; i++ {
		readers.Add(1)
		go func() {
			defer readers.Done()
			for j := 0; j < 25; j++ {
				rep := a.HealthReport()
				if len(rep.Subsystems) != len(subsystemOrder) {
					t.Errorf("a concurrent reading came back with %d rows", len(rep.Subsystems))
					return
				}
			}
		}()
	}
	readers.Wait()
	close(done)
	writers.Wait()
}

// TestSinceIsAbsentUntilSomethingActuallyChanges keeps "JD has been down for
// two hours" from silently becoming "JD was down when we last looked".
//
// The first reading gets no timestamp at all, and that is the honest answer:
// this process knows what the state is and cannot know when it started, so
// stamping "now" would tell somebody a sidecar failed the moment they opened
// the page.
func TestSinceIsAbsentUntilSomethingActuallyChanges(t *testing.T) {
	a := newHealthApp(t)

	for _, row := range a.HealthReport().Subsystems {
		if !row.Since.IsZero() {
			t.Errorf("%s carries since=%v on the very first reading; nothing had changed yet", row.ID, row.Since)
		}
	}
	// A second reading of an unchanged instance still stamps nothing: the queue
	// row is assembled fresh every call and must not look like it just changed.
	for _, row := range a.HealthReport().Subsystems {
		if !row.Since.IsZero() {
			t.Errorf("%s carries since=%v though its state never moved", row.ID, row.Since)
		}
	}

	// Now move one row for real. Halting the queue is the one state change this
	// test can make without a second machine.
	a.SetHalted(true)
	var queue Subsystem
	for _, row := range a.HealthReport().Subsystems {
		if row.ID == SubsystemQueue {
			queue = row
		}
	}
	if queue.State != StateDegraded {
		t.Fatalf("the queue row reads %q with the queue halted, want %q", queue.State, StateDegraded)
	}
	if queue.Since.IsZero() {
		t.Error("the queue changed state and carries no since; there is nothing to say how long it has been stopped")
	}
}

// TestAHaltedQueueIsNeverAFailure is one line of policy worth pinning, because
// it is the one somebody will "fix" while adding a rule.
//
// A stopped queue is a choice somebody made or a timetable window they wrote.
// Reporting it as a failure pages an operator for a working pause, and on a box
// whose nightly window stops downloads it would page them every night.
func TestAHaltedQueueIsNeverAFailure(t *testing.T) {
	for _, q := range []QueueState{
		{Halted: true},
		{Quiet: true},
		{Halted: true, Quiet: true},
	} {
		if got := queueSubsystem(q); got.State != StateDegraded {
			t.Errorf("queue %+v reads %q, want %q", q, got.State, StateDegraded)
		}
	}
	if got := queueSubsystem(QueueState{}); got.State != StateOK {
		t.Errorf("a running queue reads %q, want %q", got.State, StateOK)
	}
}

// TestTheSidecarRowsSayNotInUseRatherThanBroken. Two of the nine rows read
// KL_JD, and an instance that never had a sidecar must not be told that one is
// down: "unused" is what makes the summary green on a perfectly ordinary
// install, and it is the row an operator has to be able to skip past.
func TestTheSidecarRowsSayNotInUseRatherThanBroken(t *testing.T) {
	a := newHealthApp(t)
	for _, row := range a.HealthReport().Subsystems {
		switch row.ID {
		case SubsystemJD, SubsystemCaptcha:
			if row.State != StateUnused {
				t.Errorf("%s reads %q with KL_JD unset, want %q (detail %q)", row.ID, row.State, StateUnused, row.Detail)
			}
			if row.Remedy != "" {
				t.Errorf("%s offers the remedy %q for a part nobody set up", row.ID, row.Remedy)
			}
		}
	}
}

// TestTheStoreRowAnswersFromTheDatabaseItself. The row exists to catch a data
// directory that has gone away underneath a running process, so it has to be a
// real query rather than "is the handle non-nil".
func TestTheStoreRowAnswersFromTheDatabaseItself(t *testing.T) {
	a := newHealthApp(t)
	if got := a.storeSubsystem(); got.State != StateOK {
		t.Fatalf("an open store reads %q (%s), want %q", got.State, got.Detail, StateOK)
	}

	// Closed under it, which is what an unmounted data volume looks like from
	// in here. The report has to say so in the far end's own words and offer
	// the one thing anybody can do about it.
	if err := a.Store.Close(); err != nil {
		t.Fatal(err)
	}
	got := a.storeSubsystem()
	if got.State != StateFailed {
		t.Errorf("a closed database reads %q, want %q", got.State, StateFailed)
	}
	if got.Remedy != remedyStoreFailed {
		t.Errorf("remedy = %q, want %q", got.Remedy, remedyStoreFailed)
	}
	if strings.TrimSpace(got.Detail) == "" {
		t.Error("the failure carries no detail; the database's own sentence is the only clue there is")
	}
}
