package hostheaders

// store.go: where the header values live, which is the same sealed file every
// other credential in this app already lives in.
//
// See the package comment for why that matters: settings.json is what the
// diagnostics bundle serialises, and a header block kept there would be in
// every bug report anybody ever filed. Nothing in this file writes a value
// anywhere but into accounts.Store, and nothing in it returns a value except
// Get, whose result is a Set - a type that cannot be printed in the clear.

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/junkerderprovinz/knightloader/internal/accounts"
)

// Service is the pseudo catalogue id header profiles are filed under in the
// shared accounts.Store, with the profile id as the "account" half of the
// (service, account) key that store already indexes by.
//
// The same arrangement hosterauth.Service and ytdlp.CookieService use, and for
// the same reason: accounts.Catalogue is a short, hand-maintained list of
// services a picker searches, while this is one row per origin from a list
// only the user's own pasting decides.
const Service = "hostheaders"

// Store keeps one header profile per user-chosen id.
type Store struct {
	// accounts is the app's own store rather than one opened here, for the
	// reason hosterauth.NewStore documents at length: two accounts.Store
	// instances over one accounts.json each hold their own in-memory snapshot
	// of the whole file, and the second to write silently erases what the
	// first had just saved.
	accounts *accounts.Store

	// mu guards the origin index below.
	mu sync.RWMutex
	// byOrigin maps an origin to the profile id serving it, or nil while the
	// index has not been built.
	//
	// IT EXISTS BECAUSE Match IS CALLED UNDER THE APP'S LOCK, once per
	// registered resolver per staged link - the same constraint
	// remotefs.Resolver states for its Accounts interface. Answering it from
	// the sealed store would mean an AES-GCM open per profile per link, on a
	// paste of several thousand, while the dispatcher's mutex is held.
	byOrigin map[string]string
}

// NewStore wraps the app's existing encrypted store.
func NewStore(a *accounts.Store) *Store { return &Store{accounts: a} }

// ErrNoStore is a Store nobody wired an accounts.Store into. It is an error
// and not a silent empty answer on the write paths, because a save that
// reports success and stores nothing is how a person finds out weeks later
// that their profile was never there.
var ErrNoStore = errors.New("hostheaders: no credential store configured")

// IDs lists the stored profile ids, sorted. Names only, never content: this is
// what a settings page renders.
func (s *Store) IDs() []string {
	if s == nil || s.accounts == nil {
		return nil
	}
	return s.accounts.AccountIDs(Service)
}

// Save stores (or, with a zero Set, clears) one profile. The set is normalised
// first, so what is sealed is what a later read will hand back, rather than
// whatever shape the paste happened to have.
func (s *Store) Save(id string, set Set) error {
	if s == nil || s.accounts == nil {
		return ErrNoStore
	}
	pid := ProfileID(id)
	if pid == "" {
		return fmt.Errorf("hostheaders: %q is not a usable profile name; use letters, digits, - _ or .", id)
	}
	if set.IsZero() {
		return s.remove(pid)
	}
	norm, err := Normalize(set)
	if err != nil {
		return err
	}
	if len(norm.Headers) == 0 {
		// Every header in the paste was blank. Treated as a delete rather than
		// as an error, because that is what accounts.Store.Set has always read
		// an empty secret as, and a form that clears every box means it.
		return s.remove(pid)
	}
	blob, err := json.Marshal(wire{Origin: norm.Origin, Headers: flatten(norm)})
	if err != nil {
		return err
	}
	if err := s.accounts.SetCredential(Service, pid, accounts.Credential{APIKey: string(blob)}); err != nil {
		return err
	}
	s.invalidate()
	return nil
}

// Remove clears one profile.
func (s *Store) Remove(id string) error {
	if s == nil || s.accounts == nil {
		return ErrNoStore
	}
	pid := ProfileID(id)
	if pid == "" {
		return nil
	}
	return s.remove(pid)
}

func (s *Store) remove(pid string) error {
	if err := s.accounts.SetCredential(Service, pid, accounts.Credential{}); err != nil {
		return err
	}
	s.invalidate()
	return nil
}

// Get returns one profile, or a zero Set when there is none.
//
// A decryption failure answers the zero Set and no error, the same way
// ytdlp.CookieStore.Text does and for the identical reason: this result is
// reached on a download path, an error from here would travel into a task's
// Err field and from there into the diagnostics bundle, and the only failures
// possible (a truncated accounts.json, a .keyring replaced under a running
// install) are ones the debrid credentials in the same file report far more
// loudly than a header profile ever could.
func (s *Store) Get(id string) Set {
	if s == nil || s.accounts == nil {
		return Set{}
	}
	pid := ProfileID(id)
	if pid == "" {
		return Set{}
	}
	cred, err := s.accounts.GetCredential(Service, pid)
	if err != nil || cred.APIKey == "" {
		return Set{}
	}
	var w wire
	if err := json.Unmarshal([]byte(cred.APIKey), &w); err != nil {
		return Set{}
	}
	set := Set{Origin: w.Origin}
	for _, h := range w.Headers {
		set.Headers = append(set.Headers, Header{Name: h.Name, Value: h.Value})
	}
	// Re-normalised on the way out rather than trusted as stored: the file it
	// came from can be older than the current rules about what a header may
	// look like, and a value that would be refused on save must not be sent
	// just because it was saved before the rule existed.
	norm, err := Normalize(set)
	if err != nil {
		return Set{}
	}
	return norm
}

// ForURL returns the profile serving rawurl's own origin, together with its
// id, or a zero Set when nothing is stored for that origin.
//
// It is an EXACT origin lookup and never a search: see the package comment on
// why a parent domain's profile does not cover a sub-domain.
func (s *Store) ForURL(rawurl string) (string, Set) {
	origin := OriginOf(rawurl)
	if origin == "" {
		return "", Set{}
	}
	id, ok := s.index()[origin]
	if !ok {
		return "", Set{}
	}
	return id, s.Get(id)
}

// Covers reports whether any stored profile serves rawurl's origin. It is the
// question Resolver.Match asks, and it is answered from the index rather than
// from the sealed store - see Store.byOrigin.
func (s *Store) Covers(rawurl string) bool {
	origin := OriginOf(rawurl)
	if origin == "" {
		return false
	}
	_, ok := s.index()[origin]
	return ok
}

// Listing is one profile as a settings page sees it: what it is called, which
// origin it covers, and which header names it holds.
//
// No values, and that is a property of the TYPE and not of whatever route
// renders it. A listing struct with a value field on it would be safe exactly
// as long as every future handler remembered to blank it, and the first one
// that forgot would put a session cookie in an HTTP response.
type Listing struct {
	ID      string   `json:"id"`
	Origin  string   `json:"origin"`
	Headers []string `json:"headers"`
}

// List is every stored profile, in id order, for the settings page.
func (s *Store) List() []Listing {
	ids := s.IDs()
	out := make([]Listing, 0, len(ids))
	for _, id := range ids {
		set := s.Get(id)
		if set.IsZero() {
			continue
		}
		names := set.Names()
		if names == nil {
			names = []string{}
		}
		out = append(out, Listing{ID: id, Origin: set.Origin, Headers: names})
	}
	return out
}

// Origins lists the origins that have a profile, sorted, with no values. What
// a settings page shows beside each profile name.
func (s *Store) Origins() []string {
	idx := s.index()
	out := make([]string, 0, len(idx))
	for origin := range idx {
		out = append(out, origin)
	}
	sort.Strings(out)
	return out
}

// index builds the origin lookup on first use and keeps it until a write
// invalidates it.
//
// Two profiles claiming one origin is a configuration mistake with no right
// answer, so the one whose id sorts first wins and it wins the same way on
// every boot. Picking by map order instead would route a link through a
// different credential after a restart, which is the kind of "it worked
// yesterday" that costs an evening to track down.
func (s *Store) index() map[string]string {
	if s == nil || s.accounts == nil {
		return nil
	}
	s.mu.RLock()
	idx := s.byOrigin
	s.mu.RUnlock()
	if idx != nil {
		return idx
	}
	built := map[string]string{}
	ids := s.accounts.AccountIDs(Service)
	sort.Strings(ids)
	for _, id := range ids {
		set := s.Get(id)
		if set.Origin == "" {
			continue
		}
		if _, taken := built[set.Origin]; taken {
			continue
		}
		built[set.Origin] = id
	}
	s.mu.Lock()
	s.byOrigin = built
	s.mu.Unlock()
	return built
}

func (s *Store) invalidate() {
	s.mu.Lock()
	s.byOrigin = nil
	s.mu.Unlock()
}

// wire is the sealed shape, and it is a separate type from Set on purpose.
//
// Set.MarshalJSON redacts, which is what keeps a header out of a log and out
// of the diagnostics bundle - and it is exactly why Set must never be the type
// that gets persisted: sealing it would seal the placeholders and destroy the
// credential on the first save. wire has plain string fields and no marshaller
// of its own, it is unexported, and json.Marshal on it is the only line in
// this package that turns a header value into bytes.
type wire struct {
	Origin  string      `json:"origin"`
	Headers []wireEntry `json:"headers"`
}

type wireEntry struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

func flatten(s Set) []wireEntry {
	out := make([]wireEntry, 0, len(s.Headers))
	for _, h := range s.Headers {
		out = append(out, wireEntry{Name: h.Name, Value: h.Value})
	}
	return out
}

// Import parses a pasted cookie block, header block or curl command line and
// saves it under id, scoped to the origin the paste names or, when it names
// none, to fallbackURL.
//
// The two-source origin is what makes a bare cookie block usable at all: a
// "Copy as cURL" paste carries its own URL and needs nothing else, while
// "document.cookie" or a header block copied out of the network panel carries
// no address, and asking the user to also type the site they were just looking
// at is the step that gets guessed wrong.
func (s *Store) Import(id, fallbackURL, text string) (Set, error) {
	set, err := Parse(text)
	if err != nil {
		return Set{}, err
	}
	if set.Origin == "" {
		set.Origin = strings.TrimSpace(fallbackURL)
	}
	norm, err := Normalize(set)
	if err != nil {
		return Set{}, err
	}
	if err := s.Save(id, norm); err != nil {
		return Set{}, err
	}
	return norm, nil
}
