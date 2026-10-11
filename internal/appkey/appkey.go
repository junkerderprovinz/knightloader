// Package appkey is the optional app key: 32 random bytes kept outside the data
// folder, in KL_APP_KEY for a container and in the operating system's
// credential store on the desktop. What the data folder keeps secret is sealed
// with it, so a copy of the folder alone opens nothing.
//
// Every use derives its own subkey from a purpose string, and the purpose is
// also the AEAD's additional data, so a value sealed as one thing cannot be
// moved into a file that expects another.
package appkey

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"strings"
)

// EnvVar names the environment variable a container passes the key in.
const EnvVar = "KL_APP_KEY"

// keyLen is the key's length in bytes; the variable holds twice as many hex
// digits.
const keyLen = 32

var (
	// ErrMissing is sealed data met without a key.
	ErrMissing = errors.New("it is sealed with an app key, and none is set; start KnightLoader with the same " + EnvVar)
	// ErrWrongKey is sealed data the key in use does not open, or that was
	// changed after sealing.
	ErrWrongKey = errors.New("it is sealed with a different app key; start KnightLoader with the " + EnvVar + " it was sealed with")
)

// Key is the app key. A nil *Key means the install runs without one, and
// every package that takes one then keeps its files as they always were.
type Key struct {
	raw []byte
}

// Parse reads a key written as 64 hexadecimal digits.
func Parse(s string) (*Key, error) {
	raw, err := hex.DecodeString(strings.TrimSpace(s))
	if err != nil || len(raw) != keyLen {
		return nil, fmt.Errorf("%s must be %d hexadecimal characters, such as the output of: openssl rand -hex %d", EnvVar, keyLen*2, keyLen)
	}
	return &Key{raw: raw}, nil
}

// FromEnv reads KL_APP_KEY. An unset or empty variable is no key and no error.
func FromEnv() (*Key, error) {
	v := os.Getenv(EnvVar)
	if strings.TrimSpace(v) == "" {
		return nil, nil
	}
	return Parse(v)
}

// Generate makes a fresh key, for the desktop's first start.
func Generate() (*Key, error) {
	raw := make([]byte, keyLen)
	if _, err := rand.Read(raw); err != nil {
		return nil, err
	}
	return &Key{raw: raw}, nil
}

// Hex is the key in the form Parse reads, for storing it in a credential store.
func (k *Key) Hex() string {
	return hex.EncodeToString(k.raw)
}

func (k *Key) derive(purpose string) []byte {
	m := hmac.New(sha256.New, k.raw)
	m.Write([]byte("knightloader:" + purpose))
	return m.Sum(nil)
}

// MAC is an HMAC-SHA256 of msg under the subkey for purpose.
func (k *Key) MAC(purpose string, msg []byte) []byte {
	m := hmac.New(sha256.New, k.derive(purpose))
	m.Write(msg)
	return m.Sum(nil)
}

func (k *Key) aead(purpose string) (cipher.AEAD, error) {
	block, err := aes.NewCipher(k.derive("seal:" + purpose))
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

// Seal encrypts plain with AES-256-GCM under the subkey for purpose and
// returns nonce and ciphertext together.
func (k *Key) Seal(purpose string, plain []byte) ([]byte, error) {
	if k == nil {
		return nil, errors.New("appkey: sealing needs a key")
	}
	gcm, err := k.aead(purpose)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	return gcm.Seal(nonce, nonce, plain, []byte(purpose)), nil
}

// Open reverses Seal. It answers ErrMissing without a key and ErrWrongKey for
// anything the key does not open.
func (k *Key) Open(purpose string, sealed []byte) ([]byte, error) {
	if k == nil {
		return nil, ErrMissing
	}
	gcm, err := k.aead(purpose)
	if err != nil {
		return nil, err
	}
	n := gcm.NonceSize()
	if len(sealed) < n {
		return nil, ErrWrongKey
	}
	plain, err := gcm.Open(nil, sealed[:n], sealed[n:], []byte(purpose))
	if err != nil {
		return nil, ErrWrongKey
	}
	return plain, nil
}

// textPrefix marks a sealed value written as text, in a JSON field or as a
// whole file.
const textPrefix = "kl-sealed:v1:"

// IsSealed reports whether s is a value SealString wrote.
func IsSealed(s string) bool {
	return strings.HasPrefix(strings.TrimSpace(s), textPrefix)
}

// SealString is Seal for a value stored as text.
func (k *Key) SealString(purpose, plain string) (string, error) {
	b, err := k.Seal(purpose, []byte(plain))
	if err != nil {
		return "", err
	}
	return textPrefix + base64.RawURLEncoding.EncodeToString(b), nil
}

// OpenString reverses SealString.
func (k *Key) OpenString(purpose, s string) (string, error) {
	s = strings.TrimSpace(s)
	if !strings.HasPrefix(s, textPrefix) {
		return "", ErrWrongKey
	}
	b, err := base64.RawURLEncoding.DecodeString(s[len(textPrefix):])
	if err != nil {
		return "", ErrWrongKey
	}
	plain, err := k.Open(purpose, b)
	if err != nil {
		return "", err
	}
	return string(plain), nil
}
