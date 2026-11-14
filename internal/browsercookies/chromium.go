package browsercookies

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/sha1"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"golang.org/x/crypto/pbkdf2"
)

// chromeEpochOffset is the seconds from 1601, where Chromium's clock starts,
// to 1970.
const chromeEpochOffset = 11644473600

// chromiumHashed is the database version from which a decrypted value starts
// with the SHA-256 of its host, 32 bytes to drop.
const chromiumHashed = 24

func chromiumTime(micros int64) time.Time {
	if micros <= 0 {
		return time.Time{}
	}
	return time.UnixMicro(micros - chromeEpochOffset*1e6).UTC()
}

func readChromium(p Profile, path string, domains []string) ([]Cookie, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	defer db.Close()
	var version string
	if err := db.QueryRow(`SELECT value FROM meta WHERE key = 'version'`).Scan(&version); err != nil {
		return nil, err
	}
	meta, _ := strconv.Atoi(version)
	rows, err := db.Query(`SELECT host_key, top_frame_site_key, name, value, encrypted_value, path, expires_utc, is_secure, is_httponly, last_access_utc FROM cookies`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	dec := &decrypter{p: p, hashed: meta >= chromiumHashed, keys: map[string][][]byte{}, errs: map[string]error{}}
	var out []Cookie
	for rows.Next() {
		var (
			c                   Cookie
			topFrame, plain     string
			sealed              []byte
			expires, lastAccess int64
		)
		if err := rows.Scan(&c.Host, &topFrame, &c.Name, &plain, &sealed, &c.Path, &expires, &c.Secure, &c.HTTPOnly, &lastAccess); err != nil {
			return nil, err
		}
		// A cookie with a top frame is one the site set while embedded in
		// another, which is not the session a link is opened with.
		if topFrame != "" || !matches(c.Host, domains) {
			continue
		}
		c.Value = plain
		if len(sealed) > 0 {
			if c.Value, err = dec.open(sealed); err != nil {
				return nil, err
			}
		}
		c.Expires, c.LastUsed = chromiumTime(expires), chromiumTime(lastAccess)
		out = append(out, c)
	}
	return out, rows.Err()
}

// decrypter opens a profile's sealed values. Each key is fetched once, and
// only for a value that needs it, since on macOS fetching it may ask the
// user.
type decrypter struct {
	p      Profile
	hashed bool
	// keys are the AES keys to try for a value's version prefix, errs why a
	// prefix has none.
	keys map[string][][]byte
	errs map[string]error
}

func (d *decrypter) open(sealed []byte) (string, error) {
	prefix := string(sealed[:min(3, len(sealed))])
	if prefix == "v20" {
		return "", ErrAppBound
	}
	switch d.p.goos {
	case "windows":
		if prefix != "v10" {
			// Values from before Chromium 80 are sealed by DPAPI one by one,
			// and from before the host hash too.
			plain, err := dpapiUnprotect(sealed)
			if err != nil {
				return "", fmt.Errorf("%w: %v", errUndecryptable, err)
			}
			return text(plain, false)
		}
		keys, err := d.keysFor(prefix, d.windowsKey)
		if err != nil {
			return "", err
		}
		return d.openGCM(keys[0], sealed[3:])
	case "darwin":
		if prefix != "v10" {
			// Older versions kept the value in the clear.
			return string(sealed), nil
		}
		keys, err := d.keysFor(prefix, d.macKeys)
		if err != nil {
			return "", err
		}
		return d.openCBC(keys, sealed[3:], nil)
	}
	switch prefix {
	case "v10":
		keys, _ := d.keysFor(prefix, func() ([][]byte, error) { return [][]byte{cbcKey("peanuts", 1), cbcKey("", 1)}, nil })
		return d.openCBC(keys, sealed[3:], nil)
	case "v11":
		// Chromium falls back to an empty password when the keyring gave it
		// none, so that key is tried too.
		keys, keyringErr := d.keysFor(prefix, d.linuxKeys)
		return d.openCBC(keys, sealed[3:], keyringErr)
	}
	return "", errUndecryptable
}

func (d *decrypter) keysFor(prefix string, fetch func() ([][]byte, error)) ([][]byte, error) {
	if keys, ok := d.keys[prefix]; ok {
		return keys, d.errs[prefix]
	}
	keys, err := fetch()
	d.keys[prefix], d.errs[prefix] = keys, err
	return keys, err
}

// windowsKey is the AES key in Local State, which DPAPI seals to the user.
func (d *decrypter) windowsKey() ([][]byte, error) {
	raw, err := os.ReadFile(filepath.Join(d.p.root, "Local State"))
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrNoKey, err)
	}
	var state struct {
		OSCrypt struct {
			EncryptedKey string `json:"encrypted_key"`
		} `json:"os_crypt"`
	}
	if err := json.Unmarshal(raw, &state); err != nil {
		return nil, fmt.Errorf("%w: Local State: %v", ErrNoKey, err)
	}
	sealed, err := base64.StdEncoding.DecodeString(state.OSCrypt.EncryptedKey)
	if err != nil || !bytes.HasPrefix(sealed, []byte("DPAPI")) {
		return nil, fmt.Errorf("%w: Local State holds no DPAPI key", ErrNoKey)
	}
	key, err := dpapiUnprotect(sealed[len("DPAPI"):])
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrNoKey, err)
	}
	return [][]byte{key}, nil
}

func (d *decrypter) macKeys() ([][]byte, error) {
	name := d.p.kind.macKey
	if name == "" {
		return nil, ErrNoKey
	}
	password, err := keychainPassword(name+" Safe Storage", name)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrNoKey, err)
	}
	return [][]byte{cbcKey(string(password), 1003)}, nil
}

func (d *decrypter) linuxKeys() ([][]byte, error) {
	empty := cbcKey("", 1)
	password, err := secretServicePassword(strings.ToLower(d.p.kind.linuxKey))
	if err != nil {
		return [][]byte{empty}, fmt.Errorf("%w: %v", ErrNoKey, err)
	}
	return [][]byte{cbcKey(string(password), 1), empty}, nil
}

// cbcKey derives the key of the macOS and Linux schemes from a password.
func cbcKey(password string, iterations int) []byte {
	return pbkdf2.Key([]byte(password), []byte("saltysalt"), iterations, 16, sha1.New)
}

// openCBC tries each key on an AES-128-CBC value and takes the first that
// unpads to text. Failing all of them, it reports noKey when one was missing.
func (d *decrypter) openCBC(keys [][]byte, ct []byte, noKey error) (string, error) {
	if len(ct) == 0 || len(ct)%aes.BlockSize != 0 {
		return "", errUndecryptable
	}
	iv := bytes.Repeat([]byte{' '}, aes.BlockSize)
	for _, key := range keys {
		block, err := aes.NewCipher(key)
		if err != nil {
			continue
		}
		plain := make([]byte, len(ct))
		cipher.NewCBCDecrypter(block, iv).CryptBlocks(plain, ct)
		pad := int(plain[len(plain)-1])
		if pad == 0 || pad > aes.BlockSize || !bytes.Equal(plain[len(plain)-pad:], bytes.Repeat([]byte{byte(pad)}, pad)) {
			continue
		}
		if s, err := text(plain[:len(plain)-pad], d.hashed); err == nil {
			return s, nil
		}
	}
	if noKey != nil {
		return "", noKey
	}
	return "", errUndecryptable
}

// openGCM opens a Windows v10 value: a 12-byte nonce, then the sealed text.
func (d *decrypter) openGCM(key, raw []byte) (string, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrNoKey, err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	if len(raw) < aead.NonceSize()+aead.Overhead() {
		return "", errUndecryptable
	}
	plain, err := aead.Open(nil, raw[:aead.NonceSize()], raw[aead.NonceSize():], nil)
	if err != nil {
		return "", errUndecryptable
	}
	return text(plain, d.hashed)
}

// text drops the host hash when the value carries one, and refuses what is
// not text, which a wrong key leaves.
func text(plain []byte, hashed bool) (string, error) {
	if hashed {
		if len(plain) < 32 {
			return "", errUndecryptable
		}
		plain = plain[32:]
	}
	if !utf8.Valid(plain) {
		return "", errUndecryptable
	}
	return string(plain), nil
}
