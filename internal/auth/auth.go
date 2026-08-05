// Package auth is KnightLoader's optional password lock. Self-hosted means the
// instance often sits on a LAN where "anyone who can reach the port" is not the
// same as "anyone who should control the downloads".
//
// It is off by default: a fresh install behaves exactly as before until a
// password is set. Sessions are signed cookies rather than a server-side table,
// so a restart does not log everyone out.
package auth

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/bcrypt"
)

// SessionTTL is how long a login lasts.
const SessionTTL = 30 * 24 * time.Hour

// CookieName is the session cookie the browser sends back.
const CookieName = "kl_session"

var (
	// ErrWrongPassword is returned when the supplied password does not match.
	ErrWrongPassword = errors.New("wrong password")
	// ErrTooShort rejects a password that is not worth having.
	ErrTooShort = errors.New("the password must be at least 8 characters")
)

type stored struct {
	Hash string `json:"hash"` // bcrypt, empty = no password set
	Key  string `json:"key"`  // hex, signs session cookies
}

// Guard holds the password and signs sessions.
type Guard struct {
	path string

	mu   sync.RWMutex
	hash []byte
	key  []byte
}

// Open loads (or creates) the lock state in dir.
func Open(dir string) (*Guard, error) {
	g := &Guard{path: filepath.Join(dir, "auth.json")}
	if b, err := os.ReadFile(g.path); err == nil {
		var s stored
		if err := json.Unmarshal(b, &s); err == nil {
			g.hash = []byte(s.Hash)
			g.key, _ = hex.DecodeString(s.Key)
		}
	}
	if len(g.key) == 0 {
		g.key = make([]byte, 32)
		if _, err := rand.Read(g.key); err != nil {
			return nil, err
		}
		if err := g.flush(); err != nil {
			return nil, err
		}
	}
	return g, nil
}

// Enabled reports whether a password is required.
func (g *Guard) Enabled() bool {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return len(g.hash) > 0
}

// SetPassword sets, changes or (with an empty next) removes the password. When
// a password is already set, the current one has to be supplied — otherwise
// anyone with an open session could silently lock the owner out.
func (g *Guard) SetPassword(current, next string) error {
	if g.Enabled() && !g.Check(current) {
		return ErrWrongPassword
	}
	if next == "" {
		g.mu.Lock()
		g.hash = nil
		g.mu.Unlock()
		return g.flush()
	}
	if len(next) < 8 {
		return ErrTooShort
	}
	h, err := bcrypt.GenerateFromPassword([]byte(next), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	g.mu.Lock()
	g.hash = h
	g.mu.Unlock()
	return g.flush()
}

// Check verifies a password against the stored hash.
func (g *Guard) Check(password string) bool {
	g.mu.RLock()
	h := g.hash
	g.mu.RUnlock()
	if len(h) == 0 {
		return false
	}
	return bcrypt.CompareHashAndPassword(h, []byte(password)) == nil
}

// Issue returns a session token valid for SessionTTL.
func (g *Guard) Issue() string {
	exp := strconv.FormatInt(time.Now().Add(SessionTTL).Unix(), 10)
	return exp + "." + base64.RawURLEncoding.EncodeToString(g.sign(exp))
}

// Valid reports whether a token is well-formed, correctly signed and unexpired.
func (g *Guard) Valid(token string) bool {
	exp, sig, ok := strings.Cut(token, ".")
	if !ok {
		return false
	}
	want, err := base64.RawURLEncoding.DecodeString(sig)
	if err != nil {
		return false
	}
	if subtle.ConstantTimeCompare(want, g.sign(exp)) != 1 {
		return false
	}
	ts, err := strconv.ParseInt(exp, 10, 64)
	return err == nil && time.Now().Unix() < ts
}

func (g *Guard) sign(msg string) []byte {
	g.mu.RLock()
	key := g.key
	g.mu.RUnlock()
	m := hmac.New(sha256.New, key)
	m.Write([]byte(msg))
	return m.Sum(nil)
}

func (g *Guard) flush() error {
	g.mu.RLock()
	s := stored{Hash: string(g.hash), Key: hex.EncodeToString(g.key)}
	g.mu.RUnlock()
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(g.path, b, 0o600)
}
