package auth

import (
	"bytes"
	"crypto/subtle"
	"encoding/base64"
	"math"
	"strconv"
	"strings"

	"golang.org/x/crypto/argon2"
	"golang.org/x/crypto/bcrypt"

	"github.com/junkerderprovinz/knightloader/internal/appkey"
)

// With an app key the password was stored as Argon2id over an HMAC of it
// keyed with the app key, and without one as bcrypt.
const argonPrefix = "argon2id$"

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
