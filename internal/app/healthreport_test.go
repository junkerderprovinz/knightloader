package app

// The subsystem report: which rows exist, how a row's state is decided, how
// the rows become one summary, and that the expensive probe is shared while the
// cheap counts are not.

import (
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/settings"
)

// newHealthApp is an app on a throwaway directory with no sidecar. KL_JD is
// cleared, since two rows read it and a developer's running JD would change the
// result.
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

// Every row appears every time in a fixed order: a row that only shows up with
// something to say cannot be found, and a series that stops being written keeps
// its last value in monitoring.
func TestEveryPartIsReportedEveryTimeAndInOrder(t *testing.T) {
	a := newHealthApp(t)
	rep := a.HealthReport()

	if len(rep.Subsystems) != len(subsystemOrder) {
		t.Fatalf("got %d rows, want %d: %+v", len(rep.Subsystems), len(subsystemOrder), rep.Subsystems)
	}
	seen := map[string]bool{}
	for i, row := range rep.Subsystems {
		if row.ID != subsystemOrder[i] {
			t.Errorf("row %d is %q, want %q; the order is fixed",
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

	// No relay, sidecar or accounts on a fresh instance is not a fault.
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
		t.Errorf("startedAt %v / uptime %ds; the process start is always known", rep.StartedAt, rep.UptimeSeconds)
	}
}

// Unused and unknown rows never worsen the summary, or a fresh install would
// never be green.
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

// An unmeasurable volume carries zeroes that mean nothing (see
// VolumeReport.Known) and must not read as a full disk.
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
				// Zeroes and Known false.
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

	// The floors are off by default (0), and an unset floor never makes a
	// volume look short.
	off := diskSubsystem(DiskReport{Volumes: []VolumeReport{
		{Dir: "/downloads", Measured: "/downloads", Exists: true, Known: true, Free: 0, Total: 1000 * gib},
	}}, settings.Settings{})
	if off.State != StateOK {
		t.Errorf("with both floors off the row reads %q; 0 means the floor is not set, not that everything is below it", off.State)
	}
}

// Every task lands in one status bucket, a reason nothing waits on is absent
// rather than zero, and an unclassified failure is filed under "unknown" rather
// than the empty string.
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
		// Queued with nothing holding it back.
		"wait-none": {ID: "wait-none", Status: core.StatusQueued, Enabled: true},
		// Queued and disabled: it counts as both waiting and disabled, since it
		// is still queued.
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
		t.Errorf("waitingBy carries an empty label: %v", c.WaitingBy)
	}
	if len(c.WaitingBy) != 3 {
		t.Errorf("waitingBy = %v, want exactly the three reasons that have something behind them", c.WaitingBy)
	}
	if c.FailedBy["gone"] != 1 || c.FailedBy["unknown"] != 1 {
		t.Errorf("failedBy = %v, want gone 1 and unknown 1", c.FailedBy)
	}
	if _, ok := c.FailedBy[""]; ok {
		t.Errorf("failedBy carries an empty label: %v; core.ReasonUnknown must be named", c.FailedBy)
	}
}

// The probe is shared, since a dead sidecar makes JDStatus take up to thirty
// seconds and uncached scrapes would pile up. The task counts are a map walk
// and are taken on every call.
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

	// Aged past the TTL, the next call takes a new reading. Compared against
	// the aged marker rather than the first reading, since Windows' coarse
	// clock can make two quick readings identical.
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

// Concurrent readers are the ordinary case (every open tab polls, a collector
// scrapes), so this runs under the race detector. The probe cache, the
// seen/since pair behind Subsystem.Since and the task map are all shared.
func TestManyReadersAtOnceIsTheOrdinaryCase(t *testing.T) {
	a := newHealthApp(t)

	// The writers run as long as the readers do; a fixed count would finish
	// before the first report and leave the write paths unexercised.
	done := make(chan struct{})
	var writers sync.WaitGroup
	writers.Add(2)
	// The task list changes during reports.
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
	// The master switch flips, so readers write the shared seen/since pair.
	go func() {
		defer writers.Done()
		for i := 0; ; i++ {
			select {
			case <-done:
				return
			default:
			}
			a.SetHalted(i%2 == 0)
			// The probe cache expires under several readers too.
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

// The first reading has no Since: the process knows the state but not when it
// began, and "now" would claim a failure started when the page was opened.
func TestSinceIsAbsentUntilSomethingActuallyChanges(t *testing.T) {
	a := newHealthApp(t)

	for _, row := range a.HealthReport().Subsystems {
		if !row.Since.IsZero() {
			t.Errorf("%s carries since=%v on the very first reading; nothing had changed yet", row.ID, row.Since)
		}
	}
	// An unchanged second reading stamps nothing either.
	for _, row := range a.HealthReport().Subsystems {
		if !row.Since.IsZero() {
			t.Errorf("%s carries since=%v though its state never moved", row.ID, row.Since)
		}
	}

	// Halting the queue changes one row.
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
		t.Error("the queue changed state and carries no since")
	}
}

// A halted queue is a choice or a timetable window, never a failure that would
// page an operator every night.
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

// Without KL_JD the sidecar rows read unused, not down.
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

// The store row runs a real query, so it catches a data directory that went
// away under a running process.
func TestTheStoreRowAnswersFromTheDatabaseItself(t *testing.T) {
	a := newHealthApp(t)
	if got := a.storeSubsystem(); got.State != StateOK {
		t.Fatalf("an open store reads %q (%s), want %q", got.State, got.Detail, StateOK)
	}

	// Closed underneath, like an unmounted data volume. The row carries the
	// database's own error and the remedy.
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
		t.Error("the failure carries no detail")
	}
}
