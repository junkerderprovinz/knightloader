package app

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/settings"
)

// newMaintApp returns an app on a fresh data directory whose store already
// holds a few rows, so reported sizes are real.
func newMaintApp(t *testing.T) *App {
	t.Helper()
	a, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { a.Close() })
	for i := 0; i < 20; i++ {
		if err := a.Store.Save(&core.Task{
			ID:        fmt.Sprintf("seed-%02d", i),
			URL:       fmt.Sprintf("https://host.example/seed-%02d.bin", i),
			Name:      fmt.Sprintf("seed-%02d.bin", i),
			Status:    core.StatusPaused,
			CreatedAt: time.Now(),
		}); err != nil {
			t.Fatal(err)
		}
	}
	return a
}

// waitForIdle blocks until no maintenance pass is in flight.
func waitForIdle(t *testing.T, a *App) MaintenanceState {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for {
		st := a.MaintenanceState()
		if st.Running == "" {
			return st
		}
		if time.Now().After(deadline) {
			t.Fatalf("a %s pass was still running after 30 seconds", st.Running)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// TestStorageInfoNamesBothFilesAndTheTempVolume also covers settings.json being
// absent on an install that never saved its settings.
func TestStorageInfoNamesBothFilesAndTheTempVolume(t *testing.T) {
	a := newMaintApp(t)

	info := a.StorageInfo()
	if info.StorePath != a.Store.Path() {
		t.Errorf("storePath = %q, want the store's own %q", info.StorePath, a.Store.Path())
	}
	if info.StoreBytes <= 0 {
		t.Errorf("storeBytes = %d for a database with twenty rows in it", info.StoreBytes)
	}
	if info.TempDir == "" {
		t.Error("tempDir is empty; the compaction's scratch copy would be reported as going nowhere")
	}
	if info.SettingsPresent {
		t.Errorf("settingsPresent is true on an install that has never saved a settings page (%s)", info.SettingsPath)
	}
	if info.SettingsBytes != 0 {
		t.Errorf("settingsBytes = %d while the file does not exist", info.SettingsBytes)
	}

	if _, err := a.Settings.Set(settings.Defaults()); err != nil {
		t.Fatal(err)
	}
	saved := a.StorageInfo()
	if !saved.SettingsPresent {
		t.Errorf("settingsPresent is still false after a save; %s", saved.SettingsPath)
	}
	if saved.SettingsBytes <= 0 {
		t.Errorf("settingsBytes = %d after a save", saved.SettingsBytes)
	}
}

func TestAFreshInstallHasNoLastRun(t *testing.T) {
	a := newMaintApp(t)
	st := a.MaintenanceState()
	if st.Last != nil {
		t.Errorf("a fresh install reports a last run: %+v", *st.Last)
	}
	if st.Running != "" {
		t.Errorf("a fresh install reports %q running", st.Running)
	}
	if st.NextRunAt != nil {
		t.Errorf("a fresh install reports a next run at %v while the interval is 0", *st.NextRunAt)
	}
	if st.IntervalDays != 0 {
		t.Errorf("intervalDays = %d on a fresh install, want 0 so an update does not start anything by itself", st.IntervalDays)
	}
}

func TestCheckRecordsAVerdictThatSurvivesTheProcess(t *testing.T) {
	a := newMaintApp(t)
	if err := a.StartMaintenance(MaintenanceCheck); err != nil {
		t.Fatal(err)
	}
	st := waitForIdle(t, a)
	if st.Last == nil {
		t.Fatal("nothing was recorded after a check")
	}
	if !st.Last.OK {
		t.Errorf("a healthy database came back not OK: %+v", *st.Last)
	}
	if st.Last.Kind != MaintenanceCheck {
		t.Errorf("kind = %q, want %q", st.Last.Kind, MaintenanceCheck)
	}
	if len(st.Last.Problems) != 0 {
		t.Errorf("a healthy database reported problems: %v", st.Last.Problems)
	}
	if st.Last.Problems == nil {
		t.Error("problems is nil rather than an empty slice; it goes into JSON, where nil is null")
	}
	if st.Last.At.IsZero() {
		t.Error("the run has no timestamp")
	}

	raw, err := os.ReadFile(filepath.Join(a.DataDir, maintenanceFile))
	if err != nil {
		t.Fatalf("the record was not written to %s: %v", maintenanceFile, err)
	}
	var rec maintenanceRecord
	if err := json.Unmarshal(raw, &rec); err != nil {
		t.Fatalf("the record on disk is not readable JSON: %v (%s)", err, raw)
	}
	if rec.Last == nil || !rec.Last.OK {
		t.Errorf("the record on disk does not carry the verdict: %s", raw)
	}
	if rec.DBTag == 0 {
		t.Error("the record on disk carries no database stamp, so it could never tell a restored database from this one")
	}
}

func TestCompactRecordsBothSizes(t *testing.T) {
	a := newMaintApp(t)
	// Written and deleted rows leave free pages to reclaim.
	padding := strings.Repeat("y", 2048)
	for i := 0; i < 300; i++ {
		if err := a.Store.Save(&core.Task{
			ID: fmt.Sprintf("bulk-%03d", i), URL: "https://host.example/b.bin",
			Name: "b.bin", Comment: padding, Status: core.StatusPaused, CreatedAt: time.Now(),
		}); err != nil {
			t.Fatal(err)
		}
	}
	for i := 0; i < 300; i++ {
		if err := a.Store.Delete(fmt.Sprintf("bulk-%03d", i)); err != nil {
			t.Fatal(err)
		}
	}
	before := a.MaintenanceState()
	if before.Storage.StoreReclaimableBytes <= 0 {
		t.Fatalf("300 deleted rows left nothing reclaimable (%+v); this test cannot see what it measures", before.Storage)
	}

	if err := a.StartMaintenance(MaintenanceCompact); err != nil {
		t.Fatal(err)
	}
	st := waitForIdle(t, a)
	if st.Last == nil || st.Last.Kind != MaintenanceCompact {
		t.Fatalf("no compaction was recorded: %+v", st.Last)
	}
	if !st.Last.OK {
		t.Fatalf("the compaction failed: %+v", *st.Last)
	}
	if st.Last.BytesBefore <= 0 || st.Last.BytesAfter <= 0 {
		t.Fatalf("bytesBefore = %d, bytesAfter = %d; the page would say it gave back the whole file",
			st.Last.BytesBefore, st.Last.BytesAfter)
	}
	if st.Last.BytesAfter >= st.Last.BytesBefore {
		t.Errorf("the file was %d bytes and is %d after compacting; nothing came back", st.Last.BytesBefore, st.Last.BytesAfter)
	}
	if st.Storage.StoreReclaimableBytes >= before.Storage.StoreReclaimableBytes {
		t.Errorf("the free space inside the file is %d after compacting and was %d before",
			st.Storage.StoreReclaimableBytes, before.Storage.StoreReclaimableBytes)
	}
}

// TestASecondRunIsRefused sets the claim directly instead of racing two real
// passes, which a fast database would make meaningless.
func TestASecondRunIsRefused(t *testing.T) {
	a := newMaintApp(t)
	st := a.maintenanceStateFor()
	st.mu.Lock()
	st.running = MaintenanceCompact
	st.mu.Unlock()

	for _, kind := range []MaintenanceKind{MaintenanceCheck, MaintenanceCompact, MaintenanceAnalyze} {
		if err := a.StartMaintenance(kind); !errors.Is(err, ErrMaintenanceBusy) {
			t.Errorf("starting %q while a compaction is running returned %v, want ErrMaintenanceBusy", kind, err)
		}
	}
	if got := a.MaintenanceState().Running; got != MaintenanceCompact {
		t.Errorf("running = %q while a compaction is claimed, want %q", got, MaintenanceCompact)
	}

	st.mu.Lock()
	st.running = ""
	st.mu.Unlock()
	if err := a.StartMaintenance(MaintenanceAnalyze); err != nil {
		t.Fatalf("starting once the claim is released: %v", err)
	}
	waitForIdle(t, a)
}

// TestStateIsAnswerableWhileSomethingIsRunning checks that no pragma is issued
// while a pass holds the connection. Timing a real compaction would prove
// nothing on a test-sized database, so a sentinel figure is planted and must
// come back unchanged.
func TestStateIsAnswerableWhileSomethingIsRunning(t *testing.T) {
	a := newMaintApp(t)
	primed := a.MaintenanceState()
	if primed.Storage.StoreReclaimableBytes == sentinelReclaimable {
		t.Fatalf("the sentinel %d is a value this database really has; pick another", sentinelReclaimable)
	}

	st := a.maintenanceStateFor()
	st.mu.Lock()
	st.lastReclaimable = sentinelReclaimable
	st.running = MaintenanceCompact
	st.mu.Unlock()
	defer func() {
		st.mu.Lock()
		st.running = ""
		st.mu.Unlock()
	}()

	done := make(chan MaintenanceState, 1)
	go func() { done <- a.MaintenanceState() }()
	select {
	case got := <-done:
		if got.Running != MaintenanceCompact {
			t.Errorf("running = %q, want %q", got.Running, MaintenanceCompact)
		}
		if got.Storage.StoreBytes <= 0 {
			t.Error("the file size was not reported while a pass was running; it comes from os.Stat and never needs the database")
		}
		if got.Storage.StoreReclaimableBytes != sentinelReclaimable {
			t.Errorf("the free-space figure came back as %d rather than the remembered %d, so it was re-read from the "+
				"database, which during a real compaction means this read waits for the whole rewrite",
				got.Storage.StoreReclaimableBytes, sentinelReclaimable)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("reading the maintenance state blocked while a pass was claimed; the page could never see its own job finish")
	}
}

// sentinelReclaimable is a byte count no test database here can report.
const sentinelReclaimable int64 = 123456789

// TestARestoredDatabaseDropsTheLastRun simulates a restored database by giving
// it a different stamp, which a file from elsewhere would carry.
func TestARestoredDatabaseDropsTheLastRun(t *testing.T) {
	a := newMaintApp(t)
	if err := a.StartMaintenance(MaintenanceCheck); err != nil {
		t.Fatal(err)
	}
	if st := waitForIdle(t, a); st.Last == nil {
		t.Fatal("nothing was recorded after a check")
	}

	if err := a.Store.SetTag(999_999); err != nil {
		t.Fatal(err)
	}
	if got := a.MaintenanceState().Last; got != nil {
		t.Errorf("the verdict is still being reported about a database it was not taken on: %+v", *got)
	}

	// Withheld, not deleted.
	raw, err := os.ReadFile(filepath.Join(a.DataDir, maintenanceFile))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"kind": "check"`) {
		t.Errorf("the record on disk lost the run it had written down: %s", raw)
	}
}

func TestTheScheduleArmsBeforeItFires(t *testing.T) {
	a := newMaintApp(t)

	a.runDBMaintenanceIfDue()
	if st := a.MaintenanceState(); st.NextRunAt != nil || st.Running != "" {
		t.Fatalf("an interval of 0 armed or started something: next=%v running=%q", st.NextRunAt, st.Running)
	}

	cfg := a.Settings.Get()
	cfg.MaintenanceIntervalDays = 30
	if _, err := a.Settings.Set(cfg); err != nil {
		t.Fatal(err)
	}

	// The first tick after switching it on arms without running.
	a.runDBMaintenanceIfDue()
	st := a.MaintenanceState()
	if st.Running != "" {
		t.Errorf("switching the schedule on started a %q pass immediately", st.Running)
	}
	if st.Last != nil {
		t.Errorf("switching the schedule on recorded a run: %+v", *st.Last)
	}
	if st.NextRunAt == nil {
		t.Fatal("switching the schedule on armed nothing, so the next run would never be due")
	}
	if until := time.Until(*st.NextRunAt); until < 29*24*time.Hour {
		t.Errorf("the first run is %v away, want a whole interval; the button is there for \"now\"", until)
	}

	first := *st.NextRunAt
	a.runDBMaintenanceIfDue()
	if again := a.MaintenanceState().NextRunAt; again == nil || !again.Equal(first) {
		t.Errorf("the next run moved from %v to %v on an ordinary tick", first, again)
	}

	cfg.MaintenanceIntervalDays = 0
	if _, err := a.Settings.Set(cfg); err != nil {
		t.Fatal(err)
	}
	a.runDBMaintenanceIfDue()
	if st := a.MaintenanceState(); st.NextRunAt != nil {
		t.Errorf("switching the schedule off left a run armed for %v", *st.NextRunAt)
	}
}

func TestTheArmedClockSurvivesARestart(t *testing.T) {
	dir := t.TempDir()
	a, err := New(dir)
	if err != nil {
		t.Fatal(err)
	}
	cfg := a.Settings.Get()
	cfg.MaintenanceIntervalDays = 90
	if _, err := a.Settings.Set(cfg); err != nil {
		t.Fatal(err)
	}
	a.runDBMaintenanceIfDue()
	armed := a.MaintenanceState().NextRunAt
	if armed == nil {
		t.Fatal("nothing was armed")
	}
	a.Close()

	again, err := New(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer again.Close()
	after := again.MaintenanceState().NextRunAt
	if after == nil {
		t.Fatal("the next run was forgotten across a restart; a box that reboots nightly would never reach the interval")
	}
	if !after.Equal(*armed) {
		t.Errorf("the next run moved from %v to %v across a restart", *armed, *after)
	}
}

func TestTheScheduleStandsDownWhileDownloadsRun(t *testing.T) {
	a := newMaintApp(t)
	cfg := a.Settings.Get()
	cfg.MaintenanceIntervalDays = 30
	if _, err := a.Settings.Set(cfg); err != nil {
		t.Fatal(err)
	}
	a.runDBMaintenanceIfDue() // arms

	st := a.maintenanceStateFor()
	st.mu.Lock()
	st.recordLocked(a.DataDir).ArmedAt = time.Now().AddDate(0, 0, -31)
	st.mu.Unlock()
	a.mu.Lock()
	a.tasks["running-one"] = &core.Task{ID: "running-one", Status: core.StatusRunning, Enabled: true}
	a.mu.Unlock()

	a.runDBMaintenanceIfDue()
	state := waitForIdle(t, a)
	if state.Last == nil {
		t.Fatal("the schedule stood down without saying so; nobody could tell it from a schedule that is off")
	}
	if state.Last.Skipped != SkippedDownloadsRunning {
		t.Fatalf("skipped = %q, want %q (%+v)", state.Last.Skipped, SkippedDownloadsRunning, *state.Last)
	}
	// Still due, so it runs on the first tick after the downloads stop.
	if state.NextRunAt == nil || state.NextRunAt.After(time.Now()) {
		t.Errorf("the skipped run was pushed out to %v; it should still be due", state.NextRunAt)
	}

	a.mu.Lock()
	delete(a.tasks, "running-one")
	a.mu.Unlock()
	a.runDBMaintenanceIfDue()
	ran := waitForIdle(t, a)
	if ran.Last == nil || ran.Last.Skipped != "" {
		t.Fatalf("the pass did not run once the downloads stopped: %+v", ran.Last)
	}
	if ran.Last.Kind != MaintenanceCheck {
		t.Errorf("the scheduled pass was a %q; a schedule reads before it rewrites", ran.Last.Kind)
	}
}

// TestTheScheduledPassOnlyChecksUnlessToldOtherwise: compaction needs room for
// a second copy of the database, so the schedule only compacts when asked to.
func TestTheScheduledPassOnlyChecksUnlessToldOtherwise(t *testing.T) {
	a := newMaintApp(t)
	cfg := a.Settings.Get()
	cfg.MaintenanceIntervalDays = 30
	if _, err := a.Settings.Set(cfg); err != nil {
		t.Fatal(err)
	}
	a.runDBMaintenanceIfDue()

	st := a.maintenanceStateFor()
	st.mu.Lock()
	st.recordLocked(a.DataDir).ArmedAt = time.Now().AddDate(0, 0, -31)
	st.mu.Unlock()

	a.runDBMaintenanceIfDue()
	done := waitForIdle(t, a)
	if done.Last == nil || done.Last.Kind != MaintenanceCheck {
		t.Fatalf("the scheduled pass was %+v, want a check", done.Last)
	}
	// Longer than the follow-up's two-second poll.
	time.Sleep(3 * time.Second)
	after := waitForIdle(t, a)
	if after.Last != nil && after.Last.Kind == MaintenanceCompact {
		t.Error("the scheduled pass compacted with compactOnSchedule off")
	}
}

func TestAnUnknownActionIsRefusedAtTheEdge(t *testing.T) {
	for _, s := range []string{"", "vacuum", "Check", "check ", "delete"} {
		if kind, ok := ParseMaintenanceKind(s); ok {
			t.Errorf("ParseMaintenanceKind(%q) accepted it as %q", s, kind)
		}
	}
	for _, want := range []MaintenanceKind{MaintenanceCheck, MaintenanceCompact, MaintenanceAnalyze} {
		if got, ok := ParseMaintenanceKind(string(want)); !ok || got != want {
			t.Errorf("ParseMaintenanceKind(%q) = %q, %v", want, got, ok)
		}
	}
}

// TestTheRecordSurvivesRubbishInItsFile: an unparsable record costs the verdict
// only, not the size readout and not the boot.
func TestTheRecordSurvivesRubbishInItsFile(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, maintenanceFile), []byte("{not json at all"), 0o600); err != nil {
		t.Fatal(err)
	}
	a, err := New(dir)
	if err != nil {
		t.Fatalf("an unreadable maintenance record stopped the app from starting: %v", err)
	}
	defer a.Close()

	st := a.MaintenanceState()
	if st.Last != nil {
		t.Errorf("a record nobody could parse produced a verdict: %+v", *st.Last)
	}
	if st.Storage.StoreBytes <= 0 {
		t.Error("the size readout was lost with the record; they are two different questions")
	}
	if err := a.StartMaintenance(MaintenanceAnalyze); err != nil {
		t.Fatal(err)
	}
	if done := waitForIdle(t, a); done.Last == nil || done.Last.Kind != MaintenanceAnalyze {
		t.Fatalf("the next run did not replace the unreadable record: %+v", done.Last)
	}
}
