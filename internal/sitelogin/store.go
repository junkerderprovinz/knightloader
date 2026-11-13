package sitelogin

import (
	"errors"

	"github.com/junkerderprovinz/knightloader/internal/accounts"
)

// Service is the pseudo service id sessions are filed under in the shared
// accounts.Store, with the site id as the account half of the key, as
// ytdlp.CookieService does for pasted jars. A session is kept apart from
// those jars because it is used without the instance's cookie switch.
const Service = "sitelogins"

// Store keeps one session per site, sealed like a hoster login, so it never
// reaches settings.json or the diagnostics bundle. No error here carries its
// text.
type Store struct {
	accounts *accounts.Store
}

// NewStore wraps the app's own encrypted store; a second accounts.Store over
// the same file would overwrite the first one's writes.
func NewStore(a *accounts.Store) *Store { return &Store{accounts: a} }

// Set stores, or with an empty jar clears, a site's session.
func (s *Store) Set(id, jar string) error {
	if _, ok := Lookup(id); !ok {
		return errors.New("sitelogin: no such site " + id)
	}
	return s.accounts.SetCredential(Service, id, accounts.Credential{APIKey: jar})
}

// Session returns a site's stored cookies.txt, or "". A decryption failure
// also yields "", which reads as signed out.
func (s *Store) Session(id string) string {
	cred, err := s.accounts.GetCredential(Service, id)
	if err != nil {
		return ""
	}
	return cred.APIKey
}
