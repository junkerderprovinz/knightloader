package mega

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/subtle"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
)

// fileKey is a file's 32-byte key split into what it is made of: the AES key
// the content is encrypted with, the nonce the counter starts from, and the
// MAC the content has to match.
type fileKey struct {
	aes   []byte
	nonce []byte
	mac   []byte
}

func splitFileKey(k []byte) (fileKey, error) {
	if len(k) != 32 {
		return fileKey{}, fmt.Errorf("mega: a file key is 32 bytes, this one is %d", len(k))
	}
	a := make([]byte, 16)
	subtle.XORBytes(a, k[:16], k[16:])
	return fileKey{aes: a, nonce: k[16:24], mac: k[24:32]}, nil
}

// decryptKey opens a node key sealed with the folder's key. MEGA encrypts
// keys block by block, without chaining.
func decryptKey(kek, sealed []byte) ([]byte, error) {
	if len(sealed) == 0 || len(sealed)%aes.BlockSize != 0 {
		return nil, errors.New("mega: a node key of the wrong length")
	}
	b, err := aes.NewCipher(kek)
	if err != nil {
		return nil, err
	}
	out := make([]byte, len(sealed))
	for i := 0; i < len(sealed); i += aes.BlockSize {
		b.Decrypt(out[i:], sealed[i:])
	}
	return out, nil
}

// errBadKey is attributes that do not open with the key at hand, which is
// what a link with a wrong or mangled key comes to.
var errBadKey = errors.New("mega: the key in the link does not open this file")

// decryptName reads the file or folder name out of a node's attributes,
// which are "MEGA" and a JSON object, encrypted in CBC mode with a zero IV.
func decryptName(key []byte, attrs string) (string, error) {
	raw, err := decode64(attrs)
	if err != nil || len(raw) == 0 || len(raw)%aes.BlockSize != 0 {
		return "", errBadKey
	}
	b, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	plain := make([]byte, len(raw))
	cipher.NewCBCDecrypter(b, make([]byte, aes.BlockSize)).CryptBlocks(plain, raw)
	body, ok := bytes.CutPrefix(plain, []byte("MEGA"))
	if !ok {
		return "", errBadKey
	}
	// The padding is zero bytes, which a JSON decoder stops in front of.
	var a struct {
		N string `json:"n"`
	}
	if err := json.NewDecoder(bytes.NewReader(bytes.TrimRight(body, "\x00"))).Decode(&a); err != nil {
		return "", errBadKey
	}
	return a.N, nil
}

// ctrAt is the keystream for a file's content from offset on, which has to
// sit on a 16-byte boundary.
func ctrAt(k fileKey, offset int64) (cipher.Stream, error) {
	b, err := aes.NewCipher(k.aes)
	if err != nil {
		return nil, err
	}
	iv := make([]byte, aes.BlockSize)
	copy(iv, k.nonce)
	binary.BigEndian.PutUint64(iv[8:], uint64(offset/aes.BlockSize))
	return cipher.NewCTR(b, iv), nil
}

// chunkSize is the length of the n-th chunk of a file, counted from 1: 128
// KiB, growing by 128 KiB up to the eighth, and 1 MiB from there on.
func chunkSize(n int) int64 {
	if n <= 8 {
		return int64(n) * 128 << 10
	}
	return 1 << 20
}

// chunkMAC computes MEGA's MAC over a file's decrypted content as it streams
// past. Each chunk gets a CBC-MAC that starts from the nonce, the chunk MACs
// are chained into one, and that is condensed to the 8 bytes the key holds.
type chunkMAC struct {
	block cipher.Block
	nonce []byte

	n          int   // the chunk being read, from 1
	start, end int64 // its bounds in the file
	pos        int64 // bytes taken so far

	chunk   [16]byte
	file    [16]byte
	partial [16]byte
	held    int
}

func newChunkMAC(k fileKey) (*chunkMAC, error) {
	b, err := aes.NewCipher(k.aes)
	if err != nil {
		return nil, err
	}
	m := &chunkMAC{block: b, nonce: k.nonce, n: 1, end: chunkSize(1)}
	m.resetChunk()
	return m, nil
}

func (m *chunkMAC) resetChunk() {
	copy(m.chunk[:8], m.nonce)
	copy(m.chunk[8:], m.nonce)
}

func (m *chunkMAC) Write(p []byte) (int, error) {
	n := len(p)
	for len(p) > 0 {
		take := int64(len(p))
		if room := m.end - m.pos; take > room {
			take = room
		}
		m.absorb(p[:take])
		m.pos += take
		p = p[take:]
		if m.pos == m.end {
			m.closeChunk()
		}
	}
	return n, nil
}

func (m *chunkMAC) absorb(p []byte) {
	if m.held > 0 {
		c := copy(m.partial[m.held:], p)
		m.held += c
		p = p[c:]
		if m.held < len(m.partial) {
			return
		}
		m.mix(m.partial[:])
		m.held = 0
	}
	for len(p) >= aes.BlockSize {
		m.mix(p[:aes.BlockSize])
		p = p[aes.BlockSize:]
	}
	m.held = copy(m.partial[:], p)
}

func (m *chunkMAC) mix(block []byte) {
	subtle.XORBytes(m.chunk[:], m.chunk[:], block)
	m.block.Encrypt(m.chunk[:], m.chunk[:])
}

func (m *chunkMAC) closeChunk() {
	if m.held > 0 {
		clear(m.partial[m.held:])
		m.mix(m.partial[:])
		m.held = 0
	}
	subtle.XORBytes(m.file[:], m.file[:], m.chunk[:])
	m.block.Encrypt(m.file[:], m.file[:])
	m.n++
	m.start, m.end = m.pos, m.pos+chunkSize(m.n)
	m.resetChunk()
}

// Sum is the condensed MAC of everything written. It closes the last chunk,
// so it is called once, at the end.
func (m *chunkMAC) Sum() []byte {
	if m.pos > m.start {
		m.closeChunk()
	}
	out := make([]byte, 8)
	subtle.XORBytes(out[:4], m.file[0:4], m.file[4:8])
	subtle.XORBytes(out[4:], m.file[8:12], m.file[12:16])
	return out
}
