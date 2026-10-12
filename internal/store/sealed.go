package store

import (
	"context"
	"fmt"
	"log"

	"github.com/junkerderprovinz/knightloader/internal/appkey"
	"github.com/junkerderprovinz/knightloader/internal/core"
)

// sealPurpose binds a sealed task password to this table.
const sealPurpose = "tasks.password"

// OpenWithKey is Open with an app key. A task's archive and download
// passwords are then sealed in the database, and only there: All hands them
// out in the clear, so the interface and the redaction for group members and
// narrow tokens work on plain values. Rows written before the key are sealed
// on the spot.
func OpenWithKey(path string, key *appkey.Key) (*Store, error) {
	s, err := Open(path)
	if err != nil || key == nil {
		return s, err
	}
	s.appKey = key
	if err := s.sealPlainPasswords(); err != nil {
		s.Close()
		return nil, err
	}
	return s, nil
}

// sealPlainPasswords seals every password a row holds in the clear, in one
// transaction, so a start that stops halfway leaves every row readable. The
// plain values must not stay behind in the file either: secure_delete zeroes
// the space the old rows held, and VACUUM rebuilds the file from the live rows
// alone.
func (s *Store) sealPlainPasswords() error {
	rows, err := s.db.Query(`SELECT id, password, download_password FROM tasks WHERE password != '' OR download_password != ''`)
	if err != nil {
		return fmt.Errorf("store: sealing task passwords: %w", err)
	}
	type row struct{ id, password, download string }
	var plain []row
	for rows.Next() {
		var r row
		if err := rows.Scan(&r.id, &r.password, &r.download); err != nil {
			rows.Close()
			return fmt.Errorf("store: sealing task passwords: %w", err)
		}
		if (r.password != "" && !appkey.IsSealed(r.password)) || (r.download != "" && !appkey.IsSealed(r.download)) {
			plain = append(plain, r)
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return fmt.Errorf("store: sealing task passwords: %w", err)
	}
	if len(plain) == 0 {
		return nil
	}
	ctx := context.Background()
	// The pragma belongs to one connection, so everything runs on this one.
	conn, err := s.db.Conn(ctx)
	if err != nil {
		return fmt.Errorf("store: sealing task passwords: %w", err)
	}
	defer conn.Close()
	if _, err := conn.ExecContext(ctx, `PRAGMA secure_delete = ON`); err != nil {
		return fmt.Errorf("store: sealing task passwords: %w", err)
	}
	defer conn.ExecContext(ctx, `PRAGMA secure_delete = OFF`)
	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("store: sealing task passwords: %w", err)
	}
	defer tx.Rollback()
	for _, r := range plain {
		password, err := s.sealPassword(r.password)
		if err != nil {
			return err
		}
		download, err := s.sealPassword(r.download)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(`UPDATE tasks SET password=?, download_password=? WHERE id=?`, password, download, r.id); err != nil {
			return fmt.Errorf("store: sealing task passwords: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("store: sealing task passwords: %w", err)
	}
	// A failed rebuild leaves zeroed space behind, not the passwords.
	if _, err := conn.ExecContext(ctx, `VACUUM`); err != nil {
		log.Printf("store: the file could not be rebuilt after sealing the task passwords: %v", err)
	}
	log.Printf("store: sealed the passwords of %d task(s) with the app key", len(plain))
	return nil
}

// sealPassword is the stored form of a task password: sealed with the app key
// when there is one, and as it is otherwise or when it already is.
func (s *Store) sealPassword(v string) (string, error) {
	if s.appKey == nil || v == "" || appkey.IsSealed(v) {
		return v, nil
	}
	sealed, err := s.appKey.SealString(sealPurpose, v)
	if err != nil {
		return "", fmt.Errorf("store: sealing a task password: %w", err)
	}
	return sealed, nil
}

// openPasswords puts a task's sealed passwords back in the clear. One that
// does not open, from a database another key sealed, is dropped with a line
// in the log rather than handed to an extractor or a hoster as gibberish; the
// failure also reaches appkey's OnFailure.
func (s *Store) openPasswords(t *core.Task) {
	for _, f := range []struct {
		name string
		v    *string
	}{{"archive password", &t.Password}, {"download password", &t.DownloadPassword}} {
		if !appkey.IsSealed(*f.v) {
			continue
		}
		plain, err := s.appKey.OpenString(sealPurpose, *f.v)
		if err != nil {
			log.Printf("store: the %s of task %s cannot be read and is dropped: %v", f.name, t.ID, err)
			plain = ""
		}
		*f.v = plain
	}
}
