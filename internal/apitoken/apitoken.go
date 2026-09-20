// Package apitoken keeps named, individually revocable API tokens, so a lost
// device costs one token instead of the shared password.
//
// A token is shown once at creation and only checked afterwards, so the store
// keeps a hash, not the secret. The hash is SHA-256 rather than bcrypt: the
// secret is 256 random bits, so a slow hash adds nothing against guessing and
// would cost CPU on every authenticated request.
package apitoken

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// MaxTokens caps how many tokens one instance keeps, to bound a script that
// creates them in a loop.
const MaxTokens = 50

const maxNameLen = 64

// staleAfter is how old LastUsed has to be before Check writes it back, so
// the hot path of every authenticated request is not a disk write.
const staleAfter = time.Minute

var (
	ErrNotFound  = errors.New("apitoken: no such token")
	ErrEmptyName = errors.New("apitoken: name must not be empty")
	ErrTooMany   = errors.New("apitoken: too many tokens; revoke one before adding another")
)

// Token is one token's metadata. The secret itself is never stored.
type Token struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"createdAt"`
	// LastUsed is nil until the first successful Check.
	LastUsed *time.Time `json:"lastUsed,omitempty"`
}

type record struct {
	Token
	HashHex string `json:"hash"`
}

// Store persists tokens.json in the data dir and checks presented secrets
// against it.
type Store struct {
	path string

	mu     sync.Mutex
	byID   map[string]*record
	byHash map[string]*record
}

// Open loads (or creates) tokens.json in dir.
func Open(dir string) (*Store, error) {
	s := &Store{
		path:   filepath.Join(dir, "tokens.json"),
		byID:   map[string]*record{},
		byHash: map[string]*record{},
	}
	b, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return s, nil
		}
		return nil, err
	}
	var recs []record
	if err := json.Unmarshal(b, &recs); err != nil {
		// An unparseable file should not stop the server. Its tokens stop
		// working, which is visible rather than silent.
		return s, nil
	}
	for i := range recs {
		r := &recs[i]
		s.byID[r.ID] = r
		s.byHash[r.HashHex] = r
	}
	return s, nil
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

// Create issues a new token and returns its metadata and the plaintext
// secret. This is the only time the secret is available.
func (s *Store) Create(name string) (Token, string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return Token{}, "", ErrEmptyName
	}
	if len(name) > maxNameLen {
		name = name[:maxNameLen]
	}

	secret, err := newSecret()
	if err != nil {
		return Token{}, "", err
	}
	rec := &record{
		Token: Token{
			ID:        newID(),
			Name:      name,
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
	if err := s.flushLocked(); err != nil {
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
	recs := make([]record, 0, len(s.byID))
	for _, r := range s.byID {
		recs = append(recs, *r)
	}
	sort.Slice(recs, func(i, j int) bool { return recs[i].CreatedAt.Before(recs[j].CreatedAt) })
	b, err := json.MarshalIndent(recs, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.path, b, 0o600)
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
