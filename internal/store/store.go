// Package store persists tasks in a local SQLite database (pure-Go driver, no cgo).
package store

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/core"
	_ "modernc.org/sqlite"
)

type Store struct{ db *sql.DB }

// migrations run in order, exactly once each. The database records how far it
// has come in PRAGMA user_version, so an existing install keeps its tasks when
// a new column arrives. Never edit a shipped entry â€” append a new one.
var migrations = []string{
	// 1 â€” the original table.
	`CREATE TABLE IF NOT EXISTS tasks (
	   id         TEXT PRIMARY KEY,
	   url        TEXT,
	   name       TEXT,
	   package    TEXT,
	   resolver   TEXT,
	   size       INTEGER,
	   loaded     INTEGER,
	   speed      INTEGER,
	   status     TEXT,
	   error      TEXT,
	   created_at INTEGER
	 )`,
	// 2 â€” destination folder, archive password, availability, retry bookkeeping
	//     and queue ordering.
	`ALTER TABLE tasks ADD COLUMN dir TEXT NOT NULL DEFAULT ''`,
	`ALTER TABLE tasks ADD COLUMN password TEXT NOT NULL DEFAULT ''`,
	`ALTER TABLE tasks ADD COLUMN online TEXT NOT NULL DEFAULT ''`,
	`ALTER TABLE tasks ADD COLUMN retries INTEGER NOT NULL DEFAULT 0`,
	`ALTER TABLE tasks ADD COLUMN next_try INTEGER NOT NULL DEFAULT 0`,
	`ALTER TABLE tasks ADD COLUMN priority INTEGER NOT NULL DEFAULT 0`,
	`ALTER TABLE tasks ADD COLUMN position INTEGER NOT NULL DEFAULT 0`,
	// 3 â€” the verdict of a checksum verification.
	`ALTER TABLE tasks ADD COLUMN checksum TEXT NOT NULL DEFAULT ''`,
	// 4 â€” what a Packagizer rule decided about this task. Without these the rule
	//     applies once, at paste time, and is gone at the next restart: the
	//     archive a rule told the app not to unpack is unpacked, and a connection
	//     count set for one hoster falls back to the default. auto_extract is the
	//     one nullable column in the table, because "no rule had an opinion" is
	//     not the same answer as "a rule said no".
	`ALTER TABLE tasks ADD COLUMN comment TEXT NOT NULL DEFAULT ''`,
	`ALTER TABLE tasks ADD COLUMN chunks INTEGER NOT NULL DEFAULT 0`,
	`ALTER TABLE tasks ADD COLUMN auto_extract INTEGER`,
	`ALTER TABLE tasks ADD COLUMN matched_rules TEXT NOT NULL DEFAULT ''`,
	// 5 â€” every remaining column core.Task will need, added in one go rather than
	//     one per feature. This list is append-only and strictly ordered, so a
	//     dozen packages each appending their own ALTER TABLE would conflict in
	//     this one slice and have to be merged in sequence; done here it costs one
	//     migration and takes this file off the critical path.
	//
	//     enabled is the one that has to be read carefully. It is `DEFAULT 1`, not
	//     the 0 every other flag here gets, because ALTER TABLE writes the default
	//     into every row that already exists: with a 0 the first boot after the
	//     upgrade would disable every task in the store, and a queue that silently
	//     refuses to run is indistinguishable from a broken build.
	//
	//     resumable is nullable for the same reason auto_extract is: "nobody has
	//     asked yet" is a real answer, and read back as false it would warn about
	//     losing bytes that would in fact resume.
	`ALTER TABLE tasks ADD COLUMN finished_at INTEGER NOT NULL DEFAULT 0`,
	`ALTER TABLE tasks ADD COLUMN enabled INTEGER NOT NULL DEFAULT 1`,
	`ALTER TABLE tasks ADD COLUMN skipped INTEGER NOT NULL DEFAULT 0`,
	`ALTER TABLE tasks ADD COLUMN skip_reason TEXT NOT NULL DEFAULT ''`,
	`ALTER TABLE tasks ADD COLUMN hold INTEGER NOT NULL DEFAULT 0`,
	`ALTER TABLE tasks ADD COLUMN forced INTEGER NOT NULL DEFAULT 0`,
	`ALTER TABLE tasks ADD COLUMN download_password TEXT NOT NULL DEFAULT ''`,
	`ALTER TABLE tasks ADD COLUMN expected_hash TEXT NOT NULL DEFAULT ''`,
	`ALTER TABLE tasks ADD COLUMN connection TEXT NOT NULL DEFAULT ''`,
	`ALTER TABLE tasks ADD COLUMN host TEXT NOT NULL DEFAULT ''`,
	`ALTER TABLE tasks ADD COLUMN source TEXT NOT NULL DEFAULT ''`,
	`ALTER TABLE tasks ADD COLUMN mirror_of TEXT NOT NULL DEFAULT ''`,
	`ALTER TABLE tasks ADD COLUMN resumable INTEGER`,
	`ALTER TABLE tasks ADD COLUMN filename TEXT NOT NULL DEFAULT ''`,
	`ALTER TABLE tasks ADD COLUMN variant TEXT NOT NULL DEFAULT ''`,
	`ALTER TABLE tasks ADD COLUMN manual_package INTEGER NOT NULL DEFAULT 0`,
	`ALTER TABLE tasks ADD COLUMN reason TEXT NOT NULL DEFAULT ''`,
	`ALTER TABLE tasks ADD COLUMN origin TEXT NOT NULL DEFAULT ''`,
	`ALTER TABLE tasks ADD COLUMN changed_at INTEGER NOT NULL DEFAULT 0`,
	`ALTER TABLE tasks ADD COLUMN archive_part INTEGER NOT NULL DEFAULT 0`,
	// 6 â€” whatever the interface needs to remember per client: column widths, which
	//     packages are folded shut, the last settings page. An opaque blob per key,
	//     never a settings field per column, so a new column in the list is not a
	//     schema change and one browser's layout is not another's.
	`CREATE TABLE IF NOT EXISTS uistate (
	   key        TEXT PRIMARY KEY,
	   value      TEXT NOT NULL,
	   changed_at INTEGER NOT NULL DEFAULT 0
	 )`,
	// 7 â€” the download history: what this instance has fetched, kept where the
	//     task list being trimmed cannot reach it. See history.go for why it is a
	//     table of its own and why the destination folder is not in it.
	`CREATE TABLE IF NOT EXISTS history (
	   id          TEXT PRIMARY KEY,
	   url         TEXT NOT NULL,
	   name        TEXT NOT NULL,
	   package     TEXT NOT NULL DEFAULT '',
	   host        TEXT NOT NULL DEFAULT '',
	   resolver    TEXT NOT NULL DEFAULT '',
	   size        INTEGER NOT NULL DEFAULT 0,
	   created_at  INTEGER NOT NULL DEFAULT 0,
	   finished_at INTEGER NOT NULL DEFAULT 0
	 )`,
	// 8 â€” a finish time for the downloads that were already done when this
	//     column got a writer. Nothing recorded when they finished, so the
	//     upgrade is the only honest answer available, and it is the answer
	//     retention needs: without it every task finished by an older build has a
	//     zero stamp, is invisible to every cutoff, and stays in the list for
	//     ever â€” which is the accumulation retention exists to end.
	`UPDATE tasks SET finished_at = CAST(strftime('%s','now') AS INTEGER) * 1000
	  WHERE status = 'done' AND finished_at = 0`,
	// 9 â€” and those same downloads carried into the history in one pass, so the
	//     record does not begin at whatever moment this version was installed.
	//     After 8, because it reads the stamp that migration writes.
	`INSERT OR IGNORE INTO history (id,url,name,package,host,resolver,size,created_at,finished_at)
	   SELECT id,url,name,package,host,resolver,size,created_at,finished_at
	   FROM tasks WHERE status = 'done'`,
	// 10 â€” the order the history is always read in, and the one the trim cuts
	//     along. Descending, because every read of this table is "newest first".
	`CREATE INDEX IF NOT EXISTS history_finished_at ON history(finished_at DESC)`,
	// 11 â€” what retention sweeps on. The sweep runs on a timer for the life of
	//     the process, and without this it is a scan of every task in the list
	//     each time, to find the handful that have aged out.
	`CREATE INDEX IF NOT EXISTS tasks_finished_at ON tasks(finished_at)`,
}

func Open(path string) (*Store, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	// One connection, because a second one is what this database cannot survive.
	// SQLite takes a file-wide lock to write, a second connection writing at the
	// same moment is refused with SQLITE_BUSY at once, and every caller in the app
	// discards the error from Save â€” so of two updates that land together one is
	// simply lost, and the task comes back after a restart in a state that was
	// true for a fraction of a second. With a single connection the pool queues
	// the writers instead of letting them collide.
	db.SetMaxOpenConns(1)
	if err := migrate(db); err != nil {
		db.Close()
		return nil, err
	}
	return &Store{db: db}, nil
}

func migrate(db *sql.DB) error {
	var version int
	if err := db.QueryRow(`PRAGMA user_version`).Scan(&version); err != nil {
		return fmt.Errorf("store: read schema version: %w", err)
	}
	for i := version; i < len(migrations); i++ {
		if _, err := db.Exec(migrations[i]); err != nil {
			return fmt.Errorf("store: migration %d: %w", i+1, err)
		}
		// PRAGMA does not take a bound parameter.
		if _, err := db.Exec(fmt.Sprintf(`PRAGMA user_version = %d`, i+1)); err != nil {
			return fmt.Errorf("store: bump schema version to %d: %w", i+1, err)
		}
	}
	return nil
}

func (s *Store) Close() error { return s.db.Close() }

const columns = `id,url,name,package,resolver,size,loaded,speed,status,error,created_at,
	dir,password,online,retries,next_try,priority,position,checksum,
	comment,chunks,auto_extract,matched_rules,
	finished_at,enabled,skipped,skip_reason,hold,forced,download_password,expected_hash,
	connection,host,source,mirror_of,resumable,filename,variant,manual_package,
	reason,origin,changed_at,archive_part`

// placeholders is one ? per column, built from the list itself. Written out by
// hand it is a row of forty-three question marks that has to be recounted every
// time a column is added, and a miscount is an error at runtime rather than at
// compile time.
var placeholders = strings.TrimSuffix(strings.Repeat("?,", strings.Count(columns, ",")+1), ",")

func (s *Store) Save(t *core.Task) error {
	var nextTry int64
	if !t.NextTry.IsZero() {
		nextTry = t.NextTry.UnixMilli()
	}
	// The finish time is settled before the row is written rather than taken as
	// given, because this is the one place every task change passes through and
	// therefore the only one that cannot be forgotten by a new settle path. It
	// writes to t, which is a copy in every caller in this app - see stampFinish.
	s.stampFinish(t)
	// Zero rather than the epoch, so "never finished" and "finished at
	// 1970-01-01" stay apart on the way back in.
	var finishedAt, changedAt int64
	if !t.FinishedAt.IsZero() {
		finishedAt = t.FinishedAt.UnixMilli()
	}
	if !t.ChangedAt.IsZero() {
		changedAt = t.ChangedAt.UnixMilli()
	}
	// nil for the same reason auto_extract is nil: nobody having asked whether
	// this transfer resumes is not the same answer as "it does not".
	var resumable any
	if t.Resumable != nil {
		resumable = *t.Resumable
	}
	// nil rather than 0 or 1, because the column has to be able to say that no
	// rule had an opinion at all: read back as false, a task nothing was decided
	// about would stop obeying the global unpacking switch.
	var autoExtract any
	if t.AutoExtract != nil {
		autoExtract = *t.AutoExtract
	}
	matched := ""
	if len(t.MatchedRules) > 0 {
		b, err := json.Marshal(t.MatchedRules)
		if err != nil {
			return err
		}
		matched = string(b)
	}
	_, err := s.db.Exec(
		`INSERT OR REPLACE INTO tasks (`+columns+`)
		 VALUES (`+placeholders+`)`,
		t.ID, t.URL, t.Name, t.Package, t.Resolver, t.Size, t.Loaded, t.Speed,
		string(t.Status), t.Error, t.CreatedAt.UnixMilli(),
		t.Dir, t.Password, string(t.Online), t.Retries, nextTry, t.Priority, t.Position,
		t.Checksum, t.Comment, t.Chunks, autoExtract, matched,
		finishedAt, t.Enabled, t.Skipped, t.SkipReason, t.Hold, t.Forced,
		t.DownloadPassword, t.ExpectedHash, t.Connection, t.Host, t.Source, t.MirrorOf,
		resumable, t.Filename, t.Variant, t.ManualPackage,
		string(t.Reason), string(t.Origin), changedAt, t.ArchivePart)
	if err != nil {
		return err
	}
	// The history is written from the same save, so a finished download is in
	// the record before anything is allowed to trim it out of the list. It is a
	// no-op for a task in any other state.
	return s.recordFinished(t)
}

// Delete takes a task out of the LIST. It does not touch the history: a row
// there is the record that this instance fetched something, and clearing the
// list is not a statement about what was downloaded. That is the whole reason
// the history is not a view over this table.
func (s *Store) Delete(id string) error {
	_, err := s.db.Exec(`DELETE FROM tasks WHERE id=?`, id)
	return err
}

func (s *Store) All() ([]*core.Task, error) {
	rows, err := s.db.Query(`SELECT ` + columns + ` FROM tasks ORDER BY created_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*core.Task
	for rows.Next() {
		t := &core.Task{}
		var status, online, matched, reason, origin string
		var created, nextTry, finishedAt, changedAt int64
		var autoExtract, resumable sql.NullBool
		if err := rows.Scan(&t.ID, &t.URL, &t.Name, &t.Package, &t.Resolver,
			&t.Size, &t.Loaded, &t.Speed, &status, &t.Error, &created,
			&t.Dir, &t.Password, &online, &t.Retries, &nextTry, &t.Priority, &t.Position,
			&t.Checksum, &t.Comment, &t.Chunks, &autoExtract, &matched,
			&finishedAt, &t.Enabled, &t.Skipped, &t.SkipReason, &t.Hold, &t.Forced,
			&t.DownloadPassword, &t.ExpectedHash, &t.Connection, &t.Host, &t.Source, &t.MirrorOf,
			&resumable, &t.Filename, &t.Variant, &t.ManualPackage,
			&reason, &origin, &changedAt, &t.ArchivePart); err != nil {
			return nil, err
		}
		t.Status = core.Status(status)
		t.Online = core.Availability(online)
		t.Reason = core.Reason(reason)
		t.Origin = core.Origin(origin)
		t.CreatedAt = time.UnixMilli(created)
		if nextTry > 0 {
			t.NextTry = time.UnixMilli(nextTry)
		}
		if finishedAt > 0 {
			t.FinishedAt = time.UnixMilli(finishedAt)
		}
		if changedAt > 0 {
			t.ChangedAt = time.UnixMilli(changedAt)
		}
		if autoExtract.Valid {
			v := autoExtract.Bool
			t.AutoExtract = &v
		}
		if resumable.Valid {
			v := resumable.Bool
			t.Resumable = &v
		}
		if matched != "" {
			// A row written by a build that stored something else here is not worth
			// failing the whole reload over: the task itself is intact, and the list
			// of rule names is only there to explain where it landed.
			_ = json.Unmarshal([]byte(matched), &t.MatchedRules)
		}
		out = append(out, t)
	}
	return out, rows.Err()
}
