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

// buildDB creates a real database at path through the store package, so the
// tests validate the actual on-disk shape.
func buildDB(t *testing.T, path string) {
	t.Helper()
	s, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
}

// snapshotDB snapshots the database at dbPath the way the backup route does
// and returns the snapshot's path.
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

	restored, err := store.Open(filepath.Join(dataDir, "knightloader.db"))
	if err != nil {
		t.Fatalf("the restored database could not be opened: %v", err)
	}
	defer restored.Close()
	if _, err := restored.All(); err != nil {
		t.Errorf("the restored database could not be read: %v", err)
	}

	if _, err := os.Stat(filepath.Join(dataDir, pendingDirName)); !os.IsNotExist(err) {
		t.Errorf("the pending directory was not cleared after a successful apply")
	}
}

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

	if applied, _, err := ApplyPending(dataDir); err != nil || !applied {
		t.Fatalf("first ApplyPending: applied=%v err=%v", applied, err)
	}

	// A second start-up with nothing new staged is the ordinary no-op.
	applied, _, err := ApplyPending(dataDir)
	if err != nil {
		t.Fatalf("second ApplyPending: %v", err)
	}
	if applied {
		t.Fatal("second ApplyPending re-applied a restore that was already cleared")
	}
}

func TestStageRejectsSomethingThatIsNotAZip(t *testing.T) {
	_, err := Stage(t.TempDir(), []byte("not a zip file at all"), "v1.0.0")
	if err == nil {
		t.Fatal("expected an error")
	}
	if !strings.Contains(err.Error(), "not a valid backup archive") {
		t.Errorf("error = %q, want it to name the archive as invalid", err.Error())
	}
}

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

func TestStageRejectsAMismatchedSettingsShape(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "knightloader.db")
	buildDB(t, dbPath)
	snap := snapshotDB(t, dbPath)

	var archive bytes.Buffer
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

// TestStageRejectsADatabaseWithNoTasksTable uses a valid SQLite file that
// passes the integrity check but is not a KnightLoader database.
func TestStageRejectsADatabaseWithNoTasksTable(t *testing.T) {
	otherDB := filepath.Join(t.TempDir(), "other.db")
	s, err := store.Open(otherDB)
	if err != nil {
		t.Fatal(err)
	}
	s.Close()
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

// TestStageOversizedEntryIsBounded checks that an entry larger than
// MaxUploadBytes is read only up to the limit and then refused.
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
	huge := bytes.Repeat([]byte("a"), MaxUploadBytes+1024)
	mustWriteEntry(t, zw, settingsEntry, huge)
	mustWriteEntry(t, zw, dbEntry, dbBytes)
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}

	_, err = Stage(t.TempDir(), archive.Bytes(), "v1.0.0")
	if err == nil {
		t.Fatal("expected an error for a truncated, oversized settings entry")
	}
}

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

// openEmptySQLite creates a SQLite file at path with no schema, which
// store.Open cannot provide since its migrations always create tables.
func openEmptySQLite(t *testing.T, path string) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	// sql.Open is lazy; a write makes the driver create the file.
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

// sqliteEmptyFileHeader returns the bytes of a valid SQLite database with no
// tables.
func sqliteEmptyFileHeader(t *testing.T, unusedPathForSchema string) []byte {
	t.Helper()
	path := filepath.Join(t.TempDir(), "schemaless.db")
	db := openEmptySQLite(t, path)
	db.Close()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
