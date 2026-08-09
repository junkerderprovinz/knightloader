// Package hosterauth is the native, per-host hoster login: KL's own host
// list, username/password form and per-row sync status, all rendered in KL's
// Carbon UI (see the frontend HosterLoginSection) - never JD's own web
// interface. Saving a login here does not teach this app to speak the
// hoster's site; it writes the credential into the headless-JD sidecar's OWN
// account config through JD's Remote API, so JD's already-working, ~15 years
// of community-maintained plugins do the actual login. This is the same
// "JD's UI never shown, everything through JD's API" rule
// internal/resolver/jd/client.go already follows for AddLinks and the rest -
// only the API surface (account management, not downloads) is new this wave.
//
// Reconciler (reconcile.go) is the half that talks to JD and decides what
// changed; Store (this file) is the half that remembers what the user asked
// for. Credentials are sealed by internal/accounts.Store, the same
// AES-256-GCM-at-rest store and the same Redacted/WithSecretsFrom convention
// every other credential in this app already uses (see
// internal/accounts/accounts.go) - hoster logins are filed there under their
// own pseudo-service id rather than growing a second encryption scheme, or a
// second definition of what "redacted" means, next to the first.
package hosterauth

import "github.com/junkerderprovinz/knightloader/internal/accounts"

// service is the pseudo catalogue id native hoster logins are filed under in
// the shared accounts.Store, with the host as the "account" component of the
// (service, account) key accounts.Store already indexes by. It is
// deliberately not an entry in accounts.Catalogue: the catalogue is a short,
// hand-maintained list a picker searches (torbox, alldebrid, ...), while a
// hoster login is one row per host from a list that can run into the
// hundreds and changes with what JD itself reports - a different shape of
// list entirely, which is why this package keeps its own.
const service = "hosterauth"

// Store persists native hoster logins, one per host, in the store every other
// credential in this app already trusts.
type Store struct {
	accounts *accounts.Store
}

// NewStore wraps the app's existing encrypted store. It takes an
// *accounts.Store rather than opening its own, on purpose: two independent
// Store instances pointed at the same accounts.json would each hold their own
// in-memory snapshot of the whole file, and the second one to write would
// silently erase whatever the other had just saved. Sharing the app's one
// instance is what keeps every credential - debrid keys and hoster logins
// alike - safe under the same lock.
func NewStore(accounts *accounts.Store) *Store {
	return &Store{accounts: accounts}
}

// Hosts lists every host with a stored login, sorted.
func (s *Store) Hosts() []string {
	return s.accounts.AccountIDs(service)
}

// Get returns the stored login for host, or a zero Credential if none is set.
func (s *Store) Get(host string) (accounts.Credential, error) {
	return s.accounts.GetCredential(service, host)
}

// Set stores (or, with a zero Credential, clears) host's login.
func (s *Store) Set(host string, cred accounts.Credential) error {
	return s.accounts.SetCredential(service, host, cred)
}

// Remove clears host's stored login.
func (s *Store) Remove(host string) error {
	return s.accounts.SetCredential(service, host, accounts.Credential{})
}
