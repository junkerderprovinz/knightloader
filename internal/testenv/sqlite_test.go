package testenv

import (
	"database/sql"
	"path/filepath"
	"testing"
)

// A hook that registered but set nothing would only show as a slow suite, so
// the settings are read back from a real connection.
func TestVolatileSQLiteReachesEveryNewConnection(t *testing.T) {
	VolatileSQLite()
	VolatileSQLite()

	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "x.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	var synchronous int
	if err := db.QueryRow(`PRAGMA synchronous`).Scan(&synchronous); err != nil {
		t.Fatal(err)
	}
	if synchronous != 0 {
		t.Errorf("PRAGMA synchronous = %d, want 0 (off)", synchronous)
	}
	var journal string
	if err := db.QueryRow(`PRAGMA journal_mode`).Scan(&journal); err != nil {
		t.Fatal(err)
	}
	if journal != "memory" {
		t.Errorf("PRAGMA journal_mode = %q, want memory", journal)
	}
}
