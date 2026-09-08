package mediahook

// store.go: where the one header value per address lives, which is the same
// sealed file every other credential in this app already lives in.
//
// See the package comment for why that matters: settings.json is what the
// diagnostics bundle serialises and what the Advanced key table reflects over,
// and a token kept there would be in every bug report anybody ever filed.
// Nothing in this file writes a value anywhere but into accounts.Store, and the
// only thing that reads one back is Value, whose one caller is the code that
// puts it straight into an outbound request header.

import (
	"errors"
	"fmt"

	"github.com/junkerderprovinz/knightloader/internal/accounts"
)

// Store keeps one header value per hook id.
type Store struct {
	// accounts is the app's own store rather than one opened here, for the
	// reason hostheaders.Store and hosterauth.NewStore both document at length:
	// two accounts.Store instances over one accounts.json each hold their own
	// in-memory snapshot of the whole file, and the second to write silently
	// erases what the first had just saved.
	//
	// Unlike hostheaders.Store there is no index kept beside it, so wrapping the
	// app's store a second time is free: this type holds nothing that can go
	// stale, which is why App hands out a fresh wrapper rather than keeping one.
	accounts *accounts.Store
}

// NewStore wraps the app's existing encrypted store.
func NewStore(a *accounts.Store) *Store { return &Store{accounts: a} }

// ErrNoStore is a Store nobody wired an accounts.Store into. It is an error and
// not a silent success on the write paths, because a save that reports success
// and stores nothing is how a person finds out weeks later that their token was
// never there.
var ErrNoStore = errors.New("mediahook: no credential store configured")

// SetValue seals (or, with an empty value, clears) the header value for one
// address.
//
// An empty value is a DELETE and not an error, the same reading accounts.Store.Set
// and hostheaders.Store.Save have always given it: a form somebody cleared on
// purpose means it, and if empty did not clear, a stored token could never be
// removed through a settings page at all. That is exactly why the placeholder
// (accounts.Redacted) exists and why the route in front of this has to put the
// stored value back wherever the form sent it - see keepMediaHookValue.
func (s *Store) SetValue(id, value string) error {
	if s == nil || s.accounts == nil {
		return ErrNoStore
	}
	hid := HookID(id)
	if hid == "" {
		return fmt.Errorf("mediahook: %q is not a usable name; use letters, digits, - _ or .", id)
	}
	if value == "" {
		return s.accounts.SetCredential(Service, hid, accounts.Credential{})
	}
	return s.accounts.SetCredential(Service, hid, accounts.Credential{APIKey: value})
}

// Value returns the stored header value for one address, or "" when there is
// none.
//
// A decryption failure answers "" and NO error, the same way hostheaders.Get and
// ytdlp.CookieStore.Text do and for the identical reason: this result is reached
// on the path that calls the media server after a download, an error from here
// would travel into a log line and from there into the diagnostics bundle, and
// the only failures possible (a truncated accounts.json, a .keyring replaced
// under a running install) are ones the debrid credentials in the same file
// report far more loudly than one header value ever could.
//
// The call still goes out without the header rather than not going out at all.
// A media server that does not need the header scans, and one that does answers
// 401, which the caller turns into a sentence naming the header - which is a far
// more useful thing for the person to see than silence.
func (s *Store) Value(id string) string {
	if s == nil || s.accounts == nil {
		return ""
	}
	hid := HookID(id)
	if hid == "" {
		return ""
	}
	cred, err := s.accounts.GetCredential(Service, hid)
	if err != nil {
		return ""
	}
	return cred.APIKey
}

// Remove clears the value stored for one address. Deleting an address the user
// never gave a value to is not an error: the end state is the one they asked
// for either way.
func (s *Store) Remove(id string) error {
	if s == nil || s.accounts == nil {
		return ErrNoStore
	}
	hid := HookID(id)
	if hid == "" {
		return nil
	}
	return s.accounts.SetCredential(Service, hid, accounts.Credential{})
}

// IDs lists the ids that have a value sealed, sorted. Names only, never content.
//
// It is what "is a value stored for this row" is answered from rather than
// Value, and the difference matters exactly once: a row whose ciphertext no
// longer opens - a .keyring replaced under a running install - is listed here
// and answers "" from Value. Deciding from Value would tell the person nothing
// is stored, they would type the token again, and it would work; deciding from
// here tells them one IS stored, which is true, and the 401 from the call says
// the rest.
func (s *Store) IDs() []string {
	if s == nil || s.accounts == nil {
		return nil
	}
	return s.accounts.AccountIDs(Service)
}

// Has reports whether a value is sealed for one address. See IDs for why this is
// not Value(id) != "".
func (s *Store) Has(id string) bool {
	hid := HookID(id)
	if hid == "" {
		return false
	}
	for _, stored := range s.IDs() {
		if stored == hid {
			return true
		}
	}
	return false
}
