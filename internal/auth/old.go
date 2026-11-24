// Package auth reads the sign-in KnightLoader kept in auth.json before the
// kit's sign-in took over, so the kit can import it and check what it cannot
// work out itself: the password hash, the cookies signed with the old session
// key, the second factor and the recovery codes.
//
// Nothing here writes auth.json. The file stays as the last version left it,
// so going back to that version still signs in, until Remove takes it away
// once the kit ended one of its ways in.
package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base32"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/appkey"
	"github.com/junkerderprovinz/knightloader/internal/secret"
)

// CookieName is the session cookie of the versions before the kit.
const CookieName = "kl_session"

// SessionTTL is how long one of those sessions lasted from its last renewal.
const SessionTTL = 30 * 24 * time.Hour

type stored struct {
	Hash     string           `json:"hash"`
	Key      string           `json:"key"`
	TOTP     string           `json:"totp,omitempty"`
	Recovery []string         `json:"recovery,omitempty"`
	Epoch    uint64           `json:"epoch,omitempty"`
	Revoked  map[string]int64 `json:"revoked,omitempty"`
}

// Old is auth.json as the version before the kit left it.
type Old struct {
	path   string
	appKey *appkey.Key
	s      stored
	key    []byte
}

// Read reads auth.json in dir. A missing file is an Old without a password.
func Read(dir string, key *appkey.Key) (*Old, error) {
	o := &Old{path: filepath.Join(dir, FileName), appKey: key}
	s, _, err := readStored(o.path, key)
	if err != nil {
		return nil, err
	}
	o.s = s
	o.key, _ = hex.DecodeString(s.Key)
	return o, nil
}

// Hash is the stored password hash, empty without a password.
func (o *Old) Hash() string { return o.s.Hash }

// CheckPassword checks pw against hash, one of the hashes Hash returned.
func (o *Old) CheckPassword(hash, pw string) bool {
	return hash != "" && verifyPassword(o.appKey, pw, []byte(hash))
}

// TwoFactor is the authenticator secret, nil when the second factor was off.
func (o *Old) TwoFactor() ([]byte, error) {
	if o.s.TOTP == "" {
		return nil, nil
	}
	return base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(strings.ToUpper(o.s.TOTP))
}

// Recovery are the hashes of the recovery codes not spent yet.
func (o *Old) Recovery() []string { return o.s.Recovery }

// RecoveryMatches reports whether code is the recovery code hash stands for.
func (o *Old) RecoveryMatches(hash, code string) bool {
	return len(o.key) > 0 && secret.MatchRecoveryCode(string(o.key), code, []string{hash}) == 0
}

// PasskeyHandle is the WebAuthn user handle the passkeys were registered
// under, which the browser needs to find them.
func (o *Old) PasskeyHandle() []byte {
	if len(o.key) == 0 {
		return nil
	}
	return sign(o.key, "knightloader:derived:passkey-user")
}

// Session reads a session cookie these versions issued. It returns when the
// cookie runs out and a key that tells it apart from every other one, and ok
// only while it is signed for the stored epoch, unexpired and not signed out.
func (o *Old) Session(cookie string, now time.Time) (expires time.Time, key string, ok bool) {
	if len(o.key) == 0 || o.s.Hash == "" {
		return time.Time{}, "", false
	}
	i := strings.LastIndexByte(cookie, '.')
	if i < 0 {
		return time.Time{}, "", false
	}
	payload, sig := cookie[:i], cookie[i+1:]
	// The last character has spare bits, so a lenient decode would let other
	// spellings of a revoked cookie past the list.
	got, err := base64.RawURLEncoding.Strict().DecodeString(sig)
	if err != nil {
		return time.Time{}, "", false
	}
	msg := payload
	if o.s.Epoch != 0 {
		msg += "|" + strconv.FormatUint(o.s.Epoch, 10)
	}
	if subtle.ConstantTimeCompare(got, sign(o.key, msg)) != 1 {
		return time.Time{}, "", false
	}
	key = revocationKey(cookie)
	if _, gone := o.s.Revoked[key]; gone {
		return time.Time{}, "", false
	}
	exp, _, _ := strings.Cut(payload, ".")
	ts, err := strconv.ParseInt(exp, 10, 64)
	if err != nil || now.Unix() >= ts {
		return time.Time{}, "", false
	}
	return time.Unix(ts, 0), key, true
}

// Remove takes auth.json away, so going back to the version before cannot
// bring back a password or a session the kit ended.
func (o *Old) Remove() error {
	if err := os.Remove(o.path); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return nil
}

func revocationKey(cookie string) string {
	sum := sha256.Sum256([]byte(cookie))
	return hex.EncodeToString(sum[:16])
}

func sign(key []byte, msg string) []byte {
	m := hmac.New(sha256.New, key)
	m.Write([]byte(msg))
	return m.Sum(nil)
}
