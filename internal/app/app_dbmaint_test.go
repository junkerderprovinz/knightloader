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

// newMaintApp is an app on a fresh data directory with the store already
// carrying a few rows, so the sizes it reports are real numbers rather than the
// size of an empty schema.
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

// waitForIdle blocks until no maintenance pass is in flight. Every test here
// starts real work on a real database, and asserting on the record before the
// goroutine has written it would be asserting on the previous answer.
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

// TestStorageInfoNamesBothFilesAndTheTempVolume covers the readout the page is
// built on, including the field that is easiest to get wrong: settings.json is
// NOT there on an install nobody has saved a settings page on, because
// settings.Load reads that file and never writes it. Reporting 0 bytes for it
// would be a claim about a file that does not exist.
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

	// And once it has been saved, both halves change together.
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

// TestAFreshInstallHasNoLastRun is the distinction the pointer in
// MaintenanceState exists for. "Nothing has ever run here" and "something ran
// and found nothing wrong" are different answers, and a zero-valued struct
// would render the second - a clean bill of health nobody earned.
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
		t.Errorf("intervalDays = %d on a fresh install, want 0 - an update must not start doing anything by itself", st.IntervalDays)
	}
}

// TestCheckRecordsAVerdictThatSurvivesTheProcess is the whole point of the
// record living in a file beside the database rather than in it: the one moment
// anybody needs to read "the check failed on the 3rd" is the moment the
// database will not open.
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

	// The file itself, read the way somebody would during an incident.
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

// TestCompactRecordsBothSizes pins the honest half of the compaction report.
// The free-page figure shown before a run is a floor, so the only truthful "you
// got this much back" is the difference between two measurements of the file.
func TestCompactRecordsBothSizes(t *testing.T) {
	a := newMaintApp(t)
	// Something worth reclaiming, produced the way an install produces it:
	// rows written and rows deleted.
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

// TestASecondRunIsRefused is the guard that keeps two rewrites from queueing on
// one connection - which is not two compactions, it is one compaction followed
// by a second, pointless one, with every write in the process frozen for the sum
// of both.
//
// The claim is taken directly rather than by racing two real passes, because
// the state being tested is exactly the state StartMaintenance itself sets one
// line before it returns; a test that had to win a race against a fast database
// would be a test that reports nothing on the machines where it loses.
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

	// Released again, and then it starts: a refusal that outlived the run would
	// be a button that never works again.
	st.mu.Lock()
	st.running = ""
	st.mu.Unlock()
	if err := a.StartMaintenance(MaintenanceAnalyze); err != nil {
		t.Fatalf("starting once the claim is released: %v", err)
	}
	waitForIdle(t, a)
}

// TestStateIsAnswerableWhileSomethingIsRunning is the reason the free-page
// figure is remembered rather than re-read. A compaction holds the store's one
// connection for the whole rewrite; a status route that issued a pragma would
// not answer until the rewrite was over, which is precisely the window the page
// is polling in. On a large database that is a settings page that hangs for ten
// minutes while claiming to be watching a job.
//
// IT PROVES THE CODE PATH, NOT THE CLOCK, and deliberately so. The obvious test
// - start a real compaction and time the next read - passes on any database
// small enough to test with, because the rewrite is over before the read is
// issued; it would report nothing at all on the machines that run it. So the
// remembered figure is set to a number no database in this test could produce,
// and the assertion is that the number comes back: if anything here asks the
// store while a pass is claimed, the real value arrives instead and this fails.
func TestStateIsAnswerableWhileSomethingIsRunning(t *testing.T) {
	a := newMaintApp(t)
	// Primed the way any page load primes it, so the sentinel below is
	// replacing a real reading rather than filling an empty slot.
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
			t.Errorf("the free-space figure came back as %d rather than the remembered %d - it was re-read from the "+
				"database, which during a real compaction means this read waits for the whole rewrite",
				got.Storage.StoreReclaimableBytes, sentinelReclaimable)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("reading the maintenance state blocked while a pass was claimed; the page could never see its own job finish")
	}
}

// sentinelReclaimable is a byte count no test database here reaches: page sizes
// are powers of two and free pages are counted in the low hundreds, so nothing
// SQLite can answer lands on it.
const sentinelReclaimable int64 = 123456789

// TestARestoredDatabaseDropsTheLastRun is trap 9 in one test. The record lives
// beside the database and internal/backup's ApplyPending replaces the database
// under it on the next boot, so without this the page would report "checked
// clean two days ago" about a file that arrived from a backup an hour ago.
//
// The database being replaced is simulated the only way it can be from in here:
// by stamping it with a different identity, which is exactly what a file that
// came from somewhere else carries.
func TestARestoredDatabaseDropsTheLastRun(t *testing.T) {
	a := newMaintApp(t)
	if err := a.StartMaintenance(MaintenanceCheck); err != nil {
		t.Fatal(err)
	}
	if st := waitForIdle(t, a); st.Last == nil {
		t.Fatal("nothing was recorded after a check")
	}

	// A different database under the same record.
	if err := a.Store.SetTag(999_999); err != nil {
		t.Fatal(err)
	}
	if got := a.MaintenanceState().Last; got != nil {
		t.Errorf("the verdict is still being reported about a database it was not taken on: %+v", *got)
	}

	// The record itself is untouched on disk - it is withheld, not deleted, so
	// somebody reading the file during an incident still finds what happened.
	raw, err := os.ReadFile(filepath.Join(a.DataDir, maintenanceFile))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"kind": "check"`) {
		t.Errorf("the record on disk lost the run it had written down: %s", raw)
	}
}

// TestTheScheduleArmsBeforeItFires is trap 10. Switching a schedule on at 15:00
// and having the database freeze thirty seconds later is not what anybody
// clicked, and an interval whose clock lives only in memory either never fires
// on a box that restarts nightly or fires on every boot.
func TestTheScheduleArmsBeforeItFires(t *testing.T) {
	a := newMaintApp(t)

	// Off: nothing is armed and nothing runs.
	a.runDBMaintenanceIfDue()
	if st := a.MaintenanceState(); st.NextRunAt != nil || st.Running != "" {
		t.Fatalf("an interval of 0 armed or started something: next=%v running=%q", st.NextRunAt, st.Running)
	}

	cfg := a.Settings.Get()
	cfg.MaintenanceIntervalDays = 30
	if _, err := a.Settings.Set(cfg); err != nil {
		t.Fatal(err)
	}

	// The first tick after switching it on ARMS and does not run.
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
		t.Errorf("the first run is %v away, want a whole interval - the button is right there for \"now\"", until)
	}

	// A second tick changes nothing: the clock is not restarted every minute.
	first := *st.NextRunAt
	a.runDBMaintenanceIfDue()
	if again := a.MaintenanceState().NextRunAt; again == nil || !again.Equal(first) {
		t.Errorf("the next run moved from %v to %v on an ordinary tick", first, again)
	}

	// Switching it off disarms, so that switching it on again a year later
	// starts the clock from then rather than firing within a minute.
	cfg.MaintenanceIntervalDays = 0
	if _, err := a.Settings.Set(cfg); err != nil {
		t.Fatal(err)
	}
	a.runDBMaintenanceIfDue()
	if st := a.MaintenanceState(); st.NextRunAt != nil {
		t.Errorf("switching the schedule off left a run armed for %v", *st.NextRunAt)
	}
}

// TestTheArmedClockSurvivesARestart is the other half of trap 10, and the whole
// reason the stamp is in a file rather than in a variable: a box that restarts
// nightly must not lose the interval it is counting.
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

// TestTheScheduleStandsDownWhileDownloadsRun is the rule the manual button
// deliberately does not share, and the recorded reason is the point: a schedule
// that silently never runs is worse than one that is switched off, because
// nothing on screen can tell the two apart.
func TestTheScheduleStandsDownWhileDownloadsRun(t *testing.T) {
	a := newMaintApp(t)
	cfg := a.Settings.Get()
	cfg.MaintenanceIntervalDays = 30
	if _, err := a.Settings.Set(cfg); err != nil {
		t.Fatal(err)
	}
	a.runDBMaintenanceIfDue() // arms

	// Due, and a download in flight - both states the app reaches on its own.
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
	// And it did NOT re-arm: the pass is owed, so it runs on the first tick
	// after the downloads stop rather than after another whole interval.
	if state.NextRunAt == nil || state.NextRunAt.After(time.Now()) {
		t.Errorf("the skipped run was pushed out to %v; it should still be due", state.NextRunAt)
	}

	// Once nothing is downloading, the same tick runs it.
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

// TestTheScheduledPassOnlyChecksUnlessToldOtherwise pins the second setting.
// Off means the scheduled run reads and reports; compacting needs room for a
// full second copy of the database on a volume that is usually not the data
// volume, so it is a thing somebody switches on knowing their own box.
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
	// Nothing else follows it. Given a moment for a compaction to appear if one
	// were ever going to: the follow-up waits on a two-second poll, so this
	// window is deliberately longer than that.
	time.Sleep(3 * time.Second)
	after := waitForIdle(t, a)
	if after.Last != nil && after.Last.Kind == MaintenanceCompact {
		t.Error("the scheduled pass compacted with compactOnSchedule off")
	}
}

// TestAnUnknownActionIsRefusedAtTheEdge keeps the route from answering 202 to a
// typo and then doing nothing at all.
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

// TestTheRecordSurvivesRubbishInItsFile: the record is runtime state, and the
// database's size is the useful half of the page. A file that will not parse
// must cost the verdict and nothing else - certainly not the readout, and never
// the boot.
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
	// And the next run writes a good one over it.
	if err := a.StartMaintenance(MaintenanceAnalyze); err != nil {
		t.Fatal(err)
	}
	if done := waitForIdle(t, a); done.Last == nil || done.Last.Kind != MaintenanceAnalyze {
		t.Fatalf("the next run did not replace the unreadable record: %+v", done.Last)
	}
}
