package store

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/core"
)

// fillAndEmpty saves enough rows to leave a real free list behind and then
// deletes most of them. It uses Save and Delete rather than raw SQL so the
// fragmentation is what a compaction finds on a real install.
func fillAndEmpty(t *testing.T, s *Store, rows int) {
	t.Helper()
	// Padding makes each row a fraction of a page, so deleting frees a
	// measurable amount. The comment column is one the Packagizer writes.
	padding := strings.Repeat("x", 2048)
	for i := 0; i < rows; i++ {
		if err := s.Save(&core.Task{
			ID:        fmt.Sprintf("task-%05d", i),
			URL:       fmt.Sprintf("https://host.example/file-%05d.bin", i),
			Name:      fmt.Sprintf("file-%05d.bin", i),
			Package:   "Batch",
			Resolver:  "direct",
			Comment:   padding,
			Status:    core.StatusPaused,
			CreatedAt: time.Now(),
		}); err != nil {
			t.Fatalf("saving row %d: %v", i, err)
		}
	}
	// All but a handful, so the table still has content.
	for i := 0; i < rows-8; i++ {
		if err := s.Delete(fmt.Sprintf("task-%05d", i)); err != nil {
			t.Fatalf("deleting row %d: %v", i, err)
		}
	}
}

// TestVacuumShrinksTheFileAndKeepsTheSchemaVersion: migrate relies on PRAGMA
// user_version, and migration 2's ALTER TABLE ADD COLUMN fails when run twice,
// so a VACUUM that lost the version would make the app refuse to start on an
// intact database. SQLite documents that VACUUM keeps it; this asserts it, and
// that the file reopens.
func TestVacuumShrinksTheFileAndKeepsTheSchemaVersion(t *testing.T) {
	path := filepath.Join(t.TempDir(), "knightloader.db")
	// A database migrated forward from an older schema, as on a real install.
	openAtOldSchema(t, path)
	s, err := Open(path)
	if err != nil {
		t.Fatalf("opening at the old schema: %v", err)
	}

	fillAndEmpty(t, s, 400)

	before, err := s.Sizes()
	if err != nil {
		t.Fatal(err)
	}
	if before.FreePages == 0 {
		t.Fatalf("deleting 392 of 400 rows left no free pages at all (%+v); this test cannot see what it is measuring", before)
	}
	if before.ReclaimableBytes != before.FreePages*before.PageSize {
		t.Errorf("reclaimable = %d, want freePages(%d) * pageSize(%d)", before.ReclaimableBytes, before.FreePages, before.PageSize)
	}

	versionBefore, err := s.SchemaVersion()
	if err != nil {
		t.Fatal(err)
	}
	if versionBefore != len(migrations) {
		t.Fatalf("schema version before the compaction is %d, want every migration applied (%d)", versionBefore, len(migrations))
	}

	if err := s.Vacuum(context.Background()); err != nil {
		t.Fatalf("compacting: %v", err)
	}

	after, err := s.Sizes()
	if err != nil {
		t.Fatal(err)
	}
	if after.FileBytes >= before.FileBytes {
		t.Errorf("the file is %d bytes after compacting and was %d before; nothing was given back", after.FileBytes, before.FileBytes)
	}
	if after.FreePages != 0 {
		t.Errorf("%d free pages survived the compaction, want none", after.FreePages)
	}

	versionAfter, err := s.SchemaVersion()
	if err != nil {
		t.Fatal(err)
	}
	if versionAfter != versionBefore {
		t.Fatalf("PRAGMA user_version went from %d to %d across a VACUUM. Every migration would run again on the next boot, "+
			"migration 2's ALTER TABLE ADD COLUMN would fail with a duplicate column, and the process would refuse to start "+
			"on an intact database", versionBefore, versionAfter)
	}

	// The remaining rows are still readable.
	left, err := s.All()
	if err != nil {
		t.Fatalf("reading the tasks back after a compaction: %v", err)
	}
	if len(left) != 8 {
		t.Errorf("%d tasks survived the compaction, want the 8 that were not deleted", len(left))
	}

	// The file still opens.
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	again, err := Open(path)
	if err != nil {
		t.Fatalf("reopening a compacted database failed: %v", err)
	}
	defer again.Close()
	all, err := again.All()
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 8 {
		t.Errorf("reopened with %d tasks, want 8", len(all))
	}
}

// TestSizesCountsTheJournalBesideTheFile: a journal beside the database takes
// disk too, and the readout should agree with du.
func TestSizesCountsTheJournalBesideTheFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "knightloader.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	plain, err := s.Sizes()
	if err != nil {
		t.Fatal(err)
	}
	if plain.FileBytes <= 0 {
		t.Fatalf("a freshly migrated database measured %d bytes", plain.FileBytes)
	}
	if plain.LogicalBytes != plain.PageSize*plain.PageCount {
		t.Errorf("logical = %d, want pageSize(%d) * pageCount(%d)", plain.LogicalBytes, plain.PageSize, plain.PageCount)
	}

	// A journal written by hand under SQLite's own name, which the
	// measurement must find even though this process did not create it.
	const journalBytes = 4096
	if err := os.WriteFile(path+"-journal", make([]byte, journalBytes), 0o600); err != nil {
		t.Fatal(err)
	}
	withJournal, err := s.Sizes()
	if err != nil {
		t.Fatal(err)
	}
	if withJournal.FileBytes != plain.FileBytes+journalBytes {
		t.Errorf("with a %d byte journal beside it the database measured %d, want %d",
			journalBytes, withJournal.FileBytes, plain.FileBytes+journalBytes)
	}
	// FileSize, used during a compaction, agrees with the full measurement.
	only, err := s.FileSize()
	if err != nil {
		t.Fatal(err)
	}
	if only != withJournal.FileBytes {
		t.Errorf("FileSize = %d but Sizes said %d", only, withJournal.FileBytes)
	}
}

// TestIntegrityCheckIsSilentOnAHealthyDatabase: no problems is an empty slice,
// never one holding "ok".
func TestIntegrityCheckIsSilentOnAHealthyDatabase(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "knightloader.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	fillAndEmpty(t, s, 40)

	problems, err := s.IntegrityCheck(context.Background(), 0)
	if err != nil {
		t.Fatalf("checking a healthy database: %v", err)
	}
	if len(problems) != 0 {
		t.Errorf("a healthy database reported %d problems: %v", len(problems), problems)
	}
	if problems == nil {
		t.Error("problems is nil rather than an empty slice; it goes straight into JSON, where nil is null")
	}
}

// TestIntegrityCheckReportsEveryProblem: every finding is reported, one per
// entry. The driver returns all findings in one newline-separated row, so the
// count shown to the user depends on the flattening. The file is damaged on
// disk with the store closed, since the app cannot be driven into corruption.
func TestIntegrityCheckReportsEveryProblem(t *testing.T) {
	path := filepath.Join(t.TempDir(), "knightloader.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	fillAndEmpty(t, s, 200)
	sizes, err := s.Sizes()
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	pageSize := int(sizes.PageSize)
	if len(raw) < pageSize*6 {
		t.Fatalf("the database is only %d bytes, too small to damage a page in the middle of", len(raw))
	}
	// Page 1 holds the header, without which the file would not open at all.
	// Everything from page 3 on is overwritten, so there are many findings.
	for i := pageSize * 2; i < len(raw); i++ {
		raw[i] = 0xA5
	}
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}

	damaged, err := Open(path)
	if err != nil {
		t.Fatalf("a database with damaged content pages should still open (its header is intact): %v", err)
	}
	defer damaged.Close()

	problems, err := damaged.IntegrityCheck(context.Background(), 0)
	if err != nil {
		// SQLite can refuse the pragma on badly damaged content, but passing
		// on that would make this test unable to fail.
		t.Fatalf("the integrity check could not run at all on the damaged file: %v", err)
	}
	if len(problems) < 2 {
		t.Fatalf("a database with every page after the second overwritten reported %d problem(s): %v; "+
			"one entry is what a caller gets that reads the first row only, or that reads every row and "+
			"forgets that this driver packs them all into one", len(problems), problems)
	}
	for _, p := range problems {
		if p == "ok" {
			t.Errorf(`"ok" appeared in the problem list: %v`, problems)
		}
		if strings.Contains(p, "\n") {
			t.Errorf("a problem entry still has a newline in it (%q); the page renders these as a list", p)
		}
	}

	capped, err := damaged.IntegrityCheck(context.Background(), 3)
	if err != nil {
		t.Fatal(err)
	}
	if len(capped) > 3 {
		t.Errorf("asked for at most 3 problems and got %d", len(capped))
	}
}

// TestTagSurvivesACompactionAndAReopen: the stamp must survive the rewrite of
// every page and a restart, or the maintenance record would forget its
// verdict.
func TestTagSurvivesACompactionAndAReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "knightloader.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	fillAndEmpty(t, s, 60)

	fresh, err := s.Tag()
	if err != nil {
		t.Fatal(err)
	}
	if fresh != 0 {
		t.Errorf("a database nothing has stamped reports tag %d, want 0 - 0 is what the record reads as \"never stamped\"", fresh)
	}

	const tag = 1234567
	if err := s.SetTag(tag); err != nil {
		t.Fatal(err)
	}
	if got, err := s.Tag(); err != nil || got != tag {
		t.Fatalf("Tag() = %d, %v after stamping %d", got, err, tag)
	}
	if err := s.Vacuum(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got, err := s.Tag(); err != nil || got != tag {
		t.Fatalf("the stamp is %d, %v after a compaction, want %d - the record would report \"nothing has run yet\" "+
			"immediately after every compaction", got, err, tag)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}

	again, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer again.Close()
	if got, err := again.Tag(); err != nil || got != tag {
		t.Fatalf("the stamp is %d, %v in the next process, want %d - the record would forget its verdict on every restart", got, err, tag)
	}
}

// TestTagIsNotTouchedByOrdinaryWrites is the property that made the stamp worth
// having at all, and it is the one the obvious alternative does not have. The
// record has to survive a download saving a row; a check against the file's own
// size and modification time would not, because both move on every save, and
// the page would go back to "nothing has run yet" seconds after somebody read
// the answer they pressed for.
func TestTagIsNotTouchedByOrdinaryWrites(t *testing.T) {
	path := filepath.Join(t.TempDir(), "knightloader.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	const tag = 7654321
	if err := s.SetTag(tag); err != nil {
		t.Fatal(err)
	}
	sizeBefore, err := s.FileSize()
	if err != nil {
		t.Fatal(err)
	}
	fillAndEmpty(t, s, 120)
	sizeAfter, err := s.FileSize()
	if err != nil {
		t.Fatal(err)
	}
	if sizeAfter == sizeBefore {
		t.Fatal("the file did not change size across 120 saves and 112 deletes, so this test is not exercising what it claims to")
	}
	if got, err := s.Tag(); err != nil || got != tag {
		t.Fatalf("the stamp is %d, %v after ordinary saves and deletes, want %d", got, err, tag)
	}
}

// TestVacuumStopsWhenItsContextIsCancelled is what makes a shutdown survivable.
// Close cancels the app's context and then waits; a compaction that could not
// be interrupted would hold that Wait for the length of a full-file rewrite,
// past a container runtime's ten-second grace period, and the process would be
// killed in the middle of writing the database.
func TestVacuumStopsWhenItsContextIsCancelled(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "knightloader.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	fillAndEmpty(t, s, 200)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err = s.Vacuum(ctx)
	if err == nil {
		t.Fatal("a compaction with an already-cancelled context ran to completion; nothing would stop one during a shutdown")
	}
	if !strings.Contains(err.Error(), context.Canceled.Error()) {
		t.Errorf("compacting with a cancelled context failed with %v, which does not name the cancellation", err)
	}
	// The database is still usable afterwards, which is the other half of the
	// promise: an interrupted rewrite is rolled back, not left half applied.
	if _, err := s.All(); err != nil {
		t.Errorf("the database is unreadable after an interrupted compaction: %v", err)
	}
}

// TestIntegrityCheckStopsWhenItsContextIsCancelled is the same promise for the
// other long call. A check reads every page of the file; on a large one that is
// minutes, and a shutdown has to be able to reach it.
func TestIntegrityCheckStopsWhenItsContextIsCancelled(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "knightloader.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	fillAndEmpty(t, s, 100)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := s.IntegrityCheck(ctx, 0); err == nil {
		t.Fatal("an integrity check with an already-cancelled context ran to completion")
	}
}

// TestPathIsWhatOpenWasGiven pins the accessor the layer above builds its own
// answers from - the file size readout, and the sentence that tells somebody
// which file to copy aside when a check has just failed. A Path that drifted
// from the file actually in use would send them to copy the wrong one.
func TestPathIsWhatOpenWasGiven(t *testing.T) {
	path := filepath.Join(t.TempDir(), "knightloader.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if s.Path() != path {
		t.Errorf("Path() = %q, want %q", s.Path(), path)
	}
	// And it names a file that is really there, which is the claim the readout
	// makes when it prints a size beside it.
	if _, err := os.Stat(s.Path()); err != nil {
		t.Errorf("Path() names %q, which cannot be stat-ed: %v", s.Path(), err)
	}
}

// openRaw is a second connection to the same file, for the one assertion that
// cannot be made through *Store: that the tag really is in the header where
// SQLite itself keeps application_id, rather than somewhere this package would
// find and nothing else would.
func openRaw(t *testing.T, path string) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func TestTagIsTheSQLiteApplicationID(t *testing.T) {
	path := filepath.Join(t.TempDir(), "knightloader.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	const tag = 424242
	if err := s.SetTag(tag); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}

	db := openRaw(t, path)
	var got int64
	if err := db.QueryRow(`PRAGMA application_id`).Scan(&got); err != nil {
		t.Fatal(err)
	}
	if got != tag {
		t.Errorf("PRAGMA application_id = %d, want %d - the stamp is not where SQLite keeps it, so nothing else would see it", got, tag)
	}
}
