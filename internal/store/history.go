package store

// The download history: the record of what this instance has actually fetched,
// and the two queries retention is built out of.
//
// It is a table of its own rather than a column on the task, because the task
// list is a working set and the history is not. The list is cleared by hand, it
// is trimmed by retention, and the moment either happens "what did this box
// download last month" has no answer left anywhere. Nothing joins the two:
// deleting a task deliberately leaves its history row standing, which is the
// whole point of the separation.
//
// What it deliberately does not keep: the folder the file went to. The store
// only sees Task.Dir, which is the per-task OVERRIDE and empty for almost every
// download - the real destination is worked out from the settings, the package
// and a path template at the moment the transfer starts. A column that is right
// one time in twenty is worse than no column, because the twenty-first person
// to read it will not know which one they got.

import (
	"database/sql"
	"strings"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/core"
)

// historyColumns is the row, in the order every statement in this file writes
// and reads it.
const historyColumns = `id,url,name,package,host,resolver,size,created_at,finished_at`

// HistoryEntry is one finished download as the history keeps it.
type HistoryEntry struct {
	// TaskID is the task it was fetched as. The task itself may be long gone -
	// that is what this table is for - so it is a handle for matching a row that
	// is still in the list, never something to look up.
	TaskID string `json:"taskId"`
	URL    string `json:"url"`
	Name   string `json:"name"`
	// Package, Host and Resolver are the three things somebody searches this
	// list by: what it belonged to, where it came from, and which backend got it.
	Package  string `json:"package,omitempty"`
	Host     string `json:"host,omitempty"`
	Resolver string `json:"resolver,omitempty"`
	Size     int64  `json:"size"`
	// CreatedAt is when the link was added, kept beside FinishedAt because the
	// pair is the only record of how long something took.
	CreatedAt  time.Time `json:"createdAt"`
	FinishedAt time.Time `json:"finishedAt"`
}

// stampFinish settles the invariant the whole of this file rests on: a task row
// carries a finish time exactly while it is done.
//
// IT HAPPENS HERE, IN THE STORE, and not where a backend reports the last byte,
// because this is the one place every task change passes through whichever code
// path produced it. A stamp written at one of the settle sites is a stamp the
// next settle site - a new backend, an unpacking that completes a task, a
// restart that finishes what it picked up - has to remember to write too, and
// the column would then be empty for exactly the downloads somebody added
// support for last.
//
// The stamp is taken once and then kept. A task saved again while it is still
// done is a checksum verdict or an unpacking result arriving late, not a second
// finish, so the time already in the row wins over the zero the caller is
// carrying. A task that LEAVES the done state - restarted by hand, handed on to
// the next backend - loses the stamp again, which is why the clearing half is
// here as well: without it a row could claim to have finished and still be
// running, and retention would remove a live download from the list.
//
// It writes to the task it was handed, which in this app is always a copy: the
// caller broadcasts that same copy a line later, so the row and the screen say
// the same thing about a download that has just finished.
func (s *Store) stampFinish(t *core.Task) {
	if t.Status != core.StatusDone {
		t.FinishedAt = time.Time{}
		return
	}
	if !t.FinishedAt.IsZero() {
		return
	}
	var prev int64
	if err := s.db.QueryRow(`SELECT finished_at FROM tasks WHERE id=?`, t.ID).Scan(&prev); err == nil && prev > 0 {
		t.FinishedAt = time.UnixMilli(prev)
		return
	}
	// Truncated to the precision the column has, so the copy that goes out to
	// the browser and the row that is read back after a restart are the same
	// instant rather than two that differ in the microseconds.
	t.FinishedAt = time.UnixMilli(time.Now().UnixMilli())
}

// recordFinished files a finished download in the history, and does nothing for
// a task in any other state.
//
// One row per task, updated in place rather than appended to. A finished
// download that is saved five more times - the checksum verdict, the unpacking,
// a rename - is one thing that was fetched, and five rows saying so would make
// the history unreadable in exactly the direction people scroll it. A task
// restarted and finished again updates its row, because that is still the same
// link arriving.
func (s *Store) recordFinished(t *core.Task) error {
	if t.Status != core.StatusDone {
		return nil
	}
	_, err := s.db.Exec(
		`INSERT INTO history (`+historyColumns+`) VALUES (?,?,?,?,?,?,?,?,?)
		 ON CONFLICT(id) DO UPDATE SET
		   url=excluded.url, name=excluded.name, package=excluded.package,
		   host=excluded.host, resolver=excluded.resolver, size=excluded.size,
		   created_at=excluded.created_at, finished_at=excluded.finished_at`,
		t.ID, t.URL, t.Name, t.Package, t.Host, t.Resolver, t.Size,
		t.CreatedAt.UnixMilli(), t.FinishedAt.UnixMilli())
	return err
}

// History reports what this instance has fetched, newest first. A limit of zero
// or less returns everything, which is what an export wants and what a page
// never asks for.
func (s *Store) History(limit int) ([]HistoryEntry, error) {
	q := `SELECT ` + historyColumns + ` FROM history ORDER BY finished_at DESC, rowid DESC`
	args := []any{}
	if limit > 0 {
		q += ` LIMIT ?`
		args = append(args, limit)
	}
	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	// Never nil, so a client reads an empty list rather than null.
	out := []HistoryEntry{}
	for rows.Next() {
		var e HistoryEntry
		var created, finished int64
		if err := rows.Scan(&e.TaskID, &e.URL, &e.Name, &e.Package, &e.Host,
			&e.Resolver, &e.Size, &created, &finished); err != nil {
			return nil, err
		}
		e.CreatedAt = time.UnixMilli(created)
		e.FinishedAt = time.UnixMilli(finished)
		out = append(out, e)
	}
	return out, rows.Err()
}

// TrimHistory keeps the newest max entries and deletes the rest, reporting how
// many went. A max of zero or less keeps everything.
//
// The cut is by row rather than by age on purpose: an instance that downloads
// nothing for a year should still have its history, and an instance that
// downloads ten thousand files a week should not have a database growing
// without a ceiling on the same disk the files land on.
func (s *Store) TrimHistory(max int) (int, error) {
	if max <= 0 {
		return 0, nil
	}
	// rowid, not finished_at: two downloads that finished in the same
	// millisecond are common (a batch settling together), and a cut on the
	// timestamp alone would either keep both or drop both and never land on max.
	res, err := s.db.Exec(
		`DELETE FROM history WHERE rowid NOT IN (
		   SELECT rowid FROM history ORDER BY finished_at DESC, rowid DESC LIMIT ?)`, max)
	if err != nil {
		return 0, err
	}
	n, err := res.RowsAffected()
	return int(n), err
}

// ClearHistory empties the record. It is a deliberate act with a button of its
// own: the history is the one thing in this database that outlives the list, so
// nothing else in the app is allowed to empty it as a side effect.
func (s *Store) ClearHistory() error {
	_, err := s.db.Exec(`DELETE FROM history`)
	return err
}

// FinishedBefore reports the tasks that finished before cutoff, which is the
// whole of what retention needs to know.
//
// It asks the store rather than the app's task list because the row is the
// authority on when something finished - the stamp is written here - and
// because the query is what the finished_at index exists for. A zero stamp is
// excluded: it means the row is not settled, and no cutoff should ever be able
// to reach a download that is still running.
func (s *Store) FinishedBefore(cutoff time.Time) ([]string, error) {
	rows, err := s.db.Query(
		`SELECT id FROM tasks WHERE status=? AND finished_at>0 AND finished_at<?`,
		string(core.StatusDone), cutoff.UnixMilli())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

// FinishTimes reports the finish times recorded for the given tasks, leaving
// out the ones that have none.
//
// It exists because the stamp is written onto the copy that is saved and not
// onto the task the app holds in memory, so the app has one way to ask the row
// what it decided. Asking for the handful of ids it is unsure about rather than
// reading the table keeps that reconciliation off the size of the list.
func (s *Store) FinishTimes(ids []string) (map[string]time.Time, error) {
	out := map[string]time.Time{}
	if len(ids) == 0 {
		return out, nil
	}
	args := make([]any, len(ids))
	for i, id := range ids {
		args[i] = id
	}
	q := `SELECT id,finished_at FROM tasks WHERE finished_at>0 AND id IN (` +
		strings.TrimSuffix(strings.Repeat("?,", len(ids)), ",") + `)`
	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		var at int64
		if err := rows.Scan(&id, &at); err != nil {
			return nil, err
		}
		out[id] = time.UnixMilli(at)
	}
	return out, rows.Err()
}

// historyRows is how many entries the history holds. Only the tests and the
// trim have a use for it; a client asking for the history gets the rows.
func (s *Store) historyRows() (int, error) {
	var n int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM history`).Scan(&n)
	if err == sql.ErrNoRows {
		return 0, nil
	}
	return n, err
}
