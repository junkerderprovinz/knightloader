// Package accounts is an encrypted-at-rest store for premium/debrid credentials.
// Secrets are sealed with AES-256-GCM under a per-install key kept in the data
// dir (0600); the store never returns anything but plaintext the caller asked
// for, and the on-disk file holds only ciphertext.
package accounts

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"sync"
)

type Store struct {
	keyPath string
	dbPath  string

	mu   sync.Mutex
	key  []byte
	data map[string]string // service -> base64(nonce || ciphertext)
}

// Open loads (or initialises) the store rooted at dir.
func Open(dir string) (*Store, error) {
	s := &Store{
		keyPath: filepath.Join(dir, ".keyring"),
		dbPath:  filepath.Join(dir, "accounts.json"),
		data:    map[string]string{},
	}
	if err := s.loadKey(); err != nil {
		return nil, err
	}
	if b, err := os.ReadFile(s.dbPath); err == nil {
		_ = json.Unmarshal(b, &s.data)
	}
	return s, nil
}

func (s *Store) loadKey() error {
	if b, err := os.ReadFile(s.keyPath); err == nil && len(b) == 32 {
		s.key = b
		return nil
	}
	k := make([]byte, 32)
	if _, err := rand.Read(k); err != nil {
		return err
	}
	if err := os.WriteFile(s.keyPath, k, 0o600); err != nil {
		return err
	}
	s.key = k
	return nil
}

func (s *Store) gcm() (cipher.AEAD, error) {
	block, err := aes.NewCipher(s.key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

// Set seals a secret for a service and persists it. An empty secret deletes it.
func (s *Store) Set(service, secret string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if secret == "" {
		delete(s.data, service)
		return s.flush()
	}
	g, err := s.gcm()
	if err != nil {
		return err
	}
	nonce := make([]byte, g.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return err
	}
	ct := g.Seal(nonce, nonce, []byte(secret), nil)
	s.data[service] = base64.StdEncoding.EncodeToString(ct)
	return s.flush()
}

// Get returns the plaintext secret for a service, or "" if none is stored.
func (s *Store) Get(service string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	enc, ok := s.data[service]
	if !ok {
		return "", nil
	}
	raw, err := base64.StdEncoding.DecodeString(enc)
	if err != nil {
		return "", err
	}
	g, err := s.gcm()
	if err != nil {
		return "", err
	}
	ns := g.NonceSize()
	if len(raw) < ns {
		return "", errors.New("accounts: ciphertext too short")
	}
	pt, err := g.Open(nil, raw[:ns], raw[ns:], nil)
	if err != nil {
		return "", err
	}
	return string(pt), nil
}

// Services lists the service names that have a stored secret (no secrets).
func (s *Store) Services() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]string, 0, len(s.data))
	for k := range s.data {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func (s *Store) flush() error {
	b, err := json.MarshalIndent(s.data, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.dbPath, b, 0o600)
}
