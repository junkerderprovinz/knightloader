package store

// The download history: what this instance has fetched, and the two queries
// retention is built from.
//
// It is a table of its own because the task list is a working set: cleared by
// hand, trimmed by retention, and then "what did this box download last month"
// would have no answer. Deleting a task leaves its history row standing.
//
// The destination folder is not kept. The store only sees Task.Dir, the
// per-task override that is empty for almost every download; the real folder
// is worked out from settings and templates when the transfer starts, and a
// column that is right only sometimes would mislead.

import (
	"strings"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/core"
)

// historyColumns is the row, in the order every statement here uses.
const historyColumns = `id,url,name,package,host,resolver,size,created_at,finished_at`

// HistoryEntry is one finished download as the history keeps it.
type HistoryEntry struct {
	// TaskID is the task it was fetched as. The task may be long gone, so it
	// only matches rows still in the list.
	TaskID string `json:"taskId"`
	URL    string `json:"url"`
	Name   string `json:"name"`
	// Package, Host and Resolver are what people search the list by.
	Package  string `json:"package,omitempty"`
	Host     string `json:"host,omitempty"`
	Resolver string `json:"resolver,omitempty"`
	Size     int64  `json:"size"`
	// CreatedAt is when the link was added; with FinishedAt it records how
	// long the download took.
	CreatedAt  time.Time `json:"createdAt"`
	FinishedAt time.Time `json:"finishedAt"`
}

// stampFinish keeps the invariant that a task row carries a finish time
// exactly while it is done.
//
// It lives in the store because every task change passes through here, so a
// new settle path (a backend, an unpacking, a restart) cannot forget it. The
// stamp is taken once and kept: a later save of a done task is a checksum or
// unpacking result, not a second finish. A task that leaves the done state
// loses the stamp, or retention could remove a running download.
//
// It writes to the task it is given, which callers pass as a copy and then
// broadcast, so the row and the screen agree.
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
	// Truncated to the column's millisecond precision, so the broadcast copy
	// and the row read back after a restart are the same instant.
	t.FinishedAt = time.UnixMilli(time.Now().UnixMilli())
}

// recordFinished files a finished download in the history and does nothing for
// a task in any other state. There is one row per task, updated in place: a
// done task saved again for its checksum, unpacking or a rename is still one
// download, and so is a task restarted and finished again.
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

// History reports what this instance has fetched, newest first. A limit of
// zero or less returns everything, as an export wants.
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
// many went. A max of zero or less keeps everything. The cut is by count, not
// age, so a quiet instance keeps its history and a busy one does not grow
// without limit on the disk its downloads use.
func (s *Store) TrimHistory(max int) (int, error) {
	if max <= 0 {
		return 0, nil
	}
	// Ordered by rowid as well, since a batch often finishes within one
	// millisecond and a cut on the timestamp alone could not land on max.
	res, err := s.db.Exec(
		`DELETE FROM history WHERE rowid NOT IN (
		   SELECT rowid FROM history ORDER BY finished_at DESC, rowid DESC LIMIT ?)`, max)
	if err != nil {
		return 0, err
	}
	n, err := res.RowsAffected()
	return int(n), err
}

// ClearHistory empties the record. It has a button of its own; nothing else
// empties the history as a side effect.
func (s *Store) ClearHistory() error {
	_, err := s.db.Exec(`DELETE FROM history`)
	return err
}

// FinishedBefore reports the tasks that finished before cutoff, which is what
// retention needs. The row is the authority on finish times and has an index
// for this. A zero stamp is excluded, so no cutoff can reach a running
// download.
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
// out those without one. The stamp is written onto the saved copy, not the
// task the app holds, so this is how the app reads it back for the few ids it
// needs.
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

// historyRows is how many entries the history holds.
func (s *Store) historyRows() (int, error) {
	var n int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM history`).Scan(&n)
	return n, err
}
