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
	// TOTP is the base32 authenticator secret; empty means no second factor.
	// It is written only once a code produced from it has been confirmed - see
	// twofactor.go, where the whole reason for that is spelled out.
	//
	// omitempty on both of these, so an instance that never touches the feature
	// keeps the two-line file it has always had rather than growing two null
	// entries somebody has to wonder about.
	TOTP string `json:"totp,omitempty"`
	// Recovery holds the HMACs of the unspent single-use codes, never the codes.
	Recovery []string `json:"recovery,omitempty"`
}

// Guard holds the password, the second factor, and signs sessions.
type Guard struct {
	path string

	mu   sync.RWMutex
	hash []byte
	key  []byte
	// totp and recovery are the persisted half of the second factor; pending is
	// the enrolment in flight, which deliberately never reaches the file. See
	// twofactor.go.
	totp     string
	recovery []string
	pending  *pending
}

// Open loads (or creates) the lock state in dir.
func Open(dir string) (*Guard, error) {
	g := &Guard{path: filepath.Join(dir, "auth.json")}
	if b, err := os.ReadFile(g.path); err == nil {
		var s stored
		if err := json.Unmarshal(b, &s); err == nil {
			g.hash = []byte(s.Hash)
			g.key, _ = hex.DecodeString(s.Key)
			g.totp = s.TOTP
			g.recovery = s.Recovery
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
		// The second factor goes with it. It hangs off the password, so leaving
		// it armed would leave an instance that asks for a code with nothing to
		// add it to - and a sheet of recovery codes still valid against a lock
		// that no longer exists. Changing a password does NOT do this: rotating
		// one is no reason to make somebody re-enrol a phone.
		g.totp = ""
		g.recovery = nil
		g.pending = nil
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

// DerivedID is a stable, unguessable identifier for this instance, derived from
// the same key that signs sessions and separated from it by purpose.
//
// It exists for WebAuthn, which insists on a user handle even where there is no
// user: KnightLoader has one password and no accounts, so the account IS the
// instance. That handle has to survive restarts - a changed one makes every
// registered credential unusable - and must not be guessable from outside, and
// the signing key is the only value this app already keeps that is both.
//
// Derived rather than handed out. The key itself signs session cookies, so
// anything that let it leave the process would be a way to mint a session; an
// HMAC under a named purpose gives a caller something stable to identify the
// instance by and nothing it can work backwards from.
func (g *Guard) DerivedID(purpose string) []byte {
	return g.sign("knightloader:derived:" + purpose)
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
	s := stored{
		Hash:     string(g.hash),
		Key:      hex.EncodeToString(g.key),
		TOTP:     g.totp,
		Recovery: g.recovery,
	}
	g.mu.RUnlock()
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(g.path, b, 0o600)
}
