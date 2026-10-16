package playback

// store.go: the one token per server, sealed in the app's accounts.Store. The
// shape and the reasons are mediahook.Store's.

import (
	"errors"
	"fmt"

	"github.com/junkerderprovinz/knightloader/internal/accounts"
)

// Store keeps one token per server id. It wraps the app's own accounts.Store,
// since a second one over the same file would overwrite the first one's writes.
type Store struct{ accounts *accounts.Store }

// NewStore wraps the app's existing encrypted store.
func NewStore(a *accounts.Store) *Store { return &Store{accounts: a} }

// ErrNoStore is a Store with no accounts.Store behind it. The write paths fail
// with it rather than reporting a save that stored nothing.
var ErrNoStore = errors.New("playback: no credential store configured")

// SetToken seals the token for one server, or clears it when token is empty.
func (s *Store) SetToken(id, token string) error {
	if s == nil || s.accounts == nil {
		return ErrNoStore
	}
	sid := ServerID(id)
	if sid == "" {
		return fmt.Errorf("playback: %q is not a usable id", id)
	}
	return s.accounts.SetCredential(Service, sid, accounts.Credential{APIKey: token})
}

// Token returns the sealed token for one server, "" when there is none or it
// does not decrypt. The question then goes out without it and the 401 says the
// rest, which is what mediahook.Store.Value does too.
func (s *Store) Token(id string) string {
	if s == nil || s.accounts == nil {
		return ""
	}
	cred, err := s.accounts.GetCredential(Service, ServerID(id))
	if err != nil {
		return ""
	}
	return cred.APIKey
}

// Has reports whether a token is sealed for one server, from the id list
// rather than from Token, so one that does not decrypt still reads as stored.
func (s *Store) Has(id string) bool {
	if s == nil || s.accounts == nil {
		return false
	}
	sid := ServerID(id)
	for _, stored := range s.accounts.AccountIDs(Service) {
		if stored == sid {
			return true
		}
	}
	return false
}
