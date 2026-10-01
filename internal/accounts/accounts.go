// Package accounts is an encrypted-at-rest store for premium and debrid
// credentials. Secrets are sealed with AES-256-GCM under a per-install key
// kept in the data dir, and the file on disk holds only ciphertext.
//
// A secret stored before Credential existed decrypts to a bare string and is
// read back as an API key on every read (see decodeCredential). The file is
// never migrated in place, because a process killed mid-rewrite would leave
// it unreadable in either format.
package accounts

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// Credential is one stored account's secret. The service's catalogue Kind
// decides which fields are used; Store seals whatever it is given.
type Credential struct {
	APIKey   string `json:"apiKey,omitempty"`
	Username string `json:"username,omitempty"`
	Password string `json:"password,omitempty"`
}

// IsZero reports whether c carries no secret at all, which SetCredential
// treats as a delete.
func (c Credential) IsZero() bool { return c == Credential{} }

// Redacted is the placeholder shown in place of a stored secret. It is
// visible text rather than an empty string because empty has to keep meaning
// "clear this field" in a settings form.
const Redacted = "********"

// Redacted returns a copy with every populated secret field replaced by the
// placeholder. Username stays, since it identifies an account without
// unlocking it.
func (c Credential) Redacted() Credential {
	if c.APIKey != "" {
		c.APIKey = Redacted
	}
	if c.Password != "" {
		c.Password = Redacted
	}
	return c
}

// WithSecretsFrom restores every field that still holds the placeholder from
// prev, so saving a form that showed a redacted credential does not seal the
// placeholder itself. A field cleared to "" passes through and removes the
// secret.
func (c Credential) WithSecretsFrom(prev Credential) Credential {
	if c.APIKey == Redacted {
		c.APIKey = prev.APIKey
	}
	if c.Password == Redacted {
		c.Password = prev.Password
	}
	return c
}

// decodeCredential parses a decrypted blob written by SetCredential, and
// reads anything else as a single API key written by Set. It checks for a
// leading '{' rather than trying json.Unmarshal, because an old secret such
// as "null" is valid JSON and would silently decode to an empty Credential.
func decodeCredential(plaintext string) Credential {
	if strings.HasPrefix(strings.TrimSpace(plaintext), "{") {
		var c Credential
		if err := json.Unmarshal([]byte(plaintext), &c); err == nil {
			return c
		}
	}
	return Credential{APIKey: plaintext}
}

type Store struct {
	keyPath string
	dbPath  string

	mu   sync.Mutex
	key  []byte
	data map[string]string // accountKey -> base64(nonce || ciphertext)
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
	if err := s.load(); err != nil {
		return nil, err
	}
	return s, nil
}

// load reads accounts.json. A file that cannot be parsed is set aside under a
// new name before the store starts empty, so the next save cannot overwrite the
// only copy of every credential. A file that cannot be read at all stops Open
// for the same reason.
func (s *Store) load() error {
	b, err := os.ReadFile(s.dbPath)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("accounts: %w", err)
	}
	// An empty file holds nothing worth keeping.
	if len(bytes.TrimSpace(b)) == 0 {
		return nil
	}
	err = json.Unmarshal(b, &s.data)
	if err == nil {
		return nil
	}
	s.data = map[string]string{}
	kept := s.dbPath + ".damaged-" + time.Now().UTC().Format("20060102-150405.000000000")
	if rerr := os.Rename(s.dbPath, kept); rerr != nil {
		return fmt.Errorf("accounts: %s cannot be read (%v) and cannot be set aside: %w", s.dbPath, err, rerr)
	}
	log.Printf("accounts: %s cannot be read (%v); it was kept as %s and every stored account has to be entered again", s.dbPath, err, kept)
	return nil
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

// accountKey builds the map key for one account. The default account keeps
// the bare service id, so files written before named accounts still resolve;
// a named account is appended after a NUL. Service ids come only from
// Catalogue, but account ids may be typed by a person, so a NUL in one is
// stripped to keep it from colliding with another service's key.
func accountKey(service, account string) string {
	account = strings.ReplaceAll(account, "\x00", "")
	if account == "" {
		return service
	}
	return service + "\x00" + account
}

// serviceOf returns the service id part of a key built by accountKey.
func serviceOf(key string) string {
	if i := strings.IndexByte(key, 0); i >= 0 {
		return key[:i]
	}
	return key
}

// Set seals an API key for a service's default account and persists it. An
// empty secret deletes it.
func (s *Store) Set(service, secret string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := accountKey(service, "")
	if secret == "" {
		return s.deleteLocked(key)
	}
	return s.sealCredentialLocked(key, Credential{APIKey: secret})
}

// Get returns the API key for a service's default account, or "" if none is
// stored.
func (s *Store) Get(service string) (string, error) {
	cred, err := s.GetCredential(service, "")
	if err != nil {
		return "", err
	}
	return cred.APIKey, nil
}

// SetCredential seals a credential for one account of a service and persists
// it. An empty account is the default account; a zero Credential deletes the
// entry.
func (s *Store) SetCredential(service, account string, cred Credential) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := accountKey(service, account)
	if cred.IsZero() {
		return s.deleteLocked(key)
	}
	return s.sealCredentialLocked(key, cred)
}

// GetCredential returns the credential stored for one account of a service,
// or the zero Credential if none is stored.
func (s *Store) GetCredential(service, account string) (Credential, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	plaintext, err := s.openLocked(accountKey(service, account))
	if err != nil {
		return Credential{}, err
	}
	return decodeCredential(plaintext), nil
}

// AccountIDs lists the named (non-default) account ids stored for a service,
// sorted.
func (s *Store) AccountIDs(service string) []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	prefix := service + "\x00"
	var out []string
	for k := range s.data {
		if strings.HasPrefix(k, prefix) {
			out = append(out, k[len(prefix):])
		}
	}
	sort.Strings(out)
	return out
}

// Services lists, sorted, the service ids that have at least one stored
// credential.
func (s *Store) Services() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	seen := map[string]bool{}
	for k := range s.data {
		seen[serviceOf(k)] = true
	}
	out := make([]string, 0, len(seen))
	for id := range seen {
		out = append(out, id)
	}
	sort.Strings(out)
	return out
}

func (s *Store) sealCredentialLocked(key string, cred Credential) error {
	plaintext, err := json.Marshal(cred)
	if err != nil {
		return err
	}
	g, err := s.gcm()
	if err != nil {
		return err
	}
	nonce := make([]byte, g.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return err
	}
	ct := g.Seal(nonce, nonce, plaintext, nil)
	s.data[key] = base64.StdEncoding.EncodeToString(ct)
	return s.flush()
}

// openLocked decrypts the value stored under key, or returns "" if nothing is
// there.
func (s *Store) openLocked(key string) (string, error) {
	enc, ok := s.data[key]
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

func (s *Store) deleteLocked(key string) error {
	delete(s.data, key)
	return s.flush()
}

func (s *Store) flush() error {
	b, err := json.MarshalIndent(s.data, "", "  ")
	if err != nil {
		return err
	}
	return writeAtomic(s.dbPath, b)
}

// writeAtomic replaces path with data so that a crash at any point leaves the
// old file or the new one, never a truncated mix: the bytes go to a temporary
// file in the same folder, reach the disk, and only then take the name.
func writeAtomic(path string, data []byte) error {
	f, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".*.tmp")
	if err != nil {
		return err
	}
	tmp := f.Name()
	defer os.Remove(tmp)
	if _, err := f.Write(data); err != nil {
		f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		return err
	}
	// The rename itself lives in the folder. Windows cannot sync a folder,
	// and there the rename is durable without it.
	if d, err := os.Open(filepath.Dir(path)); err == nil {
		_ = d.Sync()
		d.Close()
	}
	return nil
}
