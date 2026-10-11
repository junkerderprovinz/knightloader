package auth

import (
	"bytes"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"math"
	"strconv"
	"strings"

	"golang.org/x/crypto/argon2"
	"golang.org/x/crypto/bcrypt"

	"github.com/junkerderprovinz/knightloader/internal/appkey"
)

// With an app key the password is stored as Argon2id over an HMAC of it keyed
// with the app key, so auth.json without the key cannot even be brute forced.
// Without one it stays bcrypt, as every install before the key had it.
//
// m=19 MiB, t=2, p=1 is OWASP's first Argon2id profile. More memory would
// risk the out-of-memory killer in a container capped at a few hundred MB,
// and the login throttle bounds how often this runs anyway.
const (
	argonMemoryKiB = 19 * 1024
	argonTime      = 2
	argonThreads   = 1
	argonKeyLen    = 32
	argonSaltLen   = 16
	argonPrefix    = "argon2id$"
)

// bcryptCost is a variable so the tests can hash at bcrypt's lowest cost,
// which the race detector would otherwise slow to a second per hash.
var bcryptCost = bcrypt.DefaultCost

func hashPassword(key *appkey.Key, password string) ([]byte, error) {
	if key == nil {
		return bcrypt.GenerateFromPassword([]byte(password), bcryptCost)
	}
	salt := make([]byte, argonSaltLen)
	if _, err := rand.Read(salt); err != nil {
		return nil, err
	}
	sum := argon2.IDKey(pepper(key, password), salt, argonTime, argonMemoryKiB, argonThreads, argonKeyLen)
	return fmt.Appendf(nil, "%s%d$%d$%d$%s$%s", argonPrefix, argonMemoryKiB, argonTime, argonThreads,
		base64.RawStdEncoding.EncodeToString(salt), base64.RawStdEncoding.EncodeToString(sum)), nil
}

func pepper(key *appkey.Key, password string) []byte {
	return key.MAC("password", []byte(password))
}

// verifyPassword checks password against either stored form. An Argon2id hash
// never verifies without the key it was peppered with.
func verifyPassword(key *appkey.Key, password string, stored []byte) bool {
	if !bytes.HasPrefix(stored, []byte(argonPrefix)) {
		return bcrypt.CompareHashAndPassword(stored, []byte(password)) == nil
	}
	if key == nil {
		return false
	}
	m, t, p, salt, want, ok := parseArgon(string(stored))
	if !ok || len(want) > math.MaxUint32 {
		return false
	}
	got := argon2.IDKey(pepper(key, password), salt, t, m, p, uint32(len(want)))
	return subtle.ConstantTimeCompare(got, want) == 1
}

// parseArgon refuses anything malformed rather than guessing at defaults, so
// a damaged hash refuses every password instead of accepting one.
func parseArgon(s string) (m, t uint32, p uint8, salt, sum []byte, ok bool) {
	parts := strings.Split(strings.TrimPrefix(s, argonPrefix), "$")
	if len(parts) != 5 {
		return 0, 0, 0, nil, nil, false
	}
	mv, err1 := strconv.ParseUint(parts[0], 10, 32)
	tv, err2 := strconv.ParseUint(parts[1], 10, 32)
	pv, err3 := strconv.ParseUint(parts[2], 10, 8)
	salt, err4 := base64.RawStdEncoding.DecodeString(parts[3])
	sum, err5 := base64.RawStdEncoding.DecodeString(parts[4])
	if err1 != nil || err2 != nil || err3 != nil || err4 != nil || err5 != nil {
		return 0, 0, 0, nil, nil, false
	}
	// argon2.IDKey panics on a zero time or thread count, and an empty sum
	// would match any password.
	if mv == 0 || tv == 0 || pv == 0 || len(salt) == 0 || len(sum) == 0 {
		return 0, 0, 0, nil, nil, false
	}
	return uint32(mv), uint32(tv), uint8(pv), salt, sum, true
}

// needsRehash reports whether a hash that just verified should be replaced:
// a bcrypt hash once there is a key, and an Argon2id hash weaker than today's.
func needsRehash(key *appkey.Key, stored []byte) bool {
	if key == nil {
		return false
	}
	if !bytes.HasPrefix(stored, []byte(argonPrefix)) {
		return true
	}
	m, t, p, _, sum, ok := parseArgon(string(stored))
	return ok && (m < argonMemoryKiB || t < argonTime || p < argonThreads || len(sum) < argonKeyLen)
}

// upgradeHash stores a fresh hash of a password that just verified against
// old. The plaintext is only at hand during a sign-in, so this is where a
// bcrypt hash becomes Argon2id. A failed write keeps the old hash on disk,
// which still verifies, so it is not reported.
func (g *Guard) upgradeHash(password string, old []byte) {
	if !needsRehash(g.appKey, old) {
		return
	}
	h, err := hashPassword(g.appKey, password)
	if err != nil {
		return
	}
	g.mu.Lock()
	// A password changed in the meantime is not overwritten.
	if !bytes.Equal(g.hash, old) {
		g.mu.Unlock()
		return
	}
	g.hash = h
	g.mu.Unlock()
	_ = g.flush()
}
