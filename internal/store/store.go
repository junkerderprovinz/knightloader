// Package store persists tasks in a local SQLite database (pure-Go driver, no cgo).
package store

import (
	"database/sql"
	"fmt"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/core"
	_ "modernc.org/sqlite"
)

type Store struct{ db *sql.DB }

// migrations run in order, exactly once each. The database records how far it
// has come in PRAGMA user_version, so an existing install keeps its tasks when
// a new column arrives. Never edit a shipped entry — append a new one.
var migrations = []string{
	// 1 — the original table.
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
	// 2 — destination folder, archive password, availability, retry bookkeeping
	//     and queue ordering.
	`ALTER TABLE tasks ADD COLUMN dir TEXT NOT NULL DEFAULT ''`,
	`ALTER TABLE tasks ADD COLUMN password TEXT NOT NULL DEFAULT ''`,
	`ALTER TABLE tasks ADD COLUMN online TEXT NOT NULL DEFAULT ''`,
	`ALTER TABLE tasks ADD COLUMN retries INTEGER NOT NULL DEFAULT 0`,
	`ALTER TABLE tasks ADD COLUMN next_try INTEGER NOT NULL DEFAULT 0`,
	`ALTER TABLE tasks ADD COLUMN priority INTEGER NOT NULL DEFAULT 0`,
	`ALTER TABLE tasks ADD COLUMN position INTEGER NOT NULL DEFAULT 0`,
}

func Open(path string) (*Store, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
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
	dir,password,online,retries,next_try,priority,position`

func (s *Store) Save(t *core.Task) error {
	var nextTry int64
	if !t.NextTry.IsZero() {
		nextTry = t.NextTry.UnixMilli()
	}
	_, err := s.db.Exec(
		`INSERT OR REPLACE INTO tasks (`+columns+`)
		 VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		t.ID, t.URL, t.Name, t.Package, t.Resolver, t.Size, t.Loaded, t.Speed,
		string(t.Status), t.Error, t.CreatedAt.UnixMilli(),
		t.Dir, t.Password, string(t.Online), t.Retries, nextTry, t.Priority, t.Position)
	return err
}

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
		var status, online string
		var created, nextTry int64
		if err := rows.Scan(&t.ID, &t.URL, &t.Name, &t.Package, &t.Resolver,
			&t.Size, &t.Loaded, &t.Speed, &status, &t.Error, &created,
			&t.Dir, &t.Password, &online, &t.Retries, &nextTry, &t.Priority, &t.Position); err != nil {
			return nil, err
		}
		t.Status = core.Status(status)
		t.Online = core.Availability(online)
		t.CreatedAt = time.UnixMilli(created)
		if nextTry > 0 {
			t.NextTry = time.UnixMilli(nextTry)
		}
		out = append(out, t)
	}
	return out, rows.Err()
}
