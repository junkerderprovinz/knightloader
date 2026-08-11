// Package apitoken is named, individually revocable API credentials: the
// answer to "a phone gets its own", so losing one device means revoking that
// one token rather than rotating the shared password for every other client.
//
// Modelled on internal/auth (a flat JSON file in the data dir, a mutex, no
// goroutine) rather than on internal/accounts (AES-GCM, reversible): a token
// is shown to its owner exactly once, at creation, and only ever CHECKED
// again after that, the same one-way relationship a password has, never a
// secret this process needs to read back in plaintext. So it is hashed, not
// sealed.
//
// It is hashed with SHA-256, deliberately not bcrypt. bcrypt's slowness is
// what protects a short, human-chosen password against being guessed from
// its hash; a token here is 256 bits from crypto/rand, so guessing it from
// the hash is not the threat bcrypt defends against, and re-running a slow
// hash on every single API call this token authenticates (potentially many
// per second from a script or an open WebSocket reconnect loop) would be
// real, avoidable CPU cost for no matching benefit. See Check's own comment
// for why a plain map lookup on that hash is the right amount of caution and
// not a shortcut.
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

// MaxTokens caps how many named tokens one instance keeps. Generous on
// purpose: this bounds a runaway script that creates tokens in a loop, not
// the handful a real person issues for their own devices.
const MaxTokens = 50

// maxNameLen keeps a token's label a label rather than somewhere to paste a
// paragraph; the id, not the name, is ever used to address one.
const maxNameLen = 64

// staleAfter is how old LastUsed has to be before Check bothers writing it
// back to disk. Every authenticated request calls Check, so persisting on
// every single one would turn "is this token valid" into a disk write on the
// hot path; a minute of slack keeps the timestamp honest for the person
// reading it without that cost.
const staleAfter = time.Minute

var (
	// ErrNotFound is a Revoke for an id this store does not hold.
	ErrNotFound = errors.New("apitoken: no such token")
	// ErrEmptyName refuses a token nobody can tell apart from another later.
	ErrEmptyName = errors.New("apitoken: name must not be empty")
	// ErrTooMany is MaxTokens reached.
	ErrTooMany = errors.New("apitoken: too many tokens; revoke one before adding another")
)

// Token is one credential's metadata, never the secret, which exists only in
// the caller's hands after Create and in this store's hash of it.
type Token struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"createdAt"`
	// LastUsed is nil until the first successful Check, the same "absent
	// means it has not happened yet" shape idleaction.State.FireAt already
	// has, rather than Task's zero-time-plus-frontend-check convention: this
	// is a small, its own file, JSON-file-backed type with no database
	// column forcing a zero value, so there is no reason to hand the
	// frontend a sentinel to test for.
	LastUsed *time.Time `json:"lastUsed,omitempty"`
}

// record is Token plus what only this package ever needs to see.
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
	byHash map[string]*record // same *record values, indexed the other way
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
		// A file this build cannot parse is not a reason to refuse to start,
		// the same call auth.Guard's own Open makes. Every token in it stops
		// working, which is visible (nothing authenticates) rather than silent.
		return s, nil
	}
	for i := range recs {
		r := &recs[i]
		s.byID[r.ID] = r
		s.byHash[r.HashHex] = r
	}
	return s, nil
}

// List returns every token's metadata, oldest first, never a secret or a
// hash, whatever the caller's own privilege.
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

// Create issues a new token and returns its metadata plus the plaintext
// secret: the only moment that secret exists anywhere but the caller's own
// hands, because only its hash is written to disk.
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

// Revoke deletes a token by id. Revoking an unknown id is ErrNotFound rather
// than a silent no-op, so a client cannot believe a token gone when the id it
// sent was simply wrong.
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
		// Put back rather than leave the in-memory and on-disk copies
		// disagreeing: a token a write failure could not actually remove
		// must go on authenticating, not vanish from List while still working.
		s.byID[id] = rec
		s.byHash[rec.HashHex] = rec
		return err
	}
	return nil
}

// RevokeAll clears every stored token. Called whenever the instance
// password is set, changed or removed (see routes_system.go's
// /api/auth/password handler): a token minted while the instance had no
// password protecting it - or under a password that has just been changed
// away from - is a standing bypass of whatever protection was just put in
// place, not a credential the new state should honour. Reproduced live
// before this fix: a token created with no password set kept authenticating
// successfully after a password was added, defeating the very protection
// the admin had just turned on.
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
		// Same reasoning as Revoke's own failure path: put back rather than
		// leave every token silently gone from List while a write failure
		// means they are, in fact, still live on disk.
		s.byID = prevByID
		s.byHash = prevByHash
		return err
	}
	return nil
}

// Check reports whether secret is a live token, and its metadata when it is.
//
// The lookup is a plain map index on sha256(secret), not a loop of
// subtle.ConstantTimeCompare against every stored hash. Constant-time
// comparison earns its cost defending a comparison an attacker can retry
// against a KNOWN target (a signature, an HMAC tag) byte by byte. There is no
// equivalent gradient here: secret is 256 bits from crypto/rand, so the only
// way to learn anything from this lookup's timing is to already be close to
// a valid SHA-256 preimage, which is not a thing repeated guessing gets you
// closer to. This is the same reasoning, and the same map-lookup shape,
// every token-auth system with this size of keyspace uses.
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
		// Best-effort: a failed write here must not turn a valid token
		// invalid, so the error is swallowed exactly as auth.Guard.Issue's
		// own housekeeping would. The in-memory copy, and therefore every
		// check until the next successful flush, is already correct.
		_ = s.flushLocked()
	}
	return rec.Token, true
}

// flushLocked writes every record to disk. Callers hold mu.
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

// newSecret is the plaintext credential handed to the caller once. The "kl_"
// prefix names it as a KnightLoader token on sight, in a paste, in a leaked
// log, in a secret scanner's rules, the same reason GitHub and Stripe prefix
// theirs.
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

// newID mirrors internal/app's own newID: 8 random bytes, hex, the same
// shape a task id already has, not a new convention for this one store.
func newID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
