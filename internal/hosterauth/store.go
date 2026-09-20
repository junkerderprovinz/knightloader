// Package hosterauth manages native per-host hoster logins in KnightLoader's
// own UI. A saved login is written into the headless JD sidecar's account
// config through JD's Remote API, so JD's plugins do the actual login; JD's
// own interface is never shown.
//
// Reconciler (reconcile.go) talks to JD; Store keeps what the user asked for.
// Credentials are sealed by internal/accounts.Store, like every other
// credential, under their own pseudo-service id.
package hosterauth

import "github.com/junkerderprovinz/knightloader/internal/accounts"

// Service is the pseudo-service id hoster logins are filed under in
// accounts.Store, with the host as the account id. It is not in
// accounts.Catalogue, which is a short fixed list, while hoster logins are
// one per host from JD's list of hundreds. The app keys its per-account
// metadata (account_meta.json) by the same pair.
const Service = "hosterauth"

// Store persists native hoster logins, one per host.
type Store struct {
	accounts *accounts.Store
}

// NewStore wraps the app's existing encrypted store. Two stores on the same
// accounts.json would each hold their own snapshot and overwrite each other's
// saves.
func NewStore(accounts *accounts.Store) *Store {
	return &Store{accounts: accounts}
}

// Hosts lists every host with a stored login, sorted.
func (s *Store) Hosts() []string {
	return s.accounts.AccountIDs(Service)
}

// Get returns the stored login for host, or a zero Credential if none is set.
func (s *Store) Get(host string) (accounts.Credential, error) {
	return s.accounts.GetCredential(Service, host)
}

// Set stores (or, with a zero Credential, clears) host's login.
func (s *Store) Set(host string, cred accounts.Credential) error {
	return s.accounts.SetCredential(Service, host, cred)
}

// Remove clears host's stored login.
func (s *Store) Remove(host string) error {
	return s.accounts.SetCredential(Service, host, accounts.Credential{})
}
