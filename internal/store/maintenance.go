package store

// What the database file itself is doing, as opposed to what is in it: how big
// it has grown, how much of that is space deleted rows left behind, whether
// SQLite still considers it whole, and the two commands that do something about
// either of those.
//
// EVERY ONE OF THESE RUNS ON THE LIVE DATABASE, THROUGH THE ONE CONNECTION.
// Open sets SetMaxOpenConns(1) and its comment explains at length why that can
// never be raised; the consequence here is that a VACUUM does not merely take a
// while, it queues every other reader and writer in the process behind it for
// as long as it lasts. That is not a defect to be worked around at this layer -
// it is the truth about the thing, and the layers above (internal/app's
// maintenance runner, and the confirm dialog in the interface) are built around
// saying so out loud rather than hiding it.
//
// WHICH IS WHY EVERY LONG CALL HERE TAKES A CONTEXT. A bare db.Exec("VACUUM")
// cannot be interrupted: the process would sit in it until the rewrite finished,
// including through a Close that had already cancelled everything else, and a
// container runtime's ten-second grace period would then end in SIGKILL in the
// middle of a full-file rewrite. ExecContext lets the app's own shutdown reach
// it, at which point SQLite rolls the rewrite back and the original file is
// still there. See internal/app/app_dbmaint.go, which registers each run
// through App.track so Close cancels it and then waits for that rollback.

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
)

// Sizes is what one look at the database file found.
//
// The pair to read carefully is LogicalBytes against FileBytes. The first is
// what SQLite says the database occupies (its own page accounting); the second
// is what the filesystem says the file plus any journal beside it costs. They
// are usually within a page of each other, and they come apart exactly when a
// journal is open - which is a fact worth showing rather than smoothing over,
// since it is the difference between "the database is 4 GB" and "4 GB of disk
// is spoken for by the database right now".
type Sizes struct {
	// PageSize, PageCount and FreePages are SQLite's own three numbers, kept
	// rather than only the bytes derived from them: a page count that has not
	// moved while the file grew is a different story from one that has, and a
	// report that only carried the product could not tell them apart.
	PageSize  int64
	PageCount int64
	FreePages int64

	// LogicalBytes is PageSize * PageCount - the size of the database as
	// SQLite accounts for it, free pages included.
	LogicalBytes int64
	// FileBytes is the .db on disk plus any -journal, -wal or -shm sitting
	// beside it. Those three are the database's working files, not clutter: a
	// rollback journal open at this instant really is occupying that space,
	// and the question this number answers ("how much disk is the database
	// costing me") has to include it.
	FileBytes int64

	// ReclaimableBytes is FreePages * PageSize: space that deleted rows left
	// inside the file, which still belongs to the file on disk and which only
	// a compaction gives back.
	//
	// IT IS A FLOOR, NOT A PROMISE, and the interface label that renders it
	// depends on that distinction. VACUUM does not merely drop the free list;
	// it rewrites every page, so partly filled pages are repacked and B-tree
	// nodes are rebuilt at their natural fill. The real shrink is therefore
	// usually LARGER than this, sometimes considerably, on a database that has
	// had a lot of rows deleted from the middle. Reporting it as an exact
	// prediction would have the readout undershoot every single time and look
	// like a bug in the arithmetic rather than what it is - the conservative
	// half of an answer nobody can compute exactly without doing the work.
	ReclaimableBytes int64
}

// journalSuffixes are the working files SQLite may keep beside the database.
// All three, not only the one this build's journal mode produces: the file on
// disk may have been left by another build, another tool, or a crash, and a
// size readout that ignored a 2 GB write-ahead log because this process happens
// to be in rollback mode would be wrong in exactly the situation somebody came
// to the page to understand. This is the same list internal/backup's
// ApplyPending clears after a restore, and for the same reason - these three
// spellings are what SQLite actually puts there.
var journalSuffixes = []string{"-journal", "-wal", "-shm"}

// FileSize is the database file plus any journal beside it, measured with
// os.Stat alone.
//
// IT NEVER TOUCHES THE CONNECTION, and that is the whole reason it is a
// separate entry point from Sizes below rather than a line inside it. A
// compaction holds the one connection for the length of the rewrite, so any
// query at all - a pragma included - blocks until it is over. A page polling
// "is it done yet" every two seconds, and a diagnostics request arriving while
// it runs, must both be answerable during exactly that window. This is what
// they call.
func (s *Store) FileSize() (int64, error) {
	fi, err := os.Stat(s.path)
	if err != nil {
		return 0, fmt.Errorf("store: measure %s: %w", s.path, err)
	}
	total := fi.Size()
	for _, suffix := range journalSuffixes {
		// Missing is the ordinary case for at least two of the three at any
		// moment, so a stat error is passed over rather than reported: "there
		// is no write-ahead log" and "there is one and it is 0 bytes" are the
		// same answer to the question this number is asking.
		if jfi, jerr := os.Stat(s.path + suffix); jerr == nil {
			total += jfi.Size()
		}
	}
	return total, nil
}

// Sizes measures the database file, header pragmas included.
//
// The three pragmas are reads against the one shared connection, so they queue
// behind whatever else is using it - including behind a compaction, for its
// whole length. Callers that must answer while one is running use FileSize
// above instead and go without the free-page figure. The pragmas themselves are
// cheap: page_count and freelist_count are header fields, not scans.
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

// DefaultIntegrityCheckMax is how many problems an integrity check is asked to
// report. SQLite's own default is 100 and this matches it: the list is for a
// person deciding what to do next, and the hundredth line has already told them
// everything the thousandth would.
const DefaultIntegrityCheckMax = 100

// maxIntegrityCheckMax is the ceiling on what a caller may ask for. The list
// goes into a JSON document that a browser renders in one block; ten thousand
// lines of it is not a report, it is a way to make the page unusable at the one
// moment somebody needs it.
const maxIntegrityCheckMax = 10000

// IntegrityCheck reads every page of the database and reports what SQLite makes
// of it. An empty result means the file is whole; anything in the slice is
// SQLite's own wording, verbatim and in its own order.
//
// EVERY ROW, NOT THE FIRST. internal/backup/backup.go:248 runs the same pragma
// with QueryRow, and that is right where it stands - it is deciding whether to
// accept an uploaded restore bundle, and one line of "not ok" settles that. It
// is wrong here. This check exists to be read at the moment somebody has to
// decide whether their database is worth saving, and "which pages are damaged"
// is the entire content of that answer; QueryRow would throw away everything
// after the first row and leave the page saying "damaged" with one line under
// it.
//
// AND EVERY LINE INSIDE EVERY ROW, which is the part nobody would guess and
// which a test in this package had to find out. The documented shape of this
// pragma is one row per problem; the driver this build uses (modernc.org/sqlite)
// hands back a SINGLE row holding all of them, separated by newlines - measured,
// not assumed: a database with every page after the second overwritten returns
// exactly one row of ninety-odd lines. So reading "all the rows" faithfully and
// stopping there would produce a one-element list containing a wall of text: the
// caller's cap would mean nothing, the page would render one unbreakable
// paragraph, and the count it puts in front of the user ("SQLite reports N
// problems") would say 1 about ninety findings. Both levels are therefore
// flattened here, once, so that no caller has to know which driver it is talking
// to.
//
// The line "ok" is folded away rather than passed through, so that
// len(problems) == 0 is the whole test for "intact" at every call site. A
// caller that had to know the literal string "ok" is a caller that will one day
// compare it case-sensitively against a build that spells it differently.
// SQLite's own "*** in database main ***" banner is NOT folded away: it says
// which database the findings are about, it is SQLite's own wording, and
// dropping lines by pattern is how a filter eventually swallows the one line
// that mattered.
func (s *Store) IntegrityCheck(ctx context.Context, max int) ([]string, error) {
	if max <= 0 {
		max = DefaultIntegrityCheckMax
	}
	// A PRAGMA argument cannot be a bound parameter - SQLite's grammar allows a
	// literal there and nothing else, which is why migrate() a few lines up in
	// store.go formats `PRAGMA user_version = %d` rather than binding it. So
	// the value is turned into a decimal by strconv, from an int that has just
	// been range-checked: there is no path from a caller's string to this
	// query.
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
	// The cap is applied again on this side because SQLite counts what IT was
	// asked to find and this function counts what it hands back, and after the
	// flattening above those are no longer the same number.
	if len(problems) > max {
		problems = problems[:max]
	}
	return problems, nil
}

// Vacuum rewrites the whole database at its real size, giving the space deleted
// rows left behind back to the filesystem.
//
// TWO THINGS IT COSTS, both of which the caller has to have told somebody about
// before this is reached:
//
//   - It holds the one connection for the entire rewrite, so every other read
//     and write in the process waits. See this file's own header.
//   - It needs room for a full second copy of the database, and that copy does
//     NOT go beside the original. SQLite writes it to the temporary directory
//     (SQLITE_TMPDIR, then TMPDIR, then /var/tmp, /usr/tmp, /tmp), which on a
//     container is the writable layer rather than the mounted data volume. The
//     app layer reports that directory and its free space alongside this, so
//     that "database or disk is full" does not send somebody to look at a data
//     volume with terabytes free.
//
// It preserves user_version, and internal/store/maintenance_test.go proves it
// rather than trusting it: a lost schema stamp would re-run migration 2, whose
// ALTER TABLE ADD COLUMN is not idempotent, and the process would refuse to
// start on a database that is perfectly intact.
func (s *Store) Vacuum(ctx context.Context) error {
	if _, err := s.db.ExecContext(ctx, `VACUUM`); err != nil {
		return fmt.Errorf("store: compact: %w", err)
	}
	return nil
}

// Analyze re-measures the tables so the query planner has current statistics.
//
// It is quick, it changes no user data, and it is honestly of limited use to
// THIS process: SQLite consults sqlite_stat1 when a statement is prepared, and
// this app holds one connection open for its whole life, so anything already
// prepared keeps the plan it was given. The gain lands on statements prepared
// after this point and is fully there after the next restart. The interface
// text says exactly that; do not let it be reworded into a promise of a faster
// list.
func (s *Store) Analyze(ctx context.Context) error {
	if _, err := s.db.ExecContext(ctx, `ANALYZE`); err != nil {
		return fmt.Errorf("store: refresh statistics: %w", err)
	}
	return nil
}

// Tag is the identity stamp this package keeps in the database's own header, in
// SQLite's application_id field. Zero means nothing has ever stamped it.
//
// WHAT IT IS FOR is one question the maintenance record cannot answer any other
// way: is the database I am looking at the one this record was written about?
// The record lives in a file beside the database (it has to - the moment you
// most want to read "the check failed on the 3rd" is the moment the database
// will not open), and internal/backup's ApplyPending replaces the database
// under it on the next boot without touching it. Left alone, the page would
// report "checked clean two days ago" about a file that arrived from a backup
// an hour ago.
//
// WHY NOT THE FILE'S SIZE AND MODIFICATION TIME, which is the obvious answer:
// because both of them move every time anything is saved. A check that compared
// them would invalidate its own result within seconds of a download writing a
// row, and the panel would go back to "nothing has run yet" moments after
// somebody pressed a button and read the answer. That is a worse lie than the
// one it set out to prevent.
//
// WHY application_id AND NOT A ROW IN A TABLE: it needs no migration (store.go's
// own rule is that a shipped migration is never edited, and appending one that
// runs on the single connection during Open is how a boot turns into a hang
// with no interface up to explain it), it is in the file header so it survives
// VACUUM, and it is not reachable through the uistate key/value route the way a
// row would be.
func (s *Store) Tag() (int64, error) {
	var tag int64
	if err := s.db.QueryRow(`PRAGMA application_id`).Scan(&tag); err != nil {
		return 0, fmt.Errorf("store: read database tag: %w", err)
	}
	return tag, nil
}

// SetTag stamps the database with a new identity.
//
// Called at the END of every maintenance run, never at the start and never at
// Open. At the end, because the point is to stamp the file that was just
// examined - and because a stamp written before a VACUUM would be relying on
// VACUUM preserving application_id rather than proving it. On every run rather
// than once, because that is what lets a restore of THIS database's own older
// backup be spotted: the restored file carries the stamp from whenever it was
// taken, which is not the stamp of the last run unless no run has happened
// since, in which case the record really is about that content.
//
// Never at Open, because Open runs before anything is in front of a person and
// a write there would be an unasked-for change to the file on every boot.
func (s *Store) SetTag(tag int64) error {
	// Same story as IntegrityCheck's max above: a PRAGMA value is a literal in
	// SQLite's grammar, so it is formatted rather than bound. The value comes
	// from this package's own caller, never from a request.
	if _, err := s.db.Exec(`PRAGMA application_id = ` + strconv.FormatInt(tag, 10)); err != nil {
		return fmt.Errorf("store: stamp database tag: %w", err)
	}
	return nil
}

// SchemaVersion is what PRAGMA user_version says, which is how far migrate()
// has come. Exported for one caller: the test that proves a VACUUM does not
// reset it. Nothing in the running app reads it - migrate() has its own copy of
// this query, deliberately, because that one runs before there is a *Store to
// call a method on.
func (s *Store) SchemaVersion() (int, error) {
	var v int
	if err := s.db.QueryRow(`PRAGMA user_version`).Scan(&v); err != nil {
		return 0, fmt.Errorf("store: read schema version: %w", err)
	}
	return v, nil
}
