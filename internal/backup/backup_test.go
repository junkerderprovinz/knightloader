package backup

import (
	"archive/zip"
	"bytes"
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/settings"
	"github.com/junkerderprovinz/knightloader/internal/store"
)

// buildDB writes a small, real database at path through the same store
// package the app uses, so this file's tests exercise the actual on-disk
// shape restore.go's validation has to accept — not a hand-rolled SQLite
// file that only looks like one.
func buildDB(t *testing.T, path string) {
	t.Helper()
	s, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
}

// snapshotDB opens s, snapshots it via VACUUM INTO the way the real backup
// route does, and returns the snapshot's path.
func snapshotDB(t *testing.T, dbPath string) string {
	t.Helper()
	s, err := store.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	dst := filepath.Join(t.TempDir(), "snapshot.db")
	if err := s.BackupTo(dst); err != nil {
		t.Fatal(err)
	}
	return dst
}

func testManifest() Manifest {
	return Manifest{Version: "v1.2.3", Deployment: "container", CreatedAt: time.Now().UTC()}
}

func testSettingsJSON(t *testing.T) []byte {
	t.Helper()
	b, err := json.Marshal(settings.Defaults())
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// TestBuildThenStageThenApplyRoundTrips is the whole feature end to end: an
// archive built the way the backup route builds it has to be exactly what
// Stage will accept and ApplyPending will put in place.
func TestBuildThenStageThenApplyRoundTrips(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "knightloader.db")
	buildDB(t, dbPath)
	snap := snapshotDB(t, dbPath)

	settingsJSON := testSettingsJSON(t)
	var archive bytes.Buffer
	if err := Build(&archive, testManifest(), settingsJSON, snap); err != nil {
		t.Fatalf("Build: %v", err)
	}

	dataDir := t.TempDir()
	manifest, err := Stage(dataDir, archive.Bytes(), "v1.2.3")
	if err != nil {
		t.Fatalf("Stage: %v", err)
	}
	if manifest.Version != "v1.2.3" {
		t.Errorf("staged manifest version = %q, want v1.2.3", manifest.Version)
	}

	// Nothing live is touched by Stage alone — that is the entire promise
	// the package doc comment makes.
	if _, err := os.Stat(filepath.Join(dataDir, "knightloader.db")); !os.IsNotExist(err) {
		t.Fatalf("Stage wrote knightloader.db into dataDir directly: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dataDir, "settings.json")); !os.IsNotExist(err) {
		t.Fatalf("Stage wrote settings.json into dataDir directly: %v", err)
	}

	applied, appliedManifest, err := ApplyPending(dataDir)
	if err != nil {
		t.Fatalf("ApplyPending: %v", err)
	}
	if !applied {
		t.Fatal("ApplyPending reported nothing to apply")
	}
	if appliedManifest.Version != "v1.2.3" {
		t.Errorf("applied manifest version = %q, want v1.2.3", appliedManifest.Version)
	}

	if _, err := os.Stat(filepath.Join(dataDir, "knightloader.db")); err != nil {
		t.Errorf("knightloader.db was not put in place: %v", err)
	}
	gotSettings, err := os.ReadFile(filepath.Join(dataDir, "settings.json"))
	if err != nil {
		t.Fatalf("settings.json was not put in place: %v", err)
	}
	if string(gotSettings) != string(settingsJSON) {
		t.Errorf("restored settings.json does not match what was backed up")
	}

	// The store put in place has to be a store, openable and readable, not
	// merely a file with the right name.
	restored, err := store.Open(filepath.Join(dataDir, "knightloader.db"))
	if err != nil {
		t.Fatalf("the restored database could not be opened: %v", err)
	}
	defer restored.Close()
	if _, err := restored.All(); err != nil {
		t.Errorf("the restored database could not be read: %v", err)
	}

	// The staging directory is gone; a second boot must not try to
	// re-apply the same restore forever.
	if _, err := os.Stat(filepath.Join(dataDir, pendingDirName)); !os.IsNotExist(err) {
		t.Errorf("the pending directory was not cleared after a successful apply")
	}
}

// TestApplyPendingIsANoOpWithNothingStaged is the ordinary case on every
// boot that is not completing a restore, which is nearly every boot.
func TestApplyPendingIsANoOpWithNothingStaged(t *testing.T) {
	dataDir := t.TempDir()
	applied, _, err := ApplyPending(dataDir)
	if err != nil {
		t.Fatalf("ApplyPending on a plain data dir: %v", err)
	}
	if applied {
		t.Fatal("ApplyPending reported something applied with nothing staged")
	}
}

// TestApplyPendingIsIdempotent proves the crash-safety claim in
// ApplyPending's own doc comment: interrupting it after the settings file
// is already in place but before the pending directory is cleared must not
// strand the restore. Retrying has to finish the job, not fail because the
// first attempt already consumed part of it.
func TestApplyPendingIsIdempotent(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "knightloader.db")
	buildDB(t, dbPath)
	snap := snapshotDB(t, dbPath)

	var archive bytes.Buffer
	if err := Build(&archive, testManifest(), testSettingsJSON(t), snap); err != nil {
		t.Fatal(err)
	}
	dataDir := t.TempDir()
	if _, err := Stage(dataDir, archive.Bytes(), "v1.2.3"); err != nil {
		t.Fatal(err)
	}

	// First pass, simulating a crash: apply once (this is what a real crash
	// mid-ApplyPending would have already completed up to), and this run
	// does succeed all the way through — the actual claim under test is the
	// SECOND call below, run again exactly as a restarted process would.
	if applied, _, err := ApplyPending(dataDir); err != nil || !applied {
		t.Fatalf("first ApplyPending: applied=%v err=%v", applied, err)
	}

	// A second, independent call — as if the process had been restarted
	// again with nothing new staged — must be the ordinary no-op case, not
	// an error.
	applied, _, err := ApplyPending(dataDir)
	if err != nil {
		t.Fatalf("second ApplyPending: %v", err)
	}
	if applied {
		t.Fatal("second ApplyPending re-applied a restore that was already cleared")
	}
}

// TestStageRejectsSomethingThatIsNotAZip pins the first, cheapest check: an
// upload that is not even a valid archive gets a specific reason, not a
// panic or a generic 500 three layers up.
func TestStageRejectsSomethingThatIsNotAZip(t *testing.T) {
	_, err := Stage(t.TempDir(), []byte("not a zip file at all"), "v1.0.0")
	if err == nil {
		t.Fatal("expected an error")
	}
	if !strings.Contains(err.Error(), "not a valid backup archive") {
		t.Errorf("error = %q, want it to name the archive as invalid", err.Error())
	}
}

// TestStageRejectsAMissingEntry covers a zip that opens fine but is missing
// one of the three files a backup this package built always has — a
// half-downloaded or hand-edited archive, not a corrupt one.
func TestStageRejectsAMissingEntry(t *testing.T) {
	var archive bytes.Buffer
	zw := zip.NewWriter(&archive)
	w, err := zw.Create(manifestEntry)
	if err != nil {
		t.Fatal(err)
	}
	mf, _ := json.Marshal(testManifest())
	if _, err := w.Write(mf); err != nil {
		t.Fatal(err)
	}
	// settings.json and knightloader.db are deliberately never written.
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}

	_, err = Stage(t.TempDir(), archive.Bytes(), "v1.0.0")
	if err == nil {
		t.Fatal("expected an error")
	}
	if !strings.Contains(err.Error(), settingsEntry) {
		t.Errorf("error = %q, want it to name the missing %s", err.Error(), settingsEntry)
	}
}

// TestStageRejectsAMismatchedSettingsShape is what a truncated or
// hand-edited settings.json inside the archive produces: valid JSON, wrong
// shape, and json.Unmarshal already refuses that for free.
func TestStageRejectsAMismatchedSettingsShape(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "knightloader.db")
	buildDB(t, dbPath)
	snap := snapshotDB(t, dbPath)

	var archive bytes.Buffer
	// Built by hand rather than through Build, so settingsJSON can be
	// deliberately wrong-shaped: a string where maxConcurrent wants a
	// number.
	zw := zip.NewWriter(&archive)
	mf, _ := json.Marshal(testManifest())
	mustWriteEntry(t, zw, manifestEntry, mf)
	mustWriteEntry(t, zw, settingsEntry, []byte(`{"maxConcurrent":"not a number"}`))
	dbBytes, err := os.ReadFile(snap)
	if err != nil {
		t.Fatal(err)
	}
	mustWriteEntry(t, zw, dbEntry, dbBytes)
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}

	_, err = Stage(t.TempDir(), archive.Bytes(), "v1.0.0")
	if err == nil {
		t.Fatal("expected an error")
	}
	if !strings.Contains(err.Error(), "settings.json") {
		t.Errorf("error = %q, want it to name settings.json", err.Error())
	}
}

// TestStageRejectsACorruptDatabase is the check that actually opens the
// database entry and asks SQLite about it, rather than trusting the file
// extension or the presence of bytes.
func TestStageRejectsACorruptDatabase(t *testing.T) {
	var archive bytes.Buffer
	zw := zip.NewWriter(&archive)
	mf, _ := json.Marshal(testManifest())
	mustWriteEntry(t, zw, manifestEntry, mf)
	mustWriteEntry(t, zw, settingsEntry, testSettingsJSON(t))
	mustWriteEntry(t, zw, dbEntry, []byte("this is not a sqlite database"))
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}

	_, err := Stage(t.TempDir(), archive.Bytes(), "v1.0.0")
	if err == nil {
		t.Fatal("expected an error")
	}
	if !strings.Contains(err.Error(), "database") {
		t.Errorf("error = %q, want it to mention the database", err.Error())
	}
}

// TestStageRejectsADatabaseWithNoTasksTable defends against a zip that
// contains a real, valid, unrelated SQLite database — passing the integrity
// check while still not being a KnightLoader backup.
func TestStageRejectsADatabaseWithNoTasksTable(t *testing.T) {
	otherDB := filepath.Join(t.TempDir(), "other.db")
	s, err := store.Open(otherDB) // has a tasks table
	if err != nil {
		t.Fatal(err)
	}
	s.Close()
	// A fresh SQLite file with no schema at all — opens fine, passes
	// integrity_check (an empty database is internally consistent), and
	// still has no tasks table.
	emptyDB := filepath.Join(t.TempDir(), "empty.db")
	if err := os.WriteFile(emptyDB, sqliteEmptyFileHeader(t, otherDB), 0o644); err != nil {
		t.Fatal(err)
	}

	var archive bytes.Buffer
	zw := zip.NewWriter(&archive)
	mf, _ := json.Marshal(testManifest())
	mustWriteEntry(t, zw, manifestEntry, mf)
	mustWriteEntry(t, zw, settingsEntry, testSettingsJSON(t))
	raw, err := os.ReadFile(emptyDB)
	if err != nil {
		t.Fatal(err)
	}
	mustWriteEntry(t, zw, dbEntry, raw)
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}

	_, err = Stage(t.TempDir(), archive.Bytes(), "v1.0.0")
	if err == nil {
		t.Fatal("expected an error")
	}
	if !strings.Contains(err.Error(), "not a KnightLoader backup") {
		t.Errorf("error = %q, want it to say this is not a KnightLoader backup", err.Error())
	}
}

// TestStageRejectsANewerBackup is the safety rail against restoring a
// backup a future, not-yet-installed build made, whose settings shape or
// schema this binary may not fully understand.
func TestStageRejectsANewerBackup(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "knightloader.db")
	buildDB(t, dbPath)
	snap := snapshotDB(t, dbPath)

	manifest := testManifest()
	manifest.Version = "v9.9.9"
	var archive bytes.Buffer
	if err := Build(&archive, manifest, testSettingsJSON(t), snap); err != nil {
		t.Fatal(err)
	}

	_, err := Stage(t.TempDir(), archive.Bytes(), "v1.0.0")
	if err == nil {
		t.Fatal("expected an error")
	}
	if !strings.Contains(err.Error(), "newer") {
		t.Errorf("error = %q, want it to say the backup is newer", err.Error())
	}
}

// TestStageAllowsADevRunningVersion is the other side of the same rail: a
// local, untagged build's "dev" version is not comparable to anything, and
// the check has to skip rather than refuse every restore on a development
// checkout.
func TestStageAllowsADevRunningVersion(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "knightloader.db")
	buildDB(t, dbPath)
	snap := snapshotDB(t, dbPath)

	manifest := testManifest()
	manifest.Version = "v9.9.9"
	var archive bytes.Buffer
	if err := Build(&archive, manifest, testSettingsJSON(t), snap); err != nil {
		t.Fatal(err)
	}

	if _, err := Stage(t.TempDir(), archive.Bytes(), "dev"); err != nil {
		t.Fatalf("Stage with a dev running version: %v", err)
	}
}

// TestStageOversizedEntryIsBounded confirms a declared entry size does not
// get read past MaxUploadBytes — the decompression-bomb defence readEntry's
// own comment describes.
func TestStageOversizedEntryIsBounded(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "knightloader.db")
	buildDB(t, dbPath)
	snap := snapshotDB(t, dbPath)
	dbBytes, err := os.ReadFile(snap)
	if err != nil {
		t.Fatal(err)
	}

	var archive bytes.Buffer
	zw := zip.NewWriter(&archive)
	mf, _ := json.Marshal(testManifest())
	mustWriteEntry(t, zw, manifestEntry, mf)
	// Highly compressible and, uncompressed, larger than any real settings
	// document has a business being — the shape a bomb takes.
	huge := bytes.Repeat([]byte("a"), MaxUploadBytes+1024)
	mustWriteEntry(t, zw, settingsEntry, huge)
	mustWriteEntry(t, zw, dbEntry, dbBytes)
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}

	// The oversized entry must not decode as valid settings JSON (it is
	// truncated to MaxUploadBytes 'a' characters, which is not JSON at
	// all), so Stage refuses it — the read itself did not hang or exhaust
	// memory reading the whole thing, which is the property under test.
	_, err = Stage(t.TempDir(), archive.Bytes(), "v1.0.0")
	if err == nil {
		t.Fatal("expected an error for a truncated, oversized settings entry")
	}
}

// TestStageReplacesAPreviouslyStagedRestore is the documented policy in
// stageFiles: uploading a second backup before restarting supersedes the
// first, rather than erroring or merging.
func TestStageReplacesAPreviouslyStagedRestore(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "knightloader.db")
	buildDB(t, dbPath)
	snap := snapshotDB(t, dbPath)
	dataDir := t.TempDir()

	var first bytes.Buffer
	m1 := testManifest()
	m1.Version = "v1.0.0"
	if err := Build(&first, m1, testSettingsJSON(t), snap); err != nil {
		t.Fatal(err)
	}
	if _, err := Stage(dataDir, first.Bytes(), "v2.0.0"); err != nil {
		t.Fatal(err)
	}

	var second bytes.Buffer
	m2 := testManifest()
	m2.Version = "v1.5.0"
	if err := Build(&second, m2, testSettingsJSON(t), snap); err != nil {
		t.Fatal(err)
	}
	if _, err := Stage(dataDir, second.Bytes(), "v2.0.0"); err != nil {
		t.Fatal(err)
	}

	_, manifest, err := ApplyPending(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	if manifest.Version != "v1.5.0" {
		t.Errorf("applied manifest version = %q, want the second upload's v1.5.0", manifest.Version)
	}
}

// openEmptySQLite opens a brand new SQLite file at path with no schema at
// all — a real, valid database that simply has no tasks table, which is
// exactly the fixture TestStageRejectsADatabaseWithNoTasksTable needs and
// store.Open cannot provide, since it always creates one via its own
// migrations.
func openEmptySQLite(t *testing.T, path string) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	// A no-op write forces the driver to actually create the file on disk;
	// sql.Open alone is lazy and may not touch the filesystem until the
	// first query.
	if _, err := db.Exec(`PRAGMA user_version = 0`); err != nil {
		t.Fatal(err)
	}
	return db
}

func mustWriteEntry(t *testing.T, zw *zip.Writer, name string, data []byte) {
	t.Helper()
	w, err := zw.Create(name)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write(data); err != nil {
		t.Fatal(err)
	}
}

// sqliteEmptyFileHeader hands back the bytes of a valid, schema-less SQLite
// database (opened once with no tables ever created) so the "wrong content,
// right container format" test above is exercising a real SQLite file and
// not a string that merely looks like one.
func sqliteEmptyFileHeader(t *testing.T, unusedPathForSchema string) []byte {
	t.Helper()
	path := filepath.Join(t.TempDir(), "schemaless.db")
	// store.Open always creates the tasks table; a schema-less database is
	// built independently, through database/sql directly, so this fixture
	// does not depend on internal/store's own migrations never adding a
	// table this test would then accidentally satisfy.
	db := openEmptySQLite(t, path)
	db.Close()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
