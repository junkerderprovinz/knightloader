// Package auth is KnightLoader's optional password lock. Self-hosted means the
// instance often sits on a LAN where "anyone who can reach the port" is not the
// same as "anyone who should control the downloads".
//
// It is off until a password is set. Sessions are signed cookies rather than a
// server-side table, so a restart does not log everyone out. The signature
// covers a session epoch, which a password change or a sign-out everywhere
// moves on, so every cookie issued before it stops working at once.
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
	// TOTP is the base32 authenticator secret, written only once a code from
	// it has been confirmed. Empty means no second factor.
	TOTP string `json:"totp,omitempty"`
	// Recovery holds the HMACs of the unspent single-use codes.
	Recovery []string `json:"recovery,omitempty"`
	// Epoch is signed into every session. Zero signs the expiry alone, which
	// keeps cookies issued before the epoch existed valid until it first moves.
	Epoch uint64 `json:"epoch,omitempty"`
	// Revoked maps the hash of each signed-out session to its expiry, so a
	// copy of the cookie taken before the sign-out does not work either.
	Revoked map[string]int64 `json:"revoked,omitempty"`
}

// maxRevoked bounds the list of signed-out sessions. Past it the epoch moves
// on instead, which signs out every session rather than letting the file grow.
const maxRevoked = 256

// Guard holds the password, the second factor, and signs sessions.
type Guard struct {
	path string

	mu       sync.RWMutex
	hash     []byte
	key      []byte
	totp     string
	recovery []string
	epoch    uint64
	revoked  map[string]int64
	pending  *pending // never written to the file
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
			g.epoch = s.Epoch
			g.revoked = s.Revoked
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
// a password is already set, the current one has to be supplied, so an open
// session cannot lock the owner out.
func (g *Guard) SetPassword(current, next string) error {
	if g.Enabled() && !g.Check(current) {
		return ErrWrongPassword
	}
	if next == "" {
		g.mu.Lock()
		g.hash = nil
		// Otherwise a password set again later would bring back every
		// unexpired cookie from before.
		g.endSessionsLocked()
		// The second factor hangs off the password and goes with it. Changing
		// the password keeps it, since rotating one is no reason to re-enrol a
		// phone.
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
	g.endSessionsLocked()
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

// Issue returns a session token valid for SessionTTL. The random part keeps two
// sessions issued in the same second apart, so signing out one leaves the
// other alone.
func (g *Guard) Issue() string {
	payload := strconv.FormatInt(time.Now().Add(SessionTTL).Unix(), 10) + "." + rand.Text()
	g.mu.RLock()
	epoch := g.epoch
	g.mu.RUnlock()
	return payload + "." + base64.RawURLEncoding.EncodeToString(g.sign(sessionMessage(payload, epoch)))
}

// Valid reports whether a token is well-formed, signed for the current epoch,
// unexpired and not signed out.
func (g *Guard) Valid(token string) bool {
	_, ok := g.expiry(token)
	return ok
}

// expiry is the Unix time a valid token runs out, and whether it is valid. A
// token is the expiry, a random part and the signature over both, or, issued
// before the random part existed, the expiry and its signature alone.
func (g *Guard) expiry(token string) (int64, bool) {
	i := strings.LastIndexByte(token, '.')
	if i < 0 {
		return 0, false
	}
	payload, sig := token[:i], token[i+1:]
	exp, _, _ := strings.Cut(payload, ".")
	// The last character of the signature has spare bits, so a lenient decode
	// would let other spellings of a revoked token past the revocation list.
	want, err := base64.RawURLEncoding.Strict().DecodeString(sig)
	if err != nil {
		return 0, false
	}
	g.mu.RLock()
	epoch := g.epoch
	_, revoked := g.revoked[revocationKey(token)]
	g.mu.RUnlock()
	if revoked || subtle.ConstantTimeCompare(want, g.sign(sessionMessage(payload, epoch))) != 1 {
		return 0, false
	}
	ts, err := strconv.ParseInt(exp, 10, 64)
	if err != nil || time.Now().Unix() >= ts {
		return 0, false
	}
	return ts, true
}

// Revoke signs out one session. A token that is not valid anyway is left
// alone, so only somebody holding a live session can add to the list.
func (g *Guard) Revoke(token string) error {
	exp, ok := g.expiry(token)
	if !ok {
		return nil
	}
	now := time.Now().Unix()
	g.mu.Lock()
	for k, until := range g.revoked {
		if until <= now {
			delete(g.revoked, k)
		}
	}
	if len(g.revoked) >= maxRevoked {
		g.endSessionsLocked()
	} else {
		if g.revoked == nil {
			g.revoked = map[string]int64{}
		}
		g.revoked[revocationKey(token)] = exp
	}
	g.mu.Unlock()
	return g.flush()
}

// RevokeAll signs out every session issued so far.
func (g *Guard) RevokeAll() error {
	g.mu.Lock()
	g.endSessionsLocked()
	g.mu.Unlock()
	return g.flush()
}

// endSessionsLocked moves the epoch on. The revocation list only ever named
// sessions of the old epoch, so it goes with it. Called with mu held.
func (g *Guard) endSessionsLocked() {
	g.epoch++
	g.revoked = nil
}

// sessionMessage is what a session's signature covers. The epoch goes after a
// character the payload cannot hold, so no payload of one epoch reads as
// another payload of a different one.
func sessionMessage(payload string, epoch uint64) string {
	if epoch == 0 {
		return payload
	}
	return payload + "|" + strconv.FormatUint(epoch, 10)
}

// revocationKey names a session in the revocation list without storing the
// cookie itself, which would be a working credential in the file until the
// epoch moved.
func revocationKey(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:16])
}

// DerivedID is a stable, unguessable identifier for this instance, derived
// from the session signing key and separated from it by purpose. WebAuthn
// needs such a user handle; handing out an HMAC rather than the key keeps the
// key, which can mint sessions, inside the process.
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
		Epoch:    g.epoch,
		Revoked:  g.revoked,
	}
	g.mu.RUnlock()
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(g.path, b, 0o600)
}
