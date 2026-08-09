package store

import (
	"database/sql"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/core"
)

// beforeTheHistoryTable is how many migrations had shipped before the history
// arrived. Everything up to here is what an installed copy of the previous
// build has in its database, which is what the upgrade has to run forward.
const beforeTheHistoryTable = 34

// openAtPreHistorySchema builds a database exactly as the build before the
// history table left it: its migrations, its version stamp, and nothing else.
// Reopening it through Open is then the real upgrade path.
func openAtPreHistorySchema(t *testing.T, path string) *sql.DB {
	t.Helper()
	if beforeTheHistoryTable >= len(migrations) {
		t.Fatalf("beforeTheHistoryTable is %d with %d migrations: the constant has to name the shape BEFORE the history table",
			beforeTheHistoryTable, len(migrations))
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < beforeTheHistoryTable; i++ {
		if _, err := db.Exec(migrations[i]); err != nil {
			t.Fatalf("old migration %d: %v", i+1, err)
		}
	}
	if _, err := db.Exec(fmt.Sprintf(`PRAGMA user_version = %d`, beforeTheHistoryTable)); err != nil {
		t.Fatal(err)
	}
	return db
}

// insertOldTask writes a row the way the previous build wrote one: through the
// column list of the day, with no finish time, because nothing set one.
func insertOldTask(t *testing.T, db *sql.DB, id, name, status string) {
	t.Helper()
	_, err := db.Exec(
		`INSERT INTO tasks (id,url,name,package,resolver,size,loaded,speed,status,error,created_at,
		   dir,password,online,retries,next_try,priority,position,checksum,
		   comment,chunks,auto_extract,matched_rules,
		   finished_at,enabled,skipped,skip_reason,hold,forced,download_password,expected_hash,
		   connection,host,source,mirror_of,resumable,filename,variant,manual_package,
		   reason,origin,changed_at,archive_part)
		 VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		id, "https://host.example/"+name, name, "Batch", "direct", 4096, 4096, 0, status, "",
		time.Now().Add(-72*time.Hour).UnixMilli(),
		"", "", "", 0, 0, 0, 0, "", "", 0, nil, "",
		0, true, false, "", false, false, "", "", "", "host.example", "", "", nil, "", "", false,
		"", "", 0, 0)
	if err != nil {
		t.Fatal(err)
	}
}

// TestUpgradeCarriesFinishedDownloadsIntoTheHistory runs the previous build's
// database forward.
//
// The two backfills are the whole of the risk. Without the finish stamp, every
// download an older build completed has a zero there, is invisible to every
// retention cutoff and stays in the list for ever - which is the accumulation
// the setting exists to end. Without the history rows, the record of everything
// this instance ever fetched begins at the moment somebody happened to upgrade.
func TestUpgradeCarriesFinishedDownloadsIntoTheHistory(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tasks.db")
	db := openAtPreHistorySchema(t, path)
	insertOldTask(t, db, "old-done", "already-fetched.bin", string(core.StatusDone))
	insertOldTask(t, db, "old-paused", "half-way.bin", string(core.StatusPaused))
	db.Close()

	s, err := Open(path)
	if err != nil {
		t.Fatalf("upgrading an existing database failed: %v", err)
	}
	defer s.Close()

	all, err := s.All()
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 2 {
		t.Fatalf("reloaded %d tasks, want the two that were already there", len(all))
	}
	byID := map[string]*core.Task{}
	for _, task := range all {
		byID[task.ID] = task
	}
	if byID["old-done"].FinishedAt.IsZero() {
		t.Error("a download the previous build had finished came back with no finish time; retention can never reach it")
	}
	if !byID["old-paused"].FinishedAt.IsZero() {
		t.Error("a paused download was stamped as finished by the upgrade")
	}

	hist, err := s.History(0)
	if err != nil {
		t.Fatal(err)
	}
	if len(hist) != 1 {
		t.Fatalf("the history holds %d entries after the upgrade, want the one finished download", len(hist))
	}
	if hist[0].TaskID != "old-done" || hist[0].Name != "already-fetched.bin" {
		t.Errorf("history carried %+v, want the finished task", hist[0])
	}
	if hist[0].FinishedAt.IsZero() {
		t.Error("the carried entry has no finish time, so it sorts before everything for ever")
	}
}

// TestFinishTimeIsTakenOnceAndKept is the invariant retention rests on. A
// finished download is saved several more times - a checksum verdict, an
// unpacking result, a rename - and each of those is the same finish arriving
// late, not a new one. A stamp that moved would make "older than 30 days" mean
// "untouched for 30 days", and a file that is verified once a week would never
// age out at all.
func TestFinishTimeIsTakenOnceAndKept(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "tasks.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	task := core.Task{ID: "one", URL: "https://host.example/f.bin", Name: "f.bin",
		Status: core.StatusDone, CreatedAt: time.Now()}
	if err := s.Save(&task); err != nil {
		t.Fatal(err)
	}
	first := task.FinishedAt
	if first.IsZero() {
		t.Fatal("saving a finished download recorded no finish time at all")
	}

	// The copy every caller hands the store is a fresh one, carrying the zero the
	// task in memory still has. That is the case the read-back exists for.
	again := core.Task{ID: "one", URL: "https://host.example/f.bin", Name: "f.bin",
		Status: core.StatusDone, Checksum: "ok", CreatedAt: task.CreatedAt}
	time.Sleep(2 * time.Millisecond)
	if err := s.Save(&again); err != nil {
		t.Fatal(err)
	}
	if !again.FinishedAt.Equal(first) {
		t.Errorf("the second save moved the finish time to %v, want the %v the first one took", again.FinishedAt, first)
	}

	reloaded, err := s.All()
	if err != nil {
		t.Fatal(err)
	}
	if !reloaded[0].FinishedAt.Equal(first) {
		t.Errorf("the row says %v, want %v", reloaded[0].FinishedAt, first)
	}
}

// TestRestartClearsTheFinishTime is the other half of the same invariant. A task
// put back in the queue by hand is running again, and a row that still claimed a
// finish time would be swept out from under the user by the very next retention
// pass.
func TestRestartClearsTheFinishTime(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "tasks.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	task := core.Task{ID: "again", URL: "https://host.example/f.bin", Name: "f.bin",
		Status: core.StatusDone, CreatedAt: time.Now()}
	if err := s.Save(&task); err != nil {
		t.Fatal(err)
	}
	task.Status = core.StatusQueued
	if err := s.Save(&task); err != nil {
		t.Fatal(err)
	}
	if !task.FinishedAt.IsZero() {
		t.Errorf("a re-queued task still carries a finish time of %v", task.FinishedAt)
	}
	stale, err := s.FinishedBefore(time.Now().Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if len(stale) != 0 {
		t.Errorf("retention would reach %v, which is queued again", stale)
	}
}

// TestHistoryOutlivesTheTask is the reason the table exists at all: the list is
// trimmed, and what this instance fetched has to survive the trimming.
func TestHistoryOutlivesTheTask(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "tasks.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	task := core.Task{ID: "gone", URL: "https://host.example/keep.bin", Name: "keep.bin",
		Package: "Batch", Host: "host.example", Resolver: "direct", Size: 4096,
		Status: core.StatusDone, CreatedAt: time.Now()}
	if err := s.Save(&task); err != nil {
		t.Fatal(err)
	}
	if err := s.Delete(task.ID); err != nil {
		t.Fatal(err)
	}
	all, err := s.All()
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 0 {
		t.Fatalf("the task is still in the list after Delete")
	}
	hist, err := s.History(0)
	if err != nil {
		t.Fatal(err)
	}
	if len(hist) != 1 {
		t.Fatalf("the history holds %d entries, want the download that was removed from the list", len(hist))
	}
	got := hist[0]
	if got.URL != task.URL || got.Name != task.Name || got.Size != task.Size ||
		got.Package != task.Package || got.Host != task.Host || got.Resolver != task.Resolver {
		t.Errorf("history entry %+v does not describe the download it came from", got)
	}
}

// TestHistoryRecordsOneRowPerDownload guards the shape of the record. A finished
// task is saved again every time anything about it changes, and one row per save
// would make the history unreadable in exactly the direction people scroll it.
func TestHistoryRecordsOneRowPerDownload(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "tasks.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	task := core.Task{ID: "same", URL: "https://host.example/f.bin", Name: "f.bin",
		Status: core.StatusDone, CreatedAt: time.Now()}
	for i := 0; i < 5; i++ {
		c := task
		if err := s.Save(&c); err != nil {
			t.Fatal(err)
		}
	}
	n, err := s.historyRows()
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Errorf("five saves of one download produced %d history entries, want 1", n)
	}
}

// TestUnfinishedDownloadsStayOutOfTheHistory keeps the table's meaning crisp:
// every row in it is something this instance actually fetched. A staged link and
// a failed attempt are not, and a "history" that contains them cannot be used to
// answer the one question it is for.
func TestUnfinishedDownloadsStayOutOfTheHistory(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "tasks.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	for _, st := range []core.Status{core.StatusCollected, core.StatusQueued,
		core.StatusRunning, core.StatusPaused, core.StatusExtracting, core.StatusError} {
		task := core.Task{ID: string(st), URL: "https://host.example/" + string(st),
			Name: string(st), Status: st, CreatedAt: time.Now()}
		if err := s.Save(&task); err != nil {
			t.Fatal(err)
		}
	}
	n, err := s.historyRows()
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Errorf("%d entries in the history, want none: not one of those downloads finished", n)
	}
}

// TestTrimHistoryKeepsTheNewest checks the cap does what it says and cuts from
// the right end. A trim that kept the oldest entries would leave a history that
// stops answering questions about last week.
func TestTrimHistoryKeepsTheNewest(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "tasks.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	base := time.Now().Add(-10 * time.Hour)
	for i := 0; i < 10; i++ {
		task := core.Task{
			ID: fmt.Sprintf("t%02d", i), URL: fmt.Sprintf("https://host.example/%02d.bin", i),
			Name: fmt.Sprintf("%02d.bin", i), Status: core.StatusDone,
			CreatedAt: base, FinishedAt: base.Add(time.Duration(i) * time.Hour),
		}
		if err := s.Save(&task); err != nil {
			t.Fatal(err)
		}
	}
	dropped, err := s.TrimHistory(4)
	if err != nil {
		t.Fatal(err)
	}
	if dropped != 6 {
		t.Errorf("trim dropped %d entries, want 6", dropped)
	}
	hist, err := s.History(0)
	if err != nil {
		t.Fatal(err)
	}
	if len(hist) != 4 {
		t.Fatalf("%d entries left, want 4", len(hist))
	}
	if hist[0].TaskID != "t09" || hist[3].TaskID != "t06" {
		t.Errorf("kept %s..%s, want the four newest (t09..t06)", hist[0].TaskID, hist[3].TaskID)
	}

	// Zero is "keep everything" and must never be read as "keep nothing".
	if dropped, err := s.TrimHistory(0); err != nil || dropped != 0 {
		t.Errorf("TrimHistory(0) dropped %d (err %v), want it to leave the history alone", dropped, err)
	}
}
