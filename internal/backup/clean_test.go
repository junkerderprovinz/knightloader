package backup

import (
	"archive/zip"
	"bytes"
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	"github.com/junkerderprovinz/knightloader/internal/store"
)

// planted is a trigger that writes a removed passkey straight back, under a
// name that would end a statement and drop the tasks table if it were ever
// spliced into SQL, and a view beside it.
var planted = []string{
	`CREATE TRIGGER "x""; DROP TABLE tasks; --" AFTER DELETE ON passkeys
	   BEGIN INSERT INTO passkeys (id, credential_id, public_key) VALUES ('back', x'01', x'02'); END`,
	`CREATE VIEW keys AS SELECT * FROM passkeys`,
}

// databaseWith builds a store with one passkey per name and the given
// statements run on it, and returns its file.
func databaseWith(t *testing.T, passkeys []string, extra ...string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "knightloader.db")
	s, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	for i, name := range passkeys {
		if _, err := s.AddPasskey(store.Passkey{ID: name, Name: name, CredentialID: []byte{byte(i + 1), 9}, PublicKey: []byte{7}}); err != nil {
			t.Fatal(err)
		}
	}
	s.Close()
	if len(extra) > 0 {
		db, err := sql.Open("sqlite", path)
		if err != nil {
			t.Fatal(err)
		}
		for _, q := range extra {
			if _, err := db.Exec(q); err != nil {
				t.Fatalf("%s: %v", q, err)
			}
		}
		db.Close()
	}
	return path
}

func archiveOf(t *testing.T, dbPath string) []byte {
	t.Helper()
	raw, err := os.ReadFile(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	var archive bytes.Buffer
	zw := zip.NewWriter(&archive)
	mustWriteEntry(t, zw, manifestEntry, []byte(`{"version":"v1.2.3"}`))
	mustWriteEntry(t, zw, settingsEntry, testSettingsJSON(t))
	mustWriteEntry(t, zw, dbEntry, raw)
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return archive.Bytes()
}

// schemaCode counts the triggers and views in the database at path.
func schemaCode(t *testing.T, path string) int {
	t.Helper()
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var n int
	if err := db.QueryRow(`SELECT count(*) FROM sqlite_master WHERE type IN ('trigger', 'view')`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func passkeyIDs(t *testing.T, path string) []string {
	t.Helper()
	s, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	keys, err := s.ListPasskeys()
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, k := range keys {
		ids = append(ids, k.ID)
	}
	return ids
}

// A backup is a file anybody can hand in. What it brings as code, a trigger or
// a view, is gone before it is staged, so nothing of it runs once it is the
// live database.
func TestARestoreBringsNoTriggerOrViewAlong(t *testing.T) {
	dataDir := t.TempDir()
	if _, err := Stage(dataDir, archiveOf(t, databaseWith(t, []string{"old"}, planted...)), "v1.2.3"); err != nil {
		t.Fatalf("Stage: %v", err)
	}
	staged := filepath.Join(dataDir, pendingDirName, dbEntry)
	if n := schemaCode(t, staged); n != 0 {
		t.Fatalf("the staged database still has %d triggers or views", n)
	}
	if _, _, err := ApplyPending(dataDir); err != nil {
		t.Fatalf("ApplyPending: %v", err)
	}
	live := filepath.Join(dataDir, dbEntry)
	s, err := store.Open(live)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.AddPasskey(store.Passkey{ID: "new", CredentialID: []byte{42}, PublicKey: []byte{7}}); err != nil {
		t.Fatal(err)
	}
	if err := s.DeletePasskey("new"); err != nil {
		t.Fatal(err)
	}
	s.Close()
	if ids := passkeyIDs(t, live); len(ids) != 0 {
		t.Errorf("removing a passkey from the restored database left %v", ids)
	}
}

// A restore staged by a build that did not strip the database is stripped when
// it is put in place.
func TestApplyingAnUnstrippedRestoreStripsIt(t *testing.T) {
	dataDir := t.TempDir()
	pending := filepath.Join(dataDir, pendingDirName)
	if err := os.MkdirAll(pending, 0o755); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(databaseWith(t, []string{"old"}, planted...))
	if err != nil {
		t.Fatal(err)
	}
	for name, data := range map[string][]byte{dbEntry: raw, settingsEntry: testSettingsJSON(t)} {
		if err := os.WriteFile(filepath.Join(pending, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if _, _, err := ApplyPending(dataDir); err != nil {
		t.Fatalf("ApplyPending: %v", err)
	}
	live := filepath.Join(dataDir, dbEntry)
	if n := schemaCode(t, live); n != 0 {
		t.Errorf("the restored database has %d triggers or views", n)
	}
	if ids := passkeyIDs(t, live); len(ids) != 0 {
		t.Errorf("the restored database has the backup's passkeys %v", ids)
	}
}

// The login of the running server is what stays. A backup made before a
// passkey was removed would otherwise bring that key back as a way in, and one
// registered since would be lost.
func TestARestoreKeepsTheRunningServersPasskeys(t *testing.T) {
	dataDir := t.TempDir()
	current := databaseWith(t, []string{"current"})
	raw, err := os.ReadFile(current)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dataDir, dbEntry), raw, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Stage(dataDir, archiveOf(t, databaseWith(t, []string{"removed"})), "v1.2.3"); err != nil {
		t.Fatalf("Stage: %v", err)
	}
	if ids := passkeyIDs(t, filepath.Join(dataDir, pendingDirName, dbEntry)); len(ids) != 0 {
		t.Errorf("the staged database carries the backup's passkeys %v", ids)
	}
	if _, _, err := ApplyPending(dataDir); err != nil {
		t.Fatalf("ApplyPending: %v", err)
	}
	ids := passkeyIDs(t, filepath.Join(dataDir, dbEntry))
	if len(ids) != 1 || ids[0] != "current" {
		t.Errorf("after the restore the passkeys are %v, want only the running server's [current]", ids)
	}
}
