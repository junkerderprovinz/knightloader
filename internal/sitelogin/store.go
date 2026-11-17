package sitelogin

import (
	"errors"
	"slices"
	"strings"

	"github.com/junkerderprovinz/knightloader/internal/accounts"
)

// Service is the pseudo service id sessions are filed under in the shared
// accounts.Store, with the site id as the account half of the key.
const Service = "sitelogins"

// Store keeps the session the extension handed over, one per site, sealed like a hoster login, so it never
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

// offService files the sites whose sign-in in the browsers here is not used:
// removed in KnightLoader while the browser stays signed in.
const offService = "sitelogins-off"

// SetDisconnected marks a site so the browsers' sign-in to it is not used, or
// with off false clears the mark.
func (s *Store) SetDisconnected(id string, off bool) error {
	if _, ok := Lookup(id); !ok {
		return errors.New("sitelogin: no such site " + id)
	}
	var cred accounts.Credential
	if off {
		cred.APIKey = "off"
	}
	return s.accounts.SetCredential(offService, id, cred)
}

// Disconnected reports whether a site is marked not to use the browsers'
// sign-in.
func (s *Store) Disconnected(id string) bool {
	cred, err := s.accounts.GetCredential(offService, id)
	return err == nil && cred.APIKey != ""
}

// readerService files, per site, the browsers whose extension said it may
// read the site's cookies, by their ids in the group.
const readerService = "sitelogins-readers"

// SetReader records whether the extension in browser app may read a site's
// cookies and so passes its sign-in on.
func (s *Store) SetReader(id, app string, reads bool) error {
	if _, ok := Lookup(id); !ok {
		return errors.New("sitelogin: no such site " + id)
	}
	readers := s.Readers(id)
	if slices.Contains(readers, app) == reads {
		return nil
	}
	if reads {
		readers = append(readers, app)
	} else {
		readers = slices.DeleteFunc(readers, func(r string) bool { return r == app })
	}
	return s.accounts.SetCredential(readerService, id, accounts.Credential{APIKey: strings.Join(readers, " ")})
}

// Readers lists the browsers whose extension said it may read a site's
// cookies.
func (s *Store) Readers(id string) []string {
	cred, err := s.accounts.GetCredential(readerService, id)
	if err != nil {
		return nil
	}
	return strings.Fields(cred.APIKey)
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
