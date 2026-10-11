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
	"errors"
	"maps"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/junkerderprovinz/knightloader/internal/appkey"
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
	// ErrTooLong rejects a password bcrypt cannot hash whole.
	ErrTooLong = errors.New("the password must be at most 72 bytes")
)

const (
	// minPasswordChars counts characters, so a password in another alphabet
	// is held to the same length as one in English.
	minPasswordChars = 8
	// maxPasswordBytes is bcrypt's limit, which counts bytes.
	maxPasswordBytes = 72
)

// checkLength refuses a password outside the lengths above.
func checkLength(password string) error {
	if utf8.RuneCountInString(password) < minPasswordChars {
		return ErrTooShort
	}
	if len(password) > maxPasswordBytes {
		return ErrTooLong
	}
	return nil
}

type stored struct {
	Hash string `json:"hash"` // bcrypt, or Argon2id with an app key; empty = no password set
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
	// totpUsed is the newest time step a code was accepted for. A code is
	// valid for three steps, and one seen over a shoulder or in a proxy's
	// log must not open a second session in that time. Kept in memory, so a
	// restart forgets it, which costs at most those three steps.
	totpUsed uint64
	// The live reset code and when it was minted, in memory only.
	reset       resetCode
	resetMinted time.Time
	// gen counts changes to the password and the second factor in this
	// process, so a session is only issued for the state a password was
	// checked against (see Credentials).
	gen uint64
	// appKey seals the file and peppers the password; nil keeps the file
	// plain and the hash bcrypt.
	appKey *appkey.Key

	// minting is held for writing while a reset, a password change or a
	// sign-out everywhere ends the credentials, and for reading by everything
	// that creates one outside this package; see Mint.
	minting sync.RWMutex
}

// ErrStale refuses to mint a credential for a caller whose own credential
// ended while the request was under way.
var ErrStale = errors.New("this session has ended, sign in again")

// Credentials is the state a password was checked against: which generation
// of the password and the second factor, and whether a code is needed on top.
type Credentials struct {
	gen       uint64
	enabled   bool
	TwoFactor bool
}

// Open loads (or creates) the lock state in dir, without an app key.
func Open(dir string) (*Guard, error) {
	return OpenWithKey(dir, nil)
}

// Enabled reports whether a password is required.
func (g *Guard) Enabled() bool {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return len(g.hash) > 0
}

// SetPassword is ChangePassword for a caller that needs no session afterwards.
func (g *Guard) SetPassword(current, next string) error {
	_, err := g.ChangePassword(current, next, nil)
	return err
}

// ChangePassword sets, changes or (with an empty next) removes the password,
// and returns the new state for the session of whoever changed it. When a
// password is already set, the current one has to be supplied, so an open
// session cannot lock the owner out. A change that lands after another one,
// a reset included, is refused: current was checked against a password that
// is gone. alsoEnd runs once the change is made, while nothing can be minted
// (see Mint), for the credentials this package does not keep.
func (g *Guard) ChangePassword(current, next string, alsoEnd func()) (Credentials, error) {
	c, ok := g.CheckPassword(current)
	if c.enabled && !ok {
		return Credentials{}, ErrWrongPassword
	}
	return g.changeFrom(c, next, alsoEnd)
}

// changeFrom applies next to the state c was checked against, and refuses
// when that state has changed since.
func (g *Guard) changeFrom(c Credentials, next string, alsoEnd func()) (Credentials, error) {
	var h []byte
	if next != "" {
		if err := checkLength(next); err != nil {
			return Credentials{}, err
		}
		var err error
		if h, err = hashPassword(g.appKey, next); err != nil {
			return Credentials{}, err
		}
	}
	g.minting.Lock()
	defer g.minting.Unlock()
	g.mu.Lock()
	if g.gen != c.gen {
		g.mu.Unlock()
		return Credentials{}, ErrWrongPassword
	}
	g.hash = h
	// Removing the password ends the sessions too, or a password set again
	// later would bring back every unexpired cookie from before.
	g.endSessionsLocked()
	if next == "" {
		// The second factor hangs off the password and goes with it. Changing
		// the password keeps it, since rotating one is no reason to re-enrol a
		// phone.
		g.totp = ""
		g.recovery = nil
		g.pending = nil
	}
	g.gen++
	c = Credentials{gen: g.gen, enabled: len(h) > 0, TwoFactor: g.totp != ""}
	g.mu.Unlock()
	if alsoEnd != nil {
		alsoEnd()
	}
	return c, g.flush()
}

// Check verifies a password against the stored hash.
func (g *Guard) Check(password string) bool {
	_, ok := g.CheckPassword(password)
	return ok
}

// CheckPassword verifies a password and returns the state it was checked
// against, which IssueFor holds the session to.
func (g *Guard) CheckPassword(password string) (Credentials, bool) {
	g.mu.RLock()
	h := g.hash
	c := Credentials{gen: g.gen, enabled: len(h) > 0, TwoFactor: g.totp != ""}
	g.mu.RUnlock()
	if len(h) == 0 {
		return c, false
	}
	if !verifyPassword(g.appKey, password, h) {
		return c, false
	}
	g.upgradeHash(password, h)
	return c, true
}

// Current is the present state, for a sign-in that proves something other than
// the password. It waits out a reset under way, so whatever the caller reads
// after it is from one side of the reset.
func (g *Guard) Current() Credentials {
	g.minting.RLock()
	defer g.minting.RUnlock()
	g.mu.RLock()
	defer g.mu.RUnlock()
	return Credentials{gen: g.gen, enabled: len(g.hash) > 0, TwoFactor: g.totp != ""}
}

// IssueFor returns a session for a caller who proved c, or false when the
// password or the second factor has changed since.
func (g *Guard) IssueFor(c Credentials) (string, bool) {
	payload := strconv.FormatInt(time.Now().Add(SessionTTL).Unix(), 10) + "." + rand.Text()
	g.mu.RLock()
	stale := g.gen != c.gen
	epoch, key := g.epoch, g.key
	g.mu.RUnlock()
	if stale {
		return "", false
	}
	return payload + "." + base64.RawURLEncoding.EncodeToString(signWith(key, sessionMessage(payload, epoch))), true
}

// Mint runs fn, which creates a credential outside this package (an API
// token, a passkey, a live stream), unless valid reports that the caller's own
// credential has ended. A reset, a password change and a sign-out everywhere
// hold minting off while they end the credentials and remove those, so fn
// runs entirely before one, and is undone by it, or after it with valid
// checked against the new state. fn must not call Current.
func (g *Guard) Mint(valid func() bool, fn func() error) error {
	g.minting.RLock()
	defer g.minting.RUnlock()
	if !valid() {
		return ErrStale
	}
	return fn()
}

// Issue returns a session token valid for SessionTTL. The random part keeps two
// sessions issued in the same second apart, so signing out one leaves the
// other alone.
func (g *Guard) Issue() string {
	payload := strconv.FormatInt(time.Now().Add(SessionTTL).Unix(), 10) + "." + rand.Text()
	g.mu.RLock()
	epoch, key := g.epoch, g.key
	g.mu.RUnlock()
	return payload + "." + base64.RawURLEncoding.EncodeToString(signWith(key, sessionMessage(payload, epoch)))
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
	epoch, key := g.epoch, g.key
	_, revoked := g.revoked[revocationKey(token)]
	g.mu.RUnlock()
	if revoked || subtle.ConstantTimeCompare(want, signWith(key, sessionMessage(payload, epoch))) != 1 {
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

// RevokeAll signs out every session issued so far. alsoEnd runs as
// ChangePassword's does.
func (g *Guard) RevokeAll(alsoEnd func()) error {
	g.minting.Lock()
	defer g.minting.Unlock()
	g.mu.Lock()
	g.endSessionsLocked()
	g.mu.Unlock()
	if alsoEnd != nil {
		alsoEnd()
	}
	return g.flush()
}

// Epoch counts how often every session was ended at once, by a password change
// or a sign-out everywhere. Credentials that should end with the sessions sign
// it in.
func (g *Guard) Epoch() uint64 {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return g.epoch
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
	return signWith(key, msg)
}

func signWith(key []byte, msg string) []byte {
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
		// A copy, since a sign-out adds to the list while this one is encoded.
		Revoked: maps.Clone(g.revoked),
	}
	g.mu.RUnlock()
	return writeState(g.path, s, g.appKey)
}

func writeState(path string, s stored, key *appkey.Key) error {
	b, err := encodeStored(s, key)
	if err != nil {
		return err
	}
	return os.WriteFile(path, b, 0o600)
}
