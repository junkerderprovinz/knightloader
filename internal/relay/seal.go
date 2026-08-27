package relay

// Sealing one proxy frame so the relay that carries it cannot read it.
//
// The relay routes on two fields - which connection a frame is for, and which
// question it answers - and has never needed anything else. Everything else
// was travelling in the clear only because nothing had encrypted it yet: the
// path being called, the body, the task list coming back, and the bearer
// token the mobile app attaches (see ProxyCall.Authorization's own comment on
// why that one is the worst of them to leak). This file is what closes that,
// keyed on DeriveFrameKey - a key derived from the connection secret under
// its own domain, which the relay does not hold and cannot compute from what
// it does hold.
//
// AES-256-GCM rather than a stream cipher plus a MAC: one primitive, one call
// per direction, authenticated by construction, and in the standard library
// with hardware acceleration on every platform this ships to. The nonce is
// 12 random bytes prefixed to the ciphertext - random rather than a counter
// because both ends seal independently, a counter would have to be
// synchronised across reconnects, and at 96 bits the birthday bound sits far
// beyond any traffic a download manager's control channel will ever produce.

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
)

// ErrSealed is returned when a frame cannot be opened: a wrong key, a
// truncated frame, or a tampered one. The three are deliberately one error.
// Telling them apart tells an attacker which of their guesses was closer,
// and tells an honest caller nothing they can act on differently - every
// case has the same answer, which is that this frame is not usable.
var ErrSealed = errors.New("relay: frame could not be opened")

// nonceLen is AES-GCM's standard nonce size, and the length seal writes in
// front of every ciphertext.
const nonceLen = 12

// aead builds the cipher for one key, rejecting a key of the wrong length
// loudly rather than silently producing frames nothing can open.
func aead(key []byte) (cipher.AEAD, error) {
	if len(key) != 32 {
		return nil, fmt.Errorf("relay: frame key is %d bytes, want 32", len(key))
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

// seal encrypts plaintext under key, binding it to aad.
//
// aad is the frame's own routing fields, which travel in the clear because
// the relay reads them. Binding them here means a relay cannot take a sealed
// frame addressed to one instance and deliver it as though it were addressed
// to another, or replay one request's answer as another's: the receiver's own
// routing fields go into open() as well, and a mismatch fails the tag rather
// than decrypting to something plausible.
func seal(key []byte, aad string, plaintext []byte) ([]byte, error) {
	gcm, err := aead(key)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, nonceLen)
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, fmt.Errorf("relay: no randomness for a frame nonce: %w", err)
	}
	// Seal appends to its first argument, so handing it the nonce slice
	// produces nonce||ciphertext in one allocation.
	return gcm.Seal(nonce, nonce, plaintext, []byte(aad)), nil
}

// open reverses seal. It returns ErrSealed for every failure - see that
// error's own comment for why the cases are not distinguished.
func open(key []byte, aad string, sealed []byte) ([]byte, error) {
	gcm, err := aead(key)
	if err != nil {
		return nil, err
	}
	if len(sealed) < nonceLen {
		return nil, ErrSealed
	}
	plaintext, err := gcm.Open(nil, sealed[:nonceLen], sealed[nonceLen:], []byte(aad))
	if err != nil {
		return nil, ErrSealed
	}
	return plaintext, nil
}
