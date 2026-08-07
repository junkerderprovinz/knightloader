package store

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

// MaxUIStateBytes caps one stored blob. The interface keeps column widths and a
// collapse tree in here, which is kilobytes; anything past this is either a bug
// writing in a loop or a client using the instance as free storage, and the
// database is on the same disk the downloads land on.
const MaxUIStateBytes = 256 << 10

// ErrUIStateTooBig is a blob over the cap, refused with its size rather than
// truncated: half a JSON document read back is a client that cannot parse its
// own layout and has no way to find out why.
var ErrUIStateTooBig = errors.New("this interface state is larger than the limit")

// UIStateKey is the default bucket, used when a client asks for no particular
// one. Two browsers sharing it is deliberate — a single-user instance wants its
// layout to follow it — and a client that wants its own passes its own key.
const UIStateKey = "default"

// ValidUIStateKey keeps the key a short, boring identifier. It reaches the
// database as a bound parameter either way, so this is not about injection: an
// unbounded key is a primary key a client can make megabytes long, and one with
// spaces or slashes in it is a key nobody can find again in a query.
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

// UIState returns what a client stored under key, or "" when it never stored
// anything. A missing key is not an error: the first load of a fresh browser is
// the normal case, and it wants the built-in layout, not a failure.
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

// SetUIState replaces what is stored under key. The value is opaque here on
// purpose: the interface owns its own shape, and a schema in this package would
// have to be migrated every time a column is added to a list.
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
