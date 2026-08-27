// Package seedphrase turns the secret that groups a person's own instances
// into twelve words, and back.
//
// This is the whole of what somebody copies from one KnightLoader to
// another (jdp, 2026-08-27: "eine zeichenfolge oder eine Seed phrase ...
// die man dann in allen anderen Instanzen einfügen kann"). It carries the
// secret and nothing else - not an address, because the relay's address is
// a compiled-in constant, which is exactly what lets the phrase stay this
// short and why there is no login step anywhere in the flow. See
// docs/superpowers/specs/2026-08-27-public-relay-seed-phrase-design.md.
//
// # Why the BIP39 wordlist
//
// Not because of cryptocurrency, and the UI must never call this a wallet
// seed. The list is reused because of one property that is expensive to
// reproduce and easy to get wrong: its 2048 words were chosen so that no
// two share their first four letters, none are near-homophones, and none
// carry accents. That is what makes a phrase safe to read aloud down a
// phone line and safe to type on a mobile keyboard - the two things this
// format exists for. The word file is the official list, embedded rather
// than fetched, and its content is pinned by TestWordlistIsTheOfficialOne.
//
// # Why 128 bits
//
// An earlier draft of the relay design specified 256. That is 24 words,
// and the difference between a phrase somebody will actually type on a
// phone and one they will give up on. At 2^128 guessing is not a threat
// model any rate limit needs to help with, so the extra 12 words buy
// nothing real and cost the feature its usability.
//
// The BIP39 checksum rides along for free and earns its place: a mistyped
// word is caught here, with the offending word named, instead of surfacing
// later as a connection that silently never finds its sibling.
package seedphrase

import (
	"crypto/rand"
	"crypto/sha256"
	_ "embed"
	"errors"
	"fmt"
	"strings"
)

//go:embed english.txt
var wordlistFile string

const (
	// SecretLen is the secret's size in bytes. 128 bits - see the package
	// comment for why not more.
	SecretLen = 16
	// WordCount is what SecretLen encodes to: 128 bits of secret plus a
	// 4-bit checksum, split into 11-bit groups.
	WordCount = 12

	bitsPerWord  = 11
	checksumBits = SecretLen * 8 / 32 // BIP39's own rule: one bit per 32 bits of entropy
)

var (
	words     []string
	wordIndex map[string]int
)

func init() {
	words = strings.Fields(wordlistFile)
	if len(words) != 1<<bitsPerWord {
		// A wordlist of the wrong length cannot encode 11 bits per word, so
		// every phrase this package produced would be undecodable. Failing
		// at init is the only honest response: there is no degraded mode.
		panic(fmt.Sprintf("seedphrase: wordlist has %d words, need %d", len(words), 1<<bitsPerWord))
	}
	wordIndex = make(map[string]int, len(words))
	for i, w := range words {
		wordIndex[w] = i
	}
}

// New mints a fresh secret and returns it with its phrase. The secret is
// what the relay sees; the phrase is what a person copies.
func New() (secret []byte, phrase string, err error) {
	secret = make([]byte, SecretLen)
	if _, err := rand.Read(secret); err != nil {
		// crypto/rand failing is a broken machine, not a path to recover
		// from - and silently returning a weak secret here would hand out a
		// phrase that looks exactly as trustworthy as a real one.
		return nil, "", fmt.Errorf("seedphrase: reading randomness: %w", err)
	}
	phrase, err = Encode(secret)
	if err != nil {
		return nil, "", err
	}
	return secret, phrase, nil
}

// Encode renders a secret as its phrase. Used both when a group is first
// created and every time the phrase is shown again later (which is gated
// behind re-entering the instance password - see the spec).
func Encode(secret []byte) (string, error) {
	if len(secret) != SecretLen {
		return "", fmt.Errorf("seedphrase: secret is %d bytes, need %d", len(secret), SecretLen)
	}
	sum := sha256.Sum256(secret)
	// secret || first checksumBits of sha256(secret), read as one bit
	// string. One spare byte is enough for any checksum this size.
	full := make([]byte, 0, SecretLen+1)
	full = append(full, secret...)
	full = append(full, sum[0])

	out := make([]string, WordCount)
	for i := range out {
		out[i] = words[bitsAt(full, i*bitsPerWord, bitsPerWord)]
	}
	return strings.Join(out, " "), nil
}

// ErrChecksum is returned when every word is real but the phrase as a whole
// does not check out - the signature of a typo that happened to land on
// another valid word, or of two words swapped.
var ErrChecksum = errors.New("seedphrase: the phrase is not valid - check for a mistyped or swapped word")

// Decode parses a phrase back into its secret.
//
// Input is normalised first, because this arrives from a paste or from
// somebody typing what they were read over the phone: case is ignored, and
// any run of whitespace (including the newlines a copied block brings with
// it) counts as one separator.
func Decode(phrase string) ([]byte, error) {
	got := strings.Fields(strings.ToLower(strings.TrimSpace(phrase)))
	if len(got) != WordCount {
		return nil, fmt.Errorf("seedphrase: the phrase has %d words, it needs %d", len(got), WordCount)
	}

	full := make([]byte, SecretLen+1)
	for i, w := range got {
		idx, ok := wordIndex[w]
		if !ok {
			// Naming the word and its position is the whole point: "word 7
			// (\"recieve\") is not in the list" is a fixable message,
			// "invalid phrase" is not.
			return nil, fmt.Errorf("seedphrase: word %d (%q) is not one of the accepted words", i+1, w)
		}
		setBits(full, i*bitsPerWord, bitsPerWord, idx)
	}

	secret := full[:SecretLen]
	sum := sha256.Sum256(secret)
	// Compare only the checksum bits, which sit immediately after the
	// secret; the rest of the trailing byte is padding this format never
	// wrote to.
	want := bitsAt(sum[:1], 0, checksumBits)
	have := bitsAt(full, SecretLen*8, checksumBits)
	if want != have {
		return nil, ErrChecksum
	}
	return secret, nil
}

// bitsAt reads count bits from b starting at bit offset, most significant
// bit first, and returns them as an integer. Written plainly rather than
// cleverly: it runs twelve times per phrase, so there is nothing to gain
// from packing this into shifts nobody can check by eye.
func bitsAt(b []byte, offset, count int) int {
	v := 0
	for i := 0; i < count; i++ {
		bit := offset + i
		v <<= 1
		if b[bit/8]&(1<<(7-bit%8)) != 0 {
			v |= 1
		}
	}
	return v
}

// setBits is bitsAt's inverse: writes the low count bits of v into b at bit
// offset, most significant bit first. Assumes the target bits start clear,
// which they do - callers hand it a freshly made slice.
func setBits(b []byte, offset, count, v int) {
	for i := 0; i < count; i++ {
		if v&(1<<(count-1-i)) != 0 {
			bit := offset + i
			b[bit/8] |= 1 << (7 - bit%8)
		}
	}
}
