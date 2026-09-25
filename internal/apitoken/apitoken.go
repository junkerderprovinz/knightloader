// Package apitoken keeps named, individually revocable API tokens, so a lost
// device costs one token instead of the shared password.
//
// A token is shown once at creation and only checked afterwards, so the store
// keeps a hash, not the secret. The hash is SHA-256 rather than bcrypt: the
// secret is 256 random bits, so a slow hash adds nothing against guessing and
// would cost CPU on every authenticated request.
//
// Each token carries scopes, the rights it was issued with, so the key typed
// into Sonarr can add and read without being able to change the settings.
package apitoken

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"
)

// Scope is one right a token can carry.
type Scope string

const (
	// ScopeRead sees the download list, the queue, the history and the
	// statistics.
	ScopeRead Scope = "read"
	// ScopeAdd puts new links, torrents, NZB files and containers in.
	ScopeAdd Scope = "add"
	// ScopeControl acts on downloads that are already there: pause, resume,
	// remove, reorder and the queue's own verbs.
	ScopeControl Scope = "control"
	// ScopeAdmin reads and changes configuration and secrets: settings,
	// accounts, tokens, instances, logs and the system itself.
	ScopeAdmin Scope = "admin"
)

// AllScopes is every scope in its canonical order. A token stored before
// tokens had scopes holds all of them, since that is what it could do.
func AllScopes() []Scope {
	return []Scope{ScopeRead, ScopeAdd, ScopeControl, ScopeAdmin}
}

// Normalize returns scopes without duplicates and in canonical order. It
// refuses an empty set, since a token that may do nothing is a mistake, and a
// name that is not a scope.
func Normalize(scopes []Scope) ([]Scope, error) {
	for _, s := range scopes {
		if !slices.Contains(AllScopes(), s) {
			return nil, fmt.Errorf("%w: %q", ErrUnknownScope, s)
		}
	}
	var out []Scope
	for _, s := range AllScopes() {
		if slices.Contains(scopes, s) {
			out = append(out, s)
		}
	}
	if len(out) == 0 {
		return nil, ErrNoScopes
	}
	return out, nil
}

// MaxTokens caps how many tokens one instance keeps, to bound a script that
// creates them in a loop.
const MaxTokens = 50

const maxNameLen = 64

// scopesFile holds each token's scopes by id, apart from tokens.json. A build
// from before scopes rewrites tokens.json with the fields it knows whenever a
// token is used, so scopes kept in it would be lost after running one for a
// while, and every narrowed token would come back able to do everything.
const scopesFile = "token-scopes.json"

// staleAfter is how old LastUsed has to be before Check writes it back, so
// the hot path of every authenticated request is not a disk write.
const staleAfter = time.Minute

var (
	ErrNotFound     = errors.New("apitoken: no such token")
	ErrEmptyName    = errors.New("apitoken: name must not be empty")
	ErrTooMany      = errors.New("apitoken: too many tokens; revoke one before adding another")
	ErrNoScopes     = errors.New("apitoken: a token needs at least one scope")
	ErrUnknownScope = errors.New("apitoken: unknown scope")
)

// Token is one token's metadata. The secret itself is never stored.
type Token struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	// Scopes is what this token may do, in canonical order.
	Scopes    []Scope   `json:"scopes"`
	CreatedAt time.Time `json:"createdAt"`
	// LastUsed is nil until the first successful Check.
	LastUsed *time.Time `json:"lastUsed,omitempty"`
}

// Has reports whether the token carries scope s.
func (t Token) Has(s Scope) bool {
	return slices.Contains(t.Scopes, s)
}

type record struct {
	Token
	HashHex string
}

// stored is a token as tokens.json holds it, in the shape every build has
// read and written. Its scopes are in scopesFile.
type stored struct {
	ID        string     `json:"id"`
	Name      string     `json:"name"`
	CreatedAt time.Time  `json:"createdAt"`
	LastUsed  *time.Time `json:"lastUsed,omitempty"`
	HashHex   string     `json:"hash"`
}

// Store persists tokens.json and scopesFile in the data dir and checks
// presented secrets against them.
type Store struct {
	path       string
	scopesPath string

	mu     sync.Mutex
	byID   map[string]*record
	byHash map[string]*record
}

// Open loads (or creates) the token files in dir.
func Open(dir string) (*Store, error) {
	s := &Store{
		path:       filepath.Join(dir, "tokens.json"),
		scopesPath: filepath.Join(dir, scopesFile),
		byID:       map[string]*record{},
		byHash:     map[string]*record{},
	}
	b, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return s, nil
		}
		return nil, err
	}
	var recs []stored
	if err := json.Unmarshal(b, &recs); err != nil {
		// An unparseable file should not stop the server. Its tokens stop
		// working, which is visible rather than silent.
		return s, nil
	}
	granted, err := s.readScopes(recs)
	if err != nil {
		return nil, err
	}
	for _, st := range recs {
		scopes, ok := granted[st.ID]
		if !ok {
			// Issued before tokens had scopes, or by a build from before
			// them, and so with every right.
			scopes = AllScopes()
		}
		r := &record{
			Token:   Token{ID: st.ID, Name: st.Name, Scopes: scopes, CreatedAt: st.CreatedAt, LastUsed: st.LastUsed},
			HashHex: st.HashHex,
		}
		s.byID[r.ID] = r
		s.byHash[r.HashHex] = r
	}
	return s, nil
}

// readScopes returns the scopes on record by token id, none before the file
// exists. A garbled file gives each of recs an empty entry: which tokens were
// narrowed cannot be told any more, and every right would be too many, so
// they stop working instead.
func (s *Store) readScopes(recs []stored) (map[string][]Scope, error) {
	granted := map[string][]Scope{}
	b, err := os.ReadFile(s.scopesPath)
	if err != nil {
		if os.IsNotExist(err) {
			return granted, nil
		}
		return nil, err
	}
	if err := json.Unmarshal(b, &granted); err != nil {
		granted = map[string][]Scope{}
		for _, st := range recs {
			granted[st.ID] = []Scope{}
		}
	}
	return granted, nil
}

// List returns every token's metadata, oldest first.
func (s *Store) List() []Token {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Token, 0, len(s.byID))
	for _, r := range s.byID {
		out = append(out, r.Token)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.Before(out[j].CreatedAt) })
	return out
}

// Create issues a token with every scope, which is what a peer instance is
// given, and returns its metadata and the plaintext secret.
func (s *Store) Create(name string) (Token, string, error) {
	return s.CreateScoped(name, AllScopes())
}

// CreateScoped issues a new token that may do what scopes allow, and returns
// its metadata and the plaintext secret. This is the only time the secret is
// available.
func (s *Store) CreateScoped(name string, scopes []Scope) (Token, string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return Token{}, "", ErrEmptyName
	}
	if len(name) > maxNameLen {
		name = name[:maxNameLen]
	}
	scopes, err := Normalize(scopes)
	if err != nil {
		return Token{}, "", err
	}

	secret, err := newSecret()
	if err != nil {
		return Token{}, "", err
	}
	rec := &record{
		Token: Token{
			ID:        newID(),
			Name:      name,
			Scopes:    scopes,
			CreatedAt: time.Now().UTC(),
		},
		HashHex: hashHex(secret),
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.byID) >= MaxTokens {
		return Token{}, "", ErrTooMany
	}
	s.byID[rec.ID] = rec
	s.byHash[rec.HashHex] = rec
	// The scopes are written first. The other order could fail between the
	// two writes and leave a token on disk with no scopes on record, which
	// reads as every right.
	err = s.flushScopesLocked()
	if err == nil {
		err = s.flushLocked()
	}
	if err != nil {
		delete(s.byID, rec.ID)
		delete(s.byHash, rec.HashHex)
		return Token{}, "", err
	}
	return rec.Token, secret, nil
}

// Revoke deletes a token by id. An unknown id is ErrNotFound, so a client
// does not believe a mistyped id removed something.
func (s *Store) Revoke(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	rec, ok := s.byID[id]
	if !ok {
		return ErrNotFound
	}
	delete(s.byID, id)
	delete(s.byHash, rec.HashHex)
	if err := s.flushLocked(); err != nil {
		// The token is still on disk, so it has to keep working in memory
		// too rather than vanish from List.
		s.byID[id] = rec
		s.byHash[rec.HashHex] = rec
		return err
	}
	// A failure leaves an entry for the revoked token, which nothing reads
	// and the next write drops.
	_ = s.flushScopesLocked()
	return nil
}

// RevokeAll clears every stored token. It runs whenever the instance password
// is set, changed or removed, because a token minted under the old state
// would otherwise bypass the new protection.
func (s *Store) RevokeAll() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.byID) == 0 {
		return nil
	}
	prevByID, prevByHash := s.byID, s.byHash
	s.byID = map[string]*record{}
	s.byHash = map[string]*record{}
	if err := s.flushLocked(); err != nil {
		s.byID = prevByID
		s.byHash = prevByHash
		return err
	}
	_ = s.flushScopesLocked()
	return nil
}

// Check reports whether secret is a live token, and returns its metadata when
// it is. A plain map lookup on the hash is enough: the secret is 256 random
// bits, so its timing leaks nothing a guesser could climb towards.
func (s *Store) Check(secret string) (Token, bool) {
	if secret == "" {
		return Token{}, false
	}
	h := hashHex(secret)
	now := time.Now().UTC()

	s.mu.Lock()
	defer s.mu.Unlock()
	rec, ok := s.byHash[h]
	if !ok {
		return Token{}, false
	}
	stale := rec.LastUsed == nil || now.Sub(*rec.LastUsed) > staleAfter
	rec.LastUsed = &now
	if stale {
		// A failed write must not turn a valid token invalid.
		_ = s.flushLocked()
	}
	return rec.Token, true
}

func (s *Store) flushLocked() error {
	recs := make([]stored, 0, len(s.byID))
	for _, r := range s.byID {
		recs = append(recs, stored{ID: r.ID, Name: r.Name, CreatedAt: r.CreatedAt, LastUsed: r.LastUsed, HashHex: r.HashHex})
	}
	sort.Slice(recs, func(i, j int) bool { return recs[i].CreatedAt.Before(recs[j].CreatedAt) })
	return writeJSON(s.path, recs)
}

func (s *Store) flushScopesLocked() error {
	granted := make(map[string][]Scope, len(s.byID))
	for id, r := range s.byID {
		granted[id] = r.Scopes
	}
	return writeJSON(s.scopesPath, granted)
}

func writeJSON(path string, v any) error {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, b, 0o600)
}

// newSecret returns a new plaintext token. The "kl_" prefix makes a leaked
// token recognisable in a paste, a log or a secret scanner.
func newSecret() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return "kl_" + base64.RawURLEncoding.EncodeToString(b), nil
}

func hashHex(secret string) string {
	sum := sha256.Sum256([]byte(secret))
	return hex.EncodeToString(sum[:])
}

func newID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
