// Package store persists tasks in a local SQLite database (pure-Go driver, no cgo).
package store

import (
	"database/sql"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/core"
	_ "modernc.org/sqlite"
)

type Store struct{ db *sql.DB }

func Open(path string) (*Store, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	if _, err := db.Exec(`
CREATE TABLE IF NOT EXISTS tasks (
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
);`); err != nil {
		db.Close()
		return nil, err
	}
	return &Store{db: db}, nil
}

func (s *Store) Close() error { return s.db.Close() }

func (s *Store) Save(t *core.Task) error {
	_, err := s.db.Exec(
		`INSERT OR REPLACE INTO tasks
		 (id,url,name,package,resolver,size,loaded,speed,status,error,created_at)
		 VALUES (?,?,?,?,?,?,?,?,?,?,?)`,
		t.ID, t.URL, t.Name, t.Package, t.Resolver, t.Size, t.Loaded, t.Speed,
		string(t.Status), t.Error, t.CreatedAt.UnixMilli())
	return err
}

func (s *Store) Delete(id string) error {
	_, err := s.db.Exec(`DELETE FROM tasks WHERE id=?`, id)
	return err
}

func (s *Store) All() ([]*core.Task, error) {
	rows, err := s.db.Query(
		`SELECT id,url,name,package,resolver,size,loaded,speed,status,error,created_at
		 FROM tasks ORDER BY created_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*core.Task
	for rows.Next() {
		t := &core.Task{}
		var status string
		var created int64
		if err := rows.Scan(&t.ID, &t.URL, &t.Name, &t.Package, &t.Resolver,
			&t.Size, &t.Loaded, &t.Speed, &status, &t.Error, &created); err != nil {
			return nil, err
		}
		t.Status = core.Status(status)
		t.CreatedAt = time.UnixMilli(created)
		out = append(out, t)
	}
	return out, rows.Err()
}
