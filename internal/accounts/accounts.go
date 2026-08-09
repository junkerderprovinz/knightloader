// Package accounts is an encrypted-at-rest store for premium/debrid
// credentials. Secrets are sealed with AES-256-GCM under a per-install key
// kept in the data dir (0600); the store never returns anything but
// plaintext the caller asked for, and the on-disk file holds only
// ciphertext.
//
// What decrypts out of the ciphertext is a JSON Credential - an API key, or a
// username and password, whichever the service's catalogue entry (see
// catalogue.go) says it needs. Every secret sealed before Credential existed
// decrypts to a bare string instead of a JSON object, and is read back as
// Credential{APIKey: <that string>} forever - see decodeCredential. That is a
// read-time interpretation applied on every Get/GetCredential, never a
// one-shot rewrite of accounts.json: a process killed mid-rewrite would leave
// a file neither format can read, indistinguishable from "never configured",
// which is worse than the single-string shape it would have replaced.
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
	"strings"
	"sync"
)

// Credential is one stored account's secret. Which fields are populated is
// decided by the owning service's catalogue Kind (KindAPIKey vs
// KindUsernamePassword, see catalogue.go) - Store seals and returns whatever
// it is given and enforces nothing, the same trust it always placed in a bare
// secret string.
type Credential struct {
	APIKey   string `json:"apiKey,omitempty"`
	Username string `json:"username,omitempty"`
	Password string `json:"password,omitempty"`
}

// IsZero reports whether c carries no secret at all - the value
// SetCredential reads as "delete this entry", the same way Set has always
// treated an empty string.
func (c Credential) IsZero() bool { return c == Credential{} }

// Redacted is the placeholder Credential.Redacted puts in place of a secret
// field, and the value WithSecretsFrom reads as "the caller did not retype
// this". Mirrors reconnect.RedactedPassword: a visible placeholder rather
// than an empty string, because empty has to keep meaning "clear this field"
// - otherwise a stored secret could never be removed through a settings
// form.
const Redacted = "********"

// Redacted returns a copy with every populated secret field replaced by the
// Redacted placeholder, for handing to a browser or writing to a log.
// Username travels unredacted - the same call reconnect.Config makes for the
// router username - because it identifies an account without unlocking
// anything.
func (c Credential) Redacted() Credential {
	if c.APIKey != "" {
		c.APIKey = Redacted
	}
	if c.Password != "" {
		c.Password = Redacted
	}
	return c
}

// WithSecretsFrom puts back whatever secret field Redacted blanked out,
// exactly as reconnect.Config.WithSecretsFrom does for the router password. A
// settings form shown a redacted credential sends the placeholder back
// unchanged, and without this the save would seal the literal placeholder
// string in place of the real secret. A field the caller actually changed -
// including clearing it to "" - passes through untouched, which is how a
// stored secret gets removed on purpose.
func (c Credential) WithSecretsFrom(prev Credential) Credential {
	if c.APIKey == Redacted {
		c.APIKey = prev.APIKey
	}
	if c.Password == Redacted {
		c.Password = prev.Password
	}
	return c
}

// decodeCredential interprets a decrypted plaintext blob as a Credential. A
// value SetCredential wrote is a JSON object and is parsed as one; anything
// else - including every secret Set wrote before Credential existed - is
// treated as the whole plaintext being one API key, whatever it looks like.
//
// The test is "does it look like a JSON object", not "does json.Unmarshal
// accept it": a legacy secret that happened to be the literal text "null",
// "12345" or "true" is also valid JSON, and encoding/json's rule for a bare
// null is to leave the target untouched rather than error - which would
// silently turn a real stored secret into an empty Credential on first read.
// Requiring the leading '{' rules that whole class out before json ever sees
// it.
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
	data map[string]string // key -> base64(nonce || ciphertext); see accountKey
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

// accountKey builds the map key under which one account's credential is
// stored. The default, unnamed account - what every service had before
// multi-account support, and what Set/Get still address - keeps using the
// bare service id, so an accounts.json written before this change resolves
// under the new code exactly as it always did. A named account (a hoster
// with two premium logins configured) is suffixed behind a NUL.
//
// NUL is safe as a separator only because service ids come from one place:
// the fixed, short identifiers in Catalogue (catalogue.go), never from user
// input. Account ids may be typed by a person, so any NUL in one is stripped
// first - otherwise account "x" on service "rapidgator" could in principle be
// made to collide with the default account of a service literally named
// "rapidgator\x00x". Nobody can type \x00 through a browser form or a JSON
// string in practice, but this function's safety does not need to rely on
// that being true.
func accountKey(service, account string) string {
	account = strings.ReplaceAll(account, "\x00", "")
	if account == "" {
		return service
	}
	return service + "\x00" + account
}

// serviceOf recovers the service id from a key accountKey built: everything
// before the first NUL, or the whole string when there is none.
func serviceOf(key string) string {
	if i := strings.IndexByte(key, 0); i >= 0 {
		return key[:i]
	}
	return key
}

// Set seals a secret for a service's default account and persists it. An
// empty secret deletes it. The secret becomes a Credential holding only
// APIKey; use SetCredential for a service that needs a username and
// password, or for a second account on the same service.
func (s *Store) Set(service, secret string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := accountKey(service, "")
	if secret == "" {
		return s.deleteLocked(key)
	}
	return s.sealCredentialLocked(key, Credential{APIKey: secret})
}

// Get returns the plaintext API key for a service's default account, or ""
// if none is stored. It is Credential.APIKey off GetCredential(service, "") -
// kept as its own method because every caller so far has only ever wanted
// that one field.
func (s *Store) Get(service string) (string, error) {
	cred, err := s.GetCredential(service, "")
	if err != nil {
		return "", err
	}
	return cred.APIKey, nil
}

// SetCredential seals a credential for one account of a service and persists
// it. account distinguishes multiple stored credentials for the same service
// - pass "" for the single, unnamed account most services have (what Set
// addresses too), or a caller-chosen id to keep a second account beside the
// first. A zero Credential (see Credential.IsZero) deletes the entry.
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
// or the zero Credential if none is stored. It reads both the JSON object
// SetCredential writes and the bare secret Set wrote before Credential
// existed - see decodeCredential.
func (s *Store) GetCredential(service, account string) (Credential, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	plaintext, err := s.openLocked(accountKey(service, account))
	if err != nil {
		return Credential{}, err
	}
	return decodeCredential(plaintext), nil
}

// AccountIDs lists the non-default account ids stored for a service, sorted.
// The default account is not included in it - a caller that also supports
// the default checks GetCredential(service, "") itself, the same as it
// always has through Get.
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

// Services lists the service ids that have at least one stored credential (no
// secrets) - the default account, a named one, or both collapse to a single
// entry, because this answers "is anything configured for X" rather than
// "how many accounts does X have" (AccountIDs answers that one).
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

// sealCredentialLocked JSON-encodes cred, seals it and stores it under key.
// Caller holds mu.
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

// openLocked decrypts the value stored under key, or "" if nothing is there.
// Caller holds mu.
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

// deleteLocked removes key and persists the removal. Caller holds mu.
func (s *Store) deleteLocked(key string) error {
	delete(s.data, key)
	return s.flush()
}

func (s *Store) flush() error {
	b, err := json.MarshalIndent(s.data, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.dbPath, b, 0o600)
}
