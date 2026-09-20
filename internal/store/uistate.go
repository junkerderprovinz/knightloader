package store

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

// MaxUIStateBytes caps one stored blob. Column widths and a collapse tree take
// kilobytes; anything larger is a bug or a client using the instance as
// storage on the disk the downloads need.
const MaxUIStateBytes = 256 << 10

// ErrUIStateTooBig is a blob over the cap, refused rather than truncated, since
// half a JSON document cannot be parsed back.
var ErrUIStateTooBig = errors.New("this interface state is larger than the limit")

// UIStateKey is the bucket used when a client names none. Browsers share it so
// a single user's layout follows them; a client that wants its own passes its
// own key.
const UIStateKey = "default"

// ValidUIStateKey keeps the key a short, plain identifier. It is a bound
// parameter either way; the limits keep a client from making megabyte primary
// keys or ones nobody can find in a query.
func ValidUIStateKey(key string) error {
	if key == "" {
		return errors.New("an interface-state key cannot be empty")
	}
	if len(key) > 64 {
		return fmt.Errorf("an interface-state key is at most 64 characters, this one is %d", len(key))
	}
	const allowed = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789-_.:"
	if strings.ContainsFunc(key, func(r rune) bool { return !strings.ContainsRune(allowed, r) }) {
		return fmt.Errorf("%q is not a usable interface-state key: letters, digits, - _ . and : only", key)
	}
	return nil
}

// UIState returns what a client stored under key, or "" when it stored
// nothing, which is the normal first load of a fresh browser.
func (s *Store) UIState(key string) (string, error) {
	if err := ValidUIStateKey(key); err != nil {
		return "", err
	}
	var value string
	err := s.db.QueryRow(`SELECT value FROM uistate WHERE key=?`, key).Scan(&value)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return value, err
}

// SetUIState replaces what is stored under key. The value is opaque, so the
// interface can change its shape without a schema migration here.
func (s *Store) SetUIState(key, value string) error {
	if err := ValidUIStateKey(key); err != nil {
		return err
	}
	if len(value) > MaxUIStateBytes {
		return fmt.Errorf("%w: %d bytes, the limit is %d", ErrUIStateTooBig, len(value), MaxUIStateBytes)
	}
	_, err := s.db.Exec(
		`INSERT OR REPLACE INTO uistate (key,value,changed_at) VALUES (?,?,?)`,
		key, value, time.Now().UnixMilli())
	return err
}
