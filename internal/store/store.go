// Package store persists tasks in a local SQLite database (pure-Go driver, no cgo).
package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/core"
	_ "modernc.org/sqlite"
)

// Store is the one handle on the database. path is kept for callers that need
// to talk about the file itself (see Path), rather than spelling the file name
// a third time besides internal/app and internal/backup.
type Store struct {
	db   *sql.DB
	path string
}

// migrations run in order, exactly once each. The database records how far it
// has come in PRAGMA user_version, so an existing install keeps its tasks when
// a new column arrives. Never edit a shipped entry; append a new one.
var migrations = []string{
	// 1: the original table.
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
	// 2: destination folder, archive password, availability, retry bookkeeping
	// and queue ordering.
	`ALTER TABLE tasks ADD COLUMN dir TEXT NOT NULL DEFAULT ''`,
	`ALTER TABLE tasks ADD COLUMN password TEXT NOT NULL DEFAULT ''`,
	`ALTER TABLE tasks ADD COLUMN online TEXT NOT NULL DEFAULT ''`,
	`ALTER TABLE tasks ADD COLUMN retries INTEGER NOT NULL DEFAULT 0`,
	`ALTER TABLE tasks ADD COLUMN next_try INTEGER NOT NULL DEFAULT 0`,
	`ALTER TABLE tasks ADD COLUMN priority INTEGER NOT NULL DEFAULT 0`,
	`ALTER TABLE tasks ADD COLUMN position INTEGER NOT NULL DEFAULT 0`,
	// 3: the verdict of a checksum verification.
	`ALTER TABLE tasks ADD COLUMN checksum TEXT NOT NULL DEFAULT ''`,
	// 4: what a Packagizer rule decided, so it survives a restart. auto_extract
	// is nullable because "no rule had an opinion" differs from "a rule said
	// no".
	`ALTER TABLE tasks ADD COLUMN comment TEXT NOT NULL DEFAULT ''`,
	`ALTER TABLE tasks ADD COLUMN chunks INTEGER NOT NULL DEFAULT 0`,
	`ALTER TABLE tasks ADD COLUMN auto_extract INTEGER`,
	`ALTER TABLE tasks ADD COLUMN matched_rules TEXT NOT NULL DEFAULT ''`,
	// 5: the remaining core.Task columns in one migration.
	//
	// enabled defaults to 1 because ALTER TABLE writes the default into every
	// existing row, and 0 would disable every stored task on upgrade.
	// resumable is nullable like auto_extract: "not asked yet" read back as
	// false would warn about losing bytes that would in fact resume.
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
	// 6: per-client interface state (column widths, folded packages, the last
	// settings page), an opaque blob per key so a new column in the list is not
	// a schema change.
	`CREATE TABLE IF NOT EXISTS uistate (
	   key        TEXT PRIMARY KEY,
	   value      TEXT NOT NULL,
	   changed_at INTEGER NOT NULL DEFAULT 0
	 )`,
	// 7: the download history, out of reach of the task list being trimmed
	// (see history.go).
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
	// 8: a finish time for downloads that were done before anything recorded
	// one. The upgrade time is the only answer available, and without a stamp
	// retention would never remove them.
	`UPDATE tasks SET finished_at = CAST(strftime('%s','now') AS INTEGER) * 1000
	  WHERE status = 'done' AND finished_at = 0`,
	// 9: those downloads copied into the history, so it does not start at the
	// upgrade. After 8, because it reads the stamp 8 writes.
	`INSERT OR IGNORE INTO history (id,url,name,package,host,resolver,size,created_at,finished_at)
	   SELECT id,url,name,package,host,resolver,size,created_at,finished_at
	   FROM tasks WHERE status = 'done'`,
	// 10: the history is always read newest first and trimmed along this order.
	`CREATE INDEX IF NOT EXISTS history_finished_at ON history(finished_at DESC)`,
	// 11: retention sweeps on a timer and would otherwise scan every task.
	`CREATE INDEX IF NOT EXISTS tasks_finished_at ON tasks(finished_at)`,
	// 12: the files selected inside a multi-file torrent, as JSON. Unlike the
	// swarm numbers (core.TorrentStats) this is the user's decision and must
	// survive a restart.
	`ALTER TABLE tasks ADD COLUMN torrent_files TEXT NOT NULL DEFAULT ''`,
	// 13: which torrent a task is: the info hash and the tracker list (JSON)
	// resolved at stage time (see core.Task.InfoHash).
	`ALTER TABLE tasks ADD COLUMN info_hash TEXT NOT NULL DEFAULT ''`,
	`ALTER TABLE tasks ADD COLUMN trackers TEXT NOT NULL DEFAULT ''`,
	// Whether a transfer uses an account or goes anonymously. Stored rather
	// than re-derived because the hoster lists it depends on arrive from the
	// JDownloader sidecar a few seconds after boot.
	`ALTER TABLE tasks ADD COLUMN mode TEXT NOT NULL DEFAULT ''`,
	// The category a link is filed under and the folder its extracted content
	// moves to. Both are decisions about the task and must survive a restart.
	`ALTER TABLE tasks ADD COLUMN category TEXT NOT NULL DEFAULT ''`,
	`ALTER TABLE tasks ADD COLUMN extract_dir TEXT NOT NULL DEFAULT ''`,
	// The registered WebAuthn passkeys (see passkeys.go). They live here
	// rather than in auth.json because nothing in them is secret: auth.json
	// holds the password hash and keys, these are public keys and handles that
	// belong with the rest of the backed-up rows.
	`CREATE TABLE IF NOT EXISTS passkeys (
	   id            TEXT PRIMARY KEY,
	   name          TEXT NOT NULL DEFAULT '',
	   credential_id BLOB NOT NULL,
	   public_key    BLOB NOT NULL,
	   aaguid        BLOB NOT NULL DEFAULT x'',
	   sign_count    INTEGER NOT NULL DEFAULT 0,
	   transports    TEXT NOT NULL DEFAULT '',
	   rp_id         TEXT NOT NULL DEFAULT '',
	   backed_up     INTEGER NOT NULL DEFAULT 0,
	   created_at    INTEGER NOT NULL DEFAULT 0,
	   last_used_at  INTEGER NOT NULL DEFAULT 0
	 )`,
	// One row per authenticator, or the sign-counter check that detects a
	// cloned key would compare against whichever duplicate it found first.
	`CREATE UNIQUE INDEX IF NOT EXISTS passkeys_credential_id ON passkeys(credential_id)`,
	// A variant row a hoster preset set aside, and the audio row's bitrate
	// pick. Both are decisions about the row: without the first a restart
	// would show every set-aside row again, without the second the download
	// would run at a bitrate nobody chose.
	`ALTER TABLE tasks ADD COLUMN variant_off INTEGER NOT NULL DEFAULT 0`,
	`ALTER TABLE tasks ADD COLUMN audio_bitrate TEXT NOT NULL DEFAULT ''`,
	// When a pending auto-confirm countdown runs out, in Unix milliseconds, 0
	// for none. Without it a restart would leave the batch in the collector.
	`ALTER TABLE tasks ADD COLUMN confirm_due INTEGER NOT NULL DEFAULT 0`,
}

func Open(path string) (*Store, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	// One connection: SQLite locks the whole file to write, a second writer
	// gets SQLITE_BUSY at once, and callers ignore Save's error, so one of two
	// concurrent updates would be lost. A single connection queues them.
	db.SetMaxOpenConns(1)
	if err := migrate(db, migrations); err != nil {
		db.Close()
		return nil, err
	}
	return &Store{db: db, path: path}, nil
}

// Path is the file this store was opened on, as given to Open. It is for
// talking about the file (its size, or which file to rescue after a failed
// integrity check), never for opening a second connection (see Open).
func (s *Store) Path() string { return s.path }

// Ping asks the database a trivial question and reports whether it answered.
//
// sql.DB.PingContext only proves a connection can be obtained, which succeeds
// even when the file has been unmounted, deleted or made read-only. SELECT 1
// takes the same path as every Save and All.
//
// Callers must pass a deadline: the single connection is shared, and Vacuum
// holds it for a whole rewrite, so without one a status page could hang for
// minutes. The query reads nothing real, so it costs the same on every install.
func (s *Store) Ping(ctx context.Context) error {
	var one int
	if err := s.db.QueryRowContext(ctx, `SELECT 1`).Scan(&one); err != nil {
		return fmt.Errorf("store: ping: %w", err)
	}
	return nil
}

// migrate brings the schema up to date with steps. A fresh database takes them
// all in one transaction, so a failure leaves no half-built schema behind. An
// existing one takes each pending step together with its version stamp, so a
// step that ran is never run again: a second ALTER TABLE ADD COLUMN fails and
// would keep the store from opening.
func migrate(db *sql.DB, steps []string) error {
	var version int
	if err := db.QueryRow(`PRAGMA user_version`).Scan(&version); err != nil {
		return fmt.Errorf("store: read schema version: %w", err)
	}
	if version == 0 {
		return applySteps(db, steps, 0, len(steps))
	}
	for i := version; i < len(steps); i++ {
		if err := applySteps(db, steps, i, i+1); err != nil {
			return err
		}
	}
	return nil
}

// applySteps runs steps[from:to] and records to as the schema version in one
// transaction.
func applySteps(db *sql.DB, steps []string, from, to int) error {
	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("store: begin migration %d: %w", from+1, err)
	}
	defer tx.Rollback()
	for i := from; i < to; i++ {
		if _, err := tx.Exec(steps[i]); err != nil {
			return fmt.Errorf("store: migration %d: %w", i+1, err)
		}
	}
	// PRAGMA does not take a bound parameter.
	if _, err := tx.Exec(fmt.Sprintf(`PRAGMA user_version = %d`, to)); err != nil {
		return fmt.Errorf("store: bump schema version to %d: %w", to, err)
	}
	return tx.Commit()
}

func (s *Store) Close() error { return s.db.Close() }

// BackupTo writes a consistent, standalone copy of the database to path with
// VACUUM INTO. Through the single shared connection it runs wholly before or
// after any concurrent write, which a raw file copy could not promise. path
// must not exist yet; VACUUM INTO refuses to overwrite.
func (s *Store) BackupTo(path string) error {
	_, err := s.db.Exec(`VACUUM INTO ?`, path)
	if err != nil {
		return fmt.Errorf("store: backup: %w", err)
	}
	return nil
}

const columns = `id,url,name,package,resolver,size,loaded,speed,status,error,created_at,
	dir,password,online,retries,next_try,priority,position,checksum,
	comment,chunks,auto_extract,matched_rules,
	finished_at,enabled,skipped,skip_reason,hold,forced,download_password,expected_hash,
	connection,host,source,mirror_of,resumable,filename,variant,manual_package,
	reason,origin,changed_at,archive_part,torrent_files,info_hash,trackers,mode,
	category,extract_dir,variant_off,audio_bitrate,confirm_due`

// placeholders is one ? per column, derived from the list so adding a column
// cannot miscount.
var placeholders = strings.TrimSuffix(strings.Repeat("?,", strings.Count(columns, ",")+1), ",")

func (s *Store) Save(t *core.Task) error {
	var nextTry int64
	if !t.NextTry.IsZero() {
		nextTry = t.NextTry.UnixMilli()
	}
	// Every task change passes through here, so no settle path can forget the
	// finish time. It writes to t, which callers pass as a copy (see
	// stampFinish).
	s.stampFinish(t)
	// Zero rather than the epoch, so "never finished" stays distinct.
	var finishedAt, changedAt, confirmDue int64
	if !t.FinishedAt.IsZero() {
		finishedAt = t.FinishedAt.UnixMilli()
	}
	if !t.ChangedAt.IsZero() {
		changedAt = t.ChangedAt.UnixMilli()
	}
	if !t.ConfirmDue.IsZero() {
		confirmDue = t.ConfirmDue.UnixMilli()
	}
	// nil when nobody has asked whether this transfer resumes.
	var resumable any
	if t.Resumable != nil {
		resumable = *t.Resumable
	}
	// nil when no rule had an opinion, so the task still follows the global
	// unpacking switch.
	var autoExtract any
	if t.AutoExtract != nil {
		autoExtract = *t.AutoExtract
	}
	// Variable-length lists are stored as JSON in one column.
	matched := ""
	if len(t.MatchedRules) > 0 {
		b, err := json.Marshal(t.MatchedRules)
		if err != nil {
			return err
		}
		matched = string(b)
	}
	torrentFiles := ""
	if len(t.TorrentFiles) > 0 {
		b, err := json.Marshal(t.TorrentFiles)
		if err != nil {
			return err
		}
		torrentFiles = string(b)
	}
	trackers := ""
	if len(t.Trackers) > 0 {
		b, err := json.Marshal(t.Trackers)
		if err != nil {
			return err
		}
		trackers = string(b)
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
		string(t.Reason), string(t.Origin), changedAt, t.ArchivePart, torrentFiles,
		t.InfoHash, trackers, string(t.Mode),
		t.Category, t.ExtractDir, t.VariantOff, t.AudioBitrate, confirmDue)
	if err != nil {
		return err
	}
	// The history is written in the same save, so a finished download is
	// recorded before anything can trim it from the list. A no-op otherwise.
	return s.recordFinished(t)
}

// Delete takes a task out of the list. The history keeps its row: clearing the
// list says nothing about what was downloaded, which is why the history is not
// a view over this table.
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
		var status, online, matched, reason, origin, torrentFiles, trackers, mode string
		var created, nextTry, finishedAt, changedAt, confirmDue int64
		var autoExtract, resumable sql.NullBool
		if err := rows.Scan(&t.ID, &t.URL, &t.Name, &t.Package, &t.Resolver,
			&t.Size, &t.Loaded, &t.Speed, &status, &t.Error, &created,
			&t.Dir, &t.Password, &online, &t.Retries, &nextTry, &t.Priority, &t.Position,
			&t.Checksum, &t.Comment, &t.Chunks, &autoExtract, &matched,
			&finishedAt, &t.Enabled, &t.Skipped, &t.SkipReason, &t.Hold, &t.Forced,
			&t.DownloadPassword, &t.ExpectedHash, &t.Connection, &t.Host, &t.Source, &t.MirrorOf,
			&resumable, &t.Filename, &t.Variant, &t.ManualPackage,
			&reason, &origin, &changedAt, &t.ArchivePart, &torrentFiles,
			&t.InfoHash, &trackers, &mode,
			&t.Category, &t.ExtractDir, &t.VariantOff, &t.AudioBitrate, &confirmDue); err != nil {
			return nil, err
		}
		t.Status = core.Status(status)
		t.Online = core.Availability(online)
		t.Reason = core.Reason(reason)
		t.Mode = core.DownloadMode(mode)
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
		if confirmDue > 0 {
			t.ConfirmDue = time.UnixMilli(confirmDue)
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
			// A malformed list is not worth failing the reload over; it only
			// explains where the task landed.
			_ = json.Unmarshal([]byte(matched), &t.MatchedRules)
		}
		if torrentFiles != "" {
			_ = json.Unmarshal([]byte(torrentFiles), &t.TorrentFiles)
		}
		if trackers != "" {
			_ = json.Unmarshal([]byte(trackers), &t.Trackers)
		}
		out = append(out, t)
	}
	return out, rows.Err()
}
