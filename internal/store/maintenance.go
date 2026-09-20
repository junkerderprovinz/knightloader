package store

// The database file itself rather than its rows: how big it is, how much space
// deleted rows left behind, whether SQLite still considers it whole, and the
// commands that act on that.
//
// All of this runs on the live database through the one connection (see
// Open), so a VACUUM holds up every other read and write while it lasts. The
// layers above say so to the user. Long calls take a context so the app's
// shutdown can interrupt them; SQLite then rolls the rewrite back and the
// original file survives, instead of a container runtime killing the process
// mid-rewrite. internal/app/app_dbmaint.go tracks each run so Close cancels
// it and waits for the rollback.

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
)

// Sizes is what one look at the database file found. LogicalBytes and
// FileBytes are usually within a page of each other and differ while a
// journal is open, which is worth showing.
type Sizes struct {
	// PageSize, PageCount and FreePages are SQLite's own numbers, kept so a
	// page count that did not move while the file grew can be told apart.
	PageSize  int64
	PageCount int64
	FreePages int64

	// LogicalBytes is PageSize * PageCount, free pages included.
	LogicalBytes int64
	// FileBytes is the .db on disk plus any -journal, -wal or -shm beside it,
	// since those occupy disk for the database too.
	FileBytes int64

	// ReclaimableBytes is FreePages * PageSize: space deleted rows left inside
	// the file that only a compaction gives back. It is a floor, not a
	// prediction: VACUUM also repacks partly filled pages and rebuilds B-tree
	// nodes, so the real shrink is usually larger.
	ReclaimableBytes int64
}

// journalSuffixes are the working files SQLite may keep beside the database.
// All three are counted, since another build, tool or crash may have left one
// this process's journal mode would not create. internal/backup's
// ApplyPending clears the same list.
var journalSuffixes = []string{"-journal", "-wal", "-shm"}

// FileSize is the database file plus any journal beside it, measured with
// os.Stat alone. It never touches the connection, so it can answer while a
// compaction holds it.
func (s *Store) FileSize() (int64, error) {
	fi, err := os.Stat(s.path)
	if err != nil {
		return 0, fmt.Errorf("store: measure %s: %w", s.path, err)
	}
	total := fi.Size()
	for _, suffix := range journalSuffixes {
		// A missing journal counts as zero bytes.
		if jfi, jerr := os.Stat(s.path + suffix); jerr == nil {
			total += jfi.Size()
		}
	}
	return total, nil
}

// Sizes measures the database file, header pragmas included. The pragmas are
// cheap header reads but queue behind a compaction on the shared connection;
// callers that must answer during one use FileSize.
func (s *Store) Sizes() (Sizes, error) {
	var out Sizes
	for _, q := range []struct {
		pragma string
		into   *int64
	}{
		{`PRAGMA page_size`, &out.PageSize},
		{`PRAGMA page_count`, &out.PageCount},
		{`PRAGMA freelist_count`, &out.FreePages},
	} {
		if err := s.db.QueryRow(q.pragma).Scan(q.into); err != nil {
			return Sizes{}, fmt.Errorf("store: %s: %w", q.pragma, err)
		}
	}
	out.LogicalBytes = out.PageSize * out.PageCount
	out.ReclaimableBytes = out.PageSize * out.FreePages

	size, err := s.FileSize()
	if err != nil {
		return Sizes{}, err
	}
	out.FileBytes = size
	return out, nil
}

// DefaultIntegrityCheckMax is how many problems an integrity check reports,
// SQLite's own default.
const DefaultIntegrityCheckMax = 100

// maxIntegrityCheckMax caps what a caller may ask for; the list is rendered
// in one block in the browser.
const maxIntegrityCheckMax = 10000

// IntegrityCheck reads every page of the database and reports what SQLite
// makes of it, in SQLite's own words and order. An empty result means the
// file is whole.
//
// Every row is read, unlike the restore check in internal/backup, because here
// the list of damaged pages is the answer. The modernc.org/sqlite driver also
// returns all problems as one row of newline-separated lines, so both levels
// are flattened here and the cap applies to lines.
//
// "ok" is folded away so len(problems) == 0 is the whole test for intact.
// SQLite's "*** in database main ***" banner is kept: it is SQLite's wording
// and says which database the findings are about.
func (s *Store) IntegrityCheck(ctx context.Context, max int) ([]string, error) {
	if max <= 0 {
		max = DefaultIntegrityCheckMax
	}
	// A PRAGMA argument must be a literal, so the range-checked int is
	// formatted in; no caller string reaches the query.
	if max > maxIntegrityCheckMax {
		max = maxIntegrityCheckMax
	}
	rows, err := s.db.QueryContext(ctx, `PRAGMA integrity_check(`+strconv.Itoa(max)+`)`)
	if err != nil {
		return nil, fmt.Errorf("store: integrity check: %w", err)
	}
	defer rows.Close()
	problems := []string{}
	for rows.Next() {
		var row string
		if err := rows.Scan(&row); err != nil {
			return nil, fmt.Errorf("store: integrity check: %w", err)
		}
		for _, line := range strings.Split(row, "\n") {
			line = strings.TrimSpace(line)
			if line == "" || line == "ok" {
				continue
			}
			problems = append(problems, line)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: integrity check: %w", err)
	}
	// Capped again, since after flattening SQLite's count and ours differ.
	if len(problems) > max {
		problems = problems[:max]
	}
	return problems, nil
}

// Vacuum rewrites the whole database, giving the space deleted rows left
// behind back to the filesystem. The caller must warn about two costs:
//
//   - It holds the one connection for the entire rewrite.
//   - It needs room for a full second copy in the temporary directory
//     (SQLITE_TMPDIR, TMPDIR, /var/tmp, /usr/tmp, /tmp), which in a container
//     is the writable layer, not the data volume. The app reports that
//     directory's free space alongside.
//
// It preserves user_version, which maintenance_test.go checks: a lost stamp
// would re-run non-idempotent migrations and stop the app from starting.
func (s *Store) Vacuum(ctx context.Context) error {
	if _, err := s.db.ExecContext(ctx, `VACUUM`); err != nil {
		return fmt.Errorf("store: compact: %w", err)
	}
	return nil
}

// Analyze re-measures the tables so the query planner has current statistics.
// It is quick and changes no data, but statements already prepared on the
// long-lived connection keep their plans, so the gain is complete only after a
// restart. The interface text says so and must not promise a faster list.
func (s *Store) Analyze(ctx context.Context) error {
	if _, err := s.db.ExecContext(ctx, `ANALYZE`); err != nil {
		return fmt.Errorf("store: refresh statistics: %w", err)
	}
	return nil
}

// Tag is the identity stamp kept in SQLite's application_id header field. Zero
// means it was never stamped.
//
// It tells whether the maintenance record, which lives in a file beside the
// database so it can be read when the database will not open, is about the
// database now present; a restore replaces the database without touching the
// record. Size and modification time move on every save and would invalidate
// the result within seconds. application_id needs no migration, survives
// VACUUM, and cannot be reached through the uistate route.
func (s *Store) Tag() (int64, error) {
	var tag int64
	if err := s.db.QueryRow(`PRAGMA application_id`).Scan(&tag); err != nil {
		return 0, fmt.Errorf("store: read database tag: %w", err)
	}
	return tag, nil
}

// SetTag stamps the database with a new identity. It is called at the end of
// every maintenance run: at the end so it marks the file just examined and
// proves VACUUM kept it, and on every run so a restored older backup carries a
// different stamp. Never at Open, which would change the file on every boot.
func (s *Store) SetTag(tag int64) error {
	// A PRAGMA value must be a literal; tag comes from this package's caller,
	// never from a request.
	if _, err := s.db.Exec(`PRAGMA application_id = ` + strconv.FormatInt(tag, 10)); err != nil {
		return fmt.Errorf("store: stamp database tag: %w", err)
	}
	return nil
}

// SchemaVersion is what PRAGMA user_version says, which is how far migrate has
// come. Only the test that checks VACUUM keeps it uses it; migrate runs its
// own query before a *Store exists.
func (s *Store) SchemaVersion() (int, error) {
	var v int
	if err := s.db.QueryRow(`PRAGMA user_version`).Scan(&v); err != nil {
		return 0, fmt.Errorf("store: read schema version: %w", err)
	}
	return v, nil
}
