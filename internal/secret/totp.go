// Package secret holds the arithmetic behind KnightLoader's second login
// factor: time-based one-time passwords (RFC 6238) and the single-use recovery
// codes that go with them.
//
// WHY IT IS A PACKAGE OF ITS OWN, next to internal/auth rather than inside it.
// internal/auth owns STATE - a file on disk, a mutex, a password hash. This owns
// no state at all: every function here takes what it needs and returns an
// answer. That split is what lets the whole algorithm be tested against RFC
// 6238's own printed vectors without a temporary directory anywhere in sight,
// and it is what keeps the guard's file format free of the details of a hash.
//
// WRITTEN OUT RATHER THAN IMPORTED, on purpose. The whole of TOTP is the forty
// lines below: an HMAC over a 30-second counter, truncated to six digits. Adding
// a module for that would put a supply-chain dependency on the one code path
// whose entire job is to be trustworthy, and the specification has not moved
// since 2011.
//
// SHA-1 IS NOT A MISTAKE HERE. RFC 6238 names it, every authenticator app
// implements it, and the construction is HMAC, where SHA-1's collision weakness
// does not apply. An install that reached for SHA-256 because it sounds safer
// would simply fail to enrol in Google Authenticator.
package secret

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1" //nolint:gosec // G505: RFC 6238 fixes SHA-1 for TOTP; every authenticator app implements that and nothing else.
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base32"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"io"
	"net/url"
	"strings"
	"time"
)

const (
	totpDigits = 6
	totpPeriod = 30 * time.Second
	// totpSkew is how many steps either side of "now" are accepted. One step
	// each way covers the ordinary case of a phone clock a few seconds off and a
	// code typed just as it rolls over. A wider window buys an attacker time and
	// buys the operator nothing.
	totpSkew = 1
	// totpSecretLen is 20 bytes, the length RFC 4226 recommends and the length
	// authenticator apps expect from a base32 secret.
	totpSecretLen = 20
)

var totpEnc = base32.StdEncoding.WithPadding(base32.NoPadding)

// NewTOTPSecret returns a fresh base32 secret suitable for an authenticator app.
func NewTOTPSecret() (string, error) {
	buf := make([]byte, totpSecretLen)
	if _, err := io.ReadFull(rand.Reader, buf); err != nil {
		return "", fmt.Errorf("secret: totp secret: %w", err)
	}
	return totpEnc.EncodeToString(buf), nil
}

// TOTPCode returns the six-digit code for secret at time t.
func TOTPCode(secret string, t time.Time) (string, error) {
	key, err := decodeSecret(secret)
	if err != nil {
		return "", err
	}
	return totpAt(key, totpStep(t)), nil
}

func decodeSecret(secret string) ([]byte, error) {
	key, err := totpEnc.DecodeString(strings.ToUpper(strings.TrimSpace(secret)))
	if err != nil {
		return nil, fmt.Errorf("secret: totp: invalid base32 secret: %w", err)
	}
	return key, nil
}

// totpStep converts a wall-clock time into an RFC 6238 counter.
//
// The clamp is the point: Unix() is an int64 and the counter is a uint64, so a
// time before the epoch would wrap to an enormous step and hand out codes from a
// window no verifier will ever reach, silently. A box whose clock has not been
// set yet is the realistic way to get there, and it is exactly the moment a
// second factor must not start producing nonsense.
func totpStep(t time.Time) uint64 {
	sec := t.Unix()
	if sec < 0 {
		return 0
	}
	return uint64(sec) / uint64(totpPeriod.Seconds())
}

func totpAt(key []byte, counter uint64) string {
	var buf [8]byte
	binary.BigEndian.PutUint64(buf[:], counter)
	mac := hmac.New(sha1.New, key)
	mac.Write(buf[:])
	sum := mac.Sum(nil)

	// Dynamic truncation (RFC 4226 section 5.3): the low nibble of the last byte
	// picks the four-byte window, and the top bit is masked off so the value is
	// positive.
	offset := sum[len(sum)-1] & 0x0f
	code := (uint32(sum[offset])&0x7f)<<24 |
		uint32(sum[offset+1])<<16 |
		uint32(sum[offset+2])<<8 |
		uint32(sum[offset+3])

	mod := uint32(1)
	for range totpDigits {
		mod *= 10
	}
	return fmt.Sprintf("%0*d", totpDigits, code%mod)
}

// ValidTOTP reports whether code is valid for secret around time t, allowing one
// step of clock skew either side.
//
// The comparison is constant-time. That matters less for a six-digit code than
// for a password, but a timing oracle on the FIRST digits would let an attacker
// find each digit independently, which turns a million guesses into sixty.
func ValidTOTP(secret, code string, t time.Time) bool {
	code = strings.TrimSpace(code)
	// Some apps, and some people, put a space in the middle.
	code = strings.ReplaceAll(code, " ", "")
	if len(code) != totpDigits {
		return false
	}
	key, err := decodeSecret(secret)
	if err != nil || len(key) == 0 {
		return false
	}
	step := totpStep(t)
	ok := false
	for d := -totpSkew; d <= totpSkew; d++ {
		c := step
		switch {
		case d < 0:
			if c < uint64(-d) {
				continue
			}
			c -= uint64(-d)
		case d > 0:
			c += uint64(d)
		}
		// No early return: every window is compared, so the time taken does not
		// reveal WHICH window matched.
		if subtle.ConstantTimeCompare([]byte(totpAt(key, c)), []byte(code)) == 1 {
			ok = true
		}
	}
	return ok
}

// TOTPURI builds the otpauth:// URI an authenticator app scans. The issuer
// appears both as the label prefix and as a parameter, which is what the two
// families of app that disagree about the format each need.
func TOTPURI(issuer, account, secret string) string {
	label := url.PathEscape(issuer + ":" + account)
	q := url.Values{}
	q.Set("secret", secret)
	q.Set("issuer", issuer)
	q.Set("algorithm", "SHA1")
	q.Set("digits", fmt.Sprint(totpDigits))
	q.Set("period", fmt.Sprint(int(totpPeriod.Seconds())))
	return "otpauth://totp/" + label + "?" + q.Encode()
}

// ---------------------------------------------------------------------------
// Recovery codes
// ---------------------------------------------------------------------------

// RecoveryCodeCount is how many single-use codes are handed out when the second
// factor is switched on. Eight is enough to survive a lost phone and few enough
// that people actually write them down.
//
// Exported because the interface has to say the number BEFORE it shows the
// codes, and a screen that promises "eight" while the store hands out ten is the
// kind of disagreement nothing notices.
const RecoveryCodeCount = 8

const recoveryHalfLen = 5 // characters per half, "abcde-fghij"

// recoveryAlphabet omits the characters people misread off a printed sheet: 0
// against O, 1 against l and I.
const recoveryAlphabet = "abcdefghjkmnpqrstuvwxyz23456789"

// NewRecoveryCodes returns fresh single-use codes in plain text - shown to the
// operator exactly once - together with their stored hashes.
//
// The stored form is a plain HMAC and deliberately not bcrypt. Unlike a
// human-chosen password these codes carry about fifty bits of entropy each, so
// there is no dictionary for a slow hash to slow down; bcrypt here would only
// make every login slower by eight verifications. It is the same argument
// internal/apitoken already makes for its own hashing, one door along.
func NewRecoveryCodes(key string) (plain []string, hashed []string, err error) {
	plain = make([]string, 0, RecoveryCodeCount)
	hashed = make([]string, 0, RecoveryCodeCount)
	for range RecoveryCodeCount {
		code, cErr := randomRecoveryCode()
		if cErr != nil {
			return nil, nil, cErr
		}
		plain = append(plain, code)
		hashed = append(hashed, HashRecoveryCode(key, code))
	}
	return plain, hashed, nil
}

func randomRecoveryCode() (string, error) {
	buf := make([]byte, recoveryHalfLen*2)
	if _, err := io.ReadFull(rand.Reader, buf); err != nil {
		return "", fmt.Errorf("secret: recovery code: %w", err)
	}
	var b strings.Builder
	for i, v := range buf {
		if i == recoveryHalfLen {
			b.WriteByte('-')
		}
		// Modulo bias over a 31-character alphabet drawn from 256 values is under
		// half a bit per character and irrelevant beside the fifty bits the code
		// carries. Rejection sampling here would be ceremony.
		b.WriteByte(recoveryAlphabet[int(v)%len(recoveryAlphabet)])
	}
	return b.String(), nil
}

// HashRecoveryCode returns the stored form of a recovery code, bound to this
// instance's own key so a sheet lifted off one install does not open another.
// Case and separators are normalised first, so somebody typing "ABCDE FGHIJ"
// still gets in.
func HashRecoveryCode(key, code string) string {
	m := hmac.New(sha256.New, []byte(key))
	// The prefix is domain separation, not decoration: the same key signs session
	// cookies, and a construction that hashed the bare input would let one of the
	// two be used to answer questions about the other.
	m.Write([]byte("knightloader:recovery:" + NormalizeRecoveryCode(code)))
	return hex.EncodeToString(m.Sum(nil))
}

// NormalizeRecoveryCode strips everything that only exists for legibility.
func NormalizeRecoveryCode(code string) string {
	code = strings.ToLower(strings.TrimSpace(code))
	code = strings.ReplaceAll(code, "-", "")
	code = strings.ReplaceAll(code, " ", "")
	return code
}

// MatchRecoveryCode returns the index of the stored hash that code matches, or
// -1. Every entry is compared, so the time taken reveals neither which code was
// used nor how many are left.
func MatchRecoveryCode(key, code string, stored []string) int {
	if NormalizeRecoveryCode(code) == "" {
		return -1
	}
	want := HashRecoveryCode(key, code)
	found := -1
	for i, h := range stored {
		if subtle.ConstantTimeCompare([]byte(want), []byte(h)) == 1 {
			found = i
		}
	}
	return found
}
