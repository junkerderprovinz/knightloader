package hostheaders

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/junkerderprovinz/knightloader/internal/accounts"
)

// Service is the pseudo service id header profiles are filed under in the
// shared accounts.Store, with the profile id as the account half of the key,
// as hosterauth and ytdlp's cookie store do.
const Service = "hostheaders"

// Store keeps one header profile per user-chosen id.
type Store struct {
	// accounts is the app's own store: a second accounts.Store over the same
	// file would overwrite the first one's writes.
	accounts *accounts.Store

	// mu guards the origin index below.
	mu sync.RWMutex
	// byOrigin maps an origin to the profile id serving it, or nil while the
	// index has not been built. Match runs under the app's lock for every
	// staged link, which rules out decrypting profiles there.
	byOrigin map[string]string
}

// NewStore wraps the app's existing encrypted store.
func NewStore(a *accounts.Store) *Store { return &Store{accounts: a} }

// ErrNoStore is returned by the write paths of a Store without an
// accounts.Store, so a save never reports success without storing anything.
var ErrNoStore = errors.New("hostheaders: no credential store configured")

// IDs lists the stored profile ids, sorted.
func (s *Store) IDs() []string {
	if s == nil || s.accounts == nil {
		return nil
	}
	return s.accounts.AccountIDs(Service)
}

// Save stores (or, with a zero Set, clears) one profile, normalised first.
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
		// All headers blank: a form with every box cleared means delete.
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

// Get returns one profile, or a zero Set when there is none. A decryption
// failure also yields the zero Set: this runs on the download path, and a
// broken accounts.json already shows up through every other credential in it.
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
	// Normalised again because the profile may predate the current rules.
	norm, err := Normalize(set)
	if err != nil {
		return Set{}
	}
	return norm
}

// ForURL returns the profile stored for exactly rawurl's origin, with its id,
// or a zero Set.
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

// Covers reports whether any stored profile serves rawurl's origin, answered
// from the index.
func (s *Store) Covers(rawurl string) bool {
	origin := OriginOf(rawurl)
	if origin == "" {
		return false
	}
	_, ok := s.index()[origin]
	return ok
}

// Listing is one profile as a settings page sees it. It has no value field,
// so no handler can leak a value by forgetting to blank one.
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

// Origins lists the origins that have a profile, sorted.
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
// invalidates it. When two profiles claim one origin, the id that sorts first
// wins, so the choice is the same after every restart.
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

// wire is the sealed shape. It is separate from Set because Set.MarshalJSON
// redacts, and persisting a Set would store the placeholders.
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
// none (a bare cookie block), to fallbackURL.
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
