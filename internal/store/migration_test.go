package store

import (
	"database/sql"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/core"
)

// beforeTheWideningMigration is how many migrations had shipped before the
// column batch this file is about. Everything up to here is what an installed
// copy of the previous build actually has in its database.
const beforeTheWideningMigration = 13

// openAtOldSchema builds a database exactly as the previous build left it:
// the migrations that had shipped, the version stamp they set, and one task in
// the table. Reopening it through Open is then the real upgrade path, not a
// simulation of one.
func openAtOldSchema(t *testing.T, path string) {
	t.Helper()
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for i := 0; i < beforeTheWideningMigration; i++ {
		if _, err := db.Exec(migrations[i]); err != nil {
			t.Fatalf("old migration %d: %v", i+1, err)
		}
	}
	if _, err := db.Exec(fmt.Sprintf(`PRAGMA user_version = %d`, beforeTheWideningMigration)); err != nil {
		t.Fatal(err)
	}
}

// TestUpgradeLeavesExistingTasksEnabled is the one failure in this migration
// that would take a whole queue down without a single error message. Enabled
// defaults to true, and a column added with a bool's zero value would write 0
// into every row that already exists: on the first boot after the upgrade every
// stored task is disabled, nothing starts, and there is nothing on screen to
// connect that to an update.
func TestUpgradeLeavesExistingTasksEnabled(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tasks.db")
	openAtOldSchema(t, path)

	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	// Every column the old schema had, because that is what the old build wrote:
	// the original CREATE TABLE declares no defaults, so a partial insert here
	// would leave NULLs no released build could ever have produced.
	if _, err := db.Exec(
		`INSERT INTO tasks (id,url,name,package,resolver,size,loaded,speed,status,error,created_at,
		   dir,password,online,retries,next_try,priority,position,checksum,
		   comment,chunks,auto_extract,matched_rules)
		 VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		"old", "https://host.example/from-before.bin", "from-before.bin", "Batch", "direct",
		1024, 512, 0, string(core.StatusPaused), "", time.Now().UnixMilli(),
		"", "", "", 0, 0, 0, 0, "", "", 0, nil, ""); err != nil {
		t.Fatal(err)
	}
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
	if len(all) != 1 {
		t.Fatalf("reloaded %d tasks, want the one that was already there", len(all))
	}
	if !all[0].Enabled {
		t.Fatal("a task stored by the previous build came back disabled; every queue in existence would stop dead on upgrade")
	}
}

// TestWidenedFieldsSurviveARestart covers the rest of the batch. A column that
// is written but never read back is worse than no column: the feature works
// until the process restarts and then quietly stops, which is the shape of bug
// that gets reported as "it forgets my settings".
func TestWidenedFieldsSurviveARestart(t *testing.T) {
	yes := true
	finished := time.Now().Add(-time.Hour).Round(time.Millisecond)
	changed := time.Now().Round(time.Millisecond)
	want := core.Task{
		ID: "wide", URL: "https://host.example/part01.rar", Name: "part01.rar",
		CreatedAt:        time.Now(),
		FinishedAt:       finished,
		Enabled:          true,
		Skipped:          true,
		SkipReason:       "the destination is full",
		Hold:             true,
		Forced:           true,
		DownloadPassword: "hoster-side",
		ExpectedHash:     "sha256:abc",
		Connection:       "conn-2",
		Host:             "host.example",
		Source:           "https://host.example/gallery",
		MirrorOf:         "other",
		Resumable:        &yes,
		Filename:         "renamed.rar",
		Variant:          "1080p",
		ManualPackage:    true,
		Reason:           core.Reason("ip_blocked"),
		Origin:           core.Origin("watch"),
		ChangedAt:        changed,
		ArchivePart:      1,
	}

	dir := t.TempDir()
	s, err := Open(filepath.Join(dir, "tasks.db"))
	if err != nil {
		t.Fatal(err)
	}
	task := want
	if err := s.Save(&task); err != nil {
		t.Fatal(err)
	}
	s.Close()

	again, err := Open(filepath.Join(dir, "tasks.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer again.Close()
	all, err := again.All()
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 1 {
		t.Fatalf("reloaded %d tasks, want 1", len(all))
	}
	got := *all[0]

	checks := []struct {
		field     string
		got, want any
	}{
		{"finishedAt", got.FinishedAt.UTC(), want.FinishedAt.UTC()},
		{"enabled", got.Enabled, want.Enabled},
		{"skipped", got.Skipped, want.Skipped},
		{"skipReason", got.SkipReason, want.SkipReason},
		{"hold", got.Hold, want.Hold},
		{"forced", got.Forced, want.Forced},
		{"downloadPassword", got.DownloadPassword, want.DownloadPassword},
		{"expectedHash", got.ExpectedHash, want.ExpectedHash},
		{"connection", got.Connection, want.Connection},
		{"host", got.Host, want.Host},
		{"source", got.Source, want.Source},
		{"mirrorOf", got.MirrorOf, want.MirrorOf},
		{"filename", got.Filename, want.Filename},
		{"variant", got.Variant, want.Variant},
		{"manualPackage", got.ManualPackage, want.ManualPackage},
		{"reason", got.Reason, want.Reason},
		{"origin", got.Origin, want.Origin},
		{"changedAt", got.ChangedAt.UTC(), want.ChangedAt.UTC()},
		{"archivePart", got.ArchivePart, want.ArchivePart},
	}
	for _, c := range checks {
		if c.got != c.want {
			t.Errorf("%s = %v, want %v", c.field, c.got, c.want)
		}
	}
	if got.Resumable == nil || *got.Resumable != yes {
		t.Errorf("resumable = %v, want %v", got.Resumable, yes)
	}
}

// TestResumableKeepsItsThirdAnswer is the tri-state auto_extract already relies
// on, applied to the field a warning is built from: "nobody has asked whether
// this resumes" must not come back as "it does not", or the interface warns
// about losing bytes that would in fact be picked up where they stopped.
func TestResumableKeepsItsThirdAnswer(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(filepath.Join(dir, "tasks.db"))
	if err != nil {
		t.Fatal(err)
	}
	task := core.Task{ID: "unasked", URL: "https://host.example/x.bin", CreatedAt: time.Now()}
	if err := s.Save(&task); err != nil {
		t.Fatal(err)
	}
	s.Close()

	again, err := Open(filepath.Join(dir, "tasks.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer again.Close()
	all, err := again.All()
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 1 {
		t.Fatalf("reloaded %d tasks, want 1", len(all))
	}
	if all[0].Resumable != nil {
		t.Errorf("resumable came back as %v, want no answer at all", *all[0].Resumable)
	}
}
