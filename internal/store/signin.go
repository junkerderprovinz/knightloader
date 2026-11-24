package store

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/junkerderprovinz/parleyport/kit/signin/sqlstore"
)

// SignInTable is where the kit's sign-in keeps its documents. It sits outside
// the numbered migrations, so the version before the kit still opens a
// database that has it.
const SignInTable = "signin_docs"

// DB is the database, for the kit's sign-in, which keeps its documents in
// SignInTable on the same connection.
func (s *Store) DB() *sql.DB { return s.db }

// SignInHasPassword reports whether the sign-in documents hold an account
// with a password, its own or one the kit took over and has not rewritten yet.
func (s *Store) SignInHasPassword() (bool, error) {
	if ok, err := s.hasSignIn("main"); err != nil || !ok {
		return false, err
	}
	rows, err := s.db.Query(`SELECT data FROM ` + SignInTable + ` WHERE kind = 'account'`)
	if err != nil {
		return false, fmt.Errorf("store: reading the sign-in: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var b []byte
		if err := rows.Scan(&b); err != nil {
			return false, fmt.Errorf("store: reading the sign-in: %w", err)
		}
		var acc struct {
			Password       string `json:"password"`
			LegacyPassword string `json:"legacyPassword"`
		}
		if json.Unmarshal(b, &acc) == nil && (acc.Password != "" || acc.LegacyPassword != "") {
			return true, nil
		}
	}
	return false, rows.Err()
}

// ReplaceSignInFrom puts the sign-in documents of the database at path in
// place of this one's, for a restore that keeps the sign-in it replaces.
func (s *Store) ReplaceSignInFrom(path string) error {
	if err := s.DropSignIn(); err != nil {
		return err
	}
	if _, err := os.Stat(path); err != nil {
		return nil
	}
	if _, err := s.db.Exec(`ATTACH DATABASE ? AS source`, path); err != nil {
		return fmt.Errorf("store: ReplaceSignInFrom: %w", err)
	}
	defer s.db.Exec(`DETACH DATABASE source`) //nolint:errcheck // the connection closes with the store
	if ok, err := s.hasSignIn("source"); err != nil || !ok {
		return err
	}
	if _, err := s.db.Exec(strings.Replace(sqlstore.Schema(SignInTable), "EXISTS ", "EXISTS main.", 1)); err != nil {
		return fmt.Errorf("store: ReplaceSignInFrom: %w", err)
	}
	if _, err := s.db.Exec(`INSERT INTO main.` + SignInTable + ` (kind, id, data) SELECT kind, id, data FROM source.` + SignInTable); err != nil {
		return fmt.Errorf("store: ReplaceSignInFrom: %w", err)
	}
	return nil
}

// DropSignIn removes the sign-in documents, so the next start imports the
// sign-in from auth.json again.
func (s *Store) DropSignIn() error {
	if _, err := s.db.Exec(`DROP TABLE IF EXISTS main.` + SignInTable); err != nil {
		return fmt.Errorf("store: DropSignIn: %w", err)
	}
	return nil
}

func (s *Store) hasSignIn(schema string) (bool, error) {
	var n int
	err := s.db.QueryRow(`SELECT count(*) FROM `+schema+`.sqlite_master WHERE type = 'table' AND name = ?`, SignInTable).Scan(&n)
	if err != nil {
		return false, fmt.Errorf("store: looking for the sign-in: %w", err)
	}
	return n > 0, nil
}

// DropSignInPasskeys removes the passkeys from the sign-in documents, which
// a restore never takes from a backup.
func (s *Store) DropSignInPasskeys() error {
	if ok, err := s.hasSignIn("main"); err != nil || !ok {
		return err
	}
	if _, err := s.db.Exec(`DELETE FROM ` + SignInTable + ` WHERE kind = 'passkey'`); err != nil {
		return fmt.Errorf("store: DropSignInPasskeys: %w", err)
	}
	return nil
}

// SignInPasswordIn is SignInHasPassword for the database at path, opened as
// it is, without bringing it to this build's schema.
func SignInPasswordIn(path string) (bool, error) {
	if _, err := os.Stat(path); err != nil {
		return false, nil
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return false, err
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	return (&Store{db: db}).SignInHasPassword()
}

// SignInTokenScopes lists the scopes of every API token the sign-in keeps,
// for the health page, which says whether one of them can do a job.
func (s *Store) SignInTokenScopes() ([][]string, error) {
	if ok, err := s.hasSignIn("main"); err != nil || !ok {
		return nil, err
	}
	rows, err := s.db.Query(`SELECT data FROM ` + SignInTable + ` WHERE kind = 'token'`)
	if err != nil {
		return nil, fmt.Errorf("store: reading the sign-in: %w", err)
	}
	defer rows.Close()
	var out [][]string
	for rows.Next() {
		var b []byte
		if err := rows.Scan(&b); err != nil {
			return nil, fmt.Errorf("store: reading the sign-in: %w", err)
		}
		var t struct {
			Scopes []string `json:"scopes"`
		}
		if json.Unmarshal(b, &t) == nil {
			out = append(out, t.Scopes)
		}
	}
	return out, rows.Err()
}
